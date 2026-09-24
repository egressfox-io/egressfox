#!/bin/sh
set -eu

kind=$(./hack/setup-kind.sh)
go_command=${GO:-go}
kubernetes_minor=${K8S_VERSION:-$($go_command run ./tools/releasectl kubernetes --field minimum-supported)}
cluster=${KIND_CLUSTER:-egressfox-e2e}
namespace=egressfox-e2e
image=${EGRESSFOX_E2E_IMAGE:-egressfox:e2e}
container_cli=${CONTAINER_CLI:-docker}
if [ "${KIND_EXPERIMENTAL_PROVIDER:-}" = podman ] && [ "${image#*/}" = "$image" ]; then
  image="localhost/$image"
fi
image_repository=${image%:*}
image_tag=${image##*:}
node_image=$($go_command run ./tools/releasectl kubernetes --version "$kubernetes_minor" --field kind-node-image)
image_archive=""
cleanup() {
	status=$?
	if [ "${KEEP_KIND_CLUSTER_ON_FAILURE:-0}" = 1 ] && [ "$status" -ne 0 ]; then
		echo "keeping failed kind cluster $cluster for diagnostics" >&2
		return "$status"
	fi
  "$kind" delete cluster --name "$cluster" >/dev/null 2>&1 || true
  if [ -n "$image_archive" ]; then rm -f "$image_archive"; fi
}
trap cleanup EXIT INT TERM

cleanup
"$container_cli" build -t "$image" .
"$kind" create cluster --name "$cluster" --image "$node_image" --wait 120s
kubectl get --raw /version | tr -d '[:space:]' | grep -F "\"gitVersion\":\"v${kubernetes_minor}." >/dev/null || {
  echo "kind server version does not match pinned Kubernetes $kubernetes_minor" >&2
  exit 1
}
if [ "${KIND_EXPERIMENTAL_PROVIDER:-}" = podman ]; then
  image_archive="${TMPDIR:-/tmp}/egressfox-kind-image.$$.tar"
  "$container_cli" save -o "$image_archive" "$image"
  "$kind" load image-archive --name "$cluster" "$image_archive"
  rm -f "$image_archive"
  image_archive=""
else
  "$kind" load docker-image --name "$cluster" "$image"
fi
helm upgrade --install egressfox charts/egressfox --namespace "$namespace" --create-namespace \
  --set-string image.repository="$image_repository" --set-string image.tag="$image_tag" --set image.pullPolicy=Never --wait --timeout 3m

sa="system:serviceaccount:${namespace}:egressfox-egressfox"
test "$(kubectl auth can-i get secrets -n "$namespace" --as "$sa")" = yes
test "$(kubectl auth can-i get secrets -n default --as "$sa")" = no
test "$(kubectl auth can-i delete secrets -n "$namespace" --as "$sa")" = yes
test "$(kubectl auth can-i create deployments -n "$namespace" --as "$sa")" = yes
test "$(kubectl auth can-i create deployments -n default --as "$sa")" = no
test "$(kubectl auth can-i create statefulsets -n "$namespace" --as "$sa")" = no
test "$(kubectl auth can-i get nodes --as "$sa")" = no

uuid=11111111-1111-4111-8111-111111111111
kubectl -n "$namespace" create secret generic proxy-server --from-literal=config.json="{\"log\":{\"level\":\"warn\"},\"inbounds\":[{\"type\":\"vless\",\"tag\":\"in\",\"listen\":\"::\",\"listen_port\":8443,\"users\":[{\"uuid\":\"$uuid\"}]},{\"type\":\"vmess\",\"tag\":\"vmess\",\"listen\":\"::\",\"listen_port\":8445,\"users\":[{\"name\":\"synthetic\",\"uuid\":\"$uuid\",\"alterId\":0}]},{\"type\":\"shadowsocks\",\"tag\":\"shadowsocks\",\"listen\":\"::\",\"listen_port\":8446,\"network\":\"tcp\",\"method\":\"aes-128-gcm\",\"password\":\"synthetic-password\"}],\"outbounds\":[{\"type\":\"direct\",\"tag\":\"direct\"}]}"
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: proxy-server}
spec:
  replicas: 1
  selector: {matchLabels: {app: proxy-server}}
  template:
    metadata: {labels: {app: proxy-server}}
    spec:
      containers:
        - name: sing-box
          image: $image
          imagePullPolicy: Never
          command: [/usr/local/libexec/egressfox/egressfox-engine-s, run, -c, /config/config.json]
          volumeMounts: [{name: config, mountPath: /config, readOnly: true}]
      volumes: [{name: config, secret: {secretName: proxy-server}}]
