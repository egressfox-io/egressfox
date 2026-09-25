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

# C3: a synthetic, fixed X25519 fixture exercises source admission, through-engine
# probing, managed generation activation and authenticated SOCKS application traffic.
# The key is test material only and is unrelated to any provider subscription.
reality_private=MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY
reality_public=hlF7wMOf6Ha3ZGy1407gPa9xzLrWdFDBapcK2MvAPn4
kubectl -n "$namespace" create secret generic c3-proxy-server --from-literal=config.json="{\"log\":{\"level\":\"warn\"},\"inbounds\":[{\"type\":\"vless\",\"tag\":\"reality\",\"listen\":\"::\",\"listen_port\":8451,\"users\":[{\"name\":\"synthetic\",\"uuid\":\"$uuid\",\"flow\":\"xtls-rprx-vision\"}],\"tls\":{\"enabled\":true,\"server_name\":\"front.example.com\",\"reality\":{\"enabled\":true,\"private_key\":\"$reality_private\",\"short_id\":[\"a1b2\"],\"handshake\":{\"server\":\"127.0.0.1\",\"server_port\":8452}}}},{\"type\":\"http\",\"tag\":\"handshake\",\"listen\":\"127.0.0.1\",\"listen_port\":8452,\"tls\":{\"enabled\":true,\"certificate_path\":\"/cert/cert.pem\",\"key_path\":\"/cert/key.pem\"}},{\"type\":\"vless\",\"tag\":\"upgrade\",\"listen\":\"::\",\"listen_port\":8453,\"users\":[{\"name\":\"synthetic\",\"uuid\":\"$uuid\"}],\"transport\":{\"type\":\"httpupgrade\",\"path\":\"/upgrade\"},\"tls\":{\"enabled\":true,\"certificate_path\":\"/cert/cert.pem\",\"key_path\":\"/cert/key.pem\"}}],\"outbounds\":[{\"type\":\"direct\",\"tag\":\"direct\"}]}"
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: c3-proxy-server}
spec:
  replicas: 1
  selector: {matchLabels: {app: c3-proxy-server}}
  template:
    metadata: {labels: {app: c3-proxy-server}}
    spec:
      containers:
        - name: sing-box
          image: $image
          imagePullPolicy: Never
          command: [/usr/local/libexec/egressfox/egressfox-engine-s, run, -c, /config/config.json]
          volumeMounts: [{name: config, mountPath: /config, readOnly: true}, {name: cert, mountPath: /cert, readOnly: true}]
      volumes: [{name: config, secret: {secretName: c3-proxy-server}}, {name: cert, secret: {secretName: proxy-cert}}]
---
apiVersion: v1
kind: Service
metadata: {name: c3-proxy-server}
spec:
  selector: {app: c3-proxy-server}
  ports: [{name: reality, port: 8451, targetPort: 8451}, {name: upgrade, port: 8453, targetPort: 8453}]
YAML
kubectl -n "$namespace" rollout status deployment/c3-proxy-server --timeout=2m
c3_proxy_ip=$(kubectl -n "$namespace" get service c3-proxy-server -o jsonpath='{.spec.clusterIP}')
cat >"$cert_dir/c3-transports.json" <<JSON
{"log":{"level":"warn"},"inbounds":[{"type":"vless","tag":"h2","listen":"::","listen_port":8454,"users":[{"name":"synthetic","uuid":"$uuid"}],"transport":{"type":"http","path":"/h2"},"tls":{"enabled":true,"certificate_path":"/cert/cert.pem","key_path":"/cert/key.pem"}},{"type":"vless","tag":"grpc","listen":"::","listen_port":8455,"users":[{"name":"synthetic","uuid":"$uuid"}],"transport":{"type":"grpc","service_name":"EgressFox"},"tls":{"enabled":true,"certificate_path":"/cert/cert.pem","key_path":"/cert/key.pem"}},{"type":"vless","tag":"ws","listen":"::","listen_port":8456,"users":[{"name":"synthetic","uuid":"$uuid"}],"transport":{"type":"ws","path":"/ws"},"tls":{"enabled":true,"certificate_path":"/cert/cert.pem","key_path":"/cert/key.pem"}},{"type":"vless","tag":"utls","listen":"::","listen_port":8457,"users":[{"name":"synthetic","uuid":"$uuid"}],"tls":{"enabled":true,"alpn":["http/1.1"],"certificate_path":"/cert/cert.pem","key_path":"/cert/key.pem"}}],"outbounds":[{"type":"direct","tag":"direct"}]}
JSON
kubectl -n "$namespace" create secret generic c3-transport-server --from-file=config.json="$cert_dir/c3-transports.json"
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: c3-transport-server}
spec:
  replicas: 1
  selector: {matchLabels: {app: c3-transport-server}}
  template:
    metadata: {labels: {app: c3-transport-server}}
    spec:
      containers:
        - name: sing-box
          image: $image
          imagePullPolicy: Never
          command: [/usr/local/libexec/egressfox/egressfox-engine-s, run, -c, /config/config.json]
          volumeMounts: [{name: config, mountPath: /config, readOnly: true}, {name: cert, mountPath: /cert, readOnly: true}]
      volumes: [{name: config, secret: {secretName: c3-transport-server}}, {name: cert, secret: {secretName: proxy-cert}}]
