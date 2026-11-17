package node

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const ShadowsocksMethod = "2022-blake3-aes-128-gcm"

// Profiles enables the extra public nodes alongside the original VLESS Reality
// listener. Each entry is a separate client-selectable profile.
type Profiles struct {
	Hysteria2, TUIC, ShadowTLS, Shadowsocks, Trojan, VMessWS, VLESSWSTLS int
	H2Reality, GRPCReality, AnyTLS, NaiveHTTP2, NaiveHTTP3               int
	TLSName, TLSCertificatePath, TLSKeyPath, TLSCertificatePEM, TLSPin   string
	SSBaseKey, ShadowSSBaseKey, WSPath, GRPCService                      string
	ShadowTLSServerName                                                  string
}

// NewProfiles assigns the additional ports in subscription order.
func NewProfiles(basePort int) Profiles {
	return Profiles{
		Hysteria2: basePort + 1, TUIC: basePort + 2, ShadowTLS: basePort + 3,
		Shadowsocks: basePort + 4, Trojan: basePort + 5, VMessWS: basePort + 6,
		VLESSWSTLS: basePort + 7, H2Reality: basePort + 8, GRPCReality: basePort + 9,
		AnyTLS: basePort + 10, NaiveHTTP2: basePort + 11, NaiveHTTP3: basePort + 12,
		WSPath: "/panel", GRPCService: "panel-grpc",
	}
}

