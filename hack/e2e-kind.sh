#!/bin/sh
set -eu

kind=$(./hack/setup-kind.sh)
cluster=${KIND_CLUSTER:-egressfox-e2e}
namespace=egressfox-e2e
image=${EGRESSFOX_E2E_IMAGE:-egressfox:e2e}
container_cli=${CONTAINER_CLI:-docker}
if [ "${KIND_EXPERIMENTAL_PROVIDER:-}" = podman ] && [ "${image#*/}" = "$image" ]; then
  image="localhost/$image"
fi
image_repository=${image%:*}
image_tag=${image##*:}
node_image=kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
image_archive=""
cleanup() {
	status=$?
	if [ "${KEEP_KIND_CLUSTER_ON_FAILURE:-0}" = 1 ] && [ "$status" -ne 0 ]; then
		echo "keeping failed kind cluster $cluster for diagnostics" >&2
		return
	fi
  "$kind" delete cluster --name "$cluster" >/dev/null 2>&1 || true
  if [ -n "$image_archive" ]; then rm -f "$image_archive"; fi
}
trap cleanup EXIT INT TERM

cleanup
"$container_cli" build -t "$image" .
"$kind" create cluster --name "$cluster" --image "$node_image" --wait 120s
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
test "$(kubectl auth can-i delete secrets -n "$namespace" --as "$sa")" = no
test "$(kubectl auth can-i create deployments -n "$namespace" --as "$sa")" = no

uuid=11111111-1111-4111-8111-111111111111
kubectl -n "$namespace" create secret generic proxy-server --from-literal=config.json="{\"log\":{\"level\":\"warn\"},\"inbounds\":[{\"type\":\"vless\",\"tag\":\"in\",\"listen\":\"::\",\"listen_port\":8443,\"users\":[{\"uuid\":\"$uuid\"}]}],\"outbounds\":[{\"type\":\"direct\",\"tag\":\"direct\"}]}"
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
  ports: [{port: 8443, targetPort: 8443}]
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

helm upgrade egressfox charts/egressfox --namespace "$namespace" --set-string image.repository="$image_repository" --set-string image.tag="$image_tag" --set image.pullPolicy=Never --wait --timeout 3m
helm uninstall egressfox --namespace "$namespace"
kubectl get crd proxypools.egressfox.io egressgateways.egressfox.io >/dev/null
