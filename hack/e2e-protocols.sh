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
# C2: every new proxy family carries managed Gateway traffic on both pinned
# engines. HTTPS here uses an explicit test-only verification opt-out for the
# short-lived in-cluster certificate; local tests also cover strict TLS fields.
for variant in socks-anon socks-auth http-auth https-auth; do
  format=URIList
  allow_insecure=false
  case "$variant" in
    socks-anon) nodes="socks5://${proxy_ip}:8447" ;;
    socks-auth) nodes="socks5://synthetic-user:synthetic-password@${proxy_ip}:8448" ;;
    http-auth) nodes="http://synthetic-user:synthetic-password@${proxy_ip}:8449" ;;
    https-auth)
      nodes="{\"outbounds\":[{\"type\":\"http\",\"server\":\"${proxy_ip}\",\"server_port\":8450,\"username\":\"synthetic-user\",\"password\":\"synthetic-password\",\"tls\":{\"enabled\":true,\"server_name\":\"proxy-server\",\"insecure\":true}}]}"
      format=JSON
      allow_insecure=true
      ;;
  esac
  kubectl -n "$namespace" create secret generic "c2-${variant}-subscription" --from-literal=nodes="$nodes"
  kubectl -n "$namespace" apply -f - <<YAML
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: c2-${variant}}
spec:
  sources:
    - id: controlled
      secretRef: {name: c2-${variant}-subscription, key: nodes}
      format: ${format}
  allowInsecureTLS: ${allow_insecure}
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
metadata: {name: c2-${variant}-singbox}
spec:
  poolRef: {name: c2-${variant}}
  engine: SingBox
  runtime: {managed: {}}
---
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: c2-${variant}-mihomo}
spec:
  poolRef: {name: c2-${variant}}
  engine: Mihomo
  runtime: {managed: {}}
YAML
  kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "proxypool/c2-${variant}" --timeout=3m
  test "$(kubectl -n "$namespace" get proxypool "c2-${variant}" -o jsonpath='{.status.acceptedEndpoints}')" = 1
  for engine in singbox mihomo; do
    gateway="c2-${variant}-${engine}"
    kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/${gateway}" --timeout=5m
    service=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.serviceName}')
    auth=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.clientAuthSecretName}')
    username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
    password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
    test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
  done
  kubectl -n "$namespace" delete egressgateway "c2-${variant}-singbox" "c2-${variant}-mihomo" --wait=true
  kubectl -n "$namespace" delete proxypool "c2-${variant}" --wait=true
done
