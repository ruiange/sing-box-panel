# 开发与部署参考

趣多多流量冠军的日常安装、升级和面板操作见 [使用指南](../README.md)。

- 管理后台使用 Vue 3、TypeScript、Vite、ECharts 和 pnpm。
- Go 管理接口返回 `{code,data,message}`，`code: 0` 表示成功。订阅端点返回客户端所需的原始格式。
- SQLite 保存设备、流量、访问记录和采集游标。OpenAPI 规范位于 `/openapi.yaml`，Scalar 页面位于 `/docs`。

## 本地开发

需要 Go 1.26 或更新的兼容版本、Node.js 22.12+ 和 pnpm 11。可用 `PANEL_NODE_MODE=disabled` 先开发管理流程。此模式可操作设备，采集状态为等待，订阅接口在节点配置后可用。

```bash
export PANEL_ADMIN_PASSWORD='自行设置至少 12 字节的开发密码'
export PANEL_NODE_MODE=disabled
export PANEL_DEV_ORIGIN=http://127.0.0.1:5173
go run ./cmd/panel
```

另开终端启动网页，访问 `http://127.0.0.1:5173`。

```bash
pnpm --dir web install --frozen-lockfile
pnpm --dir web dev
```

管理接口和文档也可直接访问 `http://127.0.0.1:8080/docs`。Scalar 页面从 CDN 加载固定版本的脚本。

## 打包、离线部署与修复

在有 Go 和前端构建产物的机器上打包。如果 `web/dist` 尚未生成，先运行 `pnpm --dir web install --frozen-lockfile` 和 `pnpm --dir web build`。

```bash
bash deploy/package.sh amd64
# ARM VPS 改用 bash deploy/package.sh arm64
```

将 `bin/sing-box-panel-linux-amd64.tar.gz` 上传到 VPS，在上传目录执行：

```bash
mkdir sing-box-panel-install
tar -xzf sing-box-panel-linux-amd64.tar.gz -C sing-box-panel-install
cd sing-box-panel-install
sudo bash install.sh --http --multiprotocol 203.0.113.10
```

把 `203.0.113.10` 换成你的实际公网 IP。执行 `install.sh --http 公网IP` 可只启用 VLESS Reality，此时需要放行 TCP `9443` 和 `8443`。HTTP 模式下管理员和订阅客户端都需要访问后台端口，建议将管理访问限制在可信来源。自定义端口时，在地址后依次追加节点起始端口和网页端口。IPv6 在访问地址中使用方括号。

使用在线引导脚本时也可手动指定公网 IP。将 README 安装命令末尾的 `bash -c "$sbp"` 改为 `bash -c "$sbp" -- 203.0.113.10`，并换成服务器实际地址。需要自定义端口时，继续追加节点起始端口和网页端口。例如 `bash -c "$sbp" -- 203.0.113.10 18443 19443`。

