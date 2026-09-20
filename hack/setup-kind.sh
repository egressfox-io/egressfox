#!/bin/sh
set -eu

version=0.33.0
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac
case "$os-$arch" in
  darwin-amd64) checksum=5a99f26f57246dc9319dd294803313197a0f34d33c525b3ea8b655db5916ece0 ;;
  darwin-arm64) checksum=0c8c7dbe5e23594a198b786c4bc13dacc101fa6196b0cb0b23a1ca44e61f4b4f ;;
  linux-amd64) checksum=aee6151561422756b764a4ae28e7f44cda5af5a9eead3cc9985112b1de8d8e0d ;;
  linux-arm64) checksum=20022bee6cfcd5086cb7234d218e3454e6090022f2a8f55d1fa7fcf42c3867a2 ;;
  *) echo "unsupported kind platform" >&2; exit 1 ;;
esac
binary=${KIND_BINARY:-.cache/kind/$version/kind}
if [ ! -x "$binary" ]; then
  mkdir -p "$(dirname "$binary")"
  curl -fsSL -o "$binary" "https://github.com/kubernetes-sigs/kind/releases/download/v${version}/kind-${os}-${arch}"
  actual=$(shasum -a 256 "$binary" | awk '{print $1}')
  if [ "$actual" != "$checksum" ]; then echo "kind checksum mismatch" >&2; exit 1; fi
  chmod 0755 "$binary"
fi
printf '%s\n' "$(cd "$(dirname "$binary")" && pwd)/$(basename "$binary")"
