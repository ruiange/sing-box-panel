package node

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestMultiProtocolWithoutDevices(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_MULTIPROTOCOL_BINARY")
	if binary == "" {
		t.Skip("set TEST_SING_BOX_MULTIPROTOCOL_BINARY to a sing-box v1.14.4 build with QUIC, uTLS and V2Ray stats")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	realityKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	holder := localListener(t)
	stats := localListener(t)
	t.Cleanup(func() {
		holder.Close()
		stats.Close()
	})
	basePort := holder.Addr().(*net.TCPAddr).Port
	p := NewProfiles(basePort)
	var reservations []*net.TCPListener
	reserve := func() int {
		listener := localListener(t)
		t.Cleanup(func() { listener.Close() })
		reservations = append(reservations, listener)
		return listener.Addr().(*net.TCPAddr).Port
	}
	p.Hysteria2, p.TUIC, p.ShadowTLS, p.Shadowsocks = reserve(), reserve(), reserve(), reserve()
	p.Trojan, p.VMessWS, p.VLESSWSTLS, p.H2Reality = reserve(), reserve(), reserve(), reserve()
	p.GRPCReality, p.AnyTLS, p.NaiveHTTP2, p.NaiveHTTP3 = reserve(), reserve(), reserve(), reserve()
	p.TLSName = "example.net"
	p.TLSCertificatePath, p.TLSKeyPath = testCertificate(t, dir, p.TLSName)
	p.SSBaseKey = base64.StdEncoding.EncodeToString([]byte("public-test-key!"))
	p.ShadowSSBaseKey = base64.StdEncoding.EncodeToString([]byte("shadow-test-key!"))
	cfg := Config{
		BinaryPath: binary, ConfigPath: filepath.Join(dir, "node.json"), StatsAddress: stats.Addr().String(),
		Listen: "127.0.0.1", ListenPort: basePort, ServerName: "www.example.com",
		RealityPrivateKey: base64.RawURLEncoding.EncodeToString(realityKey.Bytes()), RealityShortID: "f87d8d91",
		HandshakeServer: "www.example.com", HandshakePort: 443, Profiles: &p,
	}
	n, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	data, err := renderConfig(n.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Inbounds []struct {
			Type     string
			Tag      string
			Password string
			Users    []map[string]string
		}
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Inbounds) != 14 {
		t.Fatalf("wanted 13 public inbounds and 1 ShadowTLS detour, got %d", len(document.Inbounds))
	}
	for _, inbound := range document.Inbounds {
		switch inbound.Type {
		case "naive", "shadowtls", "shadowsocks":
			if len(inbound.Users) != 1 || inbound.Users[0]["password"] == "" || inbound.Users[0]["password"] == inbound.Password {
				t.Fatalf("%s has no separate safe empty-user credential", inbound.Tag)
			}
		}
	}
	if err := os.WriteFile(cfg.ConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(ctx, binary, "check", "-c", cfg.ConfigPath).CombinedOutput(); err != nil {
		t.Fatalf("check multi-protocol config: %v (%s)", err, output)
	}
	// Release reservations just before sing-box binds the ports.
	for _, release := range reservations {
		release.Close()
	}
	holder.Close()
	stats.Close()
	var log bytes.Buffer
	command := exec.CommandContext(ctx, binary, "run", "-c", cfg.ConfigPath)
	command.Stdout, command.Stderr = &log, &log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		if t.Failed() {
			t.Log(log.String())
		}
	}()
	statsSnapshot := waitForSnapshot(t, ctx, n)
	if len(statsSnapshot.Counters) != 0 {
		t.Fatal("new empty node returned real device counters")
	}
	for _, port := range []int{basePort, p.ShadowTLS, p.Shadowsocks, p.Trojan, p.VMessWS, p.VLESSWSTLS, p.H2Reality, p.GRPCReality, p.AnyTLS, p.NaiveHTTP2} {
		connection, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
		if err != nil {
			t.Errorf("TCP port %d: %v", port, err)
			continue
		}
		connection.Close()
	}
	for _, port := range []int{p.Hysteria2, p.TUIC, p.Shadowsocks, p.NaiveHTTP3} {
		listener, err := net.ListenPacket("udp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			listener.Close()
			t.Errorf("UDP port %d was not bound", port)
		}
	}
}

func testCertificate(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{name},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}
