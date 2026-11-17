#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

die() {
    printf '安装失败：%s\n' "$*" >&2
    exit 1
}

install_step() {
    current_step=$2
    printf '[安装 %s/8] %s…\n' "$1" "$current_step" >&2
}

usage() {
    printf '%s\n' \
        '用法：sudo bash install.sh panel.example.com [节点端口，默认 8443]' \
        '      sudo bash install.sh <公网 IP> [节点端口，默认 8443] [网页端口，默认 9443]' \
        '      sudo bash install.sh --http <公网 IP> [节点端口，默认 8443] [网页端口，默认 9443]' \
        '      sudo bash install.sh --http --multiprotocol <公网 IP> [起始端口] [网页端口]' \
        'HTTPS 模式的安装会接受 Let’s Encrypt 服务条款：https://letsencrypt.org/repository/'
}

is_domain() {
    local domain=$1
    local pattern='^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$'
    [[ ${#domain} -le 253 && $domain =~ $pattern ]]
}

parse_port() {
    local value=$1
    local label=$2
    [[ $value =~ ^[0-9]{4,5}$ ]] || die "${label}须为 1024 至 65535。"
    value=$((10#$value))
    (( value >= 1024 && value <= 65535 )) || die "${label}须为 1024 至 65535。"
    (( value != 10085 )) || die "${label}不能使用统计端口 10085。"
    printf '%d\n' "$value"
}

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    usage
    exit 0
fi

install_step 1 '检查安装参数、Linux 环境和安装包'
trap 'exit_code=$?; printf "安装失败：%s（退出码 %d）\n" "$current_step" "$exit_code" >&2; exit "$exit_code"' ERR

https_mode=auto
multiprotocol=false
while [[ ${1:-} == --* ]]; do
    case $1 in
        --http) https_mode=http ;;
        --multiprotocol) multiprotocol=true ;;
        *) die "未知安装选项：$1" ;;
    esac
    shift
done
if [[ $# -lt 1 || $# -gt 3 ]]; then
    usage >&2
    exit 1
fi

address=$1
node_port=$(parse_port "${2:-8443}" 节点端口) || exit 1
reality_server=${PANEL_REALITY_SERVER_NAME:-www.example.com}
is_domain "$reality_server" || die 'Reality 握手域名无效。'

if [[ $address == *:* || $address =~ ^[0-9.]+$ ]]; then
    if [[ $https_mode == auto ]]; then
        https_mode=ip
    fi
elif [[ $https_mode == http ]]; then
    die '--http 需要公网 IPv4 或 IPv6 地址。'
elif ! is_domain "$address"; then
    die '请提供 VPS 的公网 IP，或直连 VPS 的域名。'
fi

web_port=443
if [[ $https_mode == auto ]]; then
    [[ $# -lt 3 ]] || die '域名模式的网页端口固定为 443。'
else
    web_port=$(parse_port "${3:-${PANEL_WEB_PORT:-9443}}" 网页端口) || exit 1
    (( node_port != web_port )) || die '节点端口和网页端口必须不同。'
fi

[[ $EUID -eq 0 ]] || die '请用 sudo bash install.sh 执行。'
[[ $(uname -s) == Linux && -d /run/systemd/system ]] ||
    die '请在运行 systemd 的 Linux VPS 上执行。'
for tool in ss openssl getent install useradd groupadd setpriv awk find timeout; do
    command -v "$tool" >/dev/null || die "找不到 ${tool}，请先安装系统对应的软件包。"
done
[[ -x /usr/bin/systemctl && -x /usr/bin/pkcheck && -d /etc/polkit-1/rules.d ]] ||
    die '需要 systemd 和支持 JavaScript 规则的 polkit（Debian 12 或 Ubuntu 24.04 可安装 polkitd）。'

stage=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
for item in bin/sing-box-panel bin/sing-box-panel-core web/index.html \
    deploy/sing-box-panel.service deploy/sing-box-panel-node.service \
    deploy/49-sing-box-panel.rules deploy/check-node-restart.sh; do
    [[ -f $stage/$item && ! -L $stage/$item ]] || die "安装包缺少 ${item}。"
done
[[ -x $stage/bin/sing-box-panel && -x $stage/bin/sing-box-panel-core && -d $stage/web/assets ]] ||
    die '安装包不完整，或解压时丢失了可执行权限。'
if [[ -n $(find "$stage/web" ! -type d ! -type f -print -quit) ]]; then
    die '网页目录中包含符号链接或特殊文件。'
fi

install_step 2 '确认访问地址，检查已有安装和端口占用'
public_ports=("$web_port")
case $https_mode in
    http) scheme=http ;;
    ip)   scheme=https; public_ports=(443 "$web_port") ;;
    auto) scheme=https ;;
esac
if [[ $https_mode != auto ]]; then
    if [[ $address == \[*\] ]]; then
        address=${address:1:${#address}-2}
    fi
    address=$("$stage/bin/sing-box-panel" validate-address "$address") ||
        die '请提供有效的公网 IPv4 或 IPv6 地址，且安装包架构须与 VPS 匹配。'
    if [[ $address == *:* ]]; then
        public_url="$scheme://[$address]:$web_port"
    else
        public_url="$scheme://$address:$web_port"
    fi
else
    address=$(printf '%s' "$address" | LC_ALL=C tr '[:upper:]' '[:lower:]')
    public_url="https://$address"
fi

node_tcp_ports=("$node_port")
node_udp_ports=()
if [[ $multiprotocol == true ]]; then
    (( node_port <= 65523 )) || die '多协议模式的节点起始端口不能大于 65523。'
    for reserved_port in "$web_port" 10085; do
        if (( reserved_port >= node_port && reserved_port <= node_port + 12 )); then
            die "节点端口范围与端口 ${reserved_port} 冲突。"
        fi
    done
    for offset in 3 4 5 6 7 8 9 10 11; do node_tcp_ports+=("$((node_port + offset))"); done
    for offset in 1 2 4 12; do node_udp_ports+=("$((node_port + offset))"); done
    printf '  后台地址 %s，节点端口范围 %s–%s。\n' "$public_url" "$node_port" "$((node_port + 12))" >&2
else
    printf '  后台地址 %s，节点端口 %s。\n' "$public_url" "$node_port" >&2
fi
public_ports+=("${node_tcp_ports[@]}")

service=sing-box-panel
env_dir=/etc/sing-box-panel
state_dir=/var/lib/sing-box-panel
web_dir=/opt/sing-box-panel/web
panel_unit=/etc/systemd/system/sing-box-panel.service
node_unit=/etc/systemd/system/sing-box-panel-node.service
polkit_rule=/etc/polkit-1/rules.d/49-sing-box-panel.rules
for target in /usr/local/bin/sing-box-panel /usr/local/bin/sing-box-panel-core \
    "$env_dir" "$state_dir" /opt/sing-box-panel "$panel_unit" "$node_unit" "$polkit_rule" \
    /etc/sudoers.d/sing-box-panel; do
    if [[ -e $target || -L $target ]]; then
        die "发现已有文件 ${target}。这是首次安装脚本，已有配置和数据请保留。"
    fi
done
for port in "${public_ports[@]}" 10085; do
    if [[ -n $(ss -H -lnt "sport = :$port") ]]; then
        die "TCP $port 已被占用，请先检查 ss -ltnp。"
    fi
done
for port in "${node_udp_ports[@]}"; do
    if [[ -n $(ss -H -lnu "sport = :$port") ]]; then
        die "UDP $port 已被占用，请先检查 ss -lnup。"
    fi
done

install_step 3 '验证 sing-box 核心，生成管理员密码和节点凭据'
core_version=$("$stage/bin/sing-box-panel-core" version) ||
    die '核心无法运行，请使用与 VPS 架构匹配的安装包。'
[[ $core_version == *with_v2ray_api* && $core_version == *with_utls* ]] ||
    die '核心缺少 Reality 或流量统计功能。'
if [[ $multiprotocol == true ]]; then
    [[ $core_version == *with_quic* && $core_version == *with_grpc* ]] ||
        die '多协议模式需要包含 QUIC 和 gRPC 的核心。'
fi
keypair=$("$stage/bin/sing-box-panel-core" generate reality-keypair)
private_key=$(awk '/^PrivateKey:/ { print $2 }' <<< "$keypair")
[[ $private_key =~ ^[a-zA-Z0-9_-]{43}$ ]] || die '无法生成 Reality 私钥。'
short_id=$(openssl rand -hex 8)
password=$(openssl rand -hex 16)

env_tmp=$(mktemp /tmp/sing-box-panel-env.XXXXXXXX)
trap 'rm -f -- "$env_tmp"' EXIT
cat > "$env_tmp" <<ENV
PANEL_ADMIN_PASSWORD=$password
PANEL_PUBLIC_URL=$public_url
PANEL_HTTPS_MODE=$https_mode
PANEL_LISTEN=:$web_port
PANEL_NODE_MODE=managed
PANEL_NODE_HOST=$address
PANEL_NODE_PORT=$node_port
PANEL_NODE_LISTEN=::
PANEL_NODE_MULTIPROTOCOL=$multiprotocol
PANEL_REALITY_SERVER_NAME=$reality_server
PANEL_REALITY_PRIVATE_KEY=$private_key
PANEL_REALITY_SHORT_ID=$short_id
PANEL_DATABASE=$state_dir/panel.db
PANEL_WEB_DIR=$web_dir
PANEL_STATS_ADDRESS=127.0.0.1:10085
PANEL_SING_BOX_BINARY=/usr/local/bin/sing-box-panel-core
PANEL_SING_BOX_CONFIG=$state_dir/node.json
PANEL_NODE_CONTROL=systemctl
ENV

install_step 4 '准备系统服务账户并验证节点重启授权'
if ! getent group "$service" >/dev/null; then
    groupadd --system "$service"
fi
if ! getent passwd "$service" >/dev/null; then
    useradd --system --gid "$service" --home-dir "$state_dir" --shell /usr/sbin/nologin "$service"
fi
install -m 0644 -o root -g root "$stage/deploy/49-sing-box-panel.rules" "$polkit_rule"
if ! bash "$stage/deploy/check-node-restart.sh"; then
    rm -f -- "$polkit_rule"
    die 'polkit 未授予节点重启权限。请确认支持 JavaScript 规则，并查看 journalctl -u polkit。'
fi

install_step 5 '安装面板、节点核心和网页文件，写入配置'
install -d -m 0700 -o root -g root "$env_dir"
install -d -m 0755 -o root -g root "$web_dir"
if [[ ! -d /usr/local/bin ]]; then
    install -d -m 0755 -o root -g root /usr/local/bin
fi
install -m 0755 -o root -g root "$stage/bin/sing-box-panel" "$stage/bin/sing-box-panel-core" /usr/local/bin/
find "$stage/web" -type d -print0 | while IFS= read -r -d '' path; do
    install -d -m 0755 -o root -g root "$web_dir${path#$stage/web}"
done
find "$stage/web" -type f -print0 | while IFS= read -r -d '' path; do
    install -m 0644 -o root -g root "$path" "$web_dir${path#$stage/web}"
done
install -m 0600 -o root -g root "$env_tmp" "$env_dir/panel.env"

install_step 6 '注册 systemd 服务'
install -m 0644 -o root -g root "$stage/deploy/sing-box-panel.service" "$panel_unit"
install -m 0644 -o root -g root "$stage/deploy/sing-box-panel-node.service" "$node_unit"

if [[ $https_mode != http ]]; then
    printf '%s\n' '面板将自动申请 HTTPS 证书。Let’s Encrypt 服务条款：https://letsencrypt.org/repository/' >&2
fi

ready() {
    local port
    waiting_for='面板 systemd 服务'
    systemctl is-active --quiet sing-box-panel.service || return 1
    waiting_for='节点 systemd 服务'
    systemctl is-active --quiet sing-box-panel-node.service || return 1
    for port in "$web_port" "${node_tcp_ports[@]}" 10085; do
        waiting_for="TCP $port 监听"
        [[ -n $(ss -H -lnt "sport = :$port") ]] || return 1
    done
    for port in "${node_udp_ports[@]}"; do
        waiting_for="UDP $port 监听"
        [[ -n $(ss -H -lnu "sport = :$port") ]] || return 1
    done
    if [[ $https_mode == http ]]; then
        waiting_for='本机 HTTP /api/auth/session 返回 200'
        timeout 3 bash -c '
            exec 3<>"/dev/tcp/127.0.0.1/$1"
            printf "GET /api/auth/session HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n" >&3
            IFS= read -r -t 2 status <&3
            [[ $status == HTTP/*\ 200\ * ]]
        ' bash "$web_port" 2>/dev/null || return 1
    fi
}

install_step 7 '启动面板和节点服务'
systemctl daemon-reload
systemctl enable --now sing-box-panel.service

install_step 8 '检查服务状态和端口监听'
for (( attempt = 0; attempt < 45; attempt++ )); do
    if ready; then
        printf '\n面板已启动：%s\n管理员密码：%s\n' "$public_url" "$password"
        printf '请放行以下 TCP 端口：%s。\n' "${public_ports[*]}"
        if (( ${#node_udp_ports[@]} )); then
            printf '请放行以下 UDP 端口：%s。\n' "${node_udp_ports[*]}"
        fi
        if [[ $https_mode == ip ]]; then
            printf '面板会自动申请 HTTPS 证书，签发后即可访问。\n'
        elif [[ $https_mode == auto ]]; then
            printf '请确认域名直连这台 VPS。首次访问时会申请 HTTPS 证书。\n'
        fi
        exit 0
    fi
    if (( attempt == 14 || attempt == 29 )); then
        printf '  正在等待%s，已完成 %d 次检查…\n' "$waiting_for" "$((attempt + 1))" >&2
    fi
    sleep 1
done
printf '%s\n' "查阅管理员密码：sudo sed -n 's/^PANEL_ADMIN_PASSWORD=//p' $env_dir/panel.env" >&2
die "服务尚未就绪，最后未通过的检查为${waiting_for}。安装配置已保存。请运行 journalctl -u sing-box-panel -u sing-box-panel-node -u polkit -n 60 查看日志。"
