// Package config loads and validates the homewizard_exporter YAML configuration.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// MinInterval is the floor on how often a device may be polled. The HomeWizard
// documentation asks for no more than one request every 500ms, and nothing
// useful lives below that/ ven a DSMR 5.0 smart meter only feeds the P1 Meter
// once a second.
const MinInterval = 500 * time.Millisecond

// Duration wraps time.Duration so intervals can be written as "10s" in YAML.
type Duration time.Duration

// Duration returns the wrapped value.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", b, err)
	}
	*d = Duration(v)
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Config is the complete runtime configuration.
type Config struct {
	Devices   []Device  `yaml:"devices"`
	Poll      Poll      `yaml:"poll"`
	Server    Server    `yaml:"server"`
	Dashboard Dashboard `yaml:"dashboard"`
	Log       Log       `yaml:"log"`
}

// APIVersion selects which generation of the local API to speak.
type APIVersion string

// Supported API versions.
const (
	APIAuto APIVersion = "auto" // probe the device and decide
	APIv1   APIVersion = "v1"   // HTTP, no authentication, must be enabled in the app
	APIv2   APIVersion = "v2"   // HTTPS, bearer token
)

// Device is one HomeWizard device on the local network.
//
// Only Host is required. Type and APIVersion are detected at startup, so a
// device can be added by IP address alone and the exporter works out what it
// is talking to.
type Device struct {
	// Name labels every series from this device. It defaults to Host.
	Name string `yaml:"name"`
	Host string `yaml:"host"`

	APIVersion APIVersion `yaml:"api_version"`
	// Type pins the product type (HWE-P1, HWE-KWH3, ...) instead of detecting
	// it. Rarely needed; it exists for a device that answers slowly at boot.
	Type string `yaml:"type"`

	// Token authorises the v2 API. TokenFile is the same thing on disk, which
	// is where `homewizard_exporter pair` writes it.
	Token     string `yaml:"token"`
	TokenFile string `yaml:"token_file"`

	// Interval overrides poll.interval for this device.
	Interval Duration `yaml:"interval"`

	TLS TLS `yaml:"tls"`
}

// TLSMode selects how the device's certificate is checked.
type TLSMode string

// Supported TLS modes.
const (
	// TLSVerify chains the certificate to the HomeWizard CA and checks that its
	// Common Name matches the device.
	TLSVerify TLSMode = "verify"
	// TLSInsecure accepts any certificate. Useful against a stand-in device.
	TLSInsecure TLSMode = "insecure"
)

// TLS controls verification of a device's certificate on the v2 API.
type TLS struct {
	Mode TLSMode `yaml:"mode"`
	// CAFile replaces the built-in HomeWizard CA, for a device presenting a
	// certificate from somewhere else.
	CAFile string `yaml:"ca_file"`
}

// Poll controls how often devices are queried. Scrapes read a cached snapshot,
// so these intervals are the only load the devices ever see.
type Poll struct {
	Interval Duration `yaml:"interval"`
	// SystemInterval paces the endpoints that describe the device rather than
	// the electricity: uptime, Wi-Fi, cloud state, battery group. None of them
	// change between one measurement and the next, so they are read far less
	// often than the readings are.
	SystemInterval Duration `yaml:"system_interval"`
	Timeout        Duration `yaml:"timeout"`
	StaleAfter     Duration `yaml:"stale_after"`
}

// Server configures the HTTP listener.
type Server struct {
	Listen      string   `yaml:"listen"`
	MetricsPath string   `yaml:"metrics_path"`
	ReadTimeout Duration `yaml:"read_timeout"`
}

// Dashboard configures the optional built-in web UI.
type Dashboard struct {
	Enabled bool     `yaml:"enabled"`
	Path    string   `yaml:"path"`
	Refresh Duration `yaml:"refresh"`
}

// Log configures slog.
type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Default returns the configuration used when nothing is specified.
func Default() Config {
	return Config{
		Poll: Poll{
			Interval:       Duration(10 * time.Second),
			SystemInterval: Duration(60 * time.Second),
			Timeout:        Duration(5 * time.Second),
			StaleAfter:     Duration(60 * time.Second),
		},
		Server: Server{
			Listen:      ":9833",
			MetricsPath: "/metrics",
			ReadTimeout: Duration(5 * time.Second),
		},
		Dashboard: Dashboard{
			Path:    "/",
			Refresh: Duration(10 * time.Second),
		},
		Log: Log{Level: "info", Format: "text"},
	}
}

