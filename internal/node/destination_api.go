package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
)

const destinationAPITag = "panel-destinations"

var destinationAPILoopback = netip.MustParseAddr("127.0.0.1")

type destinationAPIService struct {
	Type       string `json:"type"`
	Tag        string `json:"tag"`
	Listen     string `json:"listen"`
	ListenPort uint16 `json:"listen_port"`
	Secret     string `json:"secret"`
}

func (c *Config) prepareDestinationAPI() error {
	// Keep the token and address stable across panel restarts so a running
	// core can keep serving the same configuration.
	service := destinationAPIService{
		Type:   "api",
		Tag:    destinationAPITag,
		Listen: destinationAPILoopback.String(),
		Secret: base64.RawURLEncoding.EncodeToString(credentialMAC(c.RealityPrivateKey, "destination-api-v1")),
	}
	oldConfig, _, err := readConfig(c.ConfigPath)
	if err != nil {
		return err
	}
	var existing struct {
		Services []destinationAPIService `json:"services"`
	}
	if json.Unmarshal(oldConfig, &existing) == nil {
		for _, previous := range existing.Services {
			if previous.Type == service.Type && previous.Tag == service.Tag && previous.Listen == service.Listen &&
				c.usableDestinationPort(previous.ListenPort) {
				service.ListenPort = previous.ListenPort
				c.destinationAPI = &service
				return nil
			}
		}
	}
	for range 8 {
		listener, err := net.ListenTCP("tcp4", net.TCPAddrFromAddrPort(netip.AddrPortFrom(destinationAPILoopback, 0)))
		if err != nil {
			return fmt.Errorf("分配访问记录 API 本地端口: %w", err)
		}
		port := listener.Addr().(*net.TCPAddr).AddrPort().Port()
		_ = listener.Close()
		if c.usableDestinationPort(port) {
			service.ListenPort = port
			c.destinationAPI = &service
			return nil
		}
	}
	return fmt.Errorf("找不到可用的访问记录 API 本地端口")
}

func (c Config) usableDestinationPort(port uint16) bool {
	statsAddress, _ := netip.ParseAddrPort(c.StatsAddress) // Already validated by Config.prepare.
	if port == 0 || port == 443 || int(port) == c.PanelPort || port == statsAddress.Port() || int(port) == c.ListenPort {
		return false
	}
	if c.Profiles != nil {
		for _, profilePort := range c.Profiles.ports() {
			if int(port) == profilePort {
				return false
			}
		}
	}
	return true
}

func (n *Node) WatchDestinations(ctx context.Context, consume func([]ObservedConnection) error) error {
	if n.cfg.destinationAPI == nil {
		return fmt.Errorf("访问记录 API 尚未配置")
	}
	api := n.cfg.destinationAPI
	return SubscribeConnectionEvents(ctx, netip.AddrPortFrom(destinationAPILoopback, api.ListenPort).String(), api.Secret, consume)
}
