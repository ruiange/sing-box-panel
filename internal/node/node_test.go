package node

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node/v2raywire"
)

func TestParseDeviceCounters(t *testing.T) {
	values, err := parseCounters([]*v2raywire.Stat{
		{Name: "user>>>device_abc>>>traffic>>>uplink", Value: 20},
		{Name: "user>>>device_abc>>>traffic>>>downlink", Value: 50},
		{Name: "user>>>device_bcd>>>traffic>>>uplink", Value: 100},
		{Name: "outbound>>>direct>>>traffic>>>uplink", Value: 300},
		{Name: "user>>>other-device_bcd>>>traffic>>>downlink", Value: 400},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values["abc"] != (Counters{Upload: 20, Download: 50}) || values["bcd"].Upload != 100 {
		t.Fatalf("wrong attribution: %v", values)
	}
	if _, err := parseCounters([]*v2raywire.Stat{{Name: "user>>>device_abc>>>traffic>>>uplink", Value: -1}}); err == nil {
		t.Fatal("accepted negative counter")
	}
}

// An optional real-binary test exercises empty provisioning, a same-process
// SIGHUP reload, and config restoration when the restart command fails.
func TestRealSingBox(t *testing.T) {
	binary := os.Getenv("TEST_SING_BOX_BINARY")
	if binary == "" {
		t.Skip("set TEST_SING_BOX_BINARY to a sing-box v1.14.4 build with with_utls and with_v2ray_api")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stats := localListener(t)
	inbound := localListener(t)
	listenPort := inbound.Addr().(*net.TCPAddr).Port
	statsAddress := stats.Addr().String()
	stats.Close()
	inbound.Close()
	configPath := filepath.Join(dir, "config.json")
	n, err := New(Config{
		BinaryPath: binary, ConfigPath: configPath, StatsAddress: statsAddress,
		Listen: "127.0.0.1", ListenPort: listenPort, ServerName: "www.example.com",
		RealityPrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), RealityShortID: "13a187b4",
		HandshakeServer: "www.example.com", HandshakePort: 443,
		ReloadCommand: []string{"/bin/true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	payload, err := renderConfig(n.cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Include actual diagnostic output when a config schema check fails.
	if output, err := exec.CommandContext(ctx, binary, "check", "-c", configPath).CombinedOutput(); err != nil {
		t.Fatalf("check empty node configuration: %v (%s)", err, output)
	}
	var log bytes.Buffer
	command := exec.CommandContext(ctx, binary, "run", "-c", configPath)
	command.Stdout = &log
	command.Stderr = &log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
		if t.Failed() {
			t.Log(strings.TrimSpace(log.String()))
		}
	}()
	n.cfg.ReloadCommand = []string{"/bin/kill", "-HUP", strconv.Itoa(command.Process.Pid)}
	first := waitForSnapshot(t, ctx, n)
	if len(first.Counters) != 0 {
		t.Fatalf("empty node returned counters: %+v", first.Counters)
	}
	if reloaded, err := n.Apply(ctx, nil); err != nil || reloaded {
		t.Fatalf("identical configuration changed: reloaded %v, error %v", reloaded, err)
	}
	devices := []Device{
		{ID: "phone", UUID: "cb3a7e31-dc62-4265-b9da-7cf668005c36"},
		{ID: "workstation", UUID: "2b481f92-e36c-49d6-a93c-fe740395dca8"},
	}
	if reloaded, err := n.Apply(ctx, devices); err != nil || !reloaded {
		t.Fatalf("verify changed configuration: reloaded %v, error %v", reloaded, err)
	}
	second := waitForSnapshot(t, ctx, n)
	if !second.StartedAt.After(first.StartedAt) {
		t.Fatalf("SIGHUP did not advance the Stats service start: %v -> %v", first.StartedAt, second.StartedAt)
	}
	document, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(document, []byte("device_phone")) || !bytes.Contains(document, []byte("device_workstation")) {
		t.Fatal("changed device accounts were not applied")
	}
	n.cfg.ReloadCommand = []string{"/bin/false"}
	if _, err := n.Apply(ctx, nil); err == nil {
		t.Fatal("ignored restart command failure")
	}
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(document, restored) {
		t.Fatal("failed reload left the changed config on disk")
	}
}

func waitForSnapshot(t *testing.T, ctx context.Context, n *Node) Snapshot {
	t.Helper()
	for {
		sample, err := n.Collect(ctx)
		if err == nil {
			return sample
		}
		if timeout := waitUntil(ctx, time.Now().Add(100*time.Millisecond)); timeout != nil {
			t.Fatalf("sing-box startup: %v, last RPC: %v", timeout, err)
		}
	}
}

func localListener(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(fmt.Errorf("allocate test port: %w", err))
	}
	return listener
}
