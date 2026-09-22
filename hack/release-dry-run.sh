#!/bin/sh
set -eu

# Official release qualification is tag-bound and fail-closed: VERSION must be
# the release tag on HEAD and the working tree must contain no non-ignored
# changes. Ignored build/release output (dist/, .cache/, bin/) never counts.
# There is deliberately no dirty override; commit or stash first.
: "${VERSION:?VERSION must be an explicit vX.Y.Z[-dev.N|-alpha.N|-beta.N] release tag}"
version=$VERSION
revision=${REVISION:-$(git rev-parse HEAD)}
created=${CREATED:-$(git show -s --format=%cI "$revision")}
source_date_epoch=${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct "$revision")}
go_command=${GO:-go}
container_cli=${DOCKER:-docker}

release_root=$(pwd -P)
artifact_dir=$release_root/dist/release
report_dir=$release_root/dist/reports
work_dir=$release_root/dist/release-work
tool_dir=$release_root/.cache/release-tools

normalized=$($go_command run ./tools/releasectl validate-version --version "$version" --revision "$revision" --created "$created")
$go_command run ./tools/releasectl validate --root .
$go_command run ./tools/releasectl build-version --root . --version "$normalized" --require-clean >/dev/null

rm -rf "$artifact_dir" "$report_dir" "$work_dir"
mkdir -p "$artifact_dir" "$report_dir" "$work_dir" "$tool_dir"

host_os=$(uname -s | tr '[:upper:]' '[:lower:]')
host_arch=$(uname -m)
case "$host_arch" in
  x86_64) host_arch=amd64 ;;
  aarch64|arm64) host_arch=arm64 ;;
  *) echo "unsupported release-tool architecture: $host_arch" >&2; exit 1 ;;
esac
host_platform=$host_os/$host_arch

install_tool() {
  tool=$1
  tool_version=$($go_command run ./tools/releasectl metadata --tool "$tool" --field version)
  tool_path=$tool_dir/$tool-$tool_version-$host_os-$host_arch
  if [ ! -x "$tool_path" ]; then
    $go_command run ./tools/releasectl install-tool --tool "$tool" --platform "$host_platform" --output "$tool_path"
  fi
  printf '%s\n' "$tool_path"
}

syft=$(install_tool syft)
grype=$(install_tool grype)
cosign=$(install_tool cosign)
helm=$(install_tool helm)

"$syft" version >/dev/null
"$grype" version >/dev/null
"$cosign" version >/dev/null
"$helm" version --short >/dev/null

ldflags="-s -w -X github.com/egressfox-io/egressfox/internal/buildinfo.version=$normalized -X github.com/egressfox-io/egressfox/internal/buildinfo.revision=$revision -X github.com/egressfox-io/egressfox/internal/buildinfo.created=$created"
for arch in amd64 arm64; do
  binary=$artifact_dir/egressfox-operator-$normalized-linux-$arch
  comparison=$work_dir/egressfox-operator-$arch.rebuild
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch SOURCE_DATE_EPOCH=$source_date_epoch \
    $go_command build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$binary" ./cmd/operator
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch SOURCE_DATE_EPOCH=$source_date_epoch \
    $go_command build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$comparison" ./cmd/operator
  cmp "$binary" "$comparison"
  $go_command version -m "$binary" >"$report_dir/egressfox-operator-$arch.buildinfo.txt"
  "$syft" scan "file:$binary" -o "spdx-json=$artifact_dir/egressfox-operator-$normalized-linux-$arch.spdx.json"
done

