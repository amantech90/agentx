package discovery

import (
	"encoding/base64"
	"net/netip"
	"testing"
)

const (
	localDeviceID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	remoteDeviceID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestSelectEndpointPrefersLANOverTunnelAddresses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		addrs []string
		want  string
	}{
		{
			name:  "lan address wins when advertised after tunnels",
			addrs: []string{"fe80::1", "100.101.102.103", "2001:db8::5", "192.168.0.172"},
			want:  "192.168.0.172:41938",
		},
		{
			name:  "carrier-grade nat loses to lan even when listed first",
			addrs: []string{"100.64.5.5", "10.0.0.9"},
			want:  "10.0.0.9:41938",
		},
		{
			name:  "global ipv4 used only when no private lan address exists",
			addrs: []string{"2001:db8::7", "203.0.113.10"},
			want:  "203.0.113.10:41938",
		},
		{
			name:  "tunnel address used as a last resort over ipv6",
			addrs: []string{"fe80::9", "100.100.100.100"},
			want:  "100.100.100.100:41938",
		},
		{
			name:  "link-local and loopback are never selected",
			addrs: []string{"169.254.1.1", "127.0.0.1"},
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			addrs := make([]netip.Addr, 0, len(tc.addrs))
			for _, raw := range tc.addrs {
				addrs = append(addrs, netip.MustParseAddr(raw))
			}
			if got := selectEndpoint(addrs, 41938); got != tc.want {
				t.Fatalf("selectEndpoint(%v) = %q, want %q", tc.addrs, got, tc.want)
			}
		})
	}
}

func TestParseDeviceTXTValidatesUntrustedMetadata(t *testing.T) {
	t.Parallel()

	device, version, ok := parseDeviceTXT([]string{
		"v=1", "id=" + remoteDeviceID, "name=Aman Windows", "os=windows", "arch=amd64", "app=0.1.0",
	})
	if !ok {
		t.Fatal("parseDeviceTXT() rejected valid metadata")
	}
	if device.ID != remoteDeviceID || device.Name != "Aman Windows" || device.OS != "windows" || device.Arch != "amd64" {
		t.Fatalf("device = %#v", device)
	}
	if device.Trusted {
		t.Fatal("a discovered device must not be trusted")
	}
	if version != "0.1.0" {
		t.Fatalf("version = %q", version)
	}

	invalid := [][]string{
		{"v=2", "id=" + remoteDeviceID, "name=Windows", "os=windows", "arch=amd64"},
		{"v=1", "id=../../unsafe", "name=Windows", "os=windows", "arch=amd64"},
		{"v=1", "id=" + remoteDeviceID, "name=Windows", "os=unknown", "arch=amd64"},
		{"v=1", "id=" + remoteDeviceID, "name=\n", "os=windows", "arch=amd64"},
	}
	for _, records := range invalid {
		if _, _, ok := parseDeviceTXT(records); ok {
			t.Fatalf("parseDeviceTXT() accepted %#v", records)
		}
	}
}

func TestServiceKeepsPairingEndpointAndMarksOnlyMatchingKeyTrusted(t *testing.T) {
	t.Parallel()

	publicKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	service := newRegistry(localDeviceID)
	service.SetTrustProvider(func(deviceID, advertisedKey string) bool {
		return deviceID == remoteDeviceID && advertisedKey == publicKey
	})
	entry := Entry{
		Text: []string{
			"v=1", "id=" + remoteDeviceID, "name=Aman Windows", "os=windows", "arch=amd64", "app=0.1.0", "key=" + publicKey, "bridge=41938",
		},
		Addrs: []netip.Addr{netip.MustParseAddr("192.168.1.24")}, Port: servicePort,
	}
	if !service.observe(entry) {
		t.Fatal("observe() did not add pairable peer")
	}
	devices := service.Devices()
	if len(devices) != 1 || !devices[0].Trusted {
		t.Fatalf("devices = %#v, want trusted peer", devices)
	}
	peer, ok := service.Lookup(remoteDeviceID)
	if !ok || peer.PublicKey != publicKey || peer.Endpoint != "192.168.1.24:41937" || peer.BridgeEndpoint != "192.168.1.24:41938" {
		t.Fatalf("Lookup() = %#v, %v", peer, ok)
	}

	service.SetTrustProvider(func(string, string) bool { return false })
	service.RefreshTrust()
	if service.Devices()[0].Trusted {
		t.Fatal("RefreshTrust() kept a removed trust relationship")
	}
}

func TestServiceFiltersSelfDeduplicatesAndRemovesDevices(t *testing.T) {
	t.Parallel()

	service := newRegistry(localDeviceID)
	self := Entry{Text: []string{"v=1", "id=" + localDeviceID, "name=This Mac", "os=darwin", "arch=arm64", "app=0.1.0"}}
	remote := Entry{Text: []string{"v=1", "id=" + remoteDeviceID, "name=Aman Windows", "os=windows", "arch=amd64", "app=0.1.0"}}

	if service.observe(self) {
		t.Fatal("observe() reported a change for the local device")
	}
	if !service.observe(remote) {
		t.Fatal("observe() did not add the remote device")
	}
	if service.observe(remote) {
		t.Fatal("observe() reported a metadata change for a duplicate announcement")
	}
	if got := service.Devices(); len(got) != 1 || got[0].ID != remoteDeviceID {
		t.Fatalf("Devices() = %#v", got)
	}

	remote.Removed = true
	if !service.observe(remote) {
		t.Fatal("observe() did not remove a departed device")
	}
	if got := service.Devices(); len(got) != 0 {
		t.Fatalf("Devices() after removal = %#v", got)
	}
}