---
apiVersion: v1
kind: Service
metadata: {name: proxy-server}
spec:
  selector: {app: proxy-server}
  ports: [{name: primary, port: 8443, targetPort: 8443}, {name: rollout, port: 8444, targetPort: 8443}, {name: vmess, port: 8445, targetPort: 8445}, {name: shadowsocks, port: 8446, targetPort: 8446}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: probe-target}
spec:
  replicas: 1
  selector: {matchLabels: {app: probe-target}}
  template:
    metadata: {labels: {app: probe-target}}
    spec:
      containers:
        - name: http
          image: busybox:1.37.0-uclibc@sha256:8d7b1636e974e0adfd8d945955fca609304f0a56c18799dfd032d6e661382d84
          command: [/bin/sh, -c, "mkdir -p /tmp/www; echo ok >/tmp/www/index.html; /bin/busybox httpd -f -p 8080 -h /tmp/www"]
          securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, allowPrivilegeEscalation: false}
---
apiVersion: v1
kind: Service
metadata: {name: probe-target}
spec:
  selector: {app: probe-target}
  ports: [{port: 8080, targetPort: 8080}]
YAML
kubectl -n "$namespace" rollout status deployment/proxy-server --timeout=2m
kubectl -n "$namespace" rollout status deployment/probe-target --timeout=2m
proxy_ip=$(kubectl -n "$namespace" get service proxy-server -o jsonpath='{.spec.clusterIP}')
target_ip=$(kubectl -n "$namespace" get service probe-target -o jsonpath='{.spec.clusterIP}')
kubectl -n "$namespace" create secret generic subscription --from-literal=nodes="vless://${uuid}@${proxy_ip}:8443?encryption=none&security=none#e2e"
kubectl -n "$namespace" create secret generic target --from-literal=url="http://${target_ip}:8080/"
kubectl -n "$namespace" apply -f - <<'YAML'
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: e2e}
spec:
  sources:
    - id: primary
      secretRef: {name: subscription, key: nodes}
      format: URIList
  probe:
    targetSecretRef: {name: target, key: url}
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
metadata: {name: e2e}
spec:
  poolRef: {name: e2e}
  engine: SingBox
  listener: {address: 127.0.0.1, port: 1080}
  outputSecretName: e2e-engine-config
YAML

kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True proxypool/e2e --timeout=2m
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Published")].status}'=True egressgateway/e2e --timeout=4m
test "$(kubectl -n "$namespace" get secret e2e-engine-config -o jsonpath='{.metadata.ownerReferences[0].kind}')" = EgressGateway

kubectl -n "$namespace" apply -f - <<YAML
apiVersion: v1
kind: Pod
metadata: {name: byo-engine}
spec:
  restartPolicy: Never
  containers:
    - name: engine
      image: $image
      imagePullPolicy: Never
      command: [/usr/local/libexec/egressfox/egressfox-engine-s, run, -c, /config/config.json]
      volumeMounts: [{name: config, mountPath: /config, readOnly: true}]
    - name: client
      image: curlimages/curl:8.17.0@sha256:935d9100e9ba842cdb060de42472c7ca90cfe9a7c96e4dacb55e79e560b3ff40
      command: [sleep, "600"]
  volumes: [{name: config, secret: {secretName: e2e-engine-config}}]
YAML
kubectl -n "$namespace" wait --for=condition=Ready pod/byo-engine --timeout=2m
code=$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --socks5 127.0.0.1:1080 -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")
test "$code" = 200

# Owned output deletion converges through a newly validated publication.
kubectl -n "$namespace" delete secret e2e-engine-config
for _ in $(seq 1 120); do
  kubectl -n "$namespace" get secret e2e-engine-config >/dev/null 2>&1 && break
  sleep 1
done
kubectl -n "$namespace" get secret e2e-engine-config >/dev/null
before=$(kubectl -n "$namespace" get secret e2e-engine-config -o jsonpath='{.metadata.resourceVersion}')

