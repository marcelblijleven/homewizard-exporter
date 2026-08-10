package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
)

// runPair retrieves a v2 API token.
func runPair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	var (
		out      = fs.String("o", "", "write the token to this file instead of stdout")
		name     = fs.String("name", homewizard.DefaultUserName, "user name to create on the device")
		insecure = fs.Bool("insecure", false, "skip verification of the device certificate")
		wait     = fs.Duration("wait", 2*time.Minute, "how long to wait for the button press")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: homewizard_exporter pair <host> [flags]

Creates a user on a HomeWizard device and prints the API token it issues. You
will be asked to press the button on the device.

Flags:
`)
		fs.PrintDefaults()
	}

	host, err := parseWithHost(fs, args)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *wait)
	defer cancel()

	tls := config.TLS{Mode: config.TLSVerify}
	if *insecure {
		tls.Mode = config.TLSInsecure
	}

	fmt.Fprintf(os.Stderr, "connecting to %s\n", host)

	result, err := homewizard.Pair(ctx, homewizard.PairOptions{
		Host:    host,
		Name:    *name,
		TLS:     tls,
		Timeout: 10 * time.Second,
		OnWaiting: func(id homewizard.IdentityHint) {
			what := "the device"
			if id.ProductType != "" {
				what = "the " + id.ProductType
			}
			fmt.Fprintf(os.Stderr, "\nPress the button on %s", what)
			if id.Serial != "" {
				fmt.Fprintf(os.Stderr, " (serial %s)", id.Serial)
			}
			fmt.Fprintf(os.Stderr, ".\nOn a kWh Meter, hold the Wi-Fi pair button for 1 to 3 seconds.\n"+
				"Waiting up to %s...\n", *wait)
		},
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\npaired with %s (%s), serial %s, firmware %s\n",
		result.Info.ProductName, result.Info.ProductType,
		result.Info.Serial, result.Info.FirmwareVersion)

	if *out == "" {
		fmt.Println(result.Token)
		fmt.Fprintf(os.Stderr, "\nAdd it to your config:\n\n"+
			"  devices:\n    - name: %s\n      host: %s\n      token_file: /var/lib/homewizard_exporter/%s.token\n\n"+
			"or export %s\n",
			suggestName(result.Info), host, suggestName(result.Info),
			config.Device{Name: suggestName(result.Info)}.TokenEnvVar())
		return nil
	}

	if err := writeToken(*out, result.Token); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "token written to %s\n", *out)
	return nil
}

func writeToken(path, token string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func suggestName(info homewizard.Info) string {
	switch info.ProductType {
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
