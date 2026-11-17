#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

die() {
    printf '升级失败：%s\n' "$*" >&2
    exit 1
}

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    printf '%s\n' '用法：sudo bash upgrade.sh' '为现有 sing-box-panel 启用多协议，保留设备、订阅、密钥及面板地址。'
    exit 0
fi
[[ $# -eq 0 ]] || die '请运行 sudo bash upgrade.sh，不带参数。'
[[ $EUID -eq 0 ]] || die '请以 root 执行。'
[[ $(uname -s) == Linux && -d /run/systemd/system ]] || die '此脚本适用于正在运行 systemd 的 Linux。'
for tool in awk flock install mktemp ss systemctl timeout; do
    command -v "$tool" >/dev/null || die "找不到 ${tool}。"
done

stage=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
panel_new=$stage/bin/sing-box-panel
core_new=$stage/bin/sing-box-panel-core
for candidate in "$panel_new" "$core_new"; do
    [[ -f $candidate && -x $candidate && ! -L $candidate ]] || die '安装包缺少可执行文件。'
done

service=sing-box-panel
panel_unit=$service.service
node_unit=$service-node.service
panel_bin=/usr/local/bin/sing-box-panel
core_bin=/usr/local/bin/sing-box-panel-core
panel_env=/etc/sing-box-panel/panel.env
state_dir=/var/lib/sing-box-panel
node_json=$state_dir/node.json

exec 9>/run/lock/sing-box-panel-upgrade.lock
flock -n 9 || die '已有另一个升级正在执行。'
for target in "$panel_bin" "$core_bin" "$panel_env" "$node_json"; do
    [[ -f $target && ! -L $target ]] || die "找不到标准安装文件 ${target}。"
done
systemctl is-active --quiet "$panel_unit" && systemctl is-active --quiet "$node_unit" ||
    die '现有面板和节点必须正常运行，才能先保存流量再升级。'

# Read the installer-managed environment as data; never execute it as a shell file.
setting() {
    awk -v key="$1" 'index($0, key "=") == 1 { value=substr($0, length(key)+2) } END { print value }' "$panel_env"
}

setting_is_standard() {
    local value
    value=$(setting "$1")
    [[ -z $value || $value == "$2" ]] || die "当前 ${1} 使用自定义路径，请按手动升级说明处理。"
}

setting_is_standard PANEL_SING_BOX_CONFIG "$node_json"
setting_is_standard PANEL_SING_BOX_BINARY "$core_bin"
setting_is_standard PANEL_DATABASE "$state_dir/panel.db"
database=$state_dir/panel.db
[[ -f $database && ! -L $database ]] || die '找不到原数据库。'
[[ $(setting PANEL_NODE_MODE) != disabled ]] || die '现有安装没有启用面板管理的节点。'
[[ $(setting PANEL_NODE_MULTIPROTOCOL) != true ]] || die '现有安装已启用多协议，无需再次执行。'

base=$(setting PANEL_NODE_PORT)
base=${base:-8443}
[[ $base =~ ^[1-9][0-9]{3,4}$ ]] && (( base >= 1024 && base <= 65523 )) ||
    die '节点基准端口必须在 1024 至 65523 之间。'
web_listen=$(setting PANEL_LISTEN)
web_listen=${web_listen:-127.0.0.1:8080}
stats_address=$(setting PANEL_STATS_ADDRESS)
stats_address=${stats_address:-127.0.0.1:10085}
web_port=${web_listen##*:}
stats_port=${stats_address##*:}
https_mode=$(setting PANEL_HTTPS_MODE)
[[ $web_port =~ ^[1-9][0-9]{0,4}$ && $stats_port =~ ^[1-9][0-9]{0,4}$ ]] || die '无法从当前配置读取面板和统计端口。'

listen_protocol() {
    local transport=$1 port=$2
    if [[ $transport == tcp ]]; then
        ss -H -lntp "sport = :$port"
    else
        ss -H -lnup "sport = :$port"
    fi
}

node_ports() {
    local transport offset
    for transport in tcp udp; do
        if [[ $transport == tcp ]]; then
            for offset in 0 3 4 5 6 7 8 9 10 11; do printf '%s %s\n' "$transport" "$((base+offset))"; done
        else
            for offset in 1 2 4 12; do printf '%s %s\n' "$transport" "$((base+offset))"; done
        fi
    done
}

while read -r transport port; do
    (( port != web_port && port != stats_port )) || die "节点端口 ${port} 与面板或统计端口冲突。"
    if (( port != base )) && [[ -n $(listen_protocol "$transport" "$port") ]]; then
        die "${transport} ${port} 已被占用。"
    fi
done < <(node_ports)

core_version=$("$core_new" version) || die '新内核无法运行，请使用与 VPS 架构匹配的安装包。'
for tag in with_utls with_v2ray_api with_quic with_grpc; do
    [[ $core_version == *"$tag"* ]] || die "新内核缺少 ${tag}。"
done
"$core_new" check -c "$node_json" >/dev/null || die '新内核无法读取现有节点配置。'

backup=$(mktemp -d /var/backups/sing-box-panel.XXXXXXXX)
panel_stopped=false
mutation_started=false

atomic_install() {
    local source=$1 target=$2 mode=$3 user=$4 group=$5 temporary
    temporary=$(mktemp "${target%/*}/.sing-box-panel-upgrade.XXXXXXXX") || return 1
    if ! install -m "$mode" -o "$user" -g "$group" "$source" "$temporary" || ! mv -fT -- "$temporary" "$target"; then
        rm -f -- "$temporary"
        return 1
    fi
}

finish() {
    local rc=$? rollback_ok=true
    trap - EXIT INT TERM
    (( rc != 0 )) || return 0
    if [[ $mutation_started == true ]]; then
        printf '恢复升级前的程序和配置…\n' >&2
        systemctl stop "$panel_unit" || rollback_ok=false
        systemctl stop "$node_unit" || rollback_ok=false
        atomic_install "$backup/panel" "$panel_bin" 0755 root root || rollback_ok=false
        atomic_install "$backup/core" "$core_bin" 0755 root root || rollback_ok=false
        atomic_install "$backup/panel.env" "$panel_env" 0600 root root || rollback_ok=false
        atomic_install "$backup/node.json" "$node_json" 0600 "$service" "$service" || rollback_ok=false
        if [[ $rollback_ok == true ]]; then
            systemctl start "$node_unit" && systemctl start "$panel_unit" || rollback_ok=false
        fi
    elif [[ $panel_stopped == true ]]; then
        systemctl start "$panel_unit" || rollback_ok=false
    fi
    if [[ $rollback_ok != true ]]; then
        printf '自动恢复未完成，请查看 systemd 日志。备份保存在 %s\n' "$backup" >&2
    elif [[ $panel_stopped == true ]]; then
        printf '已恢复原程序和配置。备份保存在 %s\n' "$backup" >&2
    fi
    exit "$rc"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cp -p -- "$panel_bin" "$backup/panel"
cp -p -- "$core_bin" "$backup/core"
cp -p -- "$panel_env" "$backup/panel.env"
awk '$0 !~ /^PANEL_NODE_MULTIPROTOCOL=/ {print} END {print "PANEL_NODE_MULTIPROTOCOL=true"}' \
    "$backup/panel.env" > "$backup/panel.new.env"

# Stopping the old panel flushes the latest sample. The old core can keep
# serving until the new panel has rendered and validated its replacement config.
panel_stopped=true
systemctl stop "$panel_unit"
cp -p -- "$node_json" "$backup/node.json"
for suffix in '' -wal -shm -journal; do
    if [[ -f $database$suffix ]]; then cp -p -- "$database$suffix" "$backup/panel.db$suffix"; fi
done

mutation_started=true
atomic_install "$core_new" "$core_bin" 0755 root root
atomic_install "$panel_new" "$panel_bin" 0755 root root
atomic_install "$backup/panel.new.env" "$panel_env" 0600 root root
systemctl start "$panel_unit"

panel_http_ready() {
    local host=${web_listen%:*}
    host=${host#\[}
    host=${host%\]}
    case $host in
        ''|0.0.0.0) host=127.0.0.1 ;;
        ::) host=::1 ;;
    esac
    timeout 3 bash -c '
        exec 3<>"/dev/tcp/$1/$2"
        printf "GET /api/auth/session HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n" >&3
        IFS= read -r -t 2 status <&3
        [[ $status == HTTP/*\ 200\ * ]]
    ' bash "$host" "$web_port" 2>/dev/null
}

ready() {
    local node_pid panel_pid transport port
    systemctl is-active --quiet "$panel_unit" && systemctl is-active --quiet "$node_unit" || return 1
    node_pid=$(systemctl show -p MainPID --value "$node_unit")
    panel_pid=$(systemctl show -p MainPID --value "$panel_unit")
    [[ $node_pid =~ ^[1-9][0-9]*$ && $panel_pid =~ ^[1-9][0-9]*$ ]] || return 1
    [[ $(listen_protocol tcp "$web_port") == *"pid=$panel_pid,"* ]] || return 1
    [[ $(listen_protocol tcp "$stats_port") == *"pid=$node_pid,"* ]] || return 1
    while read -r transport port; do
        [[ $(listen_protocol "$transport" "$port") == *"pid=$node_pid,"* ]] || return 1
    done < <(node_ports)
    # The panel binds before reconciling its node. Check actual HTTP readiness
    # for the direct HTTP and proxy modes instead of only the bound socket.
    case $https_mode in
        ''|http|proxy) panel_http_ready || return 1 ;;
    esac
}

for ((attempt=0; attempt<45; attempt++)); do
    if ready; then
        printf '多协议节点已启用，后台地址为 %s\n' "$(setting PANEL_PUBLIC_URL)"
        printf '以下节点端口需要在防火墙与安全组中可达：\n'
        node_ports
        printf '旧版本备份：%s\n' "$backup"
        exit 0
    fi
    sleep 1
done
die '面板或多协议端口未就绪。可用 journalctl -u sing-box-panel -u sing-box-panel-node -n 80 查看日志。'
