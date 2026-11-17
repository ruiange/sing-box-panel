#!/usr/bin/env bash
set -euo pipefail

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

arch=${1:-amd64}
if [[ $# -gt 1 || ( "$arch" != amd64 && "$arch" != arm64 ) ]]; then
    fail '用法：bash deploy/package.sh [amd64|arm64]'
fi
panel_version=${PANEL_VERSION:-dev}
if [[ $panel_version != dev && ! $panel_version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    fail 'PANEL_VERSION 格式须为 v主版本.次版本.修订号，例如 v0.1.3。'
fi

project=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
go_command=$(command -v "${GO:-go}") || fail '需要 Go 1.26.0 或更新版本，请将 go 加入 PATH，或通过 GO 指定可执行文件。'
if [[ "$go_command" != /* ]]; then
    go_command=$(cd -- "$(dirname -- "$go_command")" && pwd)/$(basename -- "$go_command")
fi
if command -v sha256sum >/dev/null; then
    checksum_command=(sha256sum)
elif command -v shasum >/dev/null; then
    checksum_command=(shasum -a 256)
else
    fail '需要 sha256sum 或 shasum 来生成安装包校验文件。'
fi

web_dist="$project/web/dist"
[[ -f "$web_dist/index.html" && -d "$web_dist/assets" ]] || fail '找不到前端构建产物，请先执行 pnpm --dir web build。'
for script in install.sh upgrade.sh repair-node-control.sh; do
    [[ -f "$project/deploy/$script" ]] || fail "找不到 deploy/${script}。"
done

core_module='github.com/sagernet/sing-box'
core_version='v1.14.4'
core_tags='with_utls,with_v2ray_api,with_quic,with_grpc'
core_checksum='h1:KV6J8v1zMUu7BZA93nD9HGMmQnNC46VTuYaKEHkP+gA='

output_dir="$project/bin"
mkdir -p -- "$output_dir"
stage=$(mktemp -d "$output_dir/.package.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT
bundle="$stage/bundle"
mkdir -p -- "$bundle/bin" "$bundle/web" "$bundle/deploy" "$bundle/licenses"

printf '构建 Linux %s 面板 %s…\n' "$arch" "$panel_version"
(
    cd -- "$project"
    GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="$arch" "$go_command" build \
        -mod=readonly -trimpath -buildvcs=false -ldflags "-s -w -X main.version=$panel_version" \
        -o "$bundle/bin/sing-box-panel" ./cmd/panel
)

printf '校验并构建 sing-box %s…\n' "$core_version"
core_source=$(
    cd -- /
    download=$(GOWORK=off "$go_command" mod download -json "$core_module@$core_version")
    checksum=$(awk -F '"' '$2 == "Sum" {print $4}' <<< "$download")
    [[ "$checksum" == "$core_checksum" ]] || fail "sing-box $core_version 源码校验失败。"
    module_cache=$(GOWORK=off "$go_command" env GOMODCACHE)
    printf '%s/%s@%s\n' "$module_cache" "$core_module" "$core_version"
)
(
    cd -- "$core_source"
    GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH="$arch" "$go_command" build \
        -mod=readonly -trimpath -buildvcs=false -tags "$core_tags" \
        -ldflags "-s -w -X $core_module/constant.Version=${core_version#v}" \
        -o "$bundle/bin/sing-box-panel-core" ./cmd/sing-box
)

for script in install.sh upgrade.sh repair-node-control.sh; do
    install -m 0755 "$project/deploy/$script" "$bundle/$script"
done
install -m 0644 "$web_dist/index.html" "$bundle/web/index.html"
cp -R -- "$web_dist/assets" "$bundle/web/assets"
if [[ -f "$web_dist/favicon.svg" ]]; then
    install -m 0644 "$web_dist/favicon.svg" "$bundle/web/favicon.svg"
fi
for name in sing-box-panel.service sing-box-panel-node.service 49-sing-box-panel.rules check-node-restart.sh; do
    install -m 0644 "$project/deploy/$name" "$bundle/deploy/$name"
done
install -m 0644 "$core_source/LICENSE" "$bundle/licenses/sing-box-LICENSE"
cat > "$bundle/licenses/sing-box-SOURCE.txt" <<EOF
Upstream source: https://github.com/SagerNet/sing-box/tree/$core_version
Go module: $core_module@$core_version
Module checksum: $core_checksum
Build environment: GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$arch
Build command: go build -mod=readonly -trimpath -buildvcs=false -tags $core_tags -ldflags "-s -w -X $core_module/constant.Version=${core_version#v}" ./cmd/sing-box
EOF

archive="sing-box-panel-linux-$arch.tar.gz"
COPYFILE_DISABLE=1 tar --format ustar -czf "$stage/$archive" -C "$bundle" install.sh upgrade.sh repair-node-control.sh bin web deploy licenses
(
    cd -- "$stage"
    "${checksum_command[@]}" "$archive" > "$archive.sha256"
)
mv -f -- "$stage/$archive" "$output_dir/$archive"
mv -f -- "$stage/$archive.sha256" "$output_dir/$archive.sha256"
printf '安装包：%s/%s\n' "$output_dir" "$archive"
printf 'SHA-256 校验文件：%s/%s.sha256\n' "$output_dir" "$archive"
