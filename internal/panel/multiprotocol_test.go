package panel

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ruiange/sing-box-panel/internal/node"
	"github.com/ruiange/sing-box-panel/internal/store"
)

// TestTrafficThroughEveryProfile exercises the exact sing-box client JSON
// generated for a device and measures the device's counters after each request.
func TestTrafficThroughEveryProfile(t *testing.T) {
	binary, target := os.Getenv("TEST_FULL_SING_BOX_BINARY"), os.Getenv("TEST_REALITY_TARGET")
	if binary == "" || target == "" {
		t.Skip("set TEST_FULL_SING_BOX_BINARY and TEST_REALITY_TARGET to exercise all 13 protocols")
	}
	serverURL, err := url.Parse(target)
	if err != nil || serverURL.Scheme != "https" || serverURL.Hostname() == "" || serverURL.Port() != "" {
		t.Fatal("TEST_REALITY_TARGET must be an HTTPS URL on port 443")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	dir := t.TempDir()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	base := freeProfilePorts(t)
	stats := freePort(t)
	configPath := filepath.Join(dir, "node.json")
	corePID, coreLog := filepath.Join(dir, "pid"), filepath.Join(dir, "node.log")
	start := filepath.Join(dir, "start.sh")
	script := `#!/bin/sh
set -eu
if [ -f "$3" ]; then
  kill -HUP "$(cat "$3")"
else
  "$1" run -c "$2" >"$4" 2>&1 </dev/null &
  echo "$!" >"$3"
fi
`
	if err := os.WriteFile(start, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if raw, err := os.ReadFile(corePID); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGTERM)
			}
		}
		if t.Failed() {
			if data, err := os.ReadFile(coreLog); err == nil {
				t.Logf("server log: %s", data)
			}
		}
	})
	private := base64.RawURLEncoding.EncodeToString(key.Bytes())
	const id = "e46ecf38-e61c-4ce6-9e5a-9ce253f21b72"
	const credential = "840b0ebd-4ea7-4e1e-90d3-b40e45510fb2"
	const shortID = "e614320e8950cfd3"
	cfg := Config{
		Listen: "127.0.0.1:8080", DatabasePath: filepath.Join(dir, "test.db"),
		Endpoint: Endpoint{Host: "127.0.0.1", Port: base, ServerName: serverURL.Hostname(), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: shortID},
		Node: &node.Config{
			BinaryPath: binary, ConfigPath: configPath, StatsAddress: net.JoinHostPort("127.0.0.1", strconv.Itoa(stats)),
			Listen: "127.0.0.1", ListenPort: base, ServerName: serverURL.Hostname(),
			HandshakeServer: serverURL.Hostname(), HandshakePort: 443, RealityPrivateKey: private, RealityShortID: shortID,
			ReloadCommand: []string{start, binary, configPath, corePID, coreLog},
		},
	}
	t.Setenv("PANEL_NODE_MULTIPROTOCOL", "true")
	if err := cfg.configureProfiles(private); err != nil {
		t.Fatal(err)
	}
	n, err := node.New(*cfg.Node)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if _, err := n.Apply(ctx, []node.Device{{ID: id, UUID: credential}}); err != nil {
		t.Fatal(err)
	}
	client := cfg.Endpoint.singBoxConfig(store.Device{ID: id, UUID: credential, Name: "device"})
	raw, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	var profiles []string
	for _, outbound := range document.Outbounds {
		if outbound["type"] == "selector" {
			for _, name := range outbound["outbounds"].([]any) {
				profiles = append(profiles, name.(string))
			}
		}
	}
	if len(profiles) != 13 {
		t.Fatalf("got %d externally selectable outbounds; want 13 (%v)", len(profiles), profiles)
	}
	for _, tag := range profiles {
		t.Run(tag, func(t *testing.T) {
			before, err := n.Collect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			set := map[string]any{"inbounds": document.Inbounds, "outbounds": document.Outbounds, "route": map[string]any{"final": tag}}
			if advanced, ok := client["dns"]; ok {
				set["dns"] = advanced
			}
			payload, err := json.Marshal(set)
			if err != nil {
				t.Fatal(err)
			}
			proxyRequest(t, ctx, binary, dir, tag, target, payload, true)
			after, err := n.Collect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			a, b := before.Counters[id], after.Counters[id]
			if b.Upload <= a.Upload || b.Download <= a.Download {
				t.Fatalf("device %s did not increment both directions: before=%+v after=%+v", id, a, b)
			}
		})
	}
}

func freeProfilePorts(t *testing.T) int {
	t.Helper()
	for attempt := range 100 {
		base := 24000 + 15*attempt
		var sockets []net.Listener
		var packets []net.PacketConn
		for port := base; port <= base+12; port++ {
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			socket, err := net.Listen("tcp4", address)
			if err != nil {
				break
			}
			sockets = append(sockets, socket)
			packet, err := net.ListenPacket("udp4", address)
			if err != nil {
				break
			}
			packets = append(packets, packet)
		}
		for _, socket := range sockets {
			socket.Close()
		}
		for _, packet := range packets {
			packet.Close()
		}
		if len(sockets) == 13 && len(packets) == 13 {
			return base
		}
	}
	t.Fatal("could not reserve 13 adjacent TCP and UDP ports")
	return 0
}
