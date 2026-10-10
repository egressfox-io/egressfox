# Verified writes to the controlled E2E subscription provider.
# kubectl exec may finish successfully even when streamed stdin was lost.
provider_write() {
  provider_destination=$1
  provider_body=$(cat)
  [ -n "$provider_body" ] || { echo 'empty provider fixture input' >&2; return 1; }
  provider_checksum=$(printf '%s\n' "$provider_body" | cksum | awk '{print $1, $2}')
  provider_temporary="${provider_destination}.upload"
  for provider_attempt in 1 2 3; do
    if printf '%s\n' "$provider_body" | kubectl -n "$namespace" exec -i "$provider_pod" -- sh -c 'cat >"$1"' sh "$provider_temporary"; then
      provider_actual=$(kubectl -n "$namespace" exec "$provider_pod" -- cksum "$provider_temporary" | awk '{print $1, $2}') || provider_actual=""
      if [ "$provider_actual" = "$provider_checksum" ]; then
        kubectl -n "$namespace" exec "$provider_pod" -- mv "$provider_temporary" "$provider_destination"
        return $?
      fi
    fi
    echo "provider fixture transfer failed verification (attempt $provider_attempt/3)" >&2
  done
  kubectl -n "$namespace" exec "$provider_pod" -- rm -f "$provider_temporary" >/dev/null 2>&1 || true
  echo 'provider fixture transfer failed after 3 attempts' >&2
  return 1
}