---
apiVersion: v1
kind: Service
metadata: {name: c3-transport-server}
spec:
  selector: {app: c3-transport-server}
  ports: [{name: h2, port: 8454, targetPort: 8454}, {name: grpc, port: 8455, targetPort: 8455}, {name: ws, port: 8456, targetPort: 8456}, {name: utls, port: 8457, targetPort: 8457}]
YAML
kubectl -n "$namespace" rollout status deployment/c3-transport-server --timeout=2m
c3_transport_ip=$(kubectl -n "$namespace" get service c3-transport-server -o jsonpath='{.spec.clusterIP}')
cat >"$cert_dir/c3-xhttp.yaml" <<YAML
mode: rule
log-level: warning
allow-lan: false
listeners:
  - {name: stream-one, type: vless, listen: "0.0.0.0", port: 8458, users: [{username: synthetic, uuid: "$uuid"}], certificate: /data/cert/cert.pem, private-key: /data/cert/key.pem, xhttp-config: {path: /xhttp, host: c3-xhttp-server, mode: stream-one}}
  - {name: stream-up, type: vless, listen: "0.0.0.0", port: 8459, users: [{username: synthetic, uuid: "$uuid"}], certificate: /data/cert/cert.pem, private-key: /data/cert/key.pem, xhttp-config: {path: /xhttp, host: c3-xhttp-server, mode: stream-up}}
  - {name: packet-up, type: vless, listen: "0.0.0.0", port: 8460, users: [{username: synthetic, uuid: "$uuid"}], certificate: /data/cert/cert.pem, private-key: /data/cert/key.pem, xhttp-config: {path: /xhttp, host: c3-xhttp-server, mode: packet-up}}
rules: ["MATCH,DIRECT"]
YAML
kubectl -n "$namespace" create secret generic c3-xhttp-server --from-file=config.yaml="$cert_dir/c3-xhttp.yaml"
kubectl -n "$namespace" apply -f - <<YAML
apiVersion: apps/v1
kind: Deployment
metadata: {name: c3-xhttp-server}
spec:
  replicas: 1
  selector: {matchLabels: {app: c3-xhttp-server}}
  template:
    metadata: {labels: {app: c3-xhttp-server}}
    spec:
      containers:
        - name: mihomo
          image: $image
          imagePullPolicy: Never
          command: [/usr/local/libexec/egressfox/mihomo, -f, /config/config.yaml, -d, /data]
          volumeMounts: [{name: config, mountPath: /config, readOnly: true}, {name: cert, mountPath: /data/cert, readOnly: true}, {name: data, mountPath: /data}]
      securityContext: {fsGroup: 65532}
      volumes: [{name: config, secret: {secretName: c3-xhttp-server}}, {name: cert, secret: {secretName: proxy-cert}}, {name: data, emptyDir: {}}]
---
apiVersion: v1
kind: Service
metadata: {name: c3-xhttp-server}
spec:
  selector: {app: c3-xhttp-server}
  ports: [{name: stream-one, port: 8458, targetPort: 8458}, {name: stream-up, port: 8459, targetPort: 8459}, {name: packet-up, port: 8460, targetPort: 8460}]
