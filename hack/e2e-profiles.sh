#!/bin/sh
set -eu

namespace=$1
release=$2
image=$3
image_repository=${image%:*}
image_tag=${image##*:}
operator_deployment="${release}-egressfox"
cert_dir=""
cleanup() {
  status=$?
  if [ -n "$cert_dir" ]; then rm -rf "$cert_dir"; fi
  return "$status"
}
trap cleanup EXIT

helm upgrade --install "$release" charts/egressfox --namespace "$namespace" --create-namespace --skip-crds \
  --set-string image.repository="$image_repository" --set-string image.tag="$image_tag" --set image.pullPolicy=Never --wait --timeout 3m

. ./hack/e2e-fixtures.sh
. ./hack/e2e-provider.sh

# Both synthetic VLESS endpoints use the same credentials and engine-compatible
# protocol, but originate from distinct proxy Pods. The CGI target responds 200
# only to the intended Pod for each target query.
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: proxy-secondary}
spec:
  replicas: 1
  selector: {matchLabels: {app: proxy-secondary}}
  template:
    metadata: {labels: {app: proxy-secondary}}
    spec:
      containers:
        - name: sing-box
          image: $image
          imagePullPolicy: Never
          command: [/usr/local/libexec/egressfox/egressfox-engine-s, run, -c, /config/config.json]
          volumeMounts: [{name: config, mountPath: /config, readOnly: true}, {name: cert, mountPath: /cert, readOnly: true}]
      volumes: [{name: config, secret: {secretName: proxy-server}}, {name: cert, secret: {secretName: proxy-cert}}]
---
apiVersion: v1
kind: Service
metadata: {name: proxy-secondary}
spec:
  selector: {app: proxy-secondary}
  ports: [{port: 8443, targetPort: 8443}]
YAML
kubectl -n "$namespace" rollout status deployment/proxy-secondary --timeout=2m
primary_pod_ip=$(kubectl -n "$namespace" get pod -l app=proxy-server -o jsonpath='{.items[0].status.podIP}')
secondary_pod_ip=$(kubectl -n "$namespace" get pod -l app=proxy-secondary -o jsonpath='{.items[0].status.podIP}')
secondary_ip=$(kubectl -n "$namespace" get service proxy-secondary -o jsonpath='{.spec.clusterIP}')

kubectl -n "$namespace" apply -f - <<'YAML'
apiVersion: v1
kind: ConfigMap
metadata: {name: profile-target-script}
data:
  check: |
    #!/bin/sh
    case "${QUERY_STRING:-}:${REMOTE_ADDR:-}" in
      "alpha:${PRIMARY_PROXY_IP}"|"beta:${SECONDARY_PROXY_IP}") status='200 OK' ;;
      *) status='503 Service Unavailable' ;;
    esac
    printf 'Status: %s\r\nContent-Type: text/plain\r\n\r\nok\n' "$status"
YAML
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: profile-target}
spec:
  replicas: 1
  selector: {matchLabels: {app: profile-target}}
  template:
    metadata: {labels: {app: profile-target}}
    spec:
      securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532}
      containers:
        - name: http
          image: busybox:1.37.0-uclibc@sha256:8d7b1636e974e0adfd8d945955fca609304f0a56c18799dfd032d6e661382d84
          # CGI compares REMOTE_ADDR with IPv4 Pod IPs; bind IPv4 explicitly.
          # busybox httpd drops CGI connections when RLIMIT_NOFILE is near
          # 2^30, which containers inherit once a systemd 257 kind node has
          # raised fs.nr_open on the shared container VM; cap the soft limit.
          command: [/bin/sh, -c, 'ulimit -n 65536; mkdir -p /www/cgi-bin; cp /script/check /www/cgi-bin/check; chmod 755 /www/cgi-bin/check; exec /bin/busybox httpd -f -p 0.0.0.0:8080 -h /www']
          env:
            - {name: PRIMARY_PROXY_IP, value: "$primary_pod_ip"}
            - {name: SECONDARY_PROXY_IP, value: "$secondary_pod_ip"}
          volumeMounts: [{name: script, mountPath: /script, readOnly: true}, {name: www, mountPath: /www}]
          securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}
      volumes: [{name: script, configMap: {name: profile-target-script}}, {name: www, emptyDir: {}}]
---
apiVersion: v1
kind: Service
metadata: {name: profile-target}
spec:
  selector: {app: profile-target}
  ports: [{port: 8080, targetPort: 8080}]
