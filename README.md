# 趣多多流量冠军

给每台设备分配独立的代理订阅，在网页后台查看用量、实时网速和访问记录。

## 新服务器安装

支持运行 systemd 的 Ubuntu 24.04 及以上版本、Debian 12 及以上版本，适用 Linux amd64 和 arm64。在服务器的 root 终端执行：

```bash
sbp=$(curl -qfsSL https://github.com/ruiange/sing-box-panel/releases/latest/download/install-from-release.sh) && bash -c "$sbp"
```

服务器只有 `wget` 时用这一行：

```bash
sbp=$(wget -qO- https://github.com/ruiange/sing-box-panel/releases/latest/download/install-from-release.sh) && bash -c "$sbp"
```

脚本会自动识别公网 IP、下载并校验最新版安装包、安装依赖及启动服务。首次启动成功后，终端会打印**后台地址和管理员密码**。默认后台为 `http://服务器公网IP:9443/`，登录只需密码。

请在云安全组和服务器防火墙放行默认端口：

| 用途 | TCP | UDP |
| --- | --- | --- |
| 后台及订阅 | `9443` | — |
| 代理节点 | `8443`、`8446–8454` | `8444`、`8445`、`8447`、`8455` |

默认安装会启用 13 种节点协议，后台使用 HTTP。多公网 IP、自定义端口或 HTTPS 的用法见[开发与部署参考](docs/development.md)。

## 手机访问

手机访问后台地址会自动进入 `/m`，也可以直接打开 `/m`。移动端使用底部导航，页面地址为 `/m/overview`、`/m/devices`、`/m/traffic`、`/m/analysis` 和 `/m/access`。

原来的 `/#traffic?deviceId=…` 链接可以继续使用，手机会跳转到 `/m/traffic?deviceId=…` 并保留所选设备。接口文档的移动端地址是 `/m/docs`。

## 修改网站标题

登录后台，打开「网站设置」，填写「网站标题」并点击「保存标题」。新标题会显示在登录页、后台侧栏和浏览器标签页，重启后仍会保留。未设置时显示「趣多多流量冠军」。

## 给后台绑定域名和证书

使用默认 IP + HTTP 方式安装时，可登录后台，在「网站设置」的「域名与证书」中开启域名访问。

1. 把子域名的 DNS 记录解析到这台服务器，放行 TCP `443`。使用 Cloudflare DNS 时，将该记录设为「仅 DNS」。
2. 在「域名与证书」填写域名，选择自动申请证书并保存。证书签发后，打开面板显示的 HTTPS 地址，重新登录。
3. 如果要换用自己的证书，从 HTTPS 地址进入同一页面，导入 PEM 证书链和对应的私钥。证书到期前需要手动替换。

原 IP + HTTP 地址和已分发的 IP 订阅链接仍可使用。开启后，面板新显示的订阅链接和二维码使用 HTTPS 域名，设备凭据和节点地址保持原样。也可通过原 IP 地址登录，关闭域名访问。申请条件、排查方法及反向代理场景见[域名 HTTPS 配置](docs/development.md#域名-https-配置)。

## 添加设备并导入订阅

1. 登录后台，打开「设备管理」，点击「新增设备」，填写名称并保存。
2. 在对应设备行点击「订阅」，选择小火箭（Shadowrocket）、Clash（Clash Meta/Mihomo）、sing-box 或 v2rayN。
3. 复制订阅链接，在客户端添加并更新订阅，或用支持扫码的客户端扫描面板二维码。选一个节点连接即可。

建议为每台实际设备分别新增设备记录。共用同一份订阅时，用量和访问记录会归到同一个设备。停用设备会撤销该设备的连接权限，并保留历史数据。「更新订阅链接」会使旧下载链接失效，已导入客户端的节点凭据仍可继续使用。

## 查看流量和访问记录

- **流量**：在设备行点击「流量」，或进入「流量统计」选定设备，可查看上传、下载、历史趋势和最近 60 秒网速。
- **访问记录**：在设备行点击「访问记录」，可查看经节点转发的目标域名或 IP、端口和连接时间。点击「加载更早记录」继续查看。每次连接单独保存，已写入的记录会长期保留。

## 日常管理命令

在服务器输入 `dsb` 打开菜单，也可直接带编号执行。菜单中输入 `0` 或执行 `dsb 0` 会显示「趣多多」彩色描边字和告别语。窄终端或重定向输出时显示单行告别语。

支持彩色的终端会高亮菜单、提示和结果。输出到管道或文件时自动使用纯文本，也可用 `NO_COLOR=1 dsb` 临时关闭颜色。

| 命令 | 用途 |
| --- | --- |
| `dsb 1` | 查看当前面板版本 |
| `dsb 2` | 查询 GitHub 最新发布版本 |
| `dsb 3` | 下载并校验本机架构的最新安装包，保存到当前目录。可用 `dsb 3 /tmp` 指定已有目录 |
| `sudo dsb 4` | 原地升级到最新版本 |
| `sudo dsb 5` | 查看后台管理员密码。需使用包含编号 `5` 的版本 |
| `sudo dsb 6` | 查看后台访问地址。HTTP 直连部署还会显示已配置的 HTTPS 域名入口，以及检测到的公网 IPv4 和 IPv6 |

已登录 root 时可省略 `sudo`。如果当前版本还没有 `dsb 5`，可用 root 执行下面这条命令查看密码：

```bash
sed -n 's/^PANEL_ADMIN_PASSWORD=//p' /etc/sing-box-panel/panel.env
```

旧版可用 `sed -n 's/^PANEL_PUBLIC_URL=//p' /etc/sing-box-panel/panel.env` 查看已配置的后台地址。标有「公网出口」的地址来自外网检测，如服务器在 NAT 后，还需要将网页端口转发到服务器才能从外部访问。

## 已有服务器升级

面板和节点都正常运行时，已有 `dsb` 的服务器执行 `sudo dsb 4`。如果旧版还没有 `dsb`，用 root 执行：

```bash
sbp=$(curl -qfsSL https://github.com/ruiange/sing-box-panel/releases/latest/download/install-from-release.sh) && bash -c "$sbp" -- --upgrade
```

服务器只有 `wget` 时用这一行：

```bash
sbp=$(wget -qO- https://github.com/ruiange/sing-box-panel/releases/latest/download/install-from-release.sh) && bash -c "$sbp" -- --upgrade
```

升级会先备份，再更新并重启服务，保留原密码、配置、设备、订阅和流量数据。期间节点连接可能短暂中断。看到「两个服务均已就绪」后，用 `dsb 1` 确认版本。

提示服务未就绪时，先查看状态和最近日志：

```bash
systemctl status sing-box-panel.service sing-box-panel-node.service --no-pager -l
journalctl -u sing-box-panel.service -u sing-box-panel-node.service -n 80 --no-pager
```

## 参考资料

- 离线安装、手动部署、节点端口、故障排查、本地开发与发布流程见[开发与部署参考](docs/development.md)。
- 历史版本和双架构安装包见 [GitHub Releases](https://github.com/ruiange/sing-box-panel/releases)。
