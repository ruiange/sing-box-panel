#!/usr/bin/env bash
# sing-box-panel management command
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

panel=/usr/local/bin/sing-box-panel
launcher_url=https://github.com/ruiange/sing-box-panel/releases/latest/download/install-from-release.sh

menu() {
    printf '%s\n' \
        '============== 流量大师 ==============' \
        '(1) 查看当前版本' \
        '(2) 查询最新版本' \
        '(3) 下载最新安装包' \
        '(4) 一键升级到最新版本' \
        '(0) 退出' \
        '======================================'
}

usage() {
    printf '%s\n' '用法：dsb [0|1|2|3 [下载目录]|4]' '运行 dsb 打开菜单，也可直接传入编号。'
}

usage_error() {
    usage >&2
    exit 2
}

run_panel() {
    if [[ ! -x $panel ]]; then
        printf '%s\n' '找不到流量大师程序，请先安装面板。' >&2
        exit 1
    fi
    exec "$panel" "$@"
}

download_launcher_with_wget() {
    local url=$launcher_url destination=$1 log=$1.wget.log hop location host
    # GNU Wget's --https-only applies to recursive downloads. Check each redirect explicitly.
    for ((hop = 0; hop <= 8; hop++)); do
        if LC_ALL=C wget --no-config --no-verbose --server-response --max-redirect=0 \
            --output-file="$log" --timeout=15 --tries=2 --output-document="$destination" -- "$url"; then
            return 0
        fi
        [[ -s $log ]] || return 1
        location=$(awk '
            /^[[:space:]]+HTTP\// { status=$2; location="" }
            status ~ /^30[12378]$/ && tolower($1) == "location:" {
                sub(/^[[:space:]]+[^:]+:[[:space:]]*/, "")
                sub(/\r$/, "")
                location=$0
            }
            END { print location }
        ' "$log")
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

upgrade() (
    if [[ $EUID -ne 0 ]]; then
        printf '%s\n' '升级需要 root 权限，请运行 sudo dsb 4。' >&2
        exit 1
    fi
    stage=$(mktemp -d) || { printf '%s\n' '创建升级临时目录失败。' >&2; exit 1; }
    trap 'rm -rf -- "$stage"' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM

    printf '%s\n' '正在下载最新升级入口…' >&2
    if command -v curl >/dev/null; then
        curl --disable --fail --silent --show-error --location --max-redirs 8 \
            --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 60 --retry 2 \
            --output "$stage/install-from-release.sh" -- "$launcher_url"
    elif command -v wget >/dev/null; then
        download_launcher_with_wget "$stage/install-from-release.sh" || {
            printf '%s\n' '下载升级入口失败，请检查 GitHub 连通性。' >&2
            exit 1
        }
    else
        printf '%s\n' '找不到 curl 或 wget，请先安装其中一个。' >&2
        exit 1
    fi
    if [[ ! -s $stage/install-from-release.sh ]]; then
        printf '%s\n' '下载的升级入口为空。' >&2
        exit 1
    fi
    bash "$stage/install-from-release.sh" --upgrade
)

if [[ $# -eq 0 ]]; then
    menu
    if [[ -t 0 ]]; then
        read -r -p '请输入命令编号（0-4）：' choice || exit 0
    else
        read -r -t 1 choice || exit 0
    fi
    set -- "$choice"
fi
if [[ $# -gt 2 || ( $# -eq 2 && $1 != 3 ) ]]; then
    usage_error
fi

case "$1" in
    -h|--help) usage ;;
    0) exit 0 ;;
    1) run_panel version ;;
    2) run_panel latest ;;
    3) shift; run_panel download "$@" ;;
    4) upgrade ;;
    *) usage_error ;;
esac
