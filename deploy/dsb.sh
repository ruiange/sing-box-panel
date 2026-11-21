#!/usr/bin/env bash
# sing-box-panel management command
set -euo pipefail
export PATH=/usr/sbin:/usr/bin:/sbin:/bin
umask 077

panel=/usr/local/bin/sing-box-panel
config=/etc/sing-box-panel/panel.env
launcher_url=https://github.com/ruiange/sing-box-panel/releases/latest/download/install-from-release.sh

print_color() {
    local style=$1 text=$2
    if [[ -t 1 && ${TERM:-dumb} != dumb && -z ${NO_COLOR:-} ]]; then
        printf '\033[%sm%s\033[0m' "$style" "$text"
    else
        printf '%s' "$text"
    fi
}

print_line() {
    print_color "$@"
    printf '\n'
}

print_result() {
    print_color '1;36' "$1："
    printf '%s\n' "$2"
}

farewell() {
    local size row
    # Three 22-column glyphs with two spaces between them. Leave one column to avoid wrapping.
    if [[ -t 1 && ${TERM:-dumb} != dumb ]] &&
        { size=$(stty size <&3 2>/dev/null); } 3>&1 &&
        [[ ${size##* } -gt 70 ]]; then
        local -a qu=(
            '    ⣶⢲⠄⢠⣄⣀⣀⣀⣀⣀⣠⣄      '
            ' ⢀⣀⣀⣿⢸⣀⣸⣶⡆⡶⠒⢲⢠⣶⣟⣀⣀⣀⣀⡀ '
            ' ⠸⠭⠭⣥⢨⠭⠽⢿⡇⣇⣀⣸⢸⡿⠶⠶⠶⢶⢄⡇ '
            ' ⣀⣀⣀⣿⢸⣀⣀⣸⡇⡭⠭⢭⢸⣧⡶⣆ ⣼⢸⠇ '
            ' ⠯⠭⠭⢭⢨⡭⠭⢿⡇⡇ ⢸⢸⡇⢻⡝⣧⡏⡿  '
            '  ⡶⣦⢸⢸⣇⣀⣸⡇⣭⣭⣭⢸⡇ ⠹⡎⢹⠇  '
            '  ⡇⡏⢸⢨⡭⠭⢿⡇⡇ ⢸⢸⣧⡄⣼⢣⡜⣧  '
            ' ⢸⡇⢷⣼⢸⡇⣠⣼⠇⢷⣚⣯⢠⣶⡿⣣⠟⢻⡜⣇ '
            ' ⣼⢰⣿⣟⢸⡇⠻⠖⠛⠉⠁⢸⢸⣿⣵⠋  ⠻⠋ '
            '⢠⡏⡾ ⠙⢶⣝⡓⠶⠤⢤⣤⣼⣾⣧⣤⣤⣤⠤⠤⢤⡆'
            '⠘⠻⠃   ⠈⠉⠛⠓⠒⠒⠒⠒⠒⠒⠒⠒⠒⠒⠚⠁'
        )
        local -a duo=(
            '        ⢀⡴⢶⡤          '
            '      ⣀⡴⢛⣐⣛⣒⣒⣒⣒⡒⠒⣦    '
            '   ⣀⡴⢞⣫⣾⠋⠉⠉⠉⠉⣩⡿⣣⡾⠋    '
            '⢰⣶⣛⣭⠶⠋⠿⣯⣛⢦⣄⣴⢞⣩⠞⠋      '
            ' ⠉⠉   ⢀⣠⡽⢗⣨⡶⣫⡿⣲⡦      '
            ' ⢀⣀⣤⠴⢞⣋⣥⠶⢋⣥⠾⣋⣘⣛⣒⣒⣒⣒⡒⢳⡆'
            '⠘⣯⡥⠶⠚⣋⣡⡴⢞⣫⣵⣟⠁   ⢀⣴⢟⣵⠟ '
            '   ⢾⣟⣭⠵⠞⠋⠙⠿⣭⡳⢦⣤⡶⣋⡴⠟⠁  '
            '    ⠉    ⢀⣠⡤⠟⣢⡴⠞⠋     '
            '  ⣀⣠⣤⡤⠶⢖⣛⣭⡵⠞⠋⠉        '
            '  ⠘⠷⠶⠚⠛⠉⠉             '
        )
        for row in "${!qu[@]}"; do
            print_color '1;36' "${qu[row]}"
            printf '  '
            print_color '1;33' "${duo[row]}"
            printf '  '
            print_line '1;35' "${duo[row]}"
        done
        printf '\n'
    fi
    print_line 32 '我是趣多多 我为你的流量护航～'
}

menu() {
    local number
    local -a labels=(退出 查看当前版本 查询最新版本 下载最新安装包 一键升级到最新版本 查看后台密码 查看后台地址)
    print_line '1;36' '============== 趣多多流量冠军 =============='
    for number in 1 2 3 4 5 6 0; do
        print_color 36 "($number)"
        printf ' %s\n' "${labels[number]}"
    done
    print_line 36 '============================================'
}

usage() {
    print_line "${1:-36}" '用法：dsb [0|1|2|3 [下载目录]|4|5|6]'
    printf '%s\n' '运行 dsb 打开菜单，也可直接传入编号。'
}

usage_error() {
    usage 31 >&2
    exit 2
}

run_panel() {
    if [[ ! -x $panel ]]; then
        print_line 31 '找不到趣多多流量冠军程序，请先安装面板。' >&2
        exit 1
    fi
    DSB_COLOR=1 exec "$panel" "$@"
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
        print_line 31 '升级需要 root 权限，请运行 sudo dsb 4。' >&2
        exit 1
    fi
    stage=$(mktemp -d) || { print_line 31 '创建升级临时目录失败。' >&2; exit 1; }
    trap 'rm -rf -- "$stage"' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM

    print_line 33 '正在下载最新升级入口…' >&2
    if command -v curl >/dev/null; then
        curl --disable --fail --silent --show-error --location --max-redirs 8 \
            --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 60 --retry 2 \
            --output "$stage/install-from-release.sh" -- "$launcher_url"
    elif command -v wget >/dev/null; then
        download_launcher_with_wget "$stage/install-from-release.sh" || {
            print_line 31 '下载升级入口失败，请检查 GitHub 连通性。' >&2
            exit 1
        }
    else
        print_line 31 '找不到 curl 或 wget，请先安装其中一个。' >&2
        exit 1
    fi
    if [[ ! -s $stage/install-from-release.sh ]]; then
        print_line 31 '下载的升级入口为空。' >&2
        exit 1
    fi
    bash "$stage/install-from-release.sh" --upgrade
)

require_config() {
    local label=$1 number=$2
    if [[ $EUID -ne 0 ]]; then
        print_line 31 "查看${label}需要 root 权限，请运行 sudo dsb ${number}。" >&2
        return 1
    fi
    if [[ ! -f $config || ! -r $config ]]; then
        print_line 31 "无法读取面板配置 ${config}。" >&2
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
        print_line 31 "未能读取${label}。" >&2
        return 1
    fi
    print_result "$label" "$value"
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
    local database domain_url
    local ipv4_url='^http://[0-9.]+:([0-9]+)/?$' ipv6_url='^http://\[[[:xdigit:]:]+\]:([0-9]+)/?$'
    require_config 后台地址 6 || return 1
    if ! public_url=$(read_setting PANEL_PUBLIC_URL); then
        print_line 31 '未能读取后台地址。' >&2
        return 1
    fi
    print_result 后台地址 "$public_url"

    # An HTTPS certificate or reverse proxy may only cover the configured host.
    [[ $(read_setting PANEL_HTTPS_MODE) == http ]] || return 0

    # Resolve relative paths from the same working directory as the panel service.
    database=$(read_setting PANEL_DATABASE) || database=./data/panel.db
    [[ $database == /* ]] || database=/var/lib/sing-box-panel/$database
    # Read the saved HTTPS settings as the service account.
    if [[ -x $panel ]] &&
        domain_url=$(timeout 3 setpriv --reuid=sing-box-panel --regid=sing-box-panel \
            --clear-groups --no-new-privs -- "$panel" domain-url "$database" 2>/dev/null) &&
        [[ -n $domain_url ]]; then
        print_result '后台地址（域名）' "$domain_url"
    fi

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
    print_result "后台地址（${label}）" "http://${address}:${port}"
}

if [[ $# -eq 0 ]]; then
    menu
    if [[ -t 0 ]]; then
        print_color '1;33' '请输入命令编号（0-6）：' >&2
        read -r choice || exit 0
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
    0) farewell ;;
    1) run_panel version ;;
    2) run_panel latest ;;
    3) shift; run_panel download "$@" ;;
    4) upgrade ;;
    5) show_setting PANEL_ADMIN_PASSWORD 后台管理员密码 5 ;;
    6) show_addresses ;;
    *) usage_error ;;
esac
