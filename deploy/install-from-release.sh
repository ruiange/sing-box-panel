#!/usr/bin/env bash
set -euo pipefail
umask 077
operation=安装
total_steps=8

die() {
    printf '%s失败：%s\n' "$operation" "$*" >&2
    exit 1
}

step() {
    printf '[%s/%s] %s\n' "$1" "$total_steps" "$2" >&2
}

usage() {
    printf '%s\n' \
        '用法：sudo bash install-from-release.sh [--repo OWNER/REPO] [--version TAG] [公网IP [节点端口] [网页端口]]' \
        '      sudo bash install-from-release.sh --upgrade [--repo OWNER/REPO] [--version TAG]' \
        '从 GitHub Releases 下载安装包，安装 HTTP 面板和多协议节点。' \
        '--upgrade 原地升级已有面板，沿用服务器的配置和数据。' \
        '发布附件默认使用同一版本的安装包。未指定版本的源码脚本使用最新发布。' \
        '省略公网 IP 时自动检测，也可手动指定。' \
        '默认网页端口 9443，节点从 8443 起使用连续的 13 个端口。' \
        '在 Ubuntu 或 Debian 上缺少节点安装所需工具时，会通过 APT 补齐。'
}

download_with_wget() {
    local url=$1 destination=$2 location host hop
    local response_log="$destination.wget.log" progress=dot:mega
    if [[ -t 2 ]]; then
        progress=bar:force:noscroll
    fi
    # Wget's --https-only option governs recursive downloads. Follow each
    # release redirect explicitly so checksum and archive both stay on HTTPS.
    for ((hop = 0; hop <= 8; hop++)); do
        if LC_ALL=C wget --no-config --no-verbose --server-response --max-redirect=0 \
            --output-file="$response_log" --show-progress --progress="$progress" \
            --timeout=25 --tries=3 --output-document="$destination" -- "$url"; then
            return 0
        fi
        [[ -s $response_log ]] || return 1
        location=$(awk '
            /^[[:space:]]+HTTP\// { status=$2; location="" }
            status ~ /^30[12378]$/ && /^[[:space:]]/ && tolower($1) == "location:" {
                sub(/^[[:space:]]+[^:]+:[[:space:]]*/, "")
                sub(/\r$/, "")
                location=$0
            }
            END { print location }
        ' "$response_log")
        case "$location" in
            https://*) url=$location ;;
            //*) return 1 ;;
            /*)
                host=${url#https://}
                url="https://${host%%/*}$location"
                ;;
            *) return 1 ;;
        esac
    done
    return 1
}

download() {
    local url=$1 destination=$2
    if command -v curl >/dev/null; then
        curl --disable --fail --show-error --progress-bar --location --max-redirs 8 \
            --proto '=https' --proto-redir '=https' --connect-timeout 15 --retry 3 \
            --output "$destination" -- "$url"
    else
        download_with_wget "$url" "$destination"
    fi
}

route_source() {
    ip "-$1" route get "$2" 2>/dev/null | awk '
        { for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit } }
    '
}

query_public_ip() {
    local family=$1 url=$2
    if command -v curl >/dev/null; then
        curl --disable "-$family" --fail --silent --noproxy '*' --proto '=https' \
            --connect-timeout 3 --max-time 6 -- "$url"
    else
        timeout 7 wget --no-config "-$family" --no-proxy --quiet --max-redirect=0 \
            --timeout=5 --tries=1 --output-document=- -- "$url"
    fi
}

validated_address() {
    local panel=$1 address
    shift
    address=$("$@") || return 1
    [[ -n $address ]] && "$panel" validate-address "$address" 2>/dev/null
}

detect_public_ip() {
    local panel=$1
    validated_address "$panel" route_source 4 1.1.1.1 ||
        validated_address "$panel" route_source 6 2606:4700:4700::1111 ||
        validated_address "$panel" query_public_ip 4 https://api.ipify.org ||
        validated_address "$panel" query_public_ip 6 https://api6.ipify.org
}

repo=ruiange/sing-box-panel
# The release attachment pins both the archive and its checksum to its own tag.
bundled_version=
version=
while [[ $# -gt 0 ]]; do
    case "$1" in
        --repo)
            [[ $# -ge 2 && -n $2 ]] || die '--repo 需要 OWNER/REPO。'
            repo=$2
            shift 2
            ;;
        --version)
            [[ $# -ge 2 && -n $2 ]] || die '--version 需要发布标签。'
            version=$2
            shift 2
            ;;
        --upgrade)
            operation=升级
            total_steps=6
            shift
            ;;
        --help|-h) usage; exit 0 ;;
        --*) die "未知选项 $1。" ;;
        *) break ;;
    esac
done

if [[ $# -gt 3 ]]; then
    usage >&2
    exit 1
fi
if [[ $operation == 升级 && $# -gt 0 ]]; then
    die '升级使用现有配置，无需传入公网 IP 或端口。'
fi
[[ $repo =~ ^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$ && ${repo#*/} != . && ${repo#*/} != .. ]] ||
    die 'GitHub 仓库须为 OWNER/REPO。'
if [[ -z $version && $repo == ruiange/sing-box-panel ]]; then
    version=$bundled_version
fi
[[ -z $version || $version =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] || die '发布标签格式无效。'
step 1 '检查系统、处理器架构和下载工具'
[[ $EUID -eq 0 ]] || die '请用 root 帐号或 sudo 运行。'
[[ $(uname -s) == Linux && -d /run/systemd/system ]] || die '请在运行 systemd 的 Linux VPS 上执行。'
if [[ $operation == 升级 ]]; then
    [[ -f /etc/sing-box-panel/panel.env && ! -L /etc/sing-box-panel/panel.env ]] ||
        die '找不到已有面板配置。新服务器请运行首次安装命令。'
fi
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) die "当前 CPU 架构不受支持：$(uname -m)。" ;;
esac
for tool in awk mktemp sha256sum tar timeout; do
    command -v "$tool" >/dev/null || die "找不到 $tool，请先安装对应软件包。"
done
command -v curl >/dev/null || command -v wget >/dev/null || die '请先安装 curl 或 wget。'
printf '  已通过检查：Linux %s，发布版本 %s\n' "$arch" "${version:-latest}" >&2

release_path=latest/download
if [[ -n $version ]]; then
    release_path="download/$version"
fi
base="https://github.com/$repo/releases/$release_path"
archive="sing-box-panel-linux-$arch.tar.gz"
stage=$(mktemp -d) || die '创建安装临时目录失败。'
trap 'rm -rf -- "$stage"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

step 2 '从 GitHub Releases 下载 SHA-256 校验文件'
download "$base/$archive.sha256" "$stage/$archive.sha256" || die "下载校验文件失败，请检查发布版本和 GitHub 连通性。"
checksum=$(awk -v name="$archive" '$2 == name || $2 == "*" name {print $1}' "$stage/$archive.sha256")
[[ $checksum =~ ^[a-fA-F0-9]{64}$ ]] || die '发布校验文件未提供唯一有效的 SHA-256。'
step 3 "下载安装包 $archive"
download "$base/$archive" "$stage/$archive" || die "下载 $archive 失败。"
step 4 '校验安装包 SHA-256'
(
    cd -- "$stage"
    printf '%s  %s\n' "$checksum" "$archive" | sha256sum -c --status -
) || die '安装包 SHA-256 校验失败。'
printf '  SHA-256 校验通过。\n' >&2

step 5 '解压安装包'
mkdir -- "$stage/unpacked" || die '创建解压目录失败。'
tar -xzf "$stage/$archive" -C "$stage/unpacked" || die '解压安装包失败。'
if [[ $operation == 升级 ]]; then
    updater="$stage/unpacked/update.sh"
    [[ -f $updater && ! -L $updater ]] || die '这个发布版本缺少原地升级脚本。'
    step 6 '备份并原地升级面板'
    bash "$updater"
    exit
fi
installer="$stage/unpacked/install.sh"
[[ -f $installer && ! -L $installer ]] || die '安装包缺少 install.sh。'

step 6 '确定服务器公网 IP'
if [[ $# -eq 0 ]]; then
    panel="$stage/unpacked/bin/sing-box-panel"
    [[ -f $panel && ! -L $panel && -x $panel ]] || die '安装包缺少可执行的 sing-box-panel。'
    public_ip=$(detect_public_ip "$panel") || die '无法自动检测公网 IP，请在脚本命令末尾手动指定。'
    printf '  检测到公网 IP：%s\n' "$public_ip" >&2
    set -- "$public_ip"
else
    printf '  使用手动指定的公网 IP。\n' >&2
fi

step 7 '检查系统依赖'
packages=()
command -v ss >/dev/null || packages+=(iproute2)
command -v openssl >/dev/null || packages+=(openssl)
[[ -x /usr/bin/pkcheck && -d /etc/polkit-1/rules.d ]] || packages+=(polkitd)
if (( ${#packages[@]} > 0 )); then
    command -v apt-get >/dev/null || die "请先安装缺少的软件包：${packages[*]}。"
    printf '  更新 APT 软件包索引。\n' >&2
    apt-get update || die '更新 APT 软件包索引失败。'
    printf '  安装缺少的系统依赖：%s\n' "${packages[*]}" >&2
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${packages[@]}" ||
        die '安装系统依赖失败。'
else
    printf '  系统依赖已齐备。\n' >&2
fi
step 8 '安装并启动面板和节点'
bash "$installer" --http --multiprotocol "$@"