YAML
kubectl -n "$namespace" rollout status deployment/profile-target --timeout=2m
profile_target_ip=$(kubectl -n "$namespace" get service profile-target -o jsonpath='{.spec.clusterIP}')
kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c 'cat >/data/body; echo profiles-v1 >/data/etag' <<EOF
vless://${uuid}@${proxy_ip}:8443?encryption=none&security=none#primary
vless://${uuid}@${secondary_ip}:8443?encryption=none&security=none#secondary
EOF
kubectl -n "$namespace" create secret generic profile-source-url --from-literal=url="http://source-provider.${namespace}.svc.cluster.local:8080/subscription"
kubectl -n "$namespace" create secret generic profile-targets \
  --from-literal=default="http://${target_ip}:8080/" \
  --from-literal=alpha="http://${profile_target_ip}:8080/cgi-bin/check?alpha" \
  --from-literal=beta="http://${profile_target_ip}:8080/cgi-bin/check?beta"
kubectl -n "$namespace" apply -f - <<'YAML'
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: profiles}
spec:
  sources:
    - id: shared-http
      http:
        urlSecretRef: {name: profile-source-url, key: url}
        allowHTTP: true
        allowPrivateNetworks: true
      format: URIList
  probe:
    targetSecretRef: {name: profile-targets, key: default}
    expectedStatus: 200
    timeout: 10s
    allowHTTP: true
    allowPrivateTargets: true
    allowPrivateEndpoints: true
  selection: {strategy: Static, topN: 1}
  profiles:
    - name: alpha
      probe:
        targetSecretRef: {name: profile-targets, key: alpha}
        expectedStatus: 200
        timeout: 10s
        allowHTTP: true
        allowPrivateTargets: true
        allowPrivateEndpoints: true
      selection: {strategy: Static, topN: 1}
    - name: beta
      probe:
        targetSecretRef: {name: profile-targets, key: beta}
        expectedStatus: 200
        timeout: 10s
        allowHTTP: true
        allowPrivateTargets: true
        allowPrivateEndpoints: true
      selection: {strategy: Static, topN: 1}
  refreshInterval: 30s
---
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: profile-alpha}
spec:
  poolRef: {name: profiles}
  profileRef: alpha
  engine: Mihomo
  runtime: {managed: {}}
---
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: profile-beta}
spec:
  poolRef: {name: profiles}
  profileRef: beta
  engine: SingBox
  runtime: {managed: {}}
---
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: profile-default}
spec:
  poolRef: {name: profiles}
  engine: SingBox
  runtime: {managed: {}}
YAML
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True proxypool/profiles --timeout=3m
test "$(kubectl -n "$namespace" get proxypool profiles -o jsonpath='{.status.acceptedEndpoints}')" = 2
test "$(kubectl -n "$namespace" get proxypool profiles -o jsonpath='{.status.sources[0].id}')" = shared-http
for name in alpha beta default; do
  kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/profile-${name}" --timeout=5m
  test "$(kubectl -n "$namespace" get egressgateway "profile-${name}" -o jsonpath='{.status.profile}')" = "$name"
  test "$(kubectl -n "$namespace" get egressgateway "profile-${name}" -o jsonpath='{.status.selectedEndpoints}')" = 1
done

alpha_generation=$(kubectl -n "$namespace" get egressgateway profile-alpha -o jsonpath='{.status.activeGeneration}')
beta_generation=$(kubectl -n "$namespace" get egressgateway profile-beta -o jsonpath='{.status.activeGeneration}')
alpha_config=$(kubectl -n "$namespace" get secret "$alpha_generation" -o go-template='{{index .data "config.yaml" | base64decode}}')
beta_config=$(kubectl -n "$namespace" get secret "$beta_generation" -o go-template='{{index .data "config.json" | base64decode}}')
printf '%s' "$alpha_config" | grep -F "$proxy_ip" >/dev/null
if printf '%s' "$alpha_config" | grep -F "$secondary_ip" >/dev/null; then exit 1; fi
printf '%s' "$beta_config" | grep -F "$secondary_ip" >/dev/null
if printf '%s' "$beta_config" | grep -F "$proxy_ip" >/dev/null; then exit 1; fi

for name in alpha beta default; do
  service=$(kubectl -n "$namespace" get egressgateway "profile-${name}" -o jsonpath='{.status.serviceName}')
  auth=$(kubectl -n "$namespace" get egressgateway "profile-${name}" -o jsonpath='{.status.clientAuthSecretName}')
  username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
  password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
  test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
done

# A profile-only update must not change the other managed generation.
kubectl -n "$namespace" patch proxypool profiles --type=json -p '[{"op":"replace","path":"/spec/profiles/0/selection/topN","value":2}]'
sleep 10
test "$(kubectl -n "$namespace" get egressgateway profile-beta -o jsonpath='{.status.activeGeneration}')" = "$beta_generation"
