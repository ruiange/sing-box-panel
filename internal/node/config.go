package node

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const counterUserPrefix = "device_"

var (
	deviceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
	uuidPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

// Config describes the dedicated sing-box process managed by this panel.
type Config struct {
	BinaryPath, ConfigPath, StatsAddress string
	Listen, ServerName                   string
	RealityPrivateKey, RealityShortID    string
	HandshakeServer                      string
	ListenPort, HandshakePort            int
	Profiles                             *Profiles
	// ReloadCommand is an operator-provided executable and fixed argument list.
	ReloadCommand []string
}

type Device struct {
	ID, UUID string
}

func (c *Config) prepare() error {
	if !filepath.IsAbs(c.BinaryPath) || !filepath.IsAbs(c.ConfigPath) {
		return fmt.Errorf("sing-box 可执行文件和配置文件须使用绝对路径")
	}
	statsAddress, err := netip.ParseAddrPort(c.StatsAddress)
	if err != nil || !statsAddress.Addr().IsLoopback() || statsAddress.Port() == 0 || statsAddress.Addr().Zone() != "" {
		return fmt.Errorf("统计 API 地址须为带端口的本机回环 IP")
	}
	c.StatsAddress = statsAddress.String()

	listen, err := netip.ParseAddr(c.Listen)
	if err != nil || listen.Zone() != "" {
		return fmt.Errorf("VLESS 监听地址须为 IP")
	}
	c.Listen = listen.String()
	if !validPort(c.ListenPort) || !validPort(c.HandshakePort) {
		return fmt.Errorf("VLESS 或 Reality 握手端口无效")
	}
	if c.ListenPort == int(statsAddress.Port()) && (listen.IsUnspecified() || listen.Unmap() == statsAddress.Addr().Unmap()) {
		return fmt.Errorf("VLESS 端口与统计 API 端口冲突")
	}
	if !validHost(c.ServerName) || !validHost(c.HandshakeServer) {
		return fmt.Errorf("Reality 服务名称或握手主机无效")
	}
	privateKey, err := base64.RawURLEncoding.Strict().DecodeString(c.RealityPrivateKey)
	if err != nil || len(privateKey) != 32 {
		return fmt.Errorf("Reality 私钥须为 Base64URL 编码的 32 字节密钥")
	}
	shortID, err := hex.DecodeString(c.RealityShortID)
	if err != nil || len(shortID) < 1 || len(shortID) > 8 {
		return fmt.Errorf("Reality short ID 须为 2 至 16 位的偶数位十六进制")
	}

	if len(c.ReloadCommand) == 0 {
		c.ReloadCommand = []string{"/usr/bin/systemctl", "restart", "sing-box-panel-node.service"}
	}
	if !filepath.IsAbs(c.ReloadCommand[0]) {
		return fmt.Errorf("sing-box 重载命令须使用绝对路径")
	}
	c.ReloadCommand = slices.Clone(c.ReloadCommand)
	if c.Profiles != nil {
		profiles := *c.Profiles
		if err := profiles.validate(c.ListenPort, int(statsAddress.Port())); err != nil {
			return err
		}
		c.Profiles = &profiles
	}
	return nil
}

func validPort(port int) bool { return port > 0 && port <= 65535 }

func validHost(host string) bool {
	if strings.TrimSpace(host) != host || host == "" || strings.ContainsAny(host, " /\\?#@[]\r\n\t") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	return !strings.Contains(host, ":")
}

func renderConfig(c Config, devices []Device) ([]byte, error) {
	devices = slices.Clone(devices)
	slices.SortFunc(devices, func(a, b Device) int { return strings.Compare(a.ID, b.ID) })
	users := make([]map[string]string, 0, len(devices))
	statsUsers := make([]string, 0, len(devices))
	usedUUIDs := make(map[string]bool, len(devices))
	for index, device := range devices {
		if !deviceIDPattern.MatchString(device.ID) || !uuidPattern.MatchString(device.UUID) {
			return nil, fmt.Errorf("设备标识或 VLESS UUID 格式无效")
		}
		uuid := strings.ToLower(device.UUID)
		if usedUUIDs[uuid] || (index > 0 && devices[index-1].ID == device.ID) {
			return nil, fmt.Errorf("设备标识或 VLESS UUID 重复")
		}
		usedUUIDs[uuid] = true
		name := counterUserPrefix + device.ID
		users = append(users, map[string]string{
			"name": name, "uuid": uuid, "flow": "xtls-rprx-vision",
		})
		statsUsers = append(statsUsers, name)
	}

	// Resolve domain-based destinations before applying the private IP rule.
	// A subscriber must not be able to reach the loopback-only Stats API.
	document := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": []map[string]any{{"type": "local", "tag": "system-dns"}},
			"final":   "system-dns",
		},
		"inbounds": []map[string]any{{
			"type": "vless", "tag": "panel-vless", "listen": c.Listen,
			"listen_port": c.ListenPort,
			"users":       users,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": c.ServerName,
				"reality": map[string]any{
					"enabled": true,
					"handshake": map[string]any{
						"server": c.HandshakeServer, "server_port": c.HandshakePort,
					},
					"private_key": c.RealityPrivateKey,
					"short_id":    []string{c.RealityShortID},
				},
			},
		}},
		"outbounds": []map[string]string{{"type": "direct", "tag": "direct"}},
		"route": map[string]any{
			"rules": []map[string]any{
				{"action": "resolve", "server": "system-dns"},
				{"ip_is_private": true, "action": "reject"},
				{"ip_cidr": []string{"0.0.0.0/8", "100.64.0.0/10"}, "action": "reject"},
				{"network": "udp", "action": "route-options", "udp_connect": true},
			},
			"final": "direct",
		},
		"experimental": map[string]any{
			"v2ray_api": map[string]any{
				"listen": c.StatsAddress,
				"stats":  map[string]any{"enabled": true, "users": statsUsers},
			},
		},
	}
	if c.Profiles != nil {
		existing := document["inbounds"].([]map[string]any)
		document["inbounds"] = append(existing, renderProfiles(c, devices)...)
	}
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}
