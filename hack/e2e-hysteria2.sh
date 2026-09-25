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

kubectl -n "$namespace" create secret generic c4-hysteria2-server --from-literal=config.json='{"log":{"level":"warn"},"inbounds":[{"type":"hysteria2","tag":"hy2","listen":"::","listen_port":8461,"users":[{"password":"synthetic-hy2-auth"}],"obfs":{"type":"salamander","password":"synthetic-hy2-obfs"},"tls":{"enabled":true,"certificate_path":"/cert/cert.pem","key_path":"/cert/key.pem"}}],"outbounds":[{"type":"direct","tag":"direct"}]}'
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: c4-hysteria2-server}
spec:
  replicas: 1
  selector: {matchLabels: {app: c4-hysteria2-server}}
  template:
    metadata: {labels: {app: c4-hysteria2-server}}
    spec:
      containers:
        - name: sing-box
          image: $image
          imagePullPolicy: Never
          command: [/usr/local/libexec/egressfox/egressfox-engine-s, run, -c, /config/config.json]
          volumeMounts: [{name: config, mountPath: /config, readOnly: true}, {name: cert, mountPath: /cert, readOnly: true}]
      volumes: [{name: config, secret: {secretName: c4-hysteria2-server}}, {name: cert, secret: {secretName: proxy-cert}}]
---
apiVersion: v1
kind: Service
metadata: {name: c4-hysteria2-server}
spec:
  selector: {app: c4-hysteria2-server}
  ports: [{name: hy2-a, protocol: UDP, port: 8461, targetPort: 8461}, {name: hy2-b, protocol: UDP, port: 8462, targetPort: 8461}]
YAML
kubectl -n "$namespace" rollout status deployment/c4-hysteria2-server --timeout=2m
c4_hy2_ip=$(kubectl -n "$namespace" get service c4-hysteria2-server -o jsonpath='{.spec.clusterIP}')
kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c 'cat >/data/c4-hysteria2-body' <<EOF
hy2://synthetic-hy2-auth@${c4_hy2_ip}:8461,8462?sni=proxy-server&insecure=1&obfs=salamander&obfs-password=synthetic-hy2-obfs&upmbps=20&downmbps=40
EOF
kubectl -n "$namespace" create secret generic c4-hysteria2-source-url --from-literal=url="http://source-provider.${namespace}.svc.cluster.local:8080/c4-hysteria2"
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: c4-hysteria2}
spec:
  sources:
    - id: controlled
      http:
        urlSecretRef: {name: c4-hysteria2-source-url, key: url}
        allowHTTP: true
        allowPrivateNetworks: true
      format: URIList
  allowInsecureTLS: true
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
metadata: {name: c4-hysteria2-mihomo}
spec:
  poolRef: {name: c4-hysteria2}
  engine: Mihomo
  runtime: {managed: {}}
---
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: c4-hysteria2-singbox}
spec:
  poolRef: {name: c4-hysteria2}
  engine: SingBox
  runtime: {managed: {}}
YAML
kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True proxypool/c4-hysteria2 --timeout=3m
for engine in mihomo singbox; do
  gateway="c4-hysteria2-${engine}"
  kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/${gateway}" --timeout=5m
  service=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.serviceName}')
  auth=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.clientAuthSecretName}')
  username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
  password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
  test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
done
# Both managed engines stay active across the default 30-second hop interval.
sleep 31
for engine in mihomo singbox; do
  gateway="c4-hysteria2-${engine}"
  service=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.serviceName}')
  auth=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.clientAuthSecretName}')
  username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
  password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
  test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
done
