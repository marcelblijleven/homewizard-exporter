package homewizard

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
)

func hostOf(t *testing.T, url string) string {
	t.Helper()
	host := strings.TrimPrefix(strings.TrimPrefix(url, "http://"), "https://")
	if host == url && strings.Contains(url, "://") {
		t.Fatalf("unexpected URL %q", url)
	}
	return host
}

func connect(t *testing.T, device config.Device) (*Client, error) {
	t.Helper()
	if device.Name == "" {
		device.Name = "test"
	}
	if device.TLS.Mode == "" {
		device.TLS.Mode = config.TLSInsecure
	}
	return New(context.Background(), device, 2*time.Second, discardLogger())
}

func TestProbeDetectsV1(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			w.Write([]byte(`{"product_type":"HWE-SKT","product_name":"Energy Socket",
				"serial":"aabbccddeeff","firmware_version":"3.03","api_version":"v1"}`))
		case "/api/v1/data":
			w.Write([]byte(`{"active_power_w": 42}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := connect(t, config.Device{Host: hostOf(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}

	if got := client.APIVersion(); got != config.APIv1 {
		t.Errorf("api version = %s, want v1", got)
	}
	if got := client.Info().ProductType; got != ProductSocket {
		t.Errorf("product type = %s, want %s", got, ProductSocket)
	}

	// A socket is the one product with a relay, and only on v1.
	caps := client.Capabilities()
	if !caps.State {
		t.Error("an Energy Socket on v1 should have a state endpoint")
	}
	if caps.Telegram || caps.Batteries {
		t.Errorf("a socket should have neither telegram nor batteries: %+v", caps)
	}

	m, err := client.Measurement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantFloat(t, "power", m.PowerW, 42)
}

func TestProbePrefersV2WithToken(t *testing.T) {
	var v1Called bool
	v1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v1Called = true
		w.Write([]byte(`{"product_type":"HWE-P1","serial":"x","api_version":"v1"}`))
	}))
	defer v1.Close()

	v2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer TOKEN" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"user:unauthorized"}`))
			return
		}
		if got := r.Header.Get("X-Api-Version"); got != "2" {
			t.Errorf("X-Api-Version = %q, want 2", got)
		}
		w.Write([]byte(`{"product_type":"HWE-P1","product_name":"P1 Meter","serial":"aabb",
			"firmware_version":"6.00","api_version":"2.3.0"}`))
	}))
	defer v2.Close()

	client, err := connect(t, config.Device{Host: hostOf(t, v2.URL), Token: "TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if got := client.APIVersion(); got != config.APIv2 {
		t.Errorf("api version = %s, want v2", got)
	}
	if v1Called {
		t.Error("v1 should not have been tried once v2 answered")
	}

	if !client.Capabilities().Batteries {
		t.Error("a P1 Meter on API 2.3.0 should offer the batteries endpoint")
	}
}

func TestProbeReportsMissingToken(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"user:unauthorized"}`))
	}))
	defer server.Close()

	_, err := connect(t, config.Device{Host: hostOf(t, server.URL), APIVersion: config.APIv2})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "pair") {
		t.Errorf("error should point at the pair command, got: %v", err)
	}
}

func TestProbeReportsDisabledV1(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"id":202,"description":"API not enabled"}}`))
	}))
	defer server.Close()

	_, err := connect(t, config.Device{Host: hostOf(t, server.URL), APIVersion: config.APIv1})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrAPIDisabled) {
		t.Errorf("want ErrAPIDisabled, got %v", err)
	}
	if !strings.Contains(err.Error(), "Local API") {
		t.Errorf("error should name the app setting, got: %v", err)
	}
}

func TestTypeOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"product_type":"HWE-P1","serial":"x","api_version":"v1"}`))
	}))
	defer server.Close()

	client, err := connect(t, config.Device{Host: hostOf(t, server.URL), Type: ProductWater})
	if err != nil {
		t.Fatal(err)
	}
	if got := client.Info().ProductType; got != ProductWater {
		t.Errorf("product type = %s, want the configured %s", got, ProductWater)
	}
}

func TestDisableStopsAsking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"product_type":"HWE-P1","serial":"x","api_version":"v1"}`))
	}))
	defer server.Close()

	client, err := connect(t, config.Device{Host: hostOf(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	if !client.Capabilities().System {
		t.Fatal("system should start enabled")
	}

	client.Disable("system")
	if client.Capabilities().System {
		t.Error("system should be disabled after a 404")
	}
	if !client.Capabilities().Measurement {
		t.Error("disabling one endpoint must not disturb the others")
	}
}

func TestNotFoundIsDistinct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" {
			w.Write([]byte(`{"product_type":"HWE-P1","serial":"x","api_version":"v1"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := connect(t, config.Device{Host: hostOf(t, server.URL)})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Measurement(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestParseCommonName(t *testing.T) {
	tests := []struct {
		cn      string
		want    identity
		wantErr bool
	}{
		{
			cn:   "appliance/p1dongle/5c2fafaabbcc",
			want: identity{CertType: "p1dongle", ProductType: ProductP1, Serial: "5c2fafaabbcc"},
		},
		{
			cn:   "appliance/battery/aabbccddeeff",
			want: identity{CertType: "battery", ProductType: ProductBattery, Serial: "aabbccddeeff"},
		},
		// The certificate cannot tell a 1-phase kWh Meter from a 3-phase one;
		// only /api can, so the product type stays blank rather than guessing.
		{
			cn:   "appliance/energymeter/aabbccddeeff",
			want: identity{CertType: "energymeter", Serial: "aabbccddeeff"},
		},
		// Hardware released after this build still yields a usable serial.
		{
			cn:   "appliance/somethingnew/aabbccddeeff",
			want: identity{CertType: "somethingnew", Serial: "aabbccddeeff"},
		},
		{cn: "p1meter.local", wantErr: true},
		{cn: "appliance/p1dongle", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.cn, func(t *testing.T) {
			got, err := parseCommonName(tt.cn)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.cn)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestAtLeastAPI(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"2.1.0", true},
		{"2.3.0", true},
		{"2.0.1", false},
		{"3.0.0", true},
		{"1.0.0", false},
		// The v1 API answers with a word, which is older than any number.
		{"v1", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := atLeastAPI(tt.version, 2, 1); got != tt.want {
				t.Errorf("atLeastAPI(%q, 2, 1) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

func TestCapabilitiesByProduct(t *testing.T) {
	tests := []struct {
		product string
		version config.APIVersion
		api     string
		want    Capabilities
	}{
		{
			ProductP1, config.APIv2, "2.3.0",
			Capabilities{Measurement: true, System: true, Telegram: true, Batteries: true},
		},
		{
			ProductP1, config.APIv1, "v1",
			Capabilities{Measurement: true, System: true, Telegram: true},
		},
		{
			ProductKWh3, config.APIv2, "2.0.0",
			Capabilities{Measurement: true, System: true},
		},
		{
			ProductSocket, config.APIv1, "v1",
			Capabilities{Measurement: true, System: true, State: true},
		},
		{
			ProductBattery, config.APIv2, "2.3.0",
			Capabilities{Measurement: true, System: true},
		},
		// Unknown hardware is still worth asking for the basics.
		{
			"HWE-FUTURE", config.APIv2, "3.0.0",
			Capabilities{Measurement: true, System: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.product+"/"+string(tt.version), func(t *testing.T) {
			info := Info{ProductType: tt.product, APIVersion: tt.api}
			if got := capabilitiesFor(info, tt.version); got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// discardLogger keeps the connection banner out of test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
