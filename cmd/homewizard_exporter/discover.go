package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/discovery"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
)

// runDiscover browses the local network and prints what it finds as a config
// block
func runDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	var (
		timeout = fs.Duration("timeout", 5*time.Second, "how long to listen for devices")
		yaml    = fs.Bool("yaml", true, "print a devices: block ready to paste into the config")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: homewizard_exporter discover [flags]

Finds HomeWizard devices on the local network over mDNS and prints them.

mDNS does not cross subnets, and does not work from inside a container on a
bridge network. Run this on the same network as the devices.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "listening for %s...\n", *timeout)
	devices, err := discovery.Browse(ctx, *timeout)
	if err != nil {
		return err
	}

	if len(devices) == 0 {
		fmt.Fprintln(os.Stderr, "\nNo devices found.\n\n"+
			"mDNS is blocked on many networks and does not cross subnets or reach into\n"+
			"a container. If you know the address, you do not need this command:\n\n"+
			"  homewizard_exporter -config config.yaml\n\n"+
			"with a devices: entry naming the host.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "\nfound %d device(s)\n\n", len(devices))
	for _, device := range devices {
		fmt.Fprintf(os.Stderr, "  %-12s %-16s %s\n",
			orUnknown(device.ProductType), device.Host, summarise(device))
	}

	if !*yaml {
		return nil
	}

	fmt.Println()
	fmt.Println("devices:")
	used := make(map[string]int)
	for _, device := range devices {
		name := uniqueName(deviceName(device), used)

		fmt.Printf("  - name: %s\n", name)
		fmt.Printf("    host: %s\n", device.Host)
		if device.ProductType != "" {
			fmt.Printf("    # %s, serial %s\n", device.ProductType, device.Serial)
		}
		if device.V2 {
			fmt.Printf("    # v2 API available: run `homewizard_exporter pair %s`\n", device.Host)
			fmt.Printf("    # token_file: /var/lib/homewizard_exporter/%s.token\n", name)
		}
		if device.V1 && !device.V1Enabled {
			fmt.Printf("    # v1 API is switched off: enable Local API in the HomeWizard app\n")
		}
	}
	return nil
}

func summarise(device discovery.Device) string {
	var parts []string
	if device.V1 {
		if device.V1Enabled {
			parts = append(parts, "v1")
		} else {
			parts = append(parts, "v1 (disabled)")
		}
	}
	if device.V2 {
		version := "v2"
		if device.APIVersion != "" {
			version += " " + device.APIVersion
		}
		parts = append(parts, version)
	}
	if device.Serial != "" {
		parts = append(parts, device.Serial)
	}
	return strings.Join(parts, "  ")
}

func deviceName(device discovery.Device) string {
	switch device.ProductType {
	case homewizard.ProductP1:
		return "p1"
	case homewizard.ProductKWh1, homewizard.ProductKWh3,
		homewizard.ProductSDM230, homewizard.ProductSDM630:
		return "kwh"
	case homewizard.ProductSocket:
		return "socket"
	case homewizard.ProductWater:
		return "water"
	case homewizard.ProductBattery:
		return "battery"
	default:
		return "device"
	}
}

func uniqueName(name string, used map[string]int) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	return fmt.Sprintf("%s%d", name, used[name])
}

func orUnknown(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
