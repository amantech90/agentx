package discovery

import (
	"context"
	"fmt"
	"net/netip"
	"sync"

	"agentx/internal/model"
	"github.com/betamos/zeroconf"
)

type zeroconfTransport struct{}

type zeroconfRegistration struct {
	client *zeroconf.Client
	once   sync.Once
}

func (registration *zeroconfRegistration) Shutdown() {
	registration.once.Do(func() {
		_ = registration.client.Close()
	})
}

func (zeroconfTransport) Start(ctx context.Context, device model.Device, appVersion, publicKey string, bridgePort uint16, entries chan<- Entry) (registration, error) {
	text := []string{
		"v=" + protocolVersion,
		"id=" + device.ID,
		"name=" + device.Name,
		"os=" + device.OS,
		"arch=" + device.Arch,
		"app=" + appVersion,
	}
	if publicKey != "" {
		text = append(text, "key="+publicKey)
	}
	if bridgePort != 0 {
		text = append(text, fmt.Sprintf("bridge=%d", bridgePort))
	}
	discoveryType := zeroconf.NewType(serviceType)
	service := zeroconf.NewService(discoveryType, fmt.Sprintf("AgentX-%s", device.ID[:12]), uint16(servicePort))
	service.Text = text

	client, err := zeroconf.New().
		Publish(service).
		Expiry(deviceTimeout).
		Browse(func(event zeroconf.Event) {
			if event.Service == nil {
				return
			}
			entry := Entry{
				Text:    append([]string(nil), event.Text...),
				Addrs:   append([]netip.Addr(nil), event.Addrs...),
				Port:    event.Port,
				Removed: event.Op == zeroconf.OpRemoved,
			}
			// Zeroconf callbacks must remain non-blocking. The service consumes
			// this buffered channel continuously; stale removals are also covered
			// by zeroconf's expiry and subsequent events.
			select {
			case entries <- entry:
			case <-ctx.Done():
			default:
			}
		}, discoveryType).
		SrcAddrs(true).
		Open()
	if err != nil {
		return nil, err
	}

	active := &zeroconfRegistration{client: client}
	go func() {
		<-ctx.Done()
		active.Shutdown()
	}()
	return active, nil
}
