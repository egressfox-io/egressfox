#!/bin/sh
set -eu

parallelism=${EGRESSFOX_E2E_PARALLELISM-4}
case "$parallelism" in
  1|2|3|4|5) ;;
  *) echo "EGRESSFOX_E2E_PARALLELISM must be an integer from 1 to 5 (got '$parallelism')" >&2; exit 2 ;;
esac

kind=$(./hack/setup-kind.sh)
go_command=${GO:-go}
kubernetes_minor=${K8S_VERSION:-$($go_command run ./tools/releasectl kubernetes --field minimum-supported)}
cluster=${KIND_CLUSTER:-egressfox-e2e}
image=${EGRESSFOX_E2E_IMAGE:-egressfox:e2e}
container_cli=${CONTAINER_CLI:-docker}
if [ "${KIND_EXPERIMENTAL_PROVIDER:-}" = podman ] && [ "${image#*/}" = "$image" ]; then
  image="localhost/$image"
fi
node_image=$($go_command run ./tools/releasectl kubernetes --version "$kubernetes_minor" --field kind-node-image)
image_archive=""
log_dir=${EGRESSFOX_E2E_LOG_DIR:-dist/e2e-logs/$(date +%Y%m%d-%H%M%S)-$$}
suite_started=$(date +%s)
run_id=$(date +%s)-$$
jobs='lifecycle http-refresh protocols advanced-transports hysteria2'
started_jobs=""
all_pids=""
failed=0
on_signal() {
  trap '' INT TERM
  for pid in $all_pids; do kill "$pid" 2>/dev/null || true; done
  for pid in $all_pids; do wait "$pid" 2>/dev/null || true; done
  exit 130
}
cleanup() {
  status=$?
  if [ -d "$log_dir" ]; then
    echo "$(( $(date +%s) - suite_started ))" >"$log_dir/suite.seconds"
  fi
  if [ "${KEEP_KIND_CLUSTER_ON_FAILURE:-0}" = 1 ] && [ "$status" -ne 0 ]; then
    echo "keeping failed kind cluster $cluster for diagnostics" >&2
  else
    "$kind" delete cluster --name "$cluster" >/dev/null 2>&1 || true
  fi
  if [ -n "$image_archive" ]; then rm -f "$image_archive"; fi
  return "$status"
}
trap cleanup EXIT
trap on_signal INT TERM

# The parent alone owns the cluster, its CRDs and its image.
"$kind" delete cluster --name "$cluster" >/dev/null 2>&1 || true
if [ "${EGRESSFOX_E2E_SKIP_BUILD:-0}" != 1 ]; then
  "$container_cli" build -t "$image" .
fi
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
kubectl apply -f charts/egressfox/crds/ >/dev/null
kubectl wait --for=condition=Established crd/proxypools.egressfox.io crd/egressgateways.egressfox.io --timeout=2m
mkdir -p "$log_dir"
echo "E2E logs: $log_dir; concurrency: $parallelism" >&2

run_job() {
  job=$1
  namespace="egressfox-e2e-${run_id}-${job}"
  release="e2e-${job}"
  echo "$namespace" >"$log_dir/$job.namespace"
  echo "$release" >"$log_dir/$job.release"
  started=$(date +%s)
  result=0
  sh "./hack/e2e-${job}.sh" "$namespace" "$release" "$image" >"$log_dir/$job.log" 2>&1 || result=$?
  echo "$(( $(date +%s) - started ))" >"$log_dir/$job.seconds"
  if [ "$result" -ne 0 ]; then
    echo "scenario $job failed; collecting namespace diagnostics" >&2
    kubectl -n "$namespace" get pods,deployments,services,proxypools,egressgateways -o wide >"$log_dir/$job.resources.log" 2>&1 || true
    kubectl -n "$namespace" describe pods >"$log_dir/$job.pods.log" 2>&1 || true
    kubectl -n "$namespace" logs "deployment/${release}-egressfox" --all-containers --tail=500 >"$log_dir/$job.operator.log" 2>&1 || true
  fi
  if [ "$result" -eq 0 ] || [ "${KEEP_KIND_CLUSTER_ON_FAILURE:-0}" != 1 ]; then
    # The lifecycle scenario already uninstalls its release as an assertion.
    if helm status "$release" --namespace "$namespace" >/dev/null 2>&1; then
      helm uninstall "$release" --namespace "$namespace" >"$log_dir/$job.cleanup.log" 2>&1 || result=1
    fi
    kubectl delete namespace "$namespace" --wait=true --timeout=3m >>"$log_dir/$job.cleanup.log" 2>&1 || result=1
  fi
  return "$result"
}

# FIFO waiting bounds active namespace owners without creating extra clusters.
active=0
for job in $jobs; do
  if [ "$active" -ge "$parallelism" ]; then
    set -- $started_jobs
    oldest=$1
    shift
    started_jobs="$*"
    if ! wait "$oldest"; then failed=1; fi
    active=$((active - 1))
  fi
  run_job "$job" &
  started_jobs="$started_jobs $!"
  all_pids="$all_pids $!"
  active=$((active + 1))
done
for pid in $started_jobs; do
  if ! wait "$pid"; then failed=1; fi
done

if [ "$failed" -ne 0 ]; then
  echo "E2E scenarios failed; inspect $log_dir" >&2
  exit 1
fi
echo "E2E scenarios passed; elapsed seconds by scenario in $log_dir" >&2
