package panel

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

const realityFlow = "xtls-rprx-vision"

type Endpoint struct {
	Host, ServerName, PublicKey, ShortID string
	Port                                 int
	Profiles                             *node.Profiles
}

func (s *Service) subscription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	token := chi.URLParam(r, "token")
	if len(token) != 43 || s.node == nil {
		http.NotFound(w, r)
		return
	}
	if _, err := base64.RawURLEncoding.DecodeString(token); err != nil {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	device, err := s.store.GetDeviceBySubscriptionToken(r.Context(), token)
	s.mu.Unlock()
	if errors.Is(err, store.ErrNotFound) || (err == nil && !device.Enabled) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.log.Error("读取订阅失败", "error", err)
		http.Error(w, "订阅暂不可用", http.StatusServiceUnavailable)
		return
	}
	format := chi.URLParam(r, "format")
	// Older panel versions labelled the v2ray subscription for both clients.
	if (format == "" || format == "v2ray") && strings.Contains(strings.ToLower(r.UserAgent()), "shadowrocket") {
		format = "shadowrocket"
	}
	switch format {
	case "", "v2ray", "shadowrocket":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		var links []string
		if format == "shadowrocket" {
			links = s.cfg.Endpoint.shadowrocketLinks(device)
		} else {
			links = s.cfg.Endpoint.v2rayLinks(device)
		}
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n") + "\n"))))
	case "clash":
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		// JSON is valid YAML 1.2. Encoding it here escapes user-defined node names.
		writeSubscriptionJSON(w, s.cfg.Endpoint.clashConfig(device))
	case "sing-box":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		writeSubscriptionJSON(w, s.cfg.Endpoint.singBoxConfig(device))
	default:
		http.NotFound(w, r)
	}
}

func writeSubscriptionJSON(w http.ResponseWriter, data any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(data)
}

func (e Endpoint) shareURL(device store.Device) string {
	params := url.Values{
		"encryption": {"none"}, "type": {"tcp"}, "security": {"reality"}, "flow": {realityFlow},
		"sni": {e.ServerName}, "pbk": {e.PublicKey}, "sid": {e.ShortID}, "fp": {"chrome"},
	}
	return (&url.URL{
		Scheme: "vless", User: url.User(device.UUID), Host: net.JoinHostPort(e.Host, strconv.Itoa(e.Port)),
		RawQuery: params.Encode(), Fragment: device.Name,
	}).String()
}

func (e Endpoint) clashConfig(device store.Device) map[string]any {
	const group = "Sing-Box Panel"
	name := strings.TrimSpace(device.Name) + " · VLESS"
	proxy := map[string]any{
		"type": "vless", "name": name, "server": e.Host, "port": e.Port, "uuid": device.UUID,
		"network": "tcp", "tls": true, "udp": true, "flow": realityFlow, "servername": e.ServerName,
		"client-fingerprint": "chrome", "reality-opts": map[string]any{"public-key": e.PublicKey, "short-id": e.ShortID},
	}
	proxies := []clientConfig{proxy}
	names := []string{name}
	if e.Profiles != nil {
		extra := e.clashAdditional(device)
		for _, node := range extra {
			names = append(names, node["name"].(string))
		}
		proxies = append(proxies, extra...)
	}
	return map[string]any{
		"mixed-port": 7890, "allow-lan": false, "mode": "rule", "log-level": "info",
		"proxies":      proxies,
		"proxy-groups": []any{map[string]any{"name": group, "type": "select", "proxies": names}},
		"rules":        []string{"MATCH," + group},
	}
}

const (
	protocolHy2    = "Hysteria2"
	protocolTUIC   = "TUIC"
	protocolShadow = "ShadowTLS"
	protocolSS     = "Shadowsocks"
	protocolTrojan = "Trojan"
	protocolVMess  = "VMess WS"
	protocolVLESS  = "VLESS WS TLS"
	protocolH2     = "H2 Reality"
	protocolGRPC   = "gRPC Reality"
	protocolAnyTLS = "AnyTLS"
	protocolNaive2 = "Naive HTTP2"
	protocolNaive3 = "Naive HTTP3"
)

