package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMinimalConfig: a host and nothing else has to be enough. Everything the
// exporter needs beyond that is either defaulted or detected from the device.
func TestMinimalConfig(t *testing.T) {
	path := writeConfig(t, `
devices:
  - host: 192.168.1.10
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 {
		t.Fatalf("devices = %d, want 1", len(cfg.Devices))
	}

	device := cfg.Devices[0]
	if device.Name != "192.168.1.10" {
		t.Errorf("name = %q, want it to default to the host", device.Name)
	}
	if device.APIVersion != APIAuto {
		t.Errorf("api_version = %q, want auto", device.APIVersion)
	}
	if device.TLS.Mode != TLSVerify {
		t.Errorf("tls.mode = %q, want verify", device.TLS.Mode)
	}
	if device.Interval.Duration() != 10*time.Second {
		t.Errorf("interval = %s, want the global default", device.Interval)
	}
}

// TestNoConfigFile: the exporter has to be runnable from environment alone, so
// that a container needs no volume mount.
func TestNoConfigFile(t *testing.T) {
	t.Setenv("HOMEWIZARD_DEVICES", "192.168.1.10, p1meter.local")
	t.Setenv("HOMEWIZARD_LISTEN", ":9999")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 2 {
		t.Fatalf("devices = %d, want 2", len(cfg.Devices))
	}
	if cfg.Devices[1].Host != "p1meter.local" {
		t.Errorf("second host = %q (whitespace should be trimmed)", cfg.Devices[1].Host)
	}
	if cfg.Server.Listen != ":9999" {
		t.Errorf("listen = %q", cfg.Server.Listen)
	}
}

// TestEmptyFileIsNotAWipe: a file of nothing but comments carries no settings,
// and decoding it would otherwise zero every default and bury the one real
// problem under a wall of validation noise.
func TestEmptyFileIsNotAWipe(t *testing.T) {
	path := writeConfig(t, "# nothing here yet\n---\n")

	cfg, err := LoadUnvalidated(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != Default().Server.Listen {
		t.Errorf("listen = %q, want the default to survive", cfg.Server.Listen)
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("a config with no devices should not validate")
	}
	if got := strings.Count(err.Error(), "\n") + 1; got != 1 {
		t.Errorf("want exactly one error, got %d:\n%v", got, err)
	}
}

// TestTokenFromEnvironment: per-device secrets cannot use a fixed variable
// name, so they are derived from the device name.
func TestTokenFromEnvironment(t *testing.T) {
	t.Setenv("HOMEWIZARD_TOKEN_KWH_METER", "SECRET")

	path := writeConfig(t, `
devices:
  - name: kwh meter
    host: 192.168.1.11
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Devices[0].Token; got != "SECRET" {
		t.Errorf("token = %q, want it read from HOMEWIZARD_TOKEN_KWH_METER", got)
	}
}

func TestTokenEnvVar(t *testing.T) {
	tests := map[string]string{
		"p1":           "HOMEWIZARD_TOKEN_P1",
		"kwh meter":    "HOMEWIZARD_TOKEN_KWH_METER",
		"shed-1":       "HOMEWIZARD_TOKEN_SHED_1",
		"192.168.1.10": "HOMEWIZARD_TOKEN_192_168_1_10",
	}

	for name, want := range tests {
		if got := (Device{Name: name}).TokenEnvVar(); got != want {
			t.Errorf("%q -> %q, want %q", name, got, want)
		}
	}
}

// TestValidateReportsEverything: -check should give a complete picture in one
// run rather than one problem per attempt.
func TestValidateReportsEverything(t *testing.T) {
	path := writeConfig(t, `
devices:
  - name: p1
    host: http://192.168.1.10
  - name: p1
    host: 192.168.1.11
    api_version: v3
    tls:
      mode: sometimes
poll:
  interval: 100ms
log:
  level: chatty
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected errors")
	}

	for _, want := range []string{
		"must be a hostname or IP",
		"duplicate device name",
		"api_version",
		"tls.mode",
		"poll.interval",
		"log.level",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got:\n%v", want, err)
		}
	}
}

// TestPollIntervalFloor: the HomeWizard documentation asks for no more than one
// request every 500ms, and a device polled harder is a device that misbehaves.
func TestPollIntervalFloor(t *testing.T) {
	cfg := Default()
	cfg.Devices = []Device{{Name: "p1", Host: "h", APIVersion: APIAuto, TLS: TLS{Mode: TLSVerify}}}

	cfg.Poll.Interval = Duration(100 * time.Millisecond)
	if err := cfg.Validate(); err == nil {
		t.Error("100ms should be rejected")
	}

	cfg.Poll.Interval = Duration(MinInterval)
	if err := cfg.Validate(); err != nil {
		t.Errorf("%s is the documented floor and should be allowed: %v", MinInterval, err)
	}
}

// TestStaleAfterMustExceedInterval, or every reading is stale the moment it
// lands and homewizard_up is always 0.
func TestStaleAfterMustExceedInterval(t *testing.T) {
	cfg := Default()
	cfg.Devices = []Device{{Name: "p1", Host: "h", APIVersion: APIAuto, TLS: TLS{Mode: TLSVerify}}}
	cfg.Poll.Interval = Duration(30 * time.Second)
	cfg.Poll.StaleAfter = Duration(30 * time.Second)

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "always stale") {
		t.Errorf("want the always-stale error, got %v", err)
	}
}

// TestV2NeedsCredentials: pinning v2 without a token cannot work, and saying so
// at load time beats a connection error per poll for ever.
func TestV2NeedsCredentials(t *testing.T) {
	path := writeConfig(t, `
devices:
  - name: battery
    host: 192.168.1.12
    api_version: v2
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "pair") {
		t.Errorf("want an error pointing at the pair command, got %v", err)
	}
}

func TestHasToken(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "p1.token")

	device := Device{Name: "p1", TokenFile: tokenFile}
	if device.HasToken() {
		t.Error("no token file exists yet")
	}

	if err := os.WriteFile(tokenFile, []byte("ABC\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !device.HasToken() {
		t.Error("the token file exists now")
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	path := writeConfig(t, `
devices:
  - host: 192.168.1.10
    hostname: 192.168.1.11
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("a misspelled key should not be silently ignored")
	}
}

func TestDurationRoundTrip(t *testing.T) {
	path := writeConfig(t, `
devices:
  - host: 192.168.1.10
    interval: 2s
poll:
  interval: 1s
  stale_after: 45s
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Devices[0].Interval.Duration(); got != 2*time.Second {
		t.Errorf("device interval = %s", got)
	}
	if got := cfg.Poll.StaleAfter.Duration(); got != 45*time.Second {
		t.Errorf("stale after = %s", got)
	}
}
