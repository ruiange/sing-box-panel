package panel

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node"
)

const nodeUnit = "sing-box-panel-node.service"

type Config struct {
	Listen, DatabasePath, WebDir, Password string
	PublicURL, DevOrigin                   string
	HTTPSHost, ACMECache, ACMEEmail        string
	IPHTTPS, DirectHTTP                    bool
	SecureCookie, TrustLocalProxy          bool
	Zone                                   *time.Location
	PollInterval                           time.Duration
	Node                                   *node.Config
	Endpoint                               Endpoint
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Listen:       env("PANEL_LISTEN", "127.0.0.1:8080"),
		DatabasePath: env("PANEL_DATABASE", "./data/panel.db"),
		WebDir:       env("PANEL_WEB_DIR", "./web/dist"),
		Password:     os.Getenv("PANEL_ADMIN_PASSWORD"),
	}
	if trust := os.Getenv("PANEL_TRUST_LOCAL_PROXY"); trust != "" {
		var err error
		cfg.TrustLocalProxy, err = strconv.ParseBool(trust)
		if err != nil {
			return cfg, fmt.Errorf("PANEL_TRUST_LOCAL_PROXY 必须是 true 或 false")
		}
	}
	if len(cfg.Password) < 12 {
		return cfg, fmt.Errorf("PANEL_ADMIN_PASSWORD 至少需要 12 字节")
	}
	mode := env("PANEL_HTTPS_MODE", "proxy")
	var err error
	cfg.PublicURL, cfg.SecureCookie, err = origin(env("PANEL_PUBLIC_URL", "http://127.0.0.1:8080"), mode == "http")
	if err != nil {
		return cfg, fmt.Errorf("PANEL_PUBLIC_URL: %w", err)
	}
	if err := cfg.configureListener(mode); err != nil {
		return cfg, err
	}
	if dev := os.Getenv("PANEL_DEV_ORIGIN"); dev != "" {
		cfg.DevOrigin, _, err = origin(dev, false)
		if err != nil {
			return cfg, fmt.Errorf("PANEL_DEV_ORIGIN: %w", err)
		}
	}
	cfg.Zone, err = time.LoadLocation(env("PANEL_TIME_ZONE", "Asia/Shanghai"))
	if err != nil {
		return cfg, fmt.Errorf("PANEL_TIME_ZONE: %w", err)
	}
	cfg.PollInterval, err = time.ParseDuration(env("PANEL_POLL_INTERVAL", "15s"))
	if err != nil || cfg.PollInterval < time.Second || cfg.PollInterval > time.Minute {
		return cfg, fmt.Errorf("PANEL_POLL_INTERVAL 必须介于 1s 和 1m")
	}
	switch env("PANEL_NODE_MODE", "managed") {
	case "disabled":
		return cfg, nil
	case "managed":
	default:
		return cfg, fmt.Errorf("PANEL_NODE_MODE 只能是 managed 或 disabled")
	}
	port, err := readPort("PANEL_NODE_PORT", "8443")
	if err != nil {
		return cfg, err
	}
	if cfg.IPHTTPS || cfg.DirectHTTP {
		_, webPort, _ := net.SplitHostPort(cfg.Listen)
		if port < 1024 || webPort == strconv.Itoa(port) {
			return cfg, fmt.Errorf("节点端口必须在 1024 至 65535 之间，且与网页端口分开")
		}
	}
	handshakePort, err := readPort("PANEL_REALITY_HANDSHAKE_PORT", "443")
	if err != nil {
		return cfg, err
	}
	serverName := strings.TrimSpace(os.Getenv("PANEL_REALITY_SERVER_NAME"))
	pubKey := strings.TrimSpace(os.Getenv("PANEL_REALITY_PUBLIC_KEY"))
	privKey := strings.TrimSpace(os.Getenv("PANEL_REALITY_PRIVATE_KEY"))
	shortID := strings.TrimSpace(os.Getenv("PANEL_REALITY_SHORT_ID"))
	host := strings.TrimSpace(os.Getenv("PANEL_NODE_HOST"))
	if !validHost(host) || !validDNSName(serverName) {
		return cfg, fmt.Errorf("PANEL_NODE_HOST 和 PANEL_REALITY_SERVER_NAME 必须填写有效地址")
	}
	privateBytes, err := base64.RawURLEncoding.Strict().DecodeString(privKey)
	if err != nil {
		return cfg, fmt.Errorf("PANEL_REALITY_PRIVATE_KEY 必须是有效的 Reality 私钥")
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(privateBytes)
	if err != nil {
		return cfg, fmt.Errorf("PANEL_REALITY_PRIVATE_KEY 必须是有效的 Reality 私钥")
	}
	derived := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes())
	if pubKey != "" && pubKey != derived {
		return cfg, fmt.Errorf("PANEL_REALITY_PUBLIC_KEY 与私钥不匹配")
	}
	pubKey = derived
	if raw, err := hex.DecodeString(shortID); err != nil || len(raw) != 8 {
		return cfg, fmt.Errorf("PANEL_REALITY_SHORT_ID 必须为 16 位十六进制字符串")
	}
	statsAddress := env("PANEL_STATS_ADDRESS", "127.0.0.1:10085")
	if cfg.IPHTTPS || cfg.DirectHTTP {
		_, webPort, _ := net.SplitHostPort(cfg.Listen)
		_, statsPort, _ := net.SplitHostPort(statsAddress)
		if statsPort == "443" || statsPort == webPort {
			return cfg, fmt.Errorf("统计端口不能为 443 或网页端口")
		}
	}
	cfg.Endpoint = Endpoint{Host: host, Port: port, ServerName: serverName, PublicKey: pubKey, ShortID: shortID}
	_, listenPort, _ := net.SplitHostPort(cfg.Listen)
	panelPort, _ := strconv.Atoi(listenPort)
	cfg.Node = &node.Config{
		BinaryPath:         env("PANEL_SING_BOX_BINARY", "/usr/local/bin/sing-box-panel-core"),
		ConfigPath:         env("PANEL_SING_BOX_CONFIG", "/var/lib/sing-box-panel/node.json"),
		StatsAddress:       statsAddress,
		Listen:             env("PANEL_NODE_LISTEN", "::"),
		ListenPort:         port,
		ServerName:         serverName,
		RealityPrivateKey:  privKey,
		RealityShortID:     shortID,
		HandshakeServer:    env("PANEL_REALITY_HANDSHAKE_HOST", serverName),
		HandshakePort:      handshakePort,
		RecordDestinations: true,
		PanelPort:          panelPort,
	}
	if err := cfg.configureProfiles(privKey); err != nil {
		return cfg, err
	}

	switch env("PANEL_NODE_CONTROL", "sudo") {
	case "sudo":
		cfg.Node.ReloadCommand = []string{"/usr/bin/sudo", "-n", "/usr/bin/systemctl", "restart", nodeUnit}
	case "systemctl":
		cfg.Node.ReloadCommand = []string{"/usr/bin/systemctl", "restart", nodeUnit}
	default:
		return cfg, fmt.Errorf("PANEL_NODE_CONTROL 只能是 sudo 或 systemctl")
	}
	return cfg, nil
}