YAML
kubectl -n "$namespace" rollout status deployment/c3-xhttp-server --timeout=2m
c3_xhttp_ip=$(kubectl -n "$namespace" get service c3-xhttp-server -o jsonpath='{.spec.clusterIP}')
for variant in reality upgrade h2 grpc ws utls xhttp-stream-one xhttp-stream-up xhttp-packet-up; do
  case "$variant" in
    reality) nodes="vless://${uuid}@${c3_proxy_ip}:8451?encryption=none&type=tcp&security=reality&sni=front.example.com&pbk=${reality_public}&sid=a1b2&fp=chrome&flow=xtls-rprx-vision"; allow_insecure=false; engines="singbox mihomo" ;;
    upgrade) nodes="vless://${uuid}@${c3_proxy_ip}:8453?encryption=none&type=httpupgrade&path=%2Fupgrade&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines="singbox mihomo" ;;
    h2) nodes="vless://${uuid}@${c3_transport_ip}:8454?encryption=none&type=h2&path=%2Fh2&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines="singbox mihomo" ;;
    grpc) nodes="vless://${uuid}@${c3_transport_ip}:8455?encryption=none&type=grpc&serviceName=EgressFox&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines="singbox mihomo" ;;
    ws) nodes="vless://${uuid}@${c3_transport_ip}:8456?encryption=none&type=ws&path=%2Fws&host=front.example.com&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines="singbox mihomo" ;;
    utls) nodes="vless://${uuid}@${c3_transport_ip}:8457?encryption=none&type=tcp&fp=qq&alpn=http%2F1.1&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines="singbox mihomo" ;;
    xhttp-stream-one) nodes="vless://${uuid}@${c3_xhttp_ip}:8458?encryption=none&type=xhttp&path=%2Fxhttp&host=c3-xhttp-server&mode=stream-one&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines=mihomo ;;
    xhttp-stream-up) nodes="vless://${uuid}@${c3_xhttp_ip}:8459?encryption=none&type=xhttp&path=%2Fxhttp&host=c3-xhttp-server&mode=stream-up&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines=mihomo ;;
    xhttp-packet-up) nodes="vless://${uuid}@${c3_xhttp_ip}:8460?encryption=none&type=xhttp&path=%2Fxhttp&host=c3-xhttp-server&mode=packet-up&security=tls&sni=proxy-server&allowInsecure=1"; allow_insecure=true; engines=mihomo ;;
  esac
  kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c "cat >/data/c3-${variant}-body" <<EOF
$nodes
EOF
  kubectl -n "$namespace" create secret generic "c3-${variant}-source-url" --from-literal=url="http://source-provider.${namespace}.svc.cluster.local:8080/c3-${variant}"
  kubectl -n "$namespace" apply -f - <<YAML
apiVersion: egressfox.io/v1alpha1
kind: ProxyPool
metadata: {name: c3-${variant}}
spec:
  sources:
    - id: controlled
      http:
        urlSecretRef: {name: c3-${variant}-source-url, key: url}
        allowHTTP: true
        allowPrivateNetworks: true
      format: URIList
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
metadata: {name: c3-${variant}-mihomo}
spec:
  poolRef: {name: c3-${variant}}
  engine: Mihomo
  runtime: {managed: {}}
YAML
  if [ "$engines" = "singbox mihomo" ]; then
    kubectl -n "$namespace" apply -f - <<YAML
apiVersion: egressfox.io/v1alpha1
kind: EgressGateway
metadata: {name: c3-${variant}-singbox}
spec:
  poolRef: {name: c3-${variant}}
  engine: SingBox
  runtime: {managed: {}}
YAML
  fi
  kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "proxypool/c3-${variant}" --timeout=3m
  test "$(kubectl -n "$namespace" get proxypool "c3-${variant}" -o jsonpath='{.status.acceptedEndpoints}')" = 1
  for engine in $engines; do
    gateway="c3-${variant}-${engine}"
    kubectl -n "$namespace" wait --for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True "egressgateway/${gateway}" --timeout=5m
    service=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.serviceName}')
    auth=$(kubectl -n "$namespace" get egressgateway "$gateway" -o jsonpath='{.status.clientAuthSecretName}')
    username=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "username" | base64decode}}')
    password=$(kubectl -n "$namespace" get secret "$auth" -o go-template='{{index .data "password" | base64decode}}')
    test "$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --max-time 10 --proxy "socks5://${username}:${password}@${service}:1080" -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")" = 200
  done
  for engine in $engines; do
    kubectl -n "$namespace" delete egressgateway "c3-${variant}-${engine}" --wait=true
  done
  kubectl -n "$namespace" delete proxypool "c3-${variant}" --wait=true
done
