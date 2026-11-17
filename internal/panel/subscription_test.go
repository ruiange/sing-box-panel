package panel

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

func TestMultiProtocolSubscriptionFormats(t *testing.T) {
	profiles := node.NewProfiles(8443)
	profiles.TLSName = "panel-node.invalid"
	profiles.TLSCertificatePEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("test TLS leaf")}))
	profiles.TLSPin = base64.StdEncoding.EncodeToString(make([]byte, 32))
	profiles.SSBaseKey = "abcdefghijklmnopqrstuv=="
	profiles.ShadowSSBaseKey = "abcdefghijklmnopqrstuv=="
	endpoint := Endpoint{Host: "2001:db8::1", Port: 8443, ServerName: "example.com", PublicKey: base64.RawURLEncoding.EncodeToString([]byte("test-reality-public-key-32-bytes")), ShortID: "0123456789abcdef", Profiles: &profiles}
	device := store.Device{ID: "device-a", UUID: "840b0ebd-4ea7-4e1e-90d3-b40e45510fb2", Name: "测试 设备 #1"}

	shadow := endpoint.shadowrocketLinks(device)
	if len(shadow) != 13 {
		t.Fatalf("Shadowrocket count = %d, want 13", len(shadow))
	}
	for i, tc := range []struct{ scheme, title string }{
		{"vless", ""}, {"hysteria2", protocolHy2}, {"tuic", protocolTUIC}, {"ss", protocolShadow},
		{"ss", protocolSS}, {"trojan", protocolTrojan}, {"vmess", protocolVMess}, {"vless", protocolVLESS},
		{"vless", protocolH2}, {"vless", protocolGRPC}, {"anytls", protocolAnyTLS}, {"http2", protocolNaive2}, {"http3", protocolNaive3},
	} {
		if !strings.HasPrefix(shadow[i], tc.scheme+"://") {
			t.Errorf("unexpected Shadowrocket scheme at %d", i)
		}
		if tc.title != "" && !strings.Contains(shadow[i], url.PathEscape(tc.title)) {
			t.Errorf("missing display name %s at %d", tc.title, i)
		}
	}
	hy2, err := url.Parse(shadow[1])
	if err != nil || len(strings.Split(hy2.Query().Get("hpkp"), ":")) != 32 {
		t.Fatal("Hysteria2 missing its certificate fingerprint")
	}
	if !strings.Contains(shadow[8], "sid="+endpoint.ShortID) || !strings.Contains(shadow[9], "sid="+endpoint.ShortID) {
		t.Fatal("transported VLESS entries lost the Reality short ID")
	}

	v2ray := endpoint.v2rayLinks(device)
	if len(v2ray) != 15 {
		t.Fatalf("v2rayN count = %d, want 13 nodes plus 2 ShadowTLS helpers", len(v2ray))
	}
	var chain clientConfig
	for _, link := range v2ray {
		const prefix = "v2rayn://proxychain/"
		if !strings.HasPrefix(link, prefix) {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, prefix))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &chain); err != nil {
			t.Fatal(err)
		}
	}
	if chain == nil || chain["ProtoExtraObj"].(map[string]any)["ChildItems"] != "shadowtls-transport,shadowtls-shadowsocks" {
		t.Fatal("v2rayN ShadowTLS chain does not name both outbounds")
	}

	clash := endpoint.clashConfig(device)["proxies"].([]clientConfig)
	if len(clash) != 11 {
		t.Fatalf("Mihomo count = %d, want 11 (no Naive support)", len(clash))
	}
	if binary := os.Getenv("TEST_MIHOMO_BINARY"); binary != "" {
		dir := t.TempDir()
		config := filepath.Join(dir, "config.json")
		raw, err := json.Marshal(endpoint.clashConfig(device))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(config, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(binary, "-d", dir, "-f", config, "-t").CombinedOutput(); err != nil {
			t.Fatalf("mihomo rejected the subscription: %v (%s)", err, output)
		}
	}
	singBox := endpoint.singBoxConfig(device)["outbounds"].([]clientConfig)
	if len(singBox) != 15 || len(singBox[len(singBox)-1]["outbounds"].([]string)) != 13 {
		t.Fatal("sing-box did not expose exactly 13 selectable nodes")
	}
}
