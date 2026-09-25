# Shared namespace-local controlled HTTP subscription provider.
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
      'GET /c3-reality '*) body_file=/data/c3-reality-body; etag=c3-reality-v1 ;;
      'GET /c3-upgrade '*) body_file=/data/c3-upgrade-body; etag=c3-upgrade-v1 ;;
      'GET /c3-h2 '*) body_file=/data/c3-h2-body; etag=c3-h2-v1 ;;
      'GET /c3-grpc '*) body_file=/data/c3-grpc-body; etag=c3-grpc-v1 ;;
      'GET /c3-ws '*) body_file=/data/c3-ws-body; etag=c3-ws-v1 ;;
      'GET /c3-utls '*) body_file=/data/c3-utls-body; etag=c3-utls-v1 ;;
      'GET /c3-xhttp-stream-one '*) body_file=/data/c3-xhttp-stream-one-body; etag=c3-xhttp-stream-one-v1 ;;
      'GET /c3-xhttp-stream-up '*) body_file=/data/c3-xhttp-stream-up-body; etag=c3-xhttp-stream-up-v1 ;;
      'GET /c3-xhttp-packet-up '*) body_file=/data/c3-xhttp-packet-up-body; etag=c3-xhttp-packet-up-v1 ;;
      'GET /c4-hysteria2 '*) body_file=/data/c4-hysteria2-body; etag=c4-hysteria2-v1 ;;
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
