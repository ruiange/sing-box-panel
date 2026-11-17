#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

die() {
    printf '升级失败：%s\n' "$*" >&2
    exit 1
}

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    printf '%s\n' '用法：sudo bash update.sh' \
        '使用已校验的安装包原地升级，保留原管理员密码、配置、设备和流量数据。'
    exit 0
fi
[[ $# -eq 0 ]] || die '请运行 sudo bash update.sh，不带参数。'
[[ $EUID -eq 0 ]] || die '请以 root 执行。'
[[ $(uname -s) == Linux && -d /run/systemd/system ]] || die '请在运行 systemd 的 Linux 服务器执行。'
for tool in awk cmp cp find flock head install mktemp mountpoint mv openssl sort ss systemctl timeout; do
    command -v "$tool" >/dev/null || die "找不到 ${tool}。"
done

stage=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
service=sing-box-panel
panel_unit=$service.service
node_unit=$service-node.service
panel_bin=/usr/local/bin/sing-box-panel
core_bin=/usr/local/bin/sing-box-panel-core
dsb_bin=/usr/local/bin/dsb
panel_env=/etc/sing-box-panel/panel.env
state_dir=/var/lib/sing-box-panel
node_json=$state_dir/node.json
database=$state_dir/panel.db
web_dir=/opt/sing-box-panel/web
panel_new=$stage/bin/sing-box-panel
core_new=$stage/bin/sing-box-panel-core
dsb_new=$stage/deploy/dsb.sh

is_dsb() {
    [[ -f $1 && ! -L $1 && $(awk 'NR == 2 { print; exit }' "$1") == '# sing-box-panel management command' ]]
}

for file in "$panel_new" "$core_new"; do
    [[ -f $file && -x $file && ! -L $file ]] || die '升级包缺少可执行文件。'
done
is_dsb "$dsb_new" && bash -n "$dsb_new" || die '升级包缺少管理命令 dsb，或命令文件无效。'
[[ -f $stage/web/index.html && -d $stage/web/assets && ! -L $stage/web ]] || die '升级包缺少网页文件。'
[[ -z $(find "$stage/web" ! -type d ! -type f -print -quit) ]] || die '升级包的网页目录存在特殊文件。'
target_version=$(timeout 5 "$panel_new" version 2>/dev/null) || die '新版面板无法运行，请检查安装包架构。'
version_pattern='^v[0-9]+\.[0-9]+\.[0-9]+$'
[[ $target_version =~ $version_pattern || $target_version == dev ]] || die '升级包缺少有效的面板版本。'

lock_file=/run/lock/sing-box-panel-upgrade.lock
[[ ! -L $lock_file && ( ! -e $lock_file || -f $lock_file ) ]] || die '升级锁文件无效。'
exec 9>>"$lock_file"
flock -n 9 || die '已有另一个升级或修复正在执行。'
for file in "$panel_bin" "$core_bin" "$panel_env" "$node_json" "$database" "$web_dir/index.html"; do
    [[ -f $file && ! -L $file ]] || die "找不到标准安装文件 ${file}。"
done
for dir in "$web_dir" "$state_dir"; do
    [[ -d $dir && ! -L $dir ]] || die "找不到标准目录 ${dir}。"
    if mountpoint -q -- "$dir"; then die "${dir} 是独立挂载点，请备份并手动升级。"; fi
done
if [[ -e $dsb_bin || -L $dsb_bin ]]; then
    is_dsb "$dsb_bin" || die "已有其他程序使用 ${dsb_bin}，请先处理命令冲突。"
fi
for unit in "$panel_unit" "$node_unit"; do
    [[ $(systemctl show -p FragmentPath --value "$unit") == "/etc/systemd/system/$unit" &&
        $(systemctl show -p User --value "$unit") == "$service" ]] || die "找不到标准服务 ${unit}。"
done
[[ $(systemctl show -p ExecStart --value "$panel_unit") == "{ path=$panel_bin ;"* &&
    $(systemctl show -p ExecStart --value "$node_unit") == "{ path=$core_bin ;"* ]] ||
    die '现有服务使用非标准可执行文件，请备份并手动升级。'

# Read the installer-managed EnvironmentFile as data, without evaluating it.
setting() {
    awk -v name="$1" 'index($0, name "=") == 1 { value=substr($0, length(name)+2) } END { print value }' "$panel_env"
}

standard_path() {
    local value
    value=$(setting "$1")
    [[ $value == "$2" ]] || die "当前 $1 路径与标准安装不同，请备份并手动升级。"
}

standard_path PANEL_DATABASE "$database"
standard_path PANEL_WEB_DIR "$web_dir"
standard_path PANEL_SING_BOX_BINARY "$core_bin"
standard_path PANEL_SING_BOX_CONFIG "$node_json"
[[ $(setting PANEL_NODE_MODE) == managed ]] || die '当前面板没有启用标准节点管理。'

read_port() {
    [[ $1 =~ ^[0-9]{1,5}$ ]] && (( 10#$1 >= 1 && 10#$1 <= 65535 )) || return 1
    printf '%d\n' "$((10#$1))"
}

web_listen=$(setting PANEL_LISTEN)
web_port=$(read_port "${web_listen##*:}") || die '无法读取现有面板监听端口。'
web_host=${web_listen%:*}
web_host=${web_host#\[}
web_host=${web_host%\]}
case $web_host in
    ''|0.0.0.0) web_host=127.0.0.1 ;;
    ::) web_host=::1 ;;
esac
stats_address=$(setting PANEL_STATS_ADDRESS)
stats_port=$(read_port "${stats_address##*:}") || die '无法读取现有统计端口。'
node_port=$(setting PANEL_NODE_PORT)
node_port=$(read_port "${node_port:-8443}") || die '无法读取现有节点端口。'
node_tcp_ports=("$node_port")
node_udp_ports=()
case $(setting PANEL_NODE_MULTIPROTOCOL) in
    true|True|TRUE|1|t|T)
        (( node_port <= 65523 )) || die '现有多协议节点端口超出范围。'
        for offset in 3 4 5 6 7 8 9 10 11; do node_tcp_ports+=("$((node_port + offset))"); done
        for offset in 1 2 4 12; do node_udp_ports+=("$((node_port + offset))"); done
        ;;
esac

public_url=$(setting PANEL_PUBLIC_URL)
https_mode=$(setting PANEL_HTTPS_MODE)
tls_args=()
web_endpoint=$web_host:$web_port
[[ $web_host != *:* ]] || web_endpoint=[$web_host]:$web_port
case $https_mode in
    ''|http|proxy) ;;
    auto|ip)
        acme_cache=$(setting PANEL_ACME_CACHE)
        [[ -z $acme_cache || $acme_cache == "$state_dir/acme" ]] ||
            die '当前证书目录与标准安装不同，请备份并手动升级。'
        tls_args=(-noservername)
        if [[ $https_mode == auto ]]; then
            tls_name=${public_url#https://}
            tls_name=${tls_name%%[:/]*}
            [[ $tls_name =~ ^[a-zA-Z0-9.-]+$ ]] || die '无法读取现有 HTTPS 域名。'
            tls_args=(-servername "$tls_name")
        fi
        ;;
    *) die '无法识别现有面板的 HTTPS 模式。' ;;