// Load reads path (which may be empty for defaults plus environment), applies
// environment overrides and validates the result.
func Load(path string) (*Config, error) {
	cfg, err := LoadUnvalidated(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadUnvalidated reads the configuration without checking it. It exists for
// callers that fill in missing values before validating; the daemon always
// uses Load.
func LoadUnvalidated(path string) (*Config, error) {
	cfg := Default()

	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("no config file at %s\n\n"+
					"Copy config.example.yaml to %s and list your devices, or drop -config\n"+
					"entirely and set HOMEWIZARD_DEVICES -- the file is optional", path, path)
			}
			return nil, fmt.Errorf("read config: %w", err)
		}
		// A file that is empty or contains only comments carries no settings.
		// Decoding it anyway parses as null and zeroes the defaults, turning
		// one honest "no devices configured" into a wall of noise.
		if hasSettings(b) {
			if err := yaml.UnmarshalWithOptions(b, &cfg, yaml.DisallowUnknownField()); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
		}
	}

	applyEnv(&cfg)
	cfg.normalise()
	return &cfg, nil
}

// hasSettings reports whether the document contains anything other than blank
// lines, comments and document markers.
func hasSettings(b []byte) bool {
	for line := range strings.Lines(string(b)) {
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == "", trimmed == "---", trimmed == "...":
		case strings.HasPrefix(trimmed, "#"):
		default:
			return true
		}
	}
	return false
}

// normalise fills in the values that are derived rather than configured, so
// that everything downstream can read them without repeating the fallbacks.
func (c *Config) normalise() {
	for i := range c.Devices {
		d := &c.Devices[i]
		d.Host = strings.TrimSpace(d.Host)
		d.Name = strings.TrimSpace(d.Name)
		if d.Name == "" {
			d.Name = d.Host
		}
		if d.APIVersion == "" {
			d.APIVersion = APIAuto
		}
		if d.TLS.Mode == "" {
			d.TLS.Mode = TLSVerify
		}
		if d.Interval <= 0 {
			d.Interval = c.Poll.Interval
		}
		// A token in the environment beats both the file and the config, which
		// is what lets the container image take secrets without a config file.
		if token := os.Getenv(d.TokenEnvVar()); token != "" {
			d.Token = token
		}
	}
}