func (cfg *Config) configureListener(mode string) error {
	if mode == "proxy" {
		host, _, err := net.SplitHostPort(cfg.Listen)
		if err != nil || !isLoopback(host) {
			return fmt.Errorf("代理模式的 PANEL_LISTEN 必须为本机回环地址加端口")
		}
		return nil
	}

	public, _ := url.Parse(cfg.PublicURL) // ConfigFromEnv already validated the origin.
	host := strings.ToLower(public.Hostname())
	port := "443"
	switch mode {
	case "auto":
		if !cfg.SecureCookie || !validDNSName(host) || !strings.Contains(host, ".") || net.ParseIP(host) != nil || (public.Port() != "" && public.Port() != port) {
			return fmt.Errorf("自动 HTTPS 的 PANEL_PUBLIC_URL 必须使用公网域名和标准 HTTPS 端口")
		}
	case "ip", "http":
		scheme := "https"
		if mode == "http" {
			scheme = "http"
		}
		if public.Scheme != scheme {
			return fmt.Errorf("PANEL_HTTPS_MODE=%s 时 PANEL_PUBLIC_URL 必须使用 %s", mode, scheme)
		}
		address, err := PublicIP(host)
		if err != nil {
			return fmt.Errorf("PANEL_PUBLIC_URL: %w", err)
		}
		port = public.Port()
		number, err := strconv.Atoi(port)
		if err != nil || number < 1024 || number > 65535 || port != strconv.Itoa(number) {
			return fmt.Errorf("公网 IP 模式的 PANEL_PUBLIC_URL 必须显式指定 1024 至 65535 的端口")
		}
		host = address
		cfg.PublicURL = scheme + "://" + net.JoinHostPort(host, port)
		cfg.IPHTTPS = mode == "ip"
		cfg.DirectHTTP = mode == "http"
	default:
		return fmt.Errorf("PANEL_HTTPS_MODE 只能是 proxy、auto、ip 或 http")
	}

	cfg.Listen = env("PANEL_LISTEN", net.JoinHostPort("", port))
	_, listenPort, err := net.SplitHostPort(cfg.Listen)
	if err != nil || listenPort != port {
		return fmt.Errorf("PANEL_LISTEN 必须监听 %s 端口", port)
	}
	cfg.TrustLocalProxy = false
	if !cfg.DirectHTTP {
		cfg.HTTPSHost = host
		cfg.ACMECache = env("PANEL_ACME_CACHE", "/var/lib/sing-box-panel/acme")
		cfg.ACMEEmail = strings.TrimSpace(os.Getenv("PANEL_ACME_EMAIL"))
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func origin(value string, allowPublicHTTP bool) (string, bool, error) {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", false, fmt.Errorf("请填写不含路径的 HTTP(S) 源地址")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || (!allowPublicHTTP && !isLoopback(u.Hostname()))) {
		return "", false, fmt.Errorf("公网源地址必须使用 HTTPS")
	}
	return u.Scheme + "://" + u.Host, u.Scheme == "https", nil
}

func isLoopback(host string) bool {
	return host == "localhost" || net.ParseIP(host).IsLoopback()
}

func readPort(key, fallback string) (int, error) {
	port, err := strconv.Atoi(env(key, fallback))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s 必须介于 1 和 65535", key)
	}
	return port, nil
}

func validHost(host string) bool {
	return net.ParseIP(host) != nil || validDNSName(host)
}

func validDNSName(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, part := range strings.Split(host, ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