type clientConfig = map[string]any

func deviceCredentials(device store.Device) node.Credentials {
	return (node.Device{ID: device.ID, UUID: device.UUID}).Credentials()
}

func clientName(device store.Device, protocol string) string {
	return strings.TrimSpace(device.Name) + " · " + protocol
}

// Shadowrocket HPKP and Mihomo fingerprint use the SHA256 leaf digest in
// OpenSSL fingerprint format. Sing-box uses the separate SPKI pin.
func (e Endpoint) leafFingerprint() string {
	block, _ := pem.Decode([]byte(e.Profiles.TLSCertificatePEM))
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(fmt.Sprintf("% X", sum)), " ", ":"))
}

func (e Endpoint) shadowTLSServerName() string {
	if name := e.Profiles.ShadowTLSServerName; name != "" {
		return name
	}
	return e.ServerName
}

func (e Endpoint) address(port int) string {
	return net.JoinHostPort(e.Host, strconv.Itoa(port))
}

func userURL(scheme string, user *url.Userinfo, host string, params url.Values, fragment string) string {
	return (&url.URL{Scheme: scheme, User: user, Host: host, RawQuery: params.Encode(), Fragment: fragment}).String()
}

// Some Shadowrocket share schemes encode the whole authority as one base64
// value. Using url.URL.Host would alter slashes in that value.
func shadowrocketURL(scheme, authority string, params url.Values, fragment string) string {
	result := scheme + "://" + base64.StdEncoding.EncodeToString([]byte(authority))
	if query := params.Encode(); query != "" {
		result += "?" + strings.ReplaceAll(query, "+", "%20")
	}
	if fragment != "" {
		result += (&url.URL{Fragment: fragment}).String()
	}
	return result
}

func (e Endpoint) shadowrocketLinks(device store.Device) []string {
	links := []string{e.shareURL(device)}
	p := e.Profiles
	if p == nil {
		return links
	}
	creds := deviceCredentials(device)
	pin := e.leafFingerprint()
	pinned := func() url.Values { return url.Values{"peer": {p.TLSName}, "hpkp": {pin}} }
	reality := func(protocol, obfs, path string, port int) string {
		return shadowrocketURL("vless", "auto:"+creds.UUID+"@"+e.address(port), url.Values{
			"remarks": {clientName(device, protocol)}, "path": {path}, "obfs": {obfs}, "tls": {"1"},
			"peer": {e.ServerName}, "alpn": {"h2"}, "pbk": {e.PublicKey}, "sid": {e.ShortID}, "fp": {"chrome"},
		}, "")
	}
	hy2 := pinned()
	hy2.Set("obfs", "none")
	tuic := pinned()
	tuic.Set("alpn", "h3")
	tuic.Set("congestion_control", "bbr")
	tuic.Set("udp_relay_mode", "native")
	shadowPlugin, _ := json.Marshal(map[string]string{"version": "3", "host": e.shadowTLSServerName(), "password": creds.ShadowTLS})
	wsTLS := pinned()
	wsTLS.Set("tls", "1")
	wsTLS.Set("obfs", "websocket")
	wsTLS.Set("obfsParam", p.TLSName)
	wsTLS.Set("path", p.WSPath)
	wsTLS.Set("remarks", clientName(device, protocolVLESS))
	anyTLS := pinned()
	anyTLS.Set("udp", "1")
	naive2, naive3 := pinned(), pinned()
	naive2.Set("alpn", "h2,http/1.1")
	naive2.Set("padding", "1")
	naive2.Set("uot", "2")
	naive3.Set("alpn", "h3")
	naive3.Set("padding", "1")
	return append(links,
		userURL("hysteria2", url.User(creds.Hysteria2), e.address(p.Hysteria2), hy2, clientName(device, protocolHy2)),
		// Shadowrocket uses password:uuid for TUIC.
		userURL("tuic", url.UserPassword(creds.TUIC, creds.UUID), e.address(p.TUIC), tuic, clientName(device, protocolTUIC)),
		shadowrocketURL("ss", node.ShadowsocksMethod+":"+p.ShadowSSBaseKey+":"+creds.ShadowSS+"@"+e.address(p.ShadowTLS), url.Values{
			"shadow-tls": {base64.StdEncoding.EncodeToString(shadowPlugin)}, "uot": {"2"},
		}, clientName(device, protocolShadow)),
		shadowrocketURL("ss", node.ShadowsocksMethod+":"+p.SSBaseKey+":"+creds.Shadowsocks+"@"+e.address(p.Shadowsocks), nil, clientName(device, protocolSS)),
		userURL("trojan", url.User(creds.Trojan), e.address(p.Trojan), pinned(), clientName(device, protocolTrojan)),
		shadowrocketURL("vmess", "aes-128-gcm:"+creds.VMessUUID+"@"+e.address(p.VMessWS), url.Values{
			"remarks": {clientName(device, protocolVMess)}, "obfs": {"websocket"}, "path": {p.WSPath}, "obfsParam": {e.Host}, "alterId": {"0"},
		}, ""),
		shadowrocketURL("vless", "auto:"+creds.UUID+"@"+e.address(p.VLESSWSTLS), wsTLS, ""),
		reality(protocolH2, "h2", p.WSPath, p.H2Reality),
		reality(protocolGRPC, "grpc", p.GRPCService, p.GRPCReality),
		userURL("anytls", url.User(creds.AnyTLS), e.address(p.AnyTLS), anyTLS, clientName(device, protocolAnyTLS)),
		shadowrocketURL("http2", creds.Username+":"+creds.Naive+"@"+e.address(p.NaiveHTTP2), naive2, clientName(device, protocolNaive2)),
		shadowrocketURL("http3", creds.Username+":"+creds.Naive+"@"+e.address(p.NaiveHTTP3), naive3, clientName(device, protocolNaive3)),
	)
}