// TokenEnvVar names the variable that carries this device's v2 token. The
// device name is upper-cased with anything unusable replaced by an underscore,
// so a device called "kwh meter" reads HOMEWIZARD_TOKEN_KWH_METER.
func (d Device) TokenEnvVar() string {
	var b strings.Builder
	b.WriteString("HOMEWIZARD_TOKEN_")
	for _, r := range strings.ToUpper(d.Name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// HasToken reports whether a v2 token is available from any source. It does not
// read the file, only check that it is there.
func (d Device) HasToken() bool {
	if d.Token != "" {
		return true
	}
	if d.TokenFile == "" {
		return false
	}
	_, err := os.Stat(d.TokenFile)
	return err == nil
}

// envVars is the complete set of recognised environment variables, excluding
// the per-device HOMEWIZARD_TOKEN_* family which depends on the device names.
var envVars = []struct {
	name  string
	apply func(*Config, string)
}{
	{"HOMEWIZARD_DEVICES", func(c *Config, v string) {
		// Listing hosts here is what lets the exporter run with no config file
		// at all: `HOMEWIZARD_DEVICES=192.168.1.10,p1meter.local` is a whole
		// configuration. Anything beyond a host needs the file.
		for _, host := range strings.Split(v, ",") {
			if host = strings.TrimSpace(host); host != "" {
				c.Devices = append(c.Devices, Device{Host: host})
			}
		}
	}},
	{"HOMEWIZARD_LISTEN", func(c *Config, v string) { c.Server.Listen = v }},
	{"HOMEWIZARD_LOG_LEVEL", func(c *Config, v string) { c.Log.Level = v }},
	{"HOMEWIZARD_LOG_FORMAT", func(c *Config, v string) { c.Log.Format = v }},
	{"HOMEWIZARD_DASHBOARD", func(c *Config, v string) {
		if b, err := strconv.ParseBool(v); err == nil {
			c.Dashboard.Enabled = b
		}
	}},
}

// EnvVarNames returns the recognised environment variables, for documentation
// and for `-check` output.
func EnvVarNames() []string {
	names := make([]string, 0, len(envVars)+1)
	for _, e := range envVars {
		names = append(names, e.name)
	}
	return append(names, "HOMEWIZARD_TOKEN_<DEVICE>")
}

func applyEnv(c *Config) {
	for _, e := range envVars {
		if v, ok := os.LookupEnv(e.name); ok && v != "" {
			e.apply(c, v)
		}
	}
}

// Validate reports every problem it can find at once, so `-check` gives a
// complete picture instead of one error per run.
func (c *Config) Validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if len(c.Devices) == 0 {
		bad("no devices configured (list them under devices: or set HOMEWIZARD_DEVICES)")
	}

	seen := make(map[string]int, len(c.Devices))
	for i, d := range c.Devices {
		where := fmt.Sprintf("devices[%d]", i)
		if d.Name != "" {
			where = fmt.Sprintf("devices[%d] (%s)", i, d.Name)
		}

		switch {
		case d.Host == "":
			bad("%s: host is required", where)
		case strings.Contains(d.Host, "://"):
			bad("%s: host must be a hostname or IP, not a URL: %q", where, d.Host)
		}

		// Duplicate names would silently merge two devices into one set of
		// series, so this is an error rather than a warning.
		if first, dup := seen[d.Name]; dup {
			bad("%s: duplicate device name %q, already used by devices[%d]", where, d.Name, first)
		} else if d.Name != "" {
			seen[d.Name] = i
		}

		switch d.APIVersion {
		case APIAuto, APIv1, APIv2:
		default:
			bad("%s: api_version %q is not one of auto, v1, v2", where, d.APIVersion)
		}
		if d.APIVersion == APIv2 && !d.HasToken() {
			bad("%s: api_version is v2 but no token or token_file was given "+
				"(run `homewizard_exporter pair %s`)", where, d.Host)
		}

		switch d.TLS.Mode {
		case TLSVerify, TLSInsecure:
		default:
			bad("%s: tls.mode %q is not one of verify, insecure", where, d.TLS.Mode)
		}

		if d.Interval > 0 && d.Interval.Duration() < MinInterval {
			bad("%s: interval %s is below the %s the HomeWizard API asks for",
				where, d.Interval, MinInterval)
		}
	}

	if c.Poll.Interval.Duration() < MinInterval {
		bad("poll.interval %s is below the %s the HomeWizard API asks for",
			c.Poll.Interval, MinInterval)
	}
	if c.Poll.SystemInterval <= 0 {
		bad("poll.system_interval must be positive")
	}
	if c.Poll.Timeout <= 0 {
		bad("poll.timeout must be positive")
	}
	if c.Poll.StaleAfter <= 0 {
		bad("poll.stale_after must be positive")
	} else if c.Poll.StaleAfter <= c.Poll.Interval {
		bad("poll.stale_after (%s) must exceed poll.interval (%s) or readings are always stale",
			c.Poll.StaleAfter, c.Poll.Interval)
	}

	if c.Server.Listen == "" {
		bad("server.listen is required")
	}
	if !strings.HasPrefix(c.Server.MetricsPath, "/") {
		bad("server.metrics_path must start with /: %q", c.Server.MetricsPath)
	}
	if c.Dashboard.Enabled {
		if !strings.HasPrefix(c.Dashboard.Path, "/") {
			bad("dashboard.path must start with /: %q", c.Dashboard.Path)
		}
		if c.Dashboard.Path == c.Server.MetricsPath {
			bad("dashboard.path and server.metrics_path are both %q", c.Dashboard.Path)
		}
		if c.Dashboard.Refresh <= 0 {
			bad("dashboard.refresh must be positive")
		}
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		bad("log.level %q is not one of debug, info, warn, error", c.Log.Level)
	}
	switch c.Log.Format {
	case "text", "json":
	default:
		bad("log.format %q is not one of text, json", c.Log.Format)
	}

	return errors.Join(errs...)
}