# A process restart recovers decision state from the PVC and leaves identical
# desired bytes as an API no-op.
kubectl -n "$namespace" rollout restart deployment/egressfox-egressfox
kubectl -n "$namespace" rollout status deployment/egressfox-egressfox --timeout=3m
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Published")].status}'=True egressgateway/e2e --timeout=2m
restarted=$(kubectl -n "$namespace" get secret e2e-engine-config -o jsonpath='{.metadata.resourceVersion}')
test "$before" = "$restarted"

# A rejected credential rotation must preserve the published LKG object.
kubectl -n "$namespace" create secret generic subscription --from-literal=nodes="vless://22222222-2222-4222-8222-222222222222@${proxy_ip}:8443?encryption=none&security=none#rotated" --dry-run=client -o yaml | kubectl apply -f -
sleep 45
after=$(kubectl -n "$namespace" get secret e2e-engine-config -o jsonpath='{.metadata.resourceVersion}')
test "$before" = "$after"

# Restore the admitted source before exercising the managed path.
kubectl -n "$namespace" create secret generic subscription --from-literal=nodes="vless://${uuid}@${proxy_ip}:8443?encryption=none&security=none#managed" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True proxypool/e2e --timeout=2m

for engine in SingBox Mihomo; do
  name=$(printf '%s' "$engine" | tr '[:upper:]' '[:lower:]')
  kubectl -n "$namespace" apply -f - <<YAML
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: managed-${name}}
spec:
  poolRef: {name: e2e}
  engine: ${engine}
  runtime: {managed: {}}
YAML
  kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/managed-${name}" --timeout=5m
  service=$(kubectl -n "$namespace" get "egressgateway/managed-${name}" -o jsonpath='{.status.serviceName}')
  auth=$(kubectl -n "$namespace" get "egressgateway/managed-${name}" -o jsonpath='{.status.clientAuthSecretName}')
  username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
  password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
  test -n "$service"
  test -n "$username"
  test -n "$password"
  if kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 5 --socks5 "${service}:1080" "http://${target_ip}:8080/" >/dev/null 2>&1; then
    echo "managed ${engine} accepted unauthenticated SOCKS" >&2
    exit 1
  fi
  if kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 5 --proxy "socks5://${username}:wrong-password@${service}:1080" "http://${target_ip}:8080/" >/dev/null 2>&1; then
    echo "managed ${engine} accepted wrong SOCKS credentials" >&2
    exit 1
  fi
  code=$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")
  test "$code" = 200
  deployment=$(kubectl -n "$namespace" get deployment -l "egressfox.io/gateway-uid=$(kubectl -n "$namespace" get "egressgateway/managed-${name}" -o jsonpath='{.metadata.uid}')" -o jsonpath='{.items[0].metadata.name}')
  test "$(kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.replicas}')" = 1
  test "$(kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.strategy.rollingUpdate.maxUnavailable}')" = 0
  test "$(kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.strategy.rollingUpdate.maxSurge}')" = 1
  test "$(kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.template.spec.hostNetwork}')" != true
  test "$(kubectl -n "$namespace" get deployment "$deployment" -o jsonpath='{.spec.template.spec.automountServiceAccountToken}')" = false
  if kubectl -n "$namespace" get deployment "$deployment" -o json | grep -F "$password" >/dev/null; then
    echo "managed credentials leaked into Deployment" >&2
    exit 1
  fi
done

# A quota-blocked surge makes the replacement fail while the old ready Pod and
# its immutable generation remain available through the stable Service.
gateway=managed-singbox
old_generation=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.activeGeneration}')
pod_limit=$(kubectl -n "$namespace" get pods --no-headers | wc -l | tr -d ' ')
kubectl -n "$namespace" create quota block-managed-surge --hard="pods=${pod_limit}"
kubectl -n "$namespace" create secret generic subscription --from-literal=nodes="vless://${uuid}@${proxy_ip}:8444?encryption=none&security=none#rollout" --dry-run=client -o yaml | kubectl apply -f -
for _ in $(seq 1 180); do
  published=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.publishedGeneration}')
  [ -n "$published" ] && [ "$published" != "$old_generation" ] && break
  sleep 1
done
test "$published" != "$old_generation"
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Degraded")].status}'=True "egressgateway/${gateway}" --timeout=4m
test "$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.activeGeneration}')" = "$old_generation"
test "$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.conditions[?(@.type=="Activated")].status}')" = False
test "$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.conditions[?(@.type=="RuntimeReady")].status}')" = True
kubectl -n "$namespace" get secret "$old_generation" >/dev/null
service=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.serviceName}')
auth=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.clientAuthSecretName}')
username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
kubectl -n "$namespace" delete quota block-managed-surge
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/${gateway}" --timeout=5m