for engine in mihomo sing-box; do
  source_dir=$work_dir/source-$engine
  $go_command run ./tools/releasectl prepare-engine-source --engine "$engine" --output-dir "$source_dir"
  for dependency in $($go_command run ./tools/releasectl overrides --engine "$engine"); do
    (cd "$source_dir" && $go_command mod edit -require="$dependency")
  done
  (cd "$source_dir" && $go_command mod tidy)
  build_package=$($go_command run ./tools/releasectl engine-build --engine "$engine" --field package)
  build_tags=$($go_command run ./tools/releasectl engine-build --engine "$engine" --field tags)
  if [ "${RELEASE_SKIP_VULN:-0}" != 1 ]; then
    govulncheck=$tool_dir/govulncheck-1.8.0
    if [ ! -x "$govulncheck" ]; then
      GOBIN=$tool_dir $go_command install golang.org/x/vuln/cmd/govulncheck@v1.8.0
      mv "$tool_dir/govulncheck" "$govulncheck"
    fi
    if [ -n "$build_tags" ]; then
      (cd "$source_dir" && "$govulncheck" -tags "$build_tags" "$build_package") >"$report_dir/$engine.govulncheck.txt"
    else
      (cd "$source_dir" && "$govulncheck" "$build_package") >"$report_dir/$engine.govulncheck.txt"
    fi
  fi
  for arch in amd64 arm64; do
    binary=$work_dir/$engine-linux-$arch
    comparison=$work_dir/$engine-linux-$arch.rebuild
    if [ "$engine" = mihomo ]; then
      engine_ldflags="-s -w -buildid= -X github.com/metacubex/mihomo/constant.Version=1.19.31"
    else
      engine_ldflags="-s -w -buildid= -X github.com/sagernet/sing-box/constant.Version=1.14.1"
    fi
    if [ -n "$build_tags" ]; then
      (cd "$source_dir" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch SOURCE_DATE_EPOCH=$source_date_epoch \
        $go_command build -trimpath -buildvcs=false -tags "$build_tags" -ldflags "$engine_ldflags" -o "$binary" "$build_package")
      (cd "$source_dir" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch SOURCE_DATE_EPOCH=$source_date_epoch \
        $go_command build -trimpath -buildvcs=false -tags "$build_tags" -ldflags "$engine_ldflags" -o "$comparison" "$build_package")
    else
      (cd "$source_dir" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch SOURCE_DATE_EPOCH=$source_date_epoch \
        $go_command build -trimpath -buildvcs=false -ldflags "$engine_ldflags" -o "$binary" "$build_package")
      (cd "$source_dir" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch SOURCE_DATE_EPOCH=$source_date_epoch \
        $go_command build -trimpath -buildvcs=false -ldflags "$engine_ldflags" -o "$comparison" "$build_package")
    fi
    cmp "$binary" "$comparison"
    "$syft" scan "file:$binary" -o "spdx-json=$artifact_dir/$engine-linux-$arch.spdx.json"
  done
  build_revision=$($go_command run ./tools/releasectl engine-build --engine "$engine" --field revision)
  engine_version=$($go_command run ./tools/releasectl engine-build --engine "$engine" --field version)
  tar -czf "$artifact_dir/$engine-$engine_version-egressfox.$build_revision-source.tar.gz" -C "$source_dir" .
done

$go_command run ./tools/releasectl fetch-licenses --output-dir "$artifact_dir"
cp "$artifact_dir/mihomo-LICENSE" "$artifact_dir/GPL-3.0.txt"
cp LICENSE "$artifact_dir/EGRESSFOX-LICENSE"
cp THIRD_PARTY_NOTICES.md "$artifact_dir/THIRD_PARTY_NOTICES.md"

"$helm" dependency build charts/egressfox >/dev/null
"$helm" package charts/egressfox --version "$normalized" --app-version "$normalized" --destination "$artifact_dir" >/dev/null
chart=$artifact_dir/egressfox-$normalized.tgz
test -f "$chart"
test "$("$helm" show chart "$chart" | awk '/^version:/ {print $2}')" = "$normalized"
test "$("$helm" show chart "$chart" | awk '/^appVersion:/ {gsub(/\"/, "", $2); print $2}')" = "$normalized"
for kubernetes_minor in $($go_command run ./tools/releasectl kubernetes --field release-validation); do
  "$helm" template egressfox "$chart" --namespace egressfox-system --kube-version "$kubernetes_minor.0" \
    --set-string image.repository=ghcr.io/egressfox-io/egressfox >"$work_dir/chart-$kubernetes_minor.yaml"
  grep -F "image: \"ghcr.io/egressfox-io/egressfox:$normalized\"" "$work_dir/chart-$kubernetes_minor.yaml" >/dev/null