var (
	profilePathPattern = regexp.MustCompile(`^/[a-zA-Z0-9_/-]+$`)
	grpcServicePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

func (p *Profiles) validate(basePort, statsPort int) error {
	seen := map[int]bool{basePort: true, statsPort: true}
	for _, port := range p.ports() {
		if !validPort(port) || seen[port] {
			return fmt.Errorf("多协议入站端口无效或冲突: %d", port)
		}
		seen[port] = true
	}
	if !validHost(p.TLSName) || !filepath.IsAbs(p.TLSCertificatePath) || !filepath.IsAbs(p.TLSKeyPath) {
		return fmt.Errorf("多协议 TLS 服务名称或证书绝对路径无效")
	}
	for _, key := range []string{p.SSBaseKey, p.ShadowSSBaseKey} {
		decoded, err := base64.StdEncoding.Strict().DecodeString(key)
		if err != nil || len(decoded) != 16 {
			return fmt.Errorf("多协议 Shadowsocks 服务端密钥须为标准 Base64 编码的 16 字节密钥")
		}
	}
	if !profilePathPattern.MatchString(p.WSPath) || !grpcServicePattern.MatchString(p.GRPCService) {
		return fmt.Errorf("多协议 WebSocket 路径或 gRPC 服务名称无效")
	}
	if p.ShadowTLSServerName != "" && !validHost(p.ShadowTLSServerName) {
		return fmt.Errorf("ShadowTLS 握手服务器名称无效")
	}
	return nil
}

func (p *Profiles) ports() []int {
	return []int{p.Hysteria2, p.TUIC, p.ShadowTLS, p.Shadowsocks, p.Trojan, p.VMessWS,
		p.VLESSWSTLS, p.H2Reality, p.GRPCReality, p.AnyTLS, p.NaiveHTTP2, p.NaiveHTTP3}
}

// Credentials remain stable across restarts and when a device is disabled and
// re-enabled. A protocol only receives the secret needed for its own handshake.
type Credentials struct {
	UUID, VMessUUID, Username, Hysteria2, TUIC, ShadowTLS, ShadowSS, Shadowsocks, Trojan, AnyTLS, Naive string
}

func (d Device) Credentials() Credentials {
	key := strings.ToLower(d.UUID)
	derive := func(label string) []byte {
		return credentialMAC(key, label)
	}
	password := func(label string) string {
		return base64.RawURLEncoding.EncodeToString(derive(label))
	}
	ssKey := func(label string) string {
		return base64.StdEncoding.EncodeToString(derive(label)[:16])
	}
	vmess := uuid.UUID(derive("vmess")[:16])
	vmess[6] = vmess[6]&0x0f | 0x40
	vmess[8] = vmess[8]&0x3f | 0x80
	return Credentials{
		UUID: key, VMessUUID: vmess.String(), Username: counterUserPrefix + d.ID,
		Hysteria2: password("hysteria2"), TUIC: password("tuic"),
		ShadowTLS: password("shadowtls"), ShadowSS: ssKey("shadow-ss"),
		Shadowsocks: ssKey("shadowsocks"), Trojan: password("trojan"),
		AnyTLS: password("anytls"), Naive: password("naive"),
	}
}

func credentialMAC(key, label string) []byte {
	h := hmac.New(sha256.New, []byte(key))
	_, _ = h.Write([]byte("sing-box-panel/credential/" + label))
	return h.Sum(nil)
}

func renderProfiles(c Config, devices []Device) []map[string]any {
	p := *c.Profiles
	credentials := make([]Credentials, 0, len(devices))
	for _, device := range devices {
		credentials = append(credentials, device.Credentials())
	}
	passwordUsers := func(password func(Credentials) string) []map[string]string {
		users := make([]map[string]string, 0, len(credentials))
		for _, credential := range credentials {
			users = append(users, map[string]string{"name": credential.Username, "password": password(credential)})
		}
		return users
	}
	vlessUsers := make([]map[string]string, 0, len(credentials))
	vmessUsers := make([]map[string]any, 0, len(credentials))
	tuicUsers := make([]map[string]string, 0, len(credentials))
	naiveUsers := make([]map[string]string, 0, len(credentials))
	for _, credential := range credentials {
		vlessUsers = append(vlessUsers, map[string]string{"name": credential.Username, "uuid": credential.UUID})
		vmessUsers = append(vmessUsers, map[string]any{"name": credential.Username, "uuid": credential.VMessUUID, "alterId": 0})
		tuicUsers = append(tuicUsers, map[string]string{"name": credential.Username, "uuid": credential.UUID, "password": credential.TUIC})
		naiveUsers = append(naiveUsers, map[string]string{"username": credential.Username, "password": credential.Naive})
	}

	shadowTLSUsers := passwordUsers(func(credential Credentials) string { return credential.ShadowTLS })
	shadowSSUsers := passwordUsers(func(credential Credentials) string { return credential.ShadowSS })
	ssUsers := passwordUsers(func(credential Credentials) string { return credential.Shadowsocks })
	if len(credentials) == 0 {
		// Naive and ShadowTLS v3 require a user. Shadowsocks uses its base
		// key as a single-user password on an empty user list, which would
		// allow connections from old subscriptions after disabling everyone.
		// These secrets are independent of the advertised keys and are never
		// attached to an actual device or included in the stats allowlist.
		const unused = "panel-no-active-device"
		shadowTLSUsers = append(shadowTLSUsers, map[string]string{"name": unused, "password": base64.RawURLEncoding.EncodeToString(credentialMAC(c.RealityPrivateKey, "empty-shadowtls"))})
		shadowSSUsers = append(shadowSSUsers, map[string]string{"name": unused, "password": base64.StdEncoding.EncodeToString(credentialMAC(c.RealityPrivateKey, "empty-shadow-ss")[:16])})
		ssUsers = append(ssUsers, map[string]string{"name": unused, "password": base64.StdEncoding.EncodeToString(credentialMAC(c.RealityPrivateKey, "empty-ss")[:16])})
		naiveUsers = append(naiveUsers, map[string]string{"username": unused, "password": base64.RawURLEncoding.EncodeToString(credentialMAC(c.RealityPrivateKey, "empty-naive"))})
	}

	inbound := func(protocol, tag string, port int, users any) map[string]any {
		return map[string]any{
			"type": protocol, "tag": "panel-" + tag, "listen": c.Listen,
			"listen_port": port, "users": users,
		}
	}
	tls := func(alpn ...string) map[string]any {
		options := map[string]any{
			"enabled": true, "server_name": p.TLSName,
			"certificate_path": p.TLSCertificatePath, "key_path": p.TLSKeyPath,
		}
		if len(alpn) != 0 {
			options["alpn"] = alpn
		}
		return options
	}
	handshake := map[string]any{"server": c.HandshakeServer, "server_port": c.HandshakePort}
	shadowHandshake := handshake
	if p.ShadowTLSServerName != "" {
		shadowHandshake = map[string]any{"server": p.ShadowTLSServerName, "server_port": 443}
	}
	reality := func() map[string]any {
		return map[string]any{
			"enabled": true, "server_name": c.ServerName, "alpn": []string{"h2"},
			"reality": map[string]any{"enabled": true, "handshake": handshake, "private_key": c.RealityPrivateKey, "short_id": []string{c.RealityShortID}},
		}
	}
	ws := map[string]any{"type": "ws", "path": p.WSPath}

	hysteria2 := inbound("hysteria2", "hysteria2", p.Hysteria2, passwordUsers(func(credential Credentials) string { return credential.Hysteria2 }))
	hysteria2["tls"] = tls("h3")
	tuic := inbound("tuic", "tuic", p.TUIC, tuicUsers)
	tuic["congestion_control"], tuic["tls"] = "bbr", tls("h3")
	shadowTLS := inbound("shadowtls", "shadowtls", p.ShadowTLS, shadowTLSUsers)
	shadowTLS["version"], shadowTLS["strict_mode"], shadowTLS["handshake"], shadowTLS["detour"] = 3, true, shadowHandshake, "panel-shadowss"
	shadowSS := inbound("shadowsocks", "shadowss", 0, shadowSSUsers)
	shadowSS["network"], shadowSS["listen"], shadowSS["method"], shadowSS["password"] = "tcp", "127.0.0.1", ShadowsocksMethod, p.ShadowSSBaseKey
	ss := inbound("shadowsocks", "ss", p.Shadowsocks, ssUsers)
	ss["method"], ss["password"] = ShadowsocksMethod, p.SSBaseKey
	trojan := inbound("trojan", "trojan", p.Trojan, passwordUsers(func(credential Credentials) string { return credential.Trojan }))
	trojan["tls"] = tls()
	vmess := inbound("vmess", "vmess-ws", p.VMessWS, vmessUsers)
	vmess["transport"] = ws
	vlessWS := inbound("vless", "vless-ws-tls", p.VLESSWSTLS, vlessUsers)
	vlessWS["transport"], vlessWS["tls"] = ws, tls("http/1.1")
	h2 := inbound("vless", "h2-reality", p.H2Reality, vlessUsers)
	h2["transport"], h2["tls"] = map[string]any{"type": "http", "path": p.WSPath, "host": []string{c.ServerName}}, reality()
	grpc := inbound("vless", "grpc-reality", p.GRPCReality, vlessUsers)
	grpc["transport"], grpc["tls"] = map[string]any{"type": "grpc", "service_name": p.GRPCService}, reality()
	anyTLS := inbound("anytls", "anytls", p.AnyTLS, passwordUsers(func(credential Credentials) string { return credential.AnyTLS }))
	anyTLS["tls"] = tls()
	naiveHTTP2 := inbound("naive", "naive-h2", p.NaiveHTTP2, naiveUsers)
	naiveHTTP2["network"], naiveHTTP2["tls"] = "tcp", tls("h2")
	naiveHTTP3 := inbound("naive", "naive-h3", p.NaiveHTTP3, naiveUsers)
	naiveHTTP3["network"], naiveHTTP3["tls"] = "udp", tls("h3")

	return []map[string]any{hysteria2, tuic, shadowTLS, shadowSS, ss, trojan, vmess, vlessWS, h2, grpc, anyTLS, naiveHTTP2, naiveHTTP3}
}
