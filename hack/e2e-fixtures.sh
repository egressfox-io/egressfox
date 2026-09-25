# Shared namespace-local RBAC, proxy, target and BYO traffic fixture.
sa="system:serviceaccount:${namespace}:${operator_deployment}"
test "$(kubectl auth can-i get secrets -n "$namespace" --as "$sa")" = yes
test "$(kubectl auth can-i get secrets -n default --as "$sa")" = no
test "$(kubectl auth can-i delete secrets -n "$namespace" --as "$sa")" = yes
test "$(kubectl auth can-i create deployments -n "$namespace" --as "$sa")" = yes
test "$(kubectl auth can-i create deployments -n default --as "$sa")" = no
test "$(kubectl auth can-i create statefulsets -n "$namespace" --as "$sa")" = no
test "$(kubectl auth can-i get nodes --as "$sa")" = no

uuid=11111111-1111-4111-8111-111111111111
cert_dir=$(mktemp -d)
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$cert_dir/key.pem" -out "$cert_dir/cert.pem" -subj /CN=proxy-server -days 1 >/dev/null 2>&1
kubectl -n "$namespace" create secret generic proxy-cert --from-file=cert.pem="$cert_dir/cert.pem" --from-file=key.pem="$cert_dir/key.pem"
kubectl -n "$namespace" create secret generic proxy-server --from-literal=config.json="{\"log\":{\"level\":\"warn\"},\"inbounds\":[{\"type\":\"vless\",\"tag\":\"in\",\"listen\":\"::\",\"listen_port\":8443,\"users\":[{\"uuid\":\"$uuid\"}]},{\"type\":\"vmess\",\"tag\":\"vmess\",\"listen\":\"::\",\"listen_port\":8445,\"users\":[{\"name\":\"synthetic\",\"uuid\":\"$uuid\",\"alterId\":0}]},{\"type\":\"shadowsocks\",\"tag\":\"shadowsocks\",\"listen\":\"::\",\"listen_port\":8446,\"network\":\"tcp\",\"method\":\"aes-128-gcm\",\"password\":\"synthetic-password\"},{\"type\":\"socks\",\"tag\":\"socks-anon\",\"listen\":\"::\",\"listen_port\":8447},{\"type\":\"socks\",\"tag\":\"socks-auth\",\"listen\":\"::\",\"listen_port\":8448,\"users\":[{\"username\":\"synthetic-user\",\"password\":\"synthetic-password\"}]},{\"type\":\"http\",\"tag\":\"http-auth\",\"listen\":\"::\",\"listen_port\":8449,\"users\":[{\"username\":\"synthetic-user\",\"password\":\"synthetic-password\"}]},{\"type\":\"http\",\"tag\":\"https-auth\",\"listen\":\"::\",\"listen_port\":8450,\"users\":[{\"username\":\"synthetic-user\",\"password\":\"synthetic-password\"}],\"tls\":{\"enabled\":true,\"certificate_path\":\"/cert/cert.pem\",\"key_path\":\"/cert/key.pem\"}}],\"outbounds\":[{\"type\":\"direct\",\"tag\":\"direct\"}]}"
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
          volumeMounts: [{name: config, mountPath: /config, readOnly: true}, {name: cert, mountPath: /cert, readOnly: true}]
      volumes: [{name: config, secret: {secretName: proxy-server}}, {name: cert, secret: {secretName: proxy-cert}}]
---
apiVersion: v1
kind: Service
metadata: {name: proxy-server}
spec:
  selector: {app: proxy-server}
  ports: [{name: primary, port: 8443, targetPort: 8443}, {name: rollout, port: 8444, targetPort: 8443}, {name: vmess, port: 8445, targetPort: 8445}, {name: shadowsocks, port: 8446, targetPort: 8446}, {name: socks-anon, port: 8447, targetPort: 8447}, {name: socks-auth, port: 8448, targetPort: 8448}, {name: http-auth, port: 8449, targetPort: 8449}, {name: https-auth, port: 8450, targetPort: 8450}]
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
      command: [sleep, "3600"]
  volumes: [{name: config, secret: {secretName: e2e-engine-config}}]
YAML
kubectl -n "$namespace" wait --for=condition=Ready pod/byo-engine --timeout=2m
code=$(kubectl -n "$namespace" exec byo-engine -c client -- curl -sS --socks5 127.0.0.1:1080 -o /dev/null -w '%{http_code}' "http://${target_ip}:8080/")
test "$code" = 200