# Client-auth deletion repairs the same credentials from protected generation
# data instead of exposing new credentials before an old Pod can accept them.
active_generation=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.activeGeneration}')
old_password=$password
kubectl -n "$namespace" delete secret "$auth"
for _ in $(seq 1 120); do
  kubectl -n "$namespace" get secret "$auth" >/dev/null 2>&1 && break
  sleep 1
done
password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
test "$password" = "$old_password"
test "$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.activeGeneration}')" = "$active_generation"

# Owned child deletion is repaired and an operator restart retains exact active
# generation attribution without creating another immutable generation.
kubectl -n "$namespace" delete service "$service"
for _ in $(seq 1 120); do
  kubectl -n "$namespace" get service "$service" >/dev/null 2>&1 && break
  sleep 1
done
kubectl -n "$namespace" get service "$service" >/dev/null
generation_count=$(kubectl -n "$namespace" get secrets -l "egressfox.io/gateway-uid=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.metadata.uid}'),egressfox.io/component=runtime-generation" --no-headers | wc -l | tr -d ' ')
test "$generation_count" -le 2
kubectl -n "$namespace" rollout restart deployment/egressfox-egressfox
kubectl -n "$namespace" rollout status deployment/egressfox-egressfox --timeout=3m
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/${gateway}" --timeout=3m
test "$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.activeGeneration}')" = "$active_generation"

# Managed to BYO publishes the requested output first, removes only exact-owned
# runtime children and clears the managed Service surface.
managed_to_byo=managed-mihomo
managed_to_byo_service=$(kubectl -n "$namespace" get egressgateway "$managed_to_byo" -o jsonpath='{.status.serviceName}')
kubectl -n "$namespace" patch egressgateway "$managed_to_byo" --type=json -p='[{"op":"remove","path":"/spec/runtime"},{"op":"add","path":"/spec/outputSecretName","value":"managed-mihomo-byo-config"}]'
for _ in $(seq 1 180); do
  if kubectl -n "$namespace" get secret managed-mihomo-byo-config >/dev/null 2>&1 && ! kubectl -n "$namespace" get service "$managed_to_byo_service" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
kubectl -n "$namespace" get secret managed-mihomo-byo-config >/dev/null
if kubectl -n "$namespace" get service "$managed_to_byo_service" >/dev/null 2>&1; then
  echo "managed Service survived transition to BYO" >&2
  exit 1
fi
test "$(kubectl -n "$namespace" get egressgateway "$managed_to_byo" -o jsonpath='{.status.conditions[?(@.type=="Activated")].status}')" = Unknown

# BYO to managed retains the old owned output until managed activation, then
# removes only that output. The user-created BYO Pod remains untouched.
kubectl -n "$namespace" patch egressgateway e2e --type=json -p='[{"op":"remove","path":"/spec/listener"},{"op":"remove","path":"/spec/outputSecretName"},{"op":"add","path":"/spec/runtime","value":{"managed":{}}}]'
for _ in $(seq 1 300); do
  byo_to_managed_service=$(kubectl -n "$namespace" get egressgateway e2e -o jsonpath='{.status.serviceName}')
  if [ -n "$byo_to_managed_service" ] && kubectl -n "$namespace" get service "$byo_to_managed_service" >/dev/null 2>&1 && ! kubectl -n "$namespace" get secret e2e-engine-config >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
test -n "$byo_to_managed_service"
kubectl -n "$namespace" get service "$byo_to_managed_service" >/dev/null
if kubectl -n "$namespace" get secret e2e-engine-config >/dev/null 2>&1; then
  echo "BYO output survived completed transition to managed mode" >&2
  exit 1
fi
kubectl -n "$namespace" get pod byo-engine >/dev/null
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True egressgateway/e2e --timeout=5m