func v2rayCustomLink(scheme string, data clientConfig) string {
	data["ConfigVersion"] = 4
	encoded, _ := json.Marshal(data)
	return "v2rayn://" + scheme + "/" + base64.RawURLEncoding.EncodeToString(encoded)
}

func (e Endpoint) v2rayLinks(device store.Device) []string {
	links := []string{e.shareURL(device)}
	p := e.Profiles
	if p == nil {
		return links
	}
	creds := deviceCredentials(device)
	certificate := strings.TrimSpace(p.TLSCertificatePEM)
	profile := func(configType, port int, name, password string) clientConfig {
		return clientConfig{
			"ConfigType": configType, "CoreType": 24, "Remarks": clientName(device, name), "Address": e.Host,
			"Port": port, "Password": password, "StreamSecurity": "tls", "AllowInsecure": "false", "Sni": p.TLSName, "Cert": certificate,
		}
	}
	grpcURL := userURL("vless", url.User(creds.UUID), e.address(p.GRPCReality), url.Values{
		"encryption": {"none"}, "type": {"grpc"}, "serviceName": {p.GRPCService}, "mode": {"gun"}, "security": {"reality"},
		"sni": {e.ServerName}, "pbk": {e.PublicKey}, "sid": {e.ShortID}, "fp": {"chrome"}, "alpn": {"h2"},
	}, clientName(device, protocolGRPC))
	vmess, _ := json.Marshal(clientConfig{
		"v": "2", "ps": clientName(device, protocolVMess), "add": e.Host, "port": strconv.Itoa(p.VMessWS),
		"id": creds.VMessUUID, "aid": "0", "scy": "aes-128-gcm", "net": "ws", "type": "none", "host": e.Host, "path": p.WSPath,
	})
	tuic := profile(8, p.TUIC, protocolTUIC, creds.TUIC)
	tuic["Username"], tuic["Alpn"] = creds.UUID, "h3"
	tuic["ProtoExtraObj"] = clientConfig{"CongestionControl": "bbr"}
	trojan := profile(6, p.Trojan, protocolTrojan, creds.Trojan)
	trojan["Network"] = "raw"
	vlessWS := profile(5, p.VLESSWSTLS, protocolVLESS, creds.UUID)
	vlessWS["Network"] = "ws"
	vlessWS["TransportExtraObj"] = clientConfig{"Path": p.WSPath, "Host": p.TLSName}
	naive2, naive3 := profile(12, p.NaiveHTTP2, protocolNaive2, creds.Naive), profile(12, p.NaiveHTTP3, protocolNaive3, creds.Naive)
	naive2["Username"], naive3["Username"] = creds.Username, creds.Username
	naive3["ProtoExtraObj"] = clientConfig{"NaiveQuic": true, "CongestionControl": "bbr"}

	// v2rayN imports two custom outbounds and a native chain that references them.
	shadowOuterTag, shadowInnerTag := "shadowtls-transport", "shadowtls-shadowsocks"
	shadowOuter, shadowInner := e.singBoxShadow(creds, shadowOuterTag, shadowInnerTag)
	delete(shadowInner, "detour")
	shadowOut := v2rayCustomLink("shadowtls", clientConfig{"ConfigType": 13, "CoreType": 24, "IndexId": shadowOuterTag,
		"Remarks": clientName(device, "ShadowTLS transport"), "CustomOutboundObj": shadowOuter})
	shadowSS := v2rayCustomLink("shadowsocks", clientConfig{"ConfigType": 13, "CoreType": 24, "IndexId": shadowInnerTag,
		"Remarks": clientName(device, "ShadowTLS Shadowsocks"), "CustomOutboundObj": shadowInner})
	shadowChain := v2rayCustomLink("proxychain", clientConfig{"ConfigType": 102, "CoreType": 24,
		"Remarks": clientName(device, protocolShadow), "ProtoExtraObj": clientConfig{"ChildItems": shadowOuterTag + "," + shadowInnerTag}})
	h2 := v2rayCustomLink("vless", clientConfig{"ConfigType": 13, "CoreType": 24, "Remarks": clientName(device, protocolH2),
		"CustomOutboundObj": e.singBoxH2(creds, "h2-reality")})
	return append(links,
		v2rayCustomLink("hysteria2", profile(7, p.Hysteria2, protocolHy2, creds.Hysteria2)), v2rayCustomLink("tuic", tuic),
		shadowOut, shadowSS, shadowChain,
		userURL("ss", url.UserPassword(node.ShadowsocksMethod, p.SSBaseKey+":"+creds.Shadowsocks), e.address(p.Shadowsocks), nil, clientName(device, protocolSS)),
		v2rayCustomLink("trojan", trojan), "vmess://"+base64.StdEncoding.EncodeToString(vmess), v2rayCustomLink("vless", vlessWS),
		h2, grpcURL, v2rayCustomLink("anytls", profile(11, p.AnyTLS, protocolAnyTLS, creds.AnyTLS)),
		v2rayCustomLink("naive", naive2), v2rayCustomLink("naive", naive3),
	)
}