done
"$helm" template egressfox "$chart" --namespace egressfox-system --kube-version 1.32.0 \
  --set-string image.repository=ghcr.io/egressfox-io/egressfox \
  --set-string image.digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa >"$work_dir/chart-digest.yaml"
grep -F 'image: "ghcr.io/egressfox-io/egressfox@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"' "$work_dir/chart-digest.yaml" >/dev/null

if [ "${RELEASE_SKIP_VULN:-0}" != 1 ]; then
  grype_db=$release_root/.cache/grype-db
  mkdir -p "$grype_db"
  GRYPE_DB_CACHE_DIR=$grype_db "$grype" db update
  for sbom in "$artifact_dir"/*.spdx.json; do
    report=$report_dir/$(basename "$sbom" .spdx.json).grype.json
    GRYPE_DB_CACHE_DIR=$grype_db GRYPE_DB_AUTO_UPDATE=false "$grype" "sbom:$sbom" --fail-on high --output json --file "$report"
  done
else
  printf '%s\n' 'vulnerability scans skipped by RELEASE_SKIP_VULN=1' >"$report_dir/VULNERABILITY-SCAN-SKIPPED"
fi

if [ "${RELEASE_SKIP_IMAGE:-0}" != 1 ]; then
  image_archive=$work_dir/egressfox-$normalized.oci.tar
  if [ "$(basename "$container_cli")" = podman ]; then
    manifest_name=localhost/egressfox-release-dry-run:$normalized
    "$container_cli" manifest rm "$manifest_name" >/dev/null 2>&1 || true
    "$container_cli" manifest create "$manifest_name" >/dev/null
    for arch in amd64 arm64; do
      platform_image=localhost/egressfox-release-dry-run-$arch:$normalized
      "$container_cli" build --platform linux/$arch --tag "$platform_image" \
        --build-arg "VERSION=$normalized" --build-arg "REVISION=$revision" --build-arg "CREATED=$created" \
        --build-arg "SOURCE_DATE_EPOCH=$source_date_epoch" .
      "$container_cli" manifest add "$manifest_name" "$platform_image" >/dev/null
    done
    "$container_cli" manifest push --all "$manifest_name" "oci-archive:$image_archive"
    "$container_cli" manifest rm "$manifest_name" >/dev/null
  else
    "$container_cli" buildx build --platform linux/amd64,linux/arm64 --output "type=oci,dest=$image_archive" \
      --build-arg "VERSION=$normalized" --build-arg "REVISION=$revision" --build-arg "CREATED=$created" \
      --build-arg "SOURCE_DATE_EPOCH=$source_date_epoch" .
  fi
  "$syft" scan "oci-archive:$image_archive" -o "spdx-json=$artifact_dir/egressfox-image-$normalized.spdx.json"
  if [ "${RELEASE_SKIP_VULN:-0}" != 1 ]; then
    GRYPE_DB_CACHE_DIR=$release_root/.cache/grype-db GRYPE_DB_AUTO_UPDATE=false "$grype" \
      "sbom:$artifact_dir/egressfox-image-$normalized.spdx.json" --fail-on high --output json \
      --file "$report_dir/egressfox-image-$normalized.grype.json"
  fi
else
  printf '%s\n' 'image construction skipped by RELEASE_SKIP_IMAGE=1' >"$report_dir/IMAGE-BUILD-SKIPPED"
fi

(
  cd "$artifact_dir"
  find . -type f ! -name SHA256SUMS -print | LC_ALL=C sort | xargs shasum -a 256 >SHA256SUMS
  shasum -a 256 -c SHA256SUMS
)

printf 'release dry run complete: version=%s revision=%s artifacts=%s\n' "$normalized" "$revision" "$artifact_dir"