要使用自动 HTTPS，执行 `sudo bash install.sh 公网IP`（省略 `--http`），或 `sudo bash install.sh panel.example.com`。IP 模式的后台仍使用 `9443`，域名模式用标准 HTTPS 端口 `443`。两种 HTTPS 模式均须放行 TCP `443` 进行证书验证和续期，以及 `8443` 用于设备连接。域名的 DNS 记录应全部指向这台 VPS，Cloudflare DNS 使用「仅 DNS」。IP 模式在启动时申请证书，域名模式在首次访问时申请证书。执行 HTTPS 安装即同意 [Let’s Encrypt 服务条款](https://letsencrypt.org/repository/)。

脚本安装预编译产物，自动生成管理员密码和 Reality 密钥，并启动面板及专用节点。服务器需要 `iproute2`、`openssl` 及支持 JavaScript 规则的 `polkitd`，一键引导脚本会按需安装这些依赖。

安装脚本面向首次部署，遇到已有同名配置或端口占用会停止。数据位于 `/var/lib/sing-box-panel`。管理员密码在 `/etc/sing-box-panel/panel.env` 中，文件由 root 读取。服务未就绪时可运行 `sudo journalctl -u sing-box-panel.service -u sing-box-panel-node.service -u polkit -n 80`。

如果旧版安装后面板日志显示 `sing-box 应用配置失败: exit status 1`，并确认 systemd 沙箱的 `NoNewPrivileges` 阻止了 `sudo`，可从修复后的安装包解压并运行 `sudo bash repair-node-control.sh`。脚本会备份环境文件，并将节点重启切换到 polkit 授权，沿用原有数据库、面板密码和节点凭据。

离线升级时，先校验安装包的 SHA-256 并解包，再在包目录运行 `sudo bash update.sh`。脚本沿用已安装实例的配置，并在升级前备份状态。

## GitHub 自动发布

先推送待发布的提交，再推送形如 `vX.Y.Z` 的版本标签，触发 [发布工作流](../.github/workflows/release.yml)。GitHub Actions 构建前端、运行 Go 测试，并构建 Linux `amd64` 和 `arm64` 两个安装包。工作流在 amd64 上验证新机安装和从 `v0.1.3` 原地升级，并将标签写入面板二进制及引导脚本，使引导使用同一 Release 的安装包与校验文件。

两种架构的安装包、各自的 `.tar.gz.sha256`、`install-from-release.sh` 和对应的 sing-box 核心源码上传完成后，工作流公开该 Release 并将其标记为 Latest。需要手动选择发行版或镜像仓库时，可在引导脚本中使用 `--version` 或 `--repo`。

## 节点协议与端口

新服务器使用一键安装或 `install.sh --multiprotocol` 即可启用。已安装的单协议面板可使用安装包内的 `sudo bash upgrade.sh` 开启多协议。脚本先备份环境、节点配置、数据库和二进制，再启用多协议并等待监听就绪。原面板地址、设备、VLESS Reality 的 UUID、Reality 密钥和订阅 URL 保留。核心重载时连接会短暂中断。升级成功后，在客户端刷新当前订阅。

| 默认端口 | 传输 | 节点 |
| --- | --- | --- |
| 8443 | TCP | VLESS Reality / XTLS Vision |
| 8444 | UDP | Hysteria2 |
| 8445 | UDP | TUIC v5 |
| 8446 | TCP | ShadowTLS v3 + Shadowsocks |
| 8447 | TCP、UDP | Shadowsocks |
| 8448 | TCP | Trojan |
| 8449 | TCP | VMess WebSocket |
| 8450 | TCP | VLESS WebSocket TLS |
| 8451 | TCP | VLESS H2 Reality |
| 8452 | TCP | VLESS gRPC Reality |
| 8453 | TCP | AnyTLS |
| 8454 | TCP | Naive HTTP/2 |
| 8455 | UDP | Naive HTTP/3 |

默认节点端口如上，若原安装指定了不同的基准端口，额外节点在基准端口后依次加 1 到 12。HTTP 面板仍可走 `9443`，这些协议不要求服务器开放 `443`。设备上传和下载使用同一个设备标识计量。节点使用独立保存在数据目录中的证书，兼容的订阅格式内附证书或指纹。

Shadowrocket 可使用原有链接，或在基础订阅 URL 后加 `/shadowrocket`。各订阅只导出对应客户端可表达的协议，Clash/Mihomo 订阅不含 Naive。sing-box 全量订阅中的 Naive 需要客户端核心支持 `with_naive_outbound`。

## 手动部署和已有反向代理

1. 从 [sing-box 上游](https://github.com/SagerNet/sing-box/tree/v1.14.4) 构建经过验证的 v1.14.4。在源码目录运行下列命令，安装到 `/usr/local/bin/sing-box-panel-core`。确认 `version` 的 Tags 中有 `with_utls`、`with_v2ray_api`、`with_quic` 和 `with_grpc`，并与现有核心使用不同路径。

   ```bash
   go build -tags with_utls,with_v2ray_api,with_quic,with_grpc -o sing-box-panel-core ./cmd/sing-box
   ./sing-box-panel-core version
   ./sing-box-panel-core generate reality-keypair
   openssl rand -hex 8
   ```

2. 在本仓库构建面板和前端，将二进制安装到 `/usr/local/bin/sing-box-panel`，将 `web/dist` 的内容放到 `/opt/sing-box-panel/web`。

   ```bash
   mkdir -p bin
   go build -o bin/sing-box-panel ./cmd/panel
   pnpm --dir web install --frozen-lockfile
   pnpm --dir web build
   ```

3. 创建系统用户和组 `sing-box-panel`，将 [部署模板](../deploy/) 中两个 `.service` 安装到 `/etc/systemd/system/`。把 `panel.env.example` 复制到 `/etc/sing-box-panel/panel.env`，权限设为 `0600`，所有者为 root。填写管理员密码、面板 HTTPS 地址、节点公网 IP 或域名、Reality 握手域名、私钥及 short ID。订阅的 Reality 公钥由私钥推导。`PANEL_REALITY_HANDSHAKE_HOST` 默认与握手域名相同。
4. 安装 `polkitd`，将 `deploy/49-sing-box-panel.rules` 以 `0644` 权限安装到 `/etc/polkit-1/rules.d/`。该规则仅授权 `sing-box-panel` 系统账户重启 `sing-box-panel-node.service`。可执行文件、systemd unit、环境文件和 polkit 规则均由 root 管理。默认使用非特权端口 8443，若使用低于 1024 的端口，须为节点 unit 配置 `CAP_NET_BIND_SERVICE`。默认 Web 和统计端口只接受本机连接，公网仅开放 HTTPS 和 VLESS TCP 端口。`deploy/Caddyfile.example` 可作反向代理参考。只有代理会覆盖 `X-Real-IP` 且面板端口仅本机开放时，才设 `PANEL_TRUST_LOCAL_PROXY=true` 以按真实地址限制登录尝试。
5. 如需管理菜单，从仓库根目录运行 `sudo install -m 0755 -o root -g root deploy/dsb.sh /usr/local/bin/dsb`。本地编译且未注入发布版本时，`dsb 1` 显示 `dev`。
6. 执行 `sudo systemctl daemon-reload` 与 `sudo systemctl enable --now sing-box-panel.service`。面板首次启动会写入配置并启动节点服务。`journalctl -u sing-box-panel.service -u sing-box-panel-node.service` 可查看日志。

环境变量的可选项包括 `PANEL_DEV_ORIGIN`、`PANEL_REALITY_PUBLIC_KEY`（同时提供时校验与私钥匹配）、`PANEL_REALITY_HANDSHAKE_PORT`（默认 `443`）和 `PANEL_NODE_CONTROL`。部署模板使用 `PANEL_NODE_CONTROL=systemctl`。旧的 `sudo` 模式保留给自行配置节点重启权限的部署。修改核心路径或配置路径时，请同步修改节点 unit。配置和数据库所在目录应由服务账户独占写权限。

## 数据保存与采集

- Go 使用不清零的 gRPC 累计值，增量和游标在同一数据库事务中保存。配置发布前先采集，成功重载后重建采集基线。节点异常退出时，上次成功采集后的流量可能漏记。流量计在采集时按配置时区归属日期，跨零点的一个采集间隔可能落入新的一天。
- 节点生成的路由拒绝向回环地址和常见私网地址转发。订阅 URL 含访问凭据，生产反向代理应避免记录 `/sub/` 路径。备份时使用 SQLite 备份工具，或在停止面板后备份数据库文件。

## 后端验证

```bash
go test ./...
go vet ./...
TEST_SING_BOX_BINARY=/path/to/sing-box-panel-core go test ./...
TEST_SING_BOX_BINARY=/path/to/sing-box-panel-core TEST_REALITY_TARGET=https://www.example.com/ go test ./internal/panel
TEST_FULL_SING_BOX_BINARY=/path/to/sing-box-with-naive-outbound TEST_REALITY_TARGET=https://www.example.com/ go test ./internal/panel -run TestTrafficThroughEveryProfile
```