func boxOutbound(protocol, tag, host string, port int, options clientConfig) clientConfig {
	options["type"], options["tag"] = protocol, tag
	if host != "" {
		options["server"], options["server_port"] = host, port
	}
	return options
}

func (e Endpoint) singBoxRealityTLS() clientConfig {
	return clientConfig{
		"enabled": true, "server_name": e.ServerName,
		"utls":    clientConfig{"enabled": true, "fingerprint": "chrome"},
		"reality": clientConfig{"enabled": true, "public_key": e.PublicKey, "short_id": e.ShortID},
	}
}

func (e Endpoint) singBoxPinnedTLS(alpn ...string) clientConfig {
	tls := clientConfig{
		"enabled": true, "server_name": e.Profiles.TLSName,
		"certificate_public_key_sha256": []string{e.Profiles.TLSPin},
	}
	if len(alpn) != 0 {
		tls["alpn"] = alpn
	}
	return tls
}

func (e Endpoint) singBoxShadow(c node.Credentials, outerTag, innerTag string) (clientConfig, clientConfig) {
	p := e.Profiles
	outer := boxOutbound("shadowtls", outerTag, e.Host, p.ShadowTLS, clientConfig{
		"version": 3, "password": c.ShadowTLS,
		"tls": clientConfig{"enabled": true, "server_name": e.shadowTLSServerName()},
	})
	inner := boxOutbound("shadowsocks", innerTag, "", 0, clientConfig{
		"method": node.ShadowsocksMethod, "password": p.ShadowSSBaseKey + ":" + c.ShadowSS, "detour": outerTag, "udp_over_tcp": true,
	})
	return outer, inner
}

