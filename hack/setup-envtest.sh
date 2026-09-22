#!/bin/sh
set -eu

go_command=${GO:-go}
minor=${K8S_VERSION:-$($go_command run ./tools/releasectl kubernetes --field minimum-supported)}
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported envtest architecture: $arch" >&2; exit 1 ;;
esac
platform=$os/$arch
version=$($go_command run ./tools/releasectl kubernetes --version "$minor" --field envtest-version)
checksum=$($go_command run ./tools/releasectl kubernetes --version "$minor" --platform "$platform" --field envtest-sha512)
url=$($go_command run ./tools/releasectl kubernetes --version "$minor" --platform "$platform" --field envtest-url)

root=${ENVTEST_CACHE:-.cache/envtest/$version/$os-$arch}
assets=$root/controller-tools/envtest
if [ ! -x "$assets/kube-apiserver" ]; then
  mkdir -p "$root"
  archive=$root/envtest.tar.gz
  curl -fsSL -o "$archive" "$url"
  actual=$(shasum -a 512 "$archive" | awk '{print $1}')
  if [ "$actual" != "$checksum" ]; then
    echo "envtest checksum mismatch for Kubernetes $minor" >&2
    rm -f "$archive"
    exit 1
  fi
  tar -xzf "$archive" -C "$root"
  rm -f "$archive"
fi
"$assets/kube-apiserver" --version | grep -F "v$version" >/dev/null || {
  echo "envtest kube-apiserver version does not match pinned Kubernetes $version" >&2
  exit 1
}
printf '%s\n' "$(cd "$assets" && pwd)"
