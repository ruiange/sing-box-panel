package panel

import "testing"

func TestPublicIP(t *testing.T) {
	for input, expected := range map[string]string{
		"1.1.1.1": "1.1.1.1", "::ffff:1.1.1.1": "1.1.1.1",
		"2606:4700:0000::1111": "2606:4700::1111",
	} {
		if result, err := PublicIP(input); result != expected || err != nil {
			t.Errorf("PublicIP(%q) = %q, %v, want %q", input, result, err, expected)
		}
	}
	for _, input := range []string{"127.0.0.1", "10.0.0.8", "100.64.0.1", "192.0.2.1", "2001:db8::1", "fe80::1%eth0", "example.com"} {
		if _, err := PublicIP(input); err == nil {
			t.Errorf("accepted %q in public IP mode", input)
		}
	}
}

func TestIPHTTPSConfig(t *testing.T) {
	t.Setenv("PANEL_ADMIN_PASSWORD", "local-test-password")
	t.Setenv("PANEL_NODE_MODE", "disabled")
	t.Setenv("PANEL_HTTPS_MODE", "ip")
	t.Setenv("PANEL_LISTEN", "")
	t.Setenv("PANEL_PUBLIC_URL", "https://[2606:4700:0000::1111]:9443")
	cfg, err := ConfigFromEnv()
	if err != nil || !cfg.IPHTTPS || !cfg.SecureCookie || cfg.Listen != ":9443" || cfg.PublicURL != "https://[2606:4700::1111]:9443" {
		t.Fatalf("wrong IP HTTPS configuration: listen=%q URL=%q error=%v", cfg.Listen, cfg.PublicURL, err)
	}
	t.Setenv("PANEL_LISTEN", ":443")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("accepted an application listener on the ACME port")
	}
	t.Setenv("PANEL_HTTPS_MODE", "proxy")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("accepted a public plaintext listener in proxy mode")
	}
}

func TestDirectHTTPConfig(t *testing.T) {
	t.Setenv("PANEL_ADMIN_PASSWORD", "local-test-password")
	t.Setenv("PANEL_NODE_MODE", "disabled")
	t.Setenv("PANEL_HTTPS_MODE", "http")
	t.Setenv("PANEL_LISTEN", "")
	t.Setenv("PANEL_PUBLIC_URL", "http://[2606:4700:0000::1111]:9443")
	t.Setenv("PANEL_TRUST_LOCAL_PROXY", "true")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DirectHTTP || cfg.Listen != ":9443" || cfg.PublicURL != "http://[2606:4700::1111]:9443" || cfg.SecureCookie || cfg.HTTPSHost != "" || cfg.IPHTTPS || cfg.ACMECache != "" || cfg.TrustLocalProxy {
		t.Fatal("unexpected direct HTTP configuration")
	}
	for _, origin := range []string{"https://1.1.1.1:9443", "http://1.1.1.1:443", "http://1.1.1.1", "http://example.com:9443", "http://10.0.0.2:9443"} {
		t.Setenv("PANEL_PUBLIC_URL", origin)
		if _, err := ConfigFromEnv(); err == nil {
			t.Errorf("accepted invalid direct HTTP origin %q", origin)
		}
	}
	t.Setenv("PANEL_PUBLIC_URL", "http://1.1.1.1:9443")
	t.Setenv("PANEL_HTTPS_MODE", "proxy")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("public HTTP accepted without explicit opt-in")
	}
}