func (e Endpoint) singBoxH2(c node.Credentials, tag string) clientConfig {
	tls := e.singBoxRealityTLS()
	tls["alpn"] = []string{"h2"}
	return boxOutbound("vless", tag, e.Host, e.Profiles.H2Reality, clientConfig{
		"uuid": c.UUID, "tls": tls, "transport": clientConfig{"type": "http", "path": e.Profiles.WSPath, "host": []string{e.ServerName}},
	})
}

func (e Endpoint) singBoxConfig(device store.Device) map[string]any {
	p := e.Profiles
	realityTag := "proxy"
	if p != nil {
		realityTag = clientName(device, "VLESS Reality")
	}
	reality := boxOutbound("vless", realityTag, e.Host, e.Port, clientConfig{
		"uuid": device.UUID, "flow": realityFlow, "tls": e.singBoxRealityTLS(),
	})
	outbounds := []clientConfig{reality}
	final := "proxy"
	if p != nil {
		c := deviceCredentials(device)
		name := func(protocol string) string { return clientName(device, protocol) }
		outer, inner := e.singBoxShadow(c, "shadowtls-transport", name(protocolShadow))
		ws := clientConfig{"type": "ws", "path": p.WSPath}
		grpcTLS := e.singBoxRealityTLS()
		grpcTLS["alpn"] = []string{"h2"}
		naive := func(port int, protocol string, quic bool) clientConfig {
			options := clientConfig{
				"username": c.Username, "password": c.Naive, "quic": quic,
				"tls": clientConfig{"enabled": true, "server_name": p.TLSName, "certificate": []string{p.TLSCertificatePEM}},
			}
			if quic {
				options["quic_congestion_control"] = "bbr"
			} else {
				options["udp_over_tcp"] = true
			}
			return boxOutbound("naive", name(protocol), e.Host, port, options)
		}
		outbounds = append(outbounds,
			boxOutbound("hysteria2", name(protocolHy2), e.Host, p.Hysteria2, clientConfig{"password": c.Hysteria2, "tls": e.singBoxPinnedTLS("h3")}),
			boxOutbound("tuic", name(protocolTUIC), e.Host, p.TUIC, clientConfig{
				"uuid": c.UUID, "password": c.TUIC, "congestion_control": "bbr", "udp_relay_mode": "native", "tls": e.singBoxPinnedTLS("h3"),
			}),
			inner,
			boxOutbound("shadowsocks", name(protocolSS), e.Host, p.Shadowsocks, clientConfig{"method": node.ShadowsocksMethod, "password": p.SSBaseKey + ":" + c.Shadowsocks}),
			boxOutbound("trojan", name(protocolTrojan), e.Host, p.Trojan, clientConfig{"password": c.Trojan, "tls": e.singBoxPinnedTLS()}),
			boxOutbound("vmess", name(protocolVMess), e.Host, p.VMessWS, clientConfig{"uuid": c.VMessUUID, "security": "aes-128-gcm", "transport": ws}),
			boxOutbound("vless", name(protocolVLESS), e.Host, p.VLESSWSTLS, clientConfig{"uuid": c.UUID, "tls": e.singBoxPinnedTLS("http/1.1"), "transport": ws}),
			e.singBoxH2(c, name(protocolH2)),
			boxOutbound("vless", name(protocolGRPC), e.Host, p.GRPCReality, clientConfig{
				"uuid": c.UUID, "tls": grpcTLS, "transport": clientConfig{"type": "grpc", "service_name": p.GRPCService},
			}),
			boxOutbound("anytls", name(protocolAnyTLS), e.Host, p.AnyTLS, clientConfig{"password": c.AnyTLS, "tls": e.singBoxPinnedTLS()}),
			naive(p.NaiveHTTP2, protocolNaive2, false), naive(p.NaiveHTTP3, protocolNaive3, true),
		)
		choices := make([]string, len(outbounds))
		for i, outbound := range outbounds {
			choices[i] = outbound["tag"].(string)
		}
		outbounds = append(outbounds, outer, clientConfig{"type": "selector", "tag": final, "outbounds": choices, "default": realityTag})
	}
	result := clientConfig{
		"inbounds":  []any{map[string]any{"type": "mixed", "tag": "local", "listen": "127.0.0.1", "listen_port": 7890}},
		"outbounds": outbounds,
		"route":     clientConfig{"final": final},
	}
	if p != nil {
		result["dns"] = clientConfig{"servers": []clientConfig{{"type": "local", "tag": "system"}}, "final": "system"}
		result["route"].(clientConfig)["default_domain_resolver"] = "system"
	}
	return result
}