# A controlled subscription server exercises managed HTTP refresh and both engine
# profiles without depending on an external provider. Its state is local to kind.
kubectl -n "$namespace" apply -f - <<'YAML'
apiVersion: v1
kind: ConfigMap
metadata: {name: source-provider-script}
data:
  subscription: |
    #!/bin/sh
    # BusyBox uClibc ash's read builtin crashes under the pinned 1.37 kind
    # node. Parse the HTTP header block with awk instead.
    headers=$(awk '{sub(/\r$/, ""); if ($0 == "") exit; print}')
    request_line=$(printf '%s\n' "$headers" | sed -n '1p')
    conditional=$(printf '%s\n' "$headers" | sed -n 's/^If-None-Match: //p' | sed -n '1p')
    user_agent=$(printf '%s\n' "$headers" | sed -n 's/^User-Agent: //p' | sed -n '1p')
    hwid=$(printf '%s\n' "$headers" | sed -n 's/^X-Hwid: //p' | sed -n '1p')
    device_os=$(printf '%s\n' "$headers" | sed -n 's/^X-Device-Os: //p' | sed -n '1p')
    provider_variant=$(printf '%s\n' "$headers" | sed -n 's/^X-Provider-Variant: //p' | sed -n '1p')
    printf '%s' "$user_agent" >/data/user-agent
    printf '%s' "$hwid" >/data/hwid
    printf '%s' "$device_os" >/data/device-os
    printf '%s' "$provider_variant" >/data/provider-variant
    if [ -f /data/outage ]; then
      printf 'HTTP/1.1 503 Service Unavailable\r\nContent-Length: 0\r\nConnection: close\r\n\r\n'
      exit 0
    fi
    body_file=/data/body
    etag=$(cat /data/etag)
    case "$request_line" in
      'GET /vmess '*) body_file=/data/vmess-body; etag=vmess-v1 ;;
      'GET /shadowsocks '*) body_file=/data/shadowsocks-body; etag=shadowsocks-v1 ;;
    esac
    if [ "$conditional" = "\"$etag\"" ]; then
      touch /data/conditional
      printf 'HTTP/1.1 304 Not Modified\r\nETag: "%s"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n' "$etag"
      exit 0
    fi
    length=$(wc -c <"$body_file" | tr -d ' ')
    printf 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nETag: "%s"\r\nContent-Length: %s\r\nConnection: close\r\n\r\n' "$etag" "$length"
    cat "$body_file"
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: source-provider}
spec:
  replicas: 1
  selector: {matchLabels: {app: source-provider}}
  template:
    metadata: {labels: {app: source-provider}}
    spec:
      securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532}
      containers:
        - name: http
          image: busybox:1.37.0-uclibc@sha256:8d7b1636e974e0adfd8d945955fca609304f0a56c18799dfd032d6e661382d84
          command: [/bin/sh, -c, 'cp /script/subscription /www/subscription; chmod 755 /www/subscription; exec /bin/busybox nc -lk -p 8080 -e /www/subscription']
          volumeMounts: [{name: script, mountPath: /script, readOnly: true}, {name: www, mountPath: /www}, {name: data, mountPath: /data}]
          securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}
      volumes: [{name: script, configMap: {name: source-provider-script}}, {name: www, emptyDir: {}}, {name: data, emptyDir: {}}]
---
apiVersion: v1
kind: Service
metadata: {name: source-provider}
spec:
  selector: {app: source-provider}
  ports: [{port: 8080, targetPort: 8080}]
YAML
kubectl -n "$namespace" rollout status deployment/source-provider --timeout=2m
provider_pod=$(kubectl -n "$namespace" get pod -l app=source-provider -o jsonpath='{.items[0].metadata.name}')
kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c 'cat >/data/body; echo v1 >/data/etag' <<JSON
{"dns":{"servers":["8.8.8.8"]},"inbounds":[{"protocol":"socks","port":1080}],"routing":{"rules":[{"outboundTag":"direct"}]},"outbounds":[{"protocol":"freedom","tag":"direct"},{"protocol":"vless","tag":"http-initial","settings":{"vnext":[{"address":"${proxy_ip}","port":8443,"users":[{"id":"${uuid}","encryption":"none"}]}]},"streamSettings":{"security":"none","network":"tcp"}}]}
JSON
kubectl -n "$namespace" create secret generic source-url --from-literal=url="http://source-provider.${namespace}.svc.cluster.local:8080/subscription"
kubectl -n "$namespace" apply -f - <<'YAML'
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: http-e2e}
spec:
  sources:
    - id: managed
      http:
        urlSecretRef: {name: source-url, key: url}
        allowHTTP: true
        allowPrivateNetworks: true
        maxStale: 2m
        profile:
          userAgent: EgressFox-E2E/1
          hwid: stable-e2e-client
          deviceOS: iOS
          osVersion: "18.3"
          deviceModel: Test Device
          headers: {X-Provider-Variant: controlled}
      format: Auto
  probe:
    targetSecretRef: {name: target, key: url}
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
metadata: {name: http-singbox}
spec:
  poolRef: {name: http-e2e}
  engine: SingBox
  runtime: {managed: {}}
