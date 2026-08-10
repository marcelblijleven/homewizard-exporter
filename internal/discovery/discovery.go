// Package discovery finds HomeWizard devices on the local network over mDNS.
package discovery

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

const (
	ServiceV1 = "_hwenergy._tcp"
	ServiceV2 = "_homewizard._tcp"
)

// Device is one device found on the network.
type Device struct {
	Instance    string
	Host        string
	Port        int
	ProductType string
	ProductName string
	Serial      string

	// APIVersion is what the v2 service advertises. Empty for a v1-only device.
	APIVersion string
	// V1, V2 record which services this device answered on.
	V1, V2 bool
	// V1Enabled is the v1 service's api_enabled flag: a device can advertise
	// the v1 API while having it switched off in the app, which is the single
	// most common reason a working address returns 403.
	V1Enabled bool
}

// Browse looks for devices until timeout elapses or ctx is cancelled.
func Browse(ctx context.Context, timeout time.Duration) ([]Device, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	found := make(map[string]*Device)

	for _, service := range []string{ServiceV2, ServiceV1} {
		entries := make(chan *zeroconf.ServiceEntry, 16)
		done := make(chan struct{})

		go func() {
			defer close(done)
			for entry := range entries {
				merge(found, service, entry)
			}
		}()

		// zeroconf closes the channel when the context expires, which is what
		// ends the goroutine above.
		if err := zeroconf.Browse(ctx, service, "local.", entries); err != nil {
			return nil, fmt.Errorf("browse %s: %w", service, err)
		}
		<-done
	}

	devices := make([]Device, 0, len(found))
	for _, device := range found {
		devices = append(devices, *device)
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].ProductType != devices[j].ProductType {
			return devices[i].ProductType < devices[j].ProductType
		}
		return devices[i].Serial < devices[j].Serial
	})
	return devices, nil
}

func merge(found map[string]*Device, service string, entry *zeroconf.ServiceEntry) {
	txt := parseTXT(entry.Text)

	key := txt["serial"]
	if key == "" {
		key = entry.Instance
	}

	device, ok := found[key]
	if !ok {
		device = &Device{Instance: entry.Instance, Serial: txt["serial"]}
		found[key] = device
	}

	if host := address(entry); host != "" {
		device.Host = host
	}
	if entry.Port != 0 {
		device.Port = entry.Port
	}
	if v := txt["product_type"]; v != "" {
		device.ProductType = v
	}
	if v := txt["product_name"]; v != "" {
		device.ProductName = v
	}

	switch service {
	case ServiceV2:
		device.V2 = true
		device.APIVersion = txt["api_version"]
	case ServiceV1:
		device.V1 = true
		device.V1Enabled = txt["api_enabled"] == "1"
	}
}

func address(entry *zeroconf.ServiceEntry) string {
	for _, ip := range entry.AddrIPv4 {
		if addr, ok := netip.AddrFromSlice(ip.To4()); ok {
			return addr.String()
		}
	}
	for _, ip := range entry.AddrIPv6 {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			return addr.String()
		}
	}
	return ""
}

func parseTXT(records []string) map[string]string {
	txt := make(map[string]string, len(records))
	for _, record := range records {
		key, value, found := strings.Cut(record, "=")
		if found {
			txt[key] = value
		}
	}
	return txt
}
