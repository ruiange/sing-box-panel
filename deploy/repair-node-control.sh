#!/usr/bin/env bash
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

die() {
    printf '修复失败：%s\n' "$*" >&2
    exit 1
}

if [[ ${1:-} == --help || ${1:-} == -h ]]; then
    printf '%s\n' '用法：sudo bash repair-node-control.sh' \
        '修复已有标准安装的节点重启权限，保留设备、配置、密码和流量数据。'
    exit 0
fi
[[ $# -eq 0 ]] || die '请运行 sudo bash repair-node-control.sh，不带参数。'
[[ $EUID -eq 0 ]] || die '请以 root 运行。'
[[ $(uname -s) == Linux && -d /run/systemd/system ]] || die '请在运行 systemd 的 Linux 服务器执行。'
for tool in awk flock runuser timeout; do
    command -v "$tool" >/dev/null || die "找不到 ${tool}。"
done
[[ -x /usr/bin/pkcheck && -d /etc/polkit-1/rules.d ]] || die '缺少支持 JavaScript 规则的 polkit。'

stage=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
service=sing-box-panel
panel_unit=$service.service
node_unit=$service-node.service
panel_env=/etc/sing-box-panel/panel.env
rule_source=$stage/deploy/49-sing-box-panel.rules
rule_path=/etc/polkit-1/rules.d/49-sing-box-panel.rules
[[ -f $rule_source && ! -L $rule_source ]] || die '安装包缺少节点重启授权规则。'

exec 9>/run/lock/sing-box-panel-upgrade.lock
flock -n 9 || die '已有另一个升级或修复正在执行。'
for file in "$panel_env" /usr/local/bin/sing-box-panel /usr/local/bin/sing-box-panel-core; do
    [[ -f $file && ! -L $file ]] || die "找不到标准安装文件 ${file}。"
done
[[ $(systemctl show -p FragmentPath --value "$panel_unit") == "/etc/systemd/system/$panel_unit" &&
   $(systemctl show -p FragmentPath --value "$node_unit") == "/etc/systemd/system/$node_unit" &&
   $(systemctl show -p User --value "$panel_unit") == "$service" ]] || die '找不到标准面板或节点服务。'
if [[ -e $rule_path || -L $rule_path ]]; then
    [[ -f $rule_path && ! -L $rule_path ]] && cmp -s "$rule_source" "$rule_path" ||
        die "已有自定义规则 ${rule_path}，请先核对。"
fi

setting() {
    awk -v key="$1" 'index($0, key "=") == 1 { value=substr($0, length(key)+2) } END { print value }' "$panel_env"
}

control=$(setting PANEL_NODE_CONTROL)
https_mode=$(setting PANEL_HTTPS_MODE)
web_listen=$(setting PANEL_LISTEN)
web_port=${web_listen##*:}
web_host=${web_listen%:*}
web_host=${web_host#\[}
web_host=${web_host%\]}
case $web_host in
    ''|0.0.0.0) web_host=127.0.0.1 ;;
    ::) web_host=::1 ;;
esac
[[ $control == sudo || $control == systemctl || -z $control ]] || die '现有节点管理方式无效。'
[[ $(setting PANEL_NODE_MODE) == managed ]] || die '现有面板未启用节点管理。'
if [[ $https_mode == http ]]; then
    [[ $web_port =~ ^[1-9][0-9]{0,4}$ ]] && (( web_port <= 65535 )) || die '无法读取现有 HTTP 面板端口。'
fi

ready() {
    systemctl is-active --quiet "$panel_unit" && systemctl is-active --quiet "$node_unit" || return 1
    if [[ $https_mode == http ]]; then
        timeout 3 bash -c '
            exec 3<>"/dev/tcp/$1/$2" || exit 1
            printf "GET /api/auth/session HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n" >&3
            IFS= read -r -t 2 status <&3 && [[ $status == HTTP/*\ 200\ * ]]
        ' bash "$web_host" "$web_port" 2>/dev/null || return 1
    fi
}

check_policy() {
    timeout 3 runuser -u "$service" -- bash -c '
        start_time=$(awk "{ print \$22 }" "/proc/$$/stat") || exit 1
        [[ $start_time =~ ^[0-9]+$ ]] || exit 1
        exec /usr/bin/pkcheck --action-id org.freedesktop.systemd1.manage-units \
            --process "$$,$start_time,$UID" --detail unit "$1" --detail verb restart
    ' bash "$node_unit"
}

atomic_install() {
    local source=$1 target=$2 mode=$3 temporary
    temporary=$(mktemp "${target%/*}/.sing-box-panel-repair.XXXXXXXX") || return 1
    if install -m "$mode" -o root -g root "$source" "$temporary" && mv -fT -- "$temporary" "$target"; then
        return 0
    fi
    rm -f -- "$temporary"
    return 1
}

backup=
rule_added=false
env_changed=false

finish() {
    local status=$? restored=true
    trap - EXIT INT TERM
    (( status != 0 )) || return 0
    if [[ $env_changed == true ]]; then
        atomic_install "$backup/panel.env" "$panel_env" 0600 || restored=false
    fi
    if [[ $restored == true && $rule_added == true ]]; then
        rm -f -- "$rule_path" || restored=false
    fi
    if [[ $restored == true && $env_changed == true ]]; then
        printf '已恢复原有配置，备份位于 %s。\n' "$backup" >&2
        systemctl restart "$panel_unit" || printf '面板重新启动失败，请查看服务日志。\n' >&2
    elif [[ $restored != true ]]; then
        printf '自动回滚未完成。备份位置：%s。\n' "${backup:-无}" >&2
    fi
    exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf '[修复 1/3] 核对节点服务重启权限。\n' >&2
if [[ ! -e $rule_path ]]; then
    rule_added=true
    atomic_install "$rule_source" "$rule_path" 0644 || die '无法安装 polkit 规则。'
fi
# polkitd reloads changed rules asynchronously.
for (( attempt = 0; attempt < 10; attempt++ )); do
    if check_policy >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
(( attempt < 10 )) || die 'polkit 未授予节点重启权限，请查看 journalctl -u polkit.service。'

printf '[修复 2/3] 更新面板的节点控制方式。\n' >&2
if [[ $control != systemctl ]]; then
    backup=$(mktemp -d /var/backups/sing-box-panel-repair.XXXXXXXX)
    install -m 0600 -o root -g root "$panel_env" "$backup/panel.env"
    awk '$0 !~ /^PANEL_NODE_CONTROL=/ { print } END { print "PANEL_NODE_CONTROL=systemctl" }' \
        "$backup/panel.env" > "$backup/panel.new.env"
    env_changed=true
    atomic_install "$backup/panel.new.env" "$panel_env" 0600 || die '无法更新面板配置。'
    rm -f -- "$backup/panel.new.env"
fi

printf '[修复 3/3] 检查面板与节点服务。\n' >&2
if [[ $env_changed == true ]] || ! ready; then
    systemctl restart "$panel_unit" || die "无法启动 ${panel_unit}，请查看 systemd 日志。"
fi
for (( attempt = 0; attempt < 45; attempt++ )); do
    if ready; then
        printf '面板与节点服务已就绪。\n'
        if [[ -n $backup ]]; then
            printf '原配置备份：%s/panel.env\n' "$backup"
        fi
        exit 0
    fi
    sleep 1
done
systemctl show -p Id -p ActiveState -p SubState -p Result "$panel_unit" "$node_unit" >&2 || true
die '服务尚未就绪，请运行 journalctl -u sing-box-panel -u sing-box-panel-node -n 80 --no-pager。'
