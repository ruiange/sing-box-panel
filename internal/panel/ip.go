package panel

import (
	"errors"
	"net/netip"
)

var (
	ipv6Unicast = netip.MustParsePrefix("2000::/3")
	reservedIP  = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("3fff::/20"),
	}
)

// PublicIP checks obvious local and reserved addresses. The certificate
// authority independently verifies reachability and control of the address.
func PublicIP(value string) (string, error) {
	invalid := errors.New("请输入服务器的公网 IPv4 或 IPv6 地址")
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return "", invalid
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || (addr.Is6() && !ipv6Unicast.Contains(addr)) {
		return "", invalid
	}
	for _, prefix := range reservedIP {
		if prefix.Contains(addr) {
			return "", invalid
		}
	}
	return addr.String(), nil
}
