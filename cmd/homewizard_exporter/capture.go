package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
)

// runCapture saves a device's real responses as test fixtures.
func runCapture(args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	var (
		dir       = fs.String("dir", "internal/homewizard/testdata", "where to write the fixtures")
		tokenFile = fs.String("token-file", "", "file holding the device's v2 token")
		token     = fs.String("token", "", "the device's v2 token")
		insecure  = fs.Bool("insecure", false, "skip verification of the device certificate")
		keep      = fs.Bool("keep-identifiers", false, "do not scrub serials and meter identifiers")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: homewizard_exporter capture <host> [flags]

Saves the responses of a real device as test fixtures. Serial numbers, meter
identifiers and Wi-Fi network names are replaced with placeholders so the files
can be committed.

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

	device := config.Device{
		Name:       "capture",
		Host:       host,
		APIVersion: config.APIAuto,
		Token:      *token,
		TokenFile:  *tokenFile,
		TLS:        config.TLS{Mode: config.TLSVerify},
	}
	if *insecure {
		device.TLS.Mode = config.TLSInsecure
	}

	client, err := homewizard.New(ctx, device, 10*time.Second, nil)
	if err != nil {
		return err
	}

	info := client.Info()
	product := info.ProductType
	if product == "" {
		product = "unknown"
	}

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", *dir, err)
	}

	for _, endpoint := range captureEndpoints(client) {
		body, err := client.GetRaw(ctx, endpoint.path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", endpoint.path, err)
			continue
		}

		if !*keep {
			body = scrub(body)
		}
		if endpoint.pretty {
			body = indentJSON(body)
		}

		name := fmt.Sprintf("%s_%s_%s%s",
			client.APIVersion(), strings.ToLower(product), endpoint.name, endpoint.ext)
		path := filepath.Join(*dir, name)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Println("wrote", path)
	}
	return nil
}

type endpoint struct {
	name   string
	path   string
	ext    string
	pretty bool
}

// captureEndpoints lists what this device can be asked for
func captureEndpoints(client *homewizard.Client) []endpoint {
	v2 := client.APIVersion() == config.APIv2
	caps := client.Capabilities()

	endpoints := []endpoint{{name: "info", path: "/api", ext: ".json", pretty: true}}

	add := func(name, v1Path, v2Path string) {
		path := v1Path
		if v2 {
			path = v2Path
		}
		endpoints = append(endpoints, endpoint{name: name, path: path, ext: ".json", pretty: true})
	}

	add("measurement", "/api/v1/data", "/api/measurement")
	if caps.System {
		add("system", "/api/v1/system", "/api/system")
	}
	if caps.State {
		endpoints = append(endpoints,
			endpoint{name: "state", path: "/api/v1/state", ext: ".json", pretty: true})
	}
	if caps.Batteries {
		endpoints = append(endpoints,
			endpoint{name: "batteries", path: "/api/batteries", ext: ".json", pretty: true})
	}
	if caps.Telegram {
		// The telegram is DSMR text, not JSON, and is captured verbatim.
		path := "/api/v1/telegram"
		if v2 {
			path = "/api/telegram"
		}
		endpoints = append(endpoints, endpoint{name: "telegram", path: path, ext: ".txt"})
	}
	return endpoints
}

// secrets are the keys whose values identify a household rather than describe
// a measurement: meter serials that appear on an energy bill, and the name of
// the home's Wi-Fi network.
var secrets = map[string]string{
	"serial":        "aabbccddeeff",
	"unique_id":     "00112233445566778899AABBCCDDEEFF",
	"gas_unique_id": "FFEEDDCCBBAA99887766554433221100",
	"unique_gas_id": "FFEEDDCCBBAA99887766554433221100",
	"wifi_ssid":     "My Wi-Fi",
}

// telegramIdentifiers matches the DSMR objects that name a physical meter:
// 96.1.0 and 96.1.1 are the equipment identifiers of the electricity meter and
// of anything connected to it, and 96.13.0 is a free-text message field.
//
// These are the numbers printed on the meter and on an energy bill, so a
// telegram is not committable until they are gone.
var telegramIdentifiers = regexp.MustCompile(`(?m)^(\d+-\d+:96\.(?:1\.[01]|13\.0))\(([^)]*)\)`)

// scrub replaces identifying values so a fixture can be committed.
func scrub(body []byte) []byte {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		// Not JSON: a P1 telegram, which carries its own identifiers in its
		// own format and needs scrubbing just as much.
		return scrubTelegram(body)
	}

	walk(doc, 0)

	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}

// walk replaces secret values in place.
func walk(node any, depth int) {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if replacement, secret := secrets[key]; secret {
				if _, isString := child.(string); isString {
					value[key] = replacement
					continue
				}
			}
			walk(child, depth+1)
		}
	case []any:
		for i, child := range value {
			// Give each element of a list its own identifier suffix, so the
			// external meters remain distinguishable after scrubbing.
			if object, ok := child.(map[string]any); ok {
				walk(object, depth+1)
				if id, ok := object["unique_id"].(string); ok {
					object["unique_id"] = fmt.Sprintf("%s%02d", id[:len(id)-2], i)
				}
				continue
			}
			walk(child, depth+1)
		}
	}
}

// scrubTelegram blanks the identifying objects in a DSMR telegram, keeping the
// value's length so the shape of the data is preserved.
// Note: the CRC will probably no longer match after this
func scrubTelegram(body []byte) []byte {
	return telegramIdentifiers.ReplaceAllFunc(body, func(match []byte) []byte {
		parts := telegramIdentifiers.FindSubmatch(match)
		obis, value := parts[1], parts[2]

		return fmt.Appendf(nil, "%s(%s)", obis, strings.Repeat("0", len(value)))
	})
}

func indentJSON(body []byte) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		return body
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
