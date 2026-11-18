#!/usr/bin/env bash
# sing-box-panel management command
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

panel=/usr/local/bin/sing-box-panel
config=/etc/sing-box-panel/panel.env
launcher_url=https://github.com/ruiange/sing-box-panel/releases/latest/download/install-from-release.sh

menu() {
    printf '%s\n' \
        '============== 趣多多流量冠军 ==============' \
        '(1) 查看当前版本' \
        '(2) 查询最新版本' \
        '(3) 下载最新安装包' \
        '(4) 一键升级到最新版本' \
        '(5) 查看后台密码' \
        '(6) 查看后台地址' \
        '(0) 退出' \
        '============================================'
}

usage() {
    printf '%s\n' '用法：dsb [0|1|2|3 [下载目录]|4|5|6]' '运行 dsb 打开菜单，也可直接传入编号。'
}

usage_error() {
    usage >&2
    exit 2
}

run_panel() {
    if [[ ! -x $panel ]]; then
        printf '%s\n' '找不到趣多多流量冠军程序，请先安装面板。' >&2
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

require_config() {
    local label=$1 number=$2
    if [[ $EUID -ne 0 ]]; then
        printf '查看%s需要 root 权限，请运行 sudo dsb %s。\n' "$label" "$number" >&2
        return 1
    fi
    if [[ ! -f $config || ! -r $config ]]; then
        printf '无法读取面板配置 %s。\n' "$config" >&2
        return 1
    fi
}

read_setting() {
    awk -v key="$1" '
        $0 ~ ("^[[:blank:]]*" key "[[:blank:]]*=") {
            value = substr($0, index($0, "=") + 1)
            sub(/^[[:blank:]]+/, "", value)
            sub(/[[:blank:]\r]+$/, "", value)
            quote = substr(value, 1, 1)
            if ((quote == "\"" || quote == "\047") && length(value) >= 2 &&
                substr(value, length(value)) == quote) {
                value = substr(value, 2, length(value) - 2)
            }
        }
        END {
            if (value == "") exit 1
            print value
        }
    ' "$config"
}

show_setting() {
    local key=$1 label=$2 number=$3 value
    require_config "$label" "$number" || return 1
    if ! value=$(read_setting "$key"); then
        printf '未能读取%s。\n' "$label" >&2
        return 1
    fi
    printf '%s：%s\n' "$label" "$value"
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
    elif command -v wget >/dev/null && command -v timeout >/dev/null; then
        timeout 7 wget --no-config "-$family" --no-proxy --quiet --max-redirect=0 \
            --timeout=5 --tries=1 --output-document=- -- "$url"
    else
        return 1
    fi
}

validated_family_address() {
    local family=$1 address=$2
    [[ -n $address && -x $panel ]] || return 1
    address=$("$panel" validate-address "$address" 2>/dev/null) || return 1
    case "$family:$address" in
        4:*.*) [[ $address != *:* ]] || return 1 ;;
        6:*:*) ;;
        *) return 1 ;;
    esac
    printf '%s\n' "$address"
}

show_addresses() {
    local public_url listen port family target lookup source address label
    local ipv4_url='^http://[0-9.]+:([0-9]+)/?$' ipv6_url='^http://\[[[:xdigit:]:]+\]:([0-9]+)/?$'
    require_config 后台地址 6 || return 1
    if ! public_url=$(read_setting PANEL_PUBLIC_URL); then
        printf '%s\n' '未能读取后台地址。' >&2
        return 1
    fi
    printf '后台地址：%s\n' "$public_url"

    # An HTTPS certificate or reverse proxy may only cover the configured host.
    [[ $(read_setting PANEL_HTTPS_MODE) == http ]] || return 0
    if [[ $public_url =~ $ipv6_url ]]; then
        port=${BASH_REMATCH[1]} family=4 target=1.1.1.1 lookup=https://api.ipify.org
    elif [[ $public_url =~ $ipv4_url ]]; then
        port=${BASH_REMATCH[1]} family=6 target=2606:4700:4700::1111 lookup=https://api6.ipify.org
    else
        return 0
    fi

    listen=$(read_setting PANEL_LISTEN) || listen=:$port
    case "$listen" in
        ":$port"|"[::]:$port") ;;
        "0.0.0.0:$port") [[ $family == 4 ]] || return 0 ;;
        *) return 0 ;;
    esac
    [[ -x $panel ]] || return 0

    label=IPv$family
    if ! { source=$(route_source "$family" "$target") &&
        address=$(validated_family_address "$family" "$source"); }; then
        source=$(query_public_ip "$family" "$lookup") &&
            address=$(validated_family_address "$family" "$source") || return 0
        label+='，公网出口'
    fi
    [[ $family == 4 ]] || address=[$address]
    printf '后台地址（%s）：http://%s:%s\n' "$label" "$address" "$port"
}

if [[ $# -eq 0 ]]; then
    menu
    if [[ -t 0 ]]; then
        read -r -p '请输入命令编号（0-6）：' choice || exit 0
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
    0) printf '%s\n' '我是趣多多 我为你的流量护航～' ;;
    1) run_panel version ;;
    2) run_panel latest ;;
    3) shift; run_panel download "$@" ;;
    4) upgrade ;;
    5) show_setting PANEL_ADMIN_PASSWORD 后台管理员密码 5 ;;
    6) show_addresses ;;
    *) usage_error ;;
esac