func (e Endpoint) clashAdditional(device store.Device) []clientConfig {
	p := e.Profiles
	c := deviceCredentials(device)
	leafPin := e.leafFingerprint()
	entry := func(protocol, title string, port int, fields clientConfig) clientConfig {
		fields["name"], fields["type"], fields["server"], fields["port"] = clientName(device, title), protocol, e.Host, port
		fields["udp"] = true
		return fields
	}
	reality := func(title, network, transportKey string, transport clientConfig, port int) clientConfig {
		return entry("vless", title, port, clientConfig{
			"uuid": c.UUID, "tls": true, "servername": e.ServerName, "client-fingerprint": "chrome",
			"network": network, "alpn": []string{"h2"}, transportKey: transport,
			"reality-opts": clientConfig{"public-key": e.PublicKey, "short-id": e.ShortID},
		})
	}
	ws := func(host string) clientConfig {
		return clientConfig{"path": p.WSPath, "headers": map[string]string{"Host": host}}
	}
	return []clientConfig{
		entry("hysteria2", protocolHy2, p.Hysteria2, clientConfig{"password": c.Hysteria2, "sni": p.TLSName, "fingerprint": leafPin}),
		entry("tuic", protocolTUIC, p.TUIC, clientConfig{
			"uuid": c.UUID, "password": c.TUIC, "sni": p.TLSName, "fingerprint": leafPin,
			"alpn": []string{"h3"}, "congestion-controller": "bbr", "udp-relay-mode": "native",
		}),
		entry("ss", protocolShadow, p.ShadowTLS, clientConfig{
			"cipher": node.ShadowsocksMethod, "password": p.ShadowSSBaseKey + ":" + c.ShadowSS,
			"plugin": "shadow-tls", "client-fingerprint": "chrome", "udp-over-tcp": true, "udp-over-tcp-version": 2,
			"plugin-opts": clientConfig{"host": e.shadowTLSServerName(), "password": c.ShadowTLS, "version": 3},
		}),
		entry("ss", protocolSS, p.Shadowsocks, clientConfig{"cipher": node.ShadowsocksMethod, "password": p.SSBaseKey + ":" + c.Shadowsocks}),
		entry("trojan", protocolTrojan, p.Trojan, clientConfig{"password": c.Trojan, "sni": p.TLSName, "fingerprint": leafPin}),
		entry("vmess", protocolVMess, p.VMessWS, clientConfig{
			"uuid": c.VMessUUID, "alterId": 0, "cipher": "aes-128-gcm", "network": "ws", "ws-opts": ws(e.Host),
		}),
		entry("vless", protocolVLESS, p.VLESSWSTLS, clientConfig{
			"uuid": c.UUID, "network": "ws", "tls": true, "servername": p.TLSName, "fingerprint": leafPin, "ws-opts": ws(p.TLSName),
		}),
		reality(protocolH2, "h2", "h2-opts", clientConfig{"path": p.WSPath, "host": []string{e.ServerName}}, p.H2Reality),
		reality(protocolGRPC, "grpc", "grpc-opts", clientConfig{"grpc-service-name": p.GRPCService}, p.GRPCReality),
		entry("anytls", protocolAnyTLS, p.AnyTLS, clientConfig{"password": c.AnyTLS, "sni": p.TLSName, "fingerprint": leafPin}),
	}
}
