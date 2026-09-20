#!/bin/sh
set -eu

version=1.37.0
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
esac
case "$os-$arch" in
  darwin-amd64) checksum=76d90e9f2596194175ebc8e389a3bd8ed7416a51f397fc775f927892b4b89fa6 ;;
  darwin-arm64) checksum=23cb186d265b8c7d6cd9934f03731b4981e2c06f0fbaccc6d4803958d030fba0 ;;
  linux-amd64) checksum=f03faedfc68d2a78cee5cb02618bb44e7f67044f4c5fc957f72d3307fda61a9c ;;
  linux-arm64) checksum=ff2a0cdcadd0a6b3181faa4758e28fc63b774d2e40f29f62645dd7fa0b881a2c ;;
  *) echo "unsupported envtest platform" >&2; exit 1 ;;
esac

root=${ENVTEST_CACHE:-.cache/envtest/$version/$os-$arch}
assets=$root/controller-tools/envtest
if [ ! -x "$assets/kube-apiserver" ]; then
  mkdir -p "$root"
  archive=$root/envtest.tar.gz
  curl -fsSL -o "$archive" "https://github.com/kubernetes-sigs/controller-tools/releases/download/envtest-v${version}/envtest-v${version}-${os}-${arch}.tar.gz"
  actual=$(shasum -a 256 "$archive" | awk '{print $1}')
  if [ "$actual" != "$checksum" ]; then echo "envtest checksum mismatch" >&2; exit 1; fi
  tar -xzf "$archive" -C "$root"
  rm "$archive"
fi
printf '%s\n' "$(cd "$assets" && pwd)"
