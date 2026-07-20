package discovery

import (
	"net"
	"strings"
)

// virtualPrefixes are interface-name prefixes for VPN tunnels and other
// virtual links that carry an mDNS multicast group but are not the shared LAN.
// When a machine runs several of these (Tailscale, corporate VPNs, iCloud
// Private Relay, WireGuard, AirDrop's AWDL), the default net.Interfaces() list
// can leave the mDNS library browsing tunnels instead of the real network, so
// a peer that is plainly reachable on the LAN is never discovered.
var virtualPrefixes = []string{
	"utun", "tun", "tap", "ppp", "ipsec", "wg", // VPN / tunnels
	"awdl", "llw", // Apple Wireless Direct Link (AirDrop)
	"gif", "stf", // generic/6to4 tunnels
}

// lanInterfaces returns only the multicast-capable physical LAN interfaces,
// falling back to every interface if the filter would otherwise leave mDNS
// with nothing to bind to.
func lanInterfaces() ([]net.Interface, error) {
	all, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	filtered := make([]net.Interface, 0, len(all))
	for _, iface := range all {
		addrs, _ := iface.Addrs()
		if isLANInterface(iface, addrs) {
			filtered = append(filtered, iface)
		}
	}
	if len(filtered) == 0 {
		return all, nil
	}
	return filtered, nil
}

// isLANInterface reports whether an interface is a real shared-LAN link
// suitable for mDNS: up, multicast-capable, not loopback, not a point-to-point
// tunnel, not a known virtual interface, and carrying a routable address.
func isLANInterface(iface net.Interface, addrs []net.Addr) bool {
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 {
		return false
	}
	if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 {
		return false
	}
	if isVirtualInterfaceName(iface.Name) {
		return false
	}
	for _, addr := range addrs {
		if hasRoutableIP(addr) {
			return true
		}
	}
	return false
}

func isVirtualInterfaceName(name string) bool {
	name = strings.ToLower(name)
	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func hasRoutableIP(addr net.Addr) bool {
	var ip net.IP
	switch value := addr.(type) {
	case *net.IPNet:
		ip = value.IP
	case *net.IPAddr:
		ip = value.IP
	default:
		return false
	}
	return ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}