---
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: http-mihomo}
spec:
  poolRef: {name: http-e2e}
  engine: Mihomo
  runtime: {managed: {}}
YAML
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True proxypool/http-e2e --timeout=3m
test "$(kubectl -n "$namespace" exec "$provider_pod" -- cat /data/user-agent)" = EgressFox-E2E/1
test "$(kubectl -n "$namespace" exec "$provider_pod" -- cat /data/hwid)" = stable-e2e-client
test "$(kubectl -n "$namespace" exec "$provider_pod" -- cat /data/device-os)" = iOS
test "$(kubectl -n "$namespace" exec "$provider_pod" -- cat /data/provider-variant)" = controlled
for engine in singbox mihomo; do
  kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/http-${engine}" --timeout=5m
  service=$(kubectl -n "$namespace" get egressgateway "http-${engine}" -o jsonpath='{.status.serviceName}')
  auth=$(kubectl -n "$namespace" get egressgateway "http-${engine}" -o jsonpath='{.status.clientAuthSecretName}')
  username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
  password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
  test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
done
initial_singbox=$(kubectl -n "$namespace" get egressgateway http-singbox -o jsonpath='{.status.activeGeneration}')
initial_mihomo=$(kubectl -n "$namespace" get egressgateway http-mihomo -o jsonpath='{.status.activeGeneration}')
# Explicit URIList must reject the provider's JSON body. The incompatible cache
# is unavailable, while the previously activated Gateway remains serving.
kubectl -n "$namespace" patch proxypool http-e2e --type=json -p '[{"op":"replace","path":"/spec/sources/0/format","value":"URIList"}]'
for _ in $(seq 1 120); do
  state=$(kubectl -n "$namespace" get proxypool http-e2e -o jsonpath='{.status.sources[0].state}')
  [ "$state" = Unavailable ] && break
  sleep 1
done
test "$state" = Unavailable
test "$(kubectl -n "$namespace" get egressgateway http-singbox -o jsonpath='{.status.activeGeneration}')" = "$initial_singbox"
test "$(kubectl -n "$namespace" get egressgateway http-mihomo -o jsonpath='{.status.activeGeneration}')" = "$initial_mihomo"
kubectl -n "$namespace" patch proxypool http-e2e --type=json -p '[{"op":"replace","path":"/spec/sources/0/format","value":"Auto"}]'
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True proxypool/http-e2e --timeout=3m
test "$(kubectl -n "$namespace" get egressgateway http-singbox -o jsonpath='{.status.activeGeneration}')" = "$initial_singbox"
test "$(kubectl -n "$namespace" get egressgateway http-mihomo -o jsonpath='{.status.activeGeneration}')" = "$initial_mihomo"
# Linux managed-path proof for both newly supported protocols and both engine
# profiles. The same controlled HTTP provider supplies synthetic Xray outbounds.
kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c 'cat >/data/vmess-body' <<JSON
{"outbounds":[{"protocol":"vmess","tag":"vmess-controlled","settings":{"vnext":[{"address":"${proxy_ip}","port":8445,"users":[{"id":"${uuid}","security":"auto","alterId":0}]}]},"streamSettings":{"network":"tcp","security":"none"}}]}
JSON
kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c 'cat >/data/shadowsocks-body' <<JSON
{"outbounds":[{"protocol":"shadowsocks","tag":"ss-controlled","settings":{"servers":[{"address":"${proxy_ip}","port":8446,"method":"aes-128-gcm","password":"synthetic-password"}]},"streamSettings":{"network":"tcp","security":"none"}}]}
JSON
kubectl -n "$namespace" create secret generic vmess-source-url --from-literal=url="http://source-provider.${namespace}.svc.cluster.local:8080/vmess"
kubectl -n "$namespace" create secret generic shadowsocks-source-url --from-literal=url="http://source-provider.${namespace}.svc.cluster.local:8080/shadowsocks"
for protocol in vmess shadowsocks; do
  kubectl -n "$namespace" apply -f - <<YAML
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: http-${protocol}}
spec:
  sources:
    - id: managed
      http:
        urlSecretRef: {name: ${protocol}-source-url, key: url}
        allowHTTP: true
        allowPrivateNetworks: true
      format: Auto
  probe:
    targetSecretRef: {name: target, key: url}
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
metadata: {name: ${protocol}-singbox}
spec:
  poolRef: {name: http-${protocol}}
  engine: SingBox
  runtime: {managed: {}}
