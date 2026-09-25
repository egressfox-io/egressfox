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
kubectl -n "$namespace" rollout restart deployment/${operator_deployment}
kubectl -n "$namespace" rollout status deployment/${operator_deployment} --timeout=3m
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