esac

serves_panel_http() {
    local status
    if (( ${#tls_args[@]} == 0 )); then
        timeout 3 bash -c '
            exec 3<>"/dev/tcp/$1/$2"
            printf "GET /api/auth/session HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n" >&3
            IFS= read -r status <&3
            [[ $status == HTTP/*\ 200\ * ]]
        ' bash "$web_host" "$web_port" 2>/dev/null
    else
        status=$(
            set +o pipefail
            printf 'GET /api/auth/session HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n' "${tls_name:-localhost}" |
                timeout 4 openssl s_client -quiet -connect "$web_endpoint" "${tls_args[@]}" 2>/dev/null |
                awk 'NR == 1 { print; exit }'
        )
        [[ $status == HTTP/*\ 200\ * ]]
    fi
}

ready() {
    local node_pid panel_pid port
    systemctl is-active --quiet "$panel_unit" && systemctl is-active --quiet "$node_unit" || return 1
    node_pid=$(systemctl show -p MainPID --value "$node_unit") || return 1
    panel_pid=$(systemctl show -p MainPID --value "$panel_unit") || return 1
    [[ $node_pid =~ ^[1-9][0-9]*$ && $panel_pid =~ ^[1-9][0-9]*$ ]] || return 1
    [[ $(ss -H -lntp "sport = :$web_port") == *"pid=$panel_pid,"* ]] || return 1
    for port in "$stats_port" "${node_tcp_ports[@]}"; do
        [[ $(ss -H -lntp "sport = :$port") == *"pid=$node_pid,"* ]] || return 1
    done
    for port in "${node_udp_ports[@]}"; do
        [[ $(ss -H -lnup "sport = :$port") == *"pid=$node_pid,"* ]] || return 1
    done
    serves_panel_http
}

wait_ready() {
    local attempt
    for ((attempt = 0; attempt < 45; attempt++)); do
        if ready; then return 0; fi
        sleep 1
    done
    return 1
}

printf '[升级 1/4] 核对现有服务和新版安装包。\n' >&2
ready || die '现有面板或节点未就绪，请修复服务后再升级。'
current_version=$(timeout 5 "$panel_bin" version 2>/dev/null) || current_version=
if [[ $current_version =~ $version_pattern && $target_version =~ $version_pattern ]]; then
    if [[ $current_version == "$target_version" ]]; then
        cmp -s "$panel_new" "$panel_bin" && cmp -s "$core_new" "$core_bin" ||
            die '发现同一版本号对应不同程序，请核对安装包来源。'
        if cmp -s "$dsb_new" "$dsb_bin"; then
            printf '当前已安装 %s。\n' "$current_version"
            exit 0
        fi
    elif [[ $(printf '%s\n' "$current_version" "$target_version" | sort -V | head -n 1) == "$target_version" ]]; then
        die "目标版本 $target_version 低于当前版本 $current_version。"
    fi
fi
timeout 10 "$core_new" check -c "$node_json" >/dev/null 2>&1 || die '新版节点核心无法读取现有节点配置。'
core_changed=true
if cmp -s "$core_new" "$core_bin"; then core_changed=false; fi

backup=$(mktemp -d /var/backups/sing-box-panel-update.XXXXXXXX)
bin_stage=
web_stage=
state_stage=
panel_stopped=false
switch_started=false

restore_binaries() {
    cp -a -- "$backup/panel" "$bin_stage/panel.rollback" &&
        mv -fT -- "$bin_stage/panel.rollback" "$panel_bin" || return 1
    if [[ $core_changed == true ]]; then
        cp -a -- "$backup/core" "$bin_stage/core.rollback" &&
            mv -fT -- "$bin_stage/core.rollback" "$core_bin" || return 1
    fi
    if [[ -f $backup/dsb ]]; then
        cp -a -- "$backup/dsb" "$bin_stage/dsb.rollback" &&
            mv -fT -- "$bin_stage/dsb.rollback" "$dsb_bin" || return 1
    elif [[ -e $dsb_bin || -L $dsb_bin ]]; then
        is_dsb "$dsb_bin" && rm -f -- "$dsb_bin" || return 1
    fi
}

rollback() {
    systemctl stop "$panel_unit" && systemctl stop "$node_unit" || return 1
    # Keep the state from the failed start, including any writes made before rollback.
    state_stage=$(mktemp -d "${state_dir%/*}/.sing-box-panel-rollback.XXXXXXXX") || return 1
    cp -a -- "$backup/state" "$state_stage/old" || return 1
    mv -T -- "$state_dir" "$state_stage/failed-state" || return 1
    if ! mv -T -- "$state_stage/old" "$state_dir"; then
        mv -T -- "$state_stage/failed-state" "$state_dir" || true
        return 1
    fi
    restore_binaries || return 1
    if [[ -d $web_stage/old ]]; then
        if [[ -d $web_dir ]]; then mv -T -- "$web_dir" "$web_stage/failed" || return 1; fi
        mv -T -- "$web_stage/old" "$web_dir" || return 1
    fi
    systemctl start "$node_unit" && systemctl start "$panel_unit" && wait_ready
}

finish() {
    local rc=$? restored=true dir
    trap - EXIT INT TERM
    if (( rc != 0 )); then
        if [[ $switch_started == true ]]; then
            printf '尝试恢复升级前的程序与数据。\n' >&2
            rollback || restored=false
        elif [[ $panel_stopped == true ]]; then
            systemctl start "$node_unit" && systemctl start "$panel_unit" && wait_ready || restored=false
        fi
        if [[ $restored == false ]]; then
            printf '自动恢复未完成。备份：%s。暂存目录：%s %s %s。\n' "$backup" "$bin_stage" "$web_stage" "$state_stage" >&2
            exit "$rc"
        fi
        if [[ $panel_stopped == true ]]; then
            printf '原服务已恢复。备份：%s。\n' "$backup" >&2
        fi
        if [[ -n $state_stage && -d $state_stage/failed-state ]]; then
            printf '失败版本运行后的数据已保存在 %s/failed-state。\n' "$state_stage" >&2
        fi
    fi
    for dir in "$bin_stage" "$web_stage"; do
        if [[ -n $dir ]]; then rm -rf -- "$dir"; fi
    done
    exit "$rc"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf '[升级 2/4] 预置新版文件并备份当前部署。\n' >&2
bin_stage=$(mktemp -d "${panel_bin%/*}/.sing-box-panel-update.XXXXXXXX")
web_stage=$(mktemp -d "${web_dir%/*}/.sing-box-panel-update.XXXXXXXX")
install -m 0755 -o root -g root "$panel_new" "$bin_stage/panel"
install -m 0755 -o root -g root "$dsb_new" "$bin_stage/dsb"
if [[ $core_changed == true ]]; then install -m 0755 -o root -g root "$core_new" "$bin_stage/core"; fi
install -d -m 0755 -o root -g root "$web_stage/new"
find "$stage/web" -mindepth 1 -type d -print0 | while IFS= read -r -d '' path; do
    install -d -m 0755 -o root -g root "$web_stage/new${path#$stage/web}"
done
find "$stage/web" -type f -print0 | while IFS= read -r -d '' path; do
    install -m 0644 -o root -g root "$path" "$web_stage/new${path#$stage/web}"
done
cp -a -- "$panel_bin" "$backup/panel"
cp -a -- "$core_bin" "$backup/core"
cp -a -- "$panel_env" "$backup/panel.env"
cp -a -- "$web_dir" "$backup/web"
if [[ -f $dsb_bin ]]; then cp -a -- "$dsb_bin" "$backup/dsb"; fi

printf '[升级 3/4] 保存最新流量，备份数据库与节点状态，然后切换版本。\n' >&2
cmp -s "$backup/panel.env" "$panel_env" || die '升级期间配置发生变化，请稍后重试。'
panel_stopped=true
systemctl stop "$panel_unit"
[[ $(systemctl show -p Result --value "$panel_unit") == success ]] || die '面板未正常退出，已取消升级。'
if [[ $core_changed == true ]]; then systemctl stop "$node_unit"; fi
# The panel is the only writer of SQLite and may use WAL or a rollback journal.
# Copy the complete directory after stopping it, including certificates and sidecars.
cp -a -- "$state_dir" "$backup/state"

switch_started=true
mv -fT -- "$bin_stage/panel" "$panel_bin"
if [[ $core_changed == true ]]; then mv -fT -- "$bin_stage/core" "$core_bin"; fi
mv -fT -- "$bin_stage/dsb" "$dsb_bin"
mv -T -- "$web_dir" "$web_stage/old"
mv -T -- "$web_stage/new" "$web_dir"
systemctl start "$panel_unit"

printf '[升级 4/4] 检查面板、节点端口和登录接口。\n' >&2
wait_ready || die '新版服务未就绪，可用 journalctl -u sing-box-panel -u sing-box-panel-node -n 80 查看日志。'
printf '已升级到 %s，两个服务均已就绪。\n后台地址：%s\n升级前备份：%s\n' \
    "$target_version" "$public_url" "$backup"
