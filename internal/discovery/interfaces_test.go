package discovery

import (
	"net"
	"testing"
)

func ipNet(cidr string) net.Addr {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	network.IP = ip
	return network
}

func TestIsLANInterface(t *testing.T) {
	t.Parallel()

	const up = net.FlagUp | net.FlagMulticast
	lanAddrs := []net.Addr{ipNet("192.168.0.172/24")}

	cases := []struct {
		name  string
		iface net.Interface
		addrs []net.Addr
		want  bool
	}{
		{
			name:  "wifi lan interface is kept",
			iface: net.Interface{Name: "en0", Flags: up | net.FlagBroadcast},
			addrs: lanAddrs,
			want:  true,
		},
		{
			name:  "vpn tunnel is dropped by point-to-point flag",
			iface: net.Interface{Name: "utun4", Flags: up | net.FlagPointToPoint},
			addrs: []net.Addr{ipNet("100.101.102.103/32")},
			want:  false,
		},
		{
			name:  "tunnel is dropped by name even without point-to-point flag",
			iface: net.Interface{Name: "utun9", Flags: up},
			addrs: lanAddrs,
			want:  false,
		},
		{
			name:  "airdrop awdl interface is dropped",
			iface: net.Interface{Name: "awdl0", Flags: up},
			addrs: []net.Addr{ipNet("169.254.10.10/16")},
			want:  false,
		},
		{
			name:  "loopback is dropped",
			iface: net.Interface{Name: "lo0", Flags: up | net.FlagLoopback},
			addrs: []net.Addr{ipNet("127.0.0.1/8")},
			want:  false,
		},
		{
			name:  "down interface is dropped",
			iface: net.Interface{Name: "en1", Flags: net.FlagMulticast},
			addrs: lanAddrs,
			want:  false,
		},
		{
			name:  "lan interface without a routable address is dropped",
			iface: net.Interface{Name: "en2", Flags: up | net.FlagBroadcast},
			addrs: []net.Addr{ipNet("169.254.1.1/16")},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isLANInterface(tc.iface, tc.addrs); got != tc.want {
				t.Fatalf("isLANInterface(%s) = %v, want %v", tc.iface.Name, got, tc.want)
			}
		})
	}
}

func TestIsVirtualInterfaceName(t *testing.T) {
	t.Parallel()
	virtual := []string{"utun0", "utun7", "tun1", "tap0", "ppp0", "ipsec1", "wg0", "awdl0", "llw0", "gif0"}
	for _, name := range virtual {
		if !isVirtualInterfaceName(name) {
			t.Errorf("%q should be treated as virtual", name)
		}
	}
	physical := []string{"en0", "en1", "eth0", "Ethernet", "Wi-Fi", "bridge0"}
	for _, name := range physical {
		if isVirtualInterfaceName(name) {
			t.Errorf("%q should not be treated as virtual", name)
		}
	}
}
