package poller

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeDevice struct {
	server       *httptest.Server
	requests     atomic.Int64
	fail         atomic.Bool
	unauthorized atomic.Bool
	power        atomic.Int64
}

func newFakeDevice(t *testing.T, product string) *fakeDevice {
	t.Helper()
	d := &fakeDevice{}
	d.power.Store(100)

	d.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.requests.Add(1)
		if d.unauthorized.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"user:unauthorized"}`))
			return
		}
		if d.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		switch r.URL.Path {
		case "/api":
			w.Write([]byte(`{"product_type":"` + product + `","serial":"aabb","api_version":"v1"}`))
		case "/api/v1/data":
			w.Write([]byte(`{"active_power_w":` + itoa(d.power.Load()) + `}`))
		case "/api/v1/system":
			w.Write([]byte(`{"cloud_enabled":true}`))
		case "/api/v1/state":
			w.Write([]byte(`{"power_on":true,"switch_lock":false,"brightness":255}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(d.server.Close)
	return d
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func (d *fakeDevice) host() string {
	return strings.TrimPrefix(d.server.URL, "http://")
}

func newTestPoller(store *snapshot.Store, devices ...config.Device) *Poller {
	poll := config.Poll{
		Interval:       config.Duration(20 * time.Millisecond),
		SystemInterval: config.Duration(20 * time.Millisecond),
		Timeout:        config.Duration(time.Second),
		StaleAfter:     config.Duration(time.Minute),
	}
	return New(Options{
		Devices:    devices,
		Store:      store,
		Poll:       poll,
		Logger:     discardLogger(),
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
	})
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPollerPublishesReadings(t *testing.T) {
	device := newFakeDevice(t, homewizard.ProductSocket)
	store := &snapshot.Store{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller := newTestPoller(store, config.Device{
		Name: "socket", Host: device.host(), APIVersion: config.APIAuto,
		TLS: config.TLS{Mode: config.TLSInsecure},
	})
	go poller.Run(ctx)

	waitFor(t, "the first reading", func() bool {
		d := store.Device("socket")
		return d != nil && !d.MeasuredAt.IsZero()
	})

	got := store.Device("socket")
	if got.Measurement.PowerW == nil || *got.Measurement.PowerW != 100 {
		t.Errorf("power = %v, want 100", got.Measurement.PowerW)
	}
	if got.Info.ProductType != homewizard.ProductSocket {
		t.Errorf("product type = %q", got.Info.ProductType)
	}

	waitFor(t, "the relay state", func() bool {
		return !store.Device("socket").StateAt.IsZero()
	})

	// A changed reading has to actually land, not be cached for ever.
	device.power.Store(250)
	waitFor(t, "the reading to update", func() bool {
		p := store.Device("socket").Measurement.PowerW
		return p != nil && *p == 250
	})
}

func TestDeviceRegisteredBeforeFirstPoll(t *testing.T) {
	store := &snapshot.Store{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller := newTestPoller(store, config.Device{
		Name: "missing", Host: "203.0.113.1:1", APIVersion: config.APIv1,
	})
	go poller.Run(ctx)

	waitFor(t, "the device to be registered", func() bool {
		return store.Device("missing") != nil
	})

	device := store.Device("missing")
	if !device.LastUpdate().IsZero() {
		t.Error("a device that never answered should have no update time")
	}
	if err := poller.Healthz(); err == nil {
		t.Error("health should fail when nothing has been read")
	}
}

func TestFailedPollKeepsPreviousReadings(t *testing.T) {
	device := newFakeDevice(t, homewizard.ProductP1)
	store := &snapshot.Store{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller := newTestPoller(store, config.Device{
		Name: "p1", Host: device.host(), APIVersion: config.APIv1,
	})
	go poller.Run(ctx)

	waitFor(t, "the first reading", func() bool {
		d := store.Device("p1")
		return d != nil && !d.MeasuredAt.IsZero()
	})
	measuredAt := store.Device("p1").MeasuredAt

	device.fail.Store(true)
	before := device.requests.Load()
	waitFor(t, "a failed poll", func() bool { return device.requests.Load() > before+1 })

	got := store.Device("p1")
	if got.Measurement.PowerW == nil || *got.Measurement.PowerW != 100 {
		t.Error("the previous reading was discarded on failure")
	}
	if !got.MeasuredAt.Equal(measuredAt) {
		t.Error("a failed poll moved the timestamp, hiding the staleness")
	}
}

func TestHealthzTolerantOfOneDeadDevice(t *testing.T) {
	live := newFakeDevice(t, homewizard.ProductP1)
	store := &snapshot.Store{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller := newTestPoller(
		store,
		config.Device{Name: "p1", Host: live.host(), APIVersion: config.APIv1},
		config.Device{Name: "dead", Host: "203.0.113.1:1", APIVersion: config.APIv1},
	)
	go poller.Run(ctx)

	waitFor(t, "the live device", func() bool {
		d := store.Device("p1")
		return d != nil && !d.MeasuredAt.IsZero()
	})

	if err := poller.Healthz(); err != nil {
		t.Errorf("health should pass while any device is fresh: %v", err)
	}
}

func TestReconnectsAfterFailure(t *testing.T) {
	connects := atomic.Int64{}
	failing := atomic.Bool{}
	failing.Store(true)

	device := newFakeDevice(t, homewizard.ProductP1)
	store := &snapshot.Store{}

	poller := New(Options{
		Devices: []config.Device{{Name: "p1", Host: device.host(), APIVersion: config.APIv1}},
		Store:   store,
		Poll: config.Poll{
			Interval:       config.Duration(20 * time.Millisecond),
			SystemInterval: config.Duration(time.Hour),
			Timeout:        config.Duration(time.Second),
			StaleAfter:     config.Duration(time.Minute),
		},
		Logger:     discardLogger(),
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
		Connect: func(ctx context.Context, d config.Device) (*homewizard.Client, error) {
			connects.Add(1)
			if failing.Load() {
				return nil, errors.New("device is rebooting")
			}
			return homewizard.New(ctx, d, time.Second, discardLogger())
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go poller.Run(ctx)

	waitFor(t, "a few failed connections", func() bool { return connects.Load() >= 2 })
	if store.Device("p1").MeasuredAt != (time.Time{}) {
		t.Error("nothing should have been read while connecting failed")
	}

	failing.Store(false)
	waitFor(t, "the device to come back on its own", func() bool {
		return !store.Device("p1").MeasuredAt.IsZero()
	})
}

func TestUnauthorizedForcesReprobe(t *testing.T) {
	connects := atomic.Int64{}
	store := &snapshot.Store{}
	device := newFakeDevice(t, homewizard.ProductP1)

	poller := New(Options{
		Devices: []config.Device{{Name: "p1", Host: device.host(), APIVersion: config.APIv1}},
		Store:   store,
		Poll: config.Poll{
			Interval:       config.Duration(10 * time.Millisecond),
			SystemInterval: config.Duration(time.Hour),
			Timeout:        config.Duration(time.Second),
			StaleAfter:     config.Duration(time.Minute),
		},
		Logger:     discardLogger(),
		BackoffMin: 10 * time.Millisecond,
		BackoffMax: 50 * time.Millisecond,
		Connect: func(ctx context.Context, d config.Device) (*homewizard.Client, error) {
			connects.Add(1)
			return homewizard.New(ctx, d, time.Second, discardLogger())
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go poller.Run(ctx)

	waitFor(t, "the first connection", func() bool { return connects.Load() >= 1 })

	device.unauthorized.Store(true)
	waitFor(t, "another connection attempt", func() bool { return connects.Load() >= 2 })
}