---
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: ${protocol}-mihomo}
spec:
  poolRef: {name: http-${protocol}}
  engine: Mihomo
  runtime: {managed: {}}
YAML
  kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "proxypool/http-${protocol}" --timeout=3m
  test "$(kubectl -n "$namespace" get proxypool "http-${protocol}" -o jsonpath='{.status.acceptedEndpoints}')" = 1
  for engine in singbox mihomo; do
    gateway="${protocol}-${engine}"
    kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/${gateway}" --timeout=5m
    service=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.serviceName}')
    auth=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.clientAuthSecretName}')
    username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
    password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
    test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
  done
done
for _ in $(seq 1 75); do
  kubectl -n "$namespace" exec "$provider_pod" -- test -f /data/conditional >/dev/null 2>&1 && break
  sleep 1
done
kubectl -n "$namespace" exec "$provider_pod" -- test -f /data/conditional
test "$(kubectl -n "$namespace" get egressgateway http-singbox -o jsonpath='{.status.activeGeneration}')" = "$initial_singbox"
test "$(kubectl -n "$namespace" get egressgateway http-mihomo -o jsonpath='{.status.activeGeneration}')" = "$initial_mihomo"
kubectl -n "$namespace" exec "$provider_pod" -- touch /data/outage
sleep 40
test "$(kubectl -n "$namespace" get proxypool http-e2e -o jsonpath='{.status.acceptedEndpoints}')" = 1
kubectl -n "$namespace" rollout restart deployment/egressfox-egressfox
kubectl -n "$namespace" rollout status deployment/egressfox-egressfox --timeout=3m
test "$(kubectl -n "$namespace" get proxypool http-e2e -o jsonpath='{.status.acceptedEndpoints}')" = 1
kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c 'rm -f /data/outage; cat >/data/body; echo v2 >/data/etag' <<JSON
{"outbounds":[{"protocol":"vless","tag":"http-changed","settings":{"vnext":[{"address":"${proxy_ip}","port":8444,"users":[{"id":"${uuid}","encryption":"none"}]}]},"streamSettings":{"security":"none","network":"tcp"}}]}
JSON
for _ in $(seq 1 180); do
  changed_singbox=$(kubectl -n "$namespace" get egressgateway http-singbox -o jsonpath='{.status.activeGeneration}')
  changed_mihomo=$(kubectl -n "$namespace" get egressgateway http-mihomo -o jsonpath='{.status.activeGeneration}')
  [ "$changed_singbox" != "$initial_singbox" ] && [ "$changed_mihomo" != "$initial_mihomo" ] && break
  sleep 1
done
test "$changed_singbox" != "$initial_singbox"
test "$changed_mihomo" != "$initial_mihomo"
kubectl -n "$namespace" exec "$provider_pod" -- sh -c "printf '%s\n' 'malformed subscription' >/data/body; echo v3 >/data/etag"
sleep 40
test "$(kubectl -n "$namespace" get egressgateway http-singbox -o jsonpath='{.status.activeGeneration}')" = "$changed_singbox"
test "$(kubectl -n "$namespace" get egressgateway http-mihomo -o jsonpath='{.status.activeGeneration}')" = "$changed_mihomo"
kubectl -n "$namespace" exec "$provider_pod" -- touch /data/outage
for _ in $(seq 1 180); do
  state=$(kubectl -n "$namespace" get proxypool http-e2e -o jsonpath='{.status.sources[0].state}')
  [ "$state" = Expired ] && break
  sleep 1
done
test "$state" = Expired
test "$(kubectl -n "$namespace" get egressgateway http-singbox -o jsonpath='{.status.activeGeneration}')" = "$changed_singbox"
test "$(kubectl -n "$namespace" get egressgateway http-mihomo -o jsonpath='{.status.activeGeneration}')" = "$changed_mihomo"

helm upgrade egressfox charts/egressfox --namespace "$namespace" --set-string image.repository="$image_repository" --set-string image.tag="$image_tag" --set image.pullPolicy=Never --wait --timeout 3m
helm uninstall egressfox --namespace "$namespace"
kubectl get crd proxypools.egressfox.io egressgateways.egressfox.io >/dev/null
