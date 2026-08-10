package snapshot

import (
	"sync"
	"testing"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
)

func TestLoadBeforeAnyUpdate(t *testing.T) {
	var store Store
	if got := store.Load(); got != nil {
		t.Errorf("Load() = %+v, want nil before anything is published", got)
	}
	if got := store.Device("p1"); got != nil {
		t.Errorf("Device() = %+v, want nil", got)
	}
}

func TestUpdateDoesNotMutatePublished(t *testing.T) {
	var store Store
	store.Update("p1", func(d *Device) { d.Host = "192.168.1.10" })

	before := store.Load()
	beforeDevice := before.Devices["p1"]

	store.Update("p1", func(d *Device) { d.Host = "192.168.1.99" })
	store.Update("kwh", func(d *Device) { d.Host = "192.168.1.11" })

	if beforeDevice.Host != "192.168.1.10" {
		t.Errorf("the published device changed to %q", beforeDevice.Host)
	}
	if len(before.Devices) != 1 {
		t.Errorf("the published map grew to %d devices", len(before.Devices))
	}

	after := store.Load()
	if after.Devices["p1"].Host != "192.168.1.99" {
		t.Error("the update did not take effect")
	}
	if len(after.Devices) != 2 {
		t.Errorf("devices = %d, want 2", len(after.Devices))
	}
}

func TestUpdatePreservesOtherFields(t *testing.T) {
	var store Store
	store.Update("p1", func(d *Device) {
		d.Measurement = homewizard.Measurement{MeterModel: "ISKRA"}
		d.MeasuredAt = time.Now()
	})
	store.Update("p1", func(d *Device) {
		d.System = homewizard.System{}
		d.SystemAt = time.Now()
	})

	device := store.Device("p1")
	if device.Measurement.MeterModel != "ISKRA" {
		t.Error("the measurement was lost when the system poll landed")
	}
	if device.MeasuredAt.IsZero() {
		t.Error("the measurement timestamp was lost")
	}
}

func TestLastUpdateTakesTheNewest(t *testing.T) {
	base := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	device := Device{
		MeasuredAt:  base,
		SystemAt:    base.Add(time.Minute),
		BatteriesAt: base.Add(-time.Minute),
	}

	if got := device.LastUpdate(); !got.Equal(base.Add(time.Minute)) {
		t.Errorf("LastUpdate() = %s, want the newest of the four", got)
	}
	if got := (Device{}).LastUpdate(); !got.IsZero() {
		t.Errorf("LastUpdate() = %s, want zero for a device never read", got)
	}
}

func TestFresh(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		device Device
		want   bool
	}{
		{"just read", Device{MeasuredAt: now.Add(-time.Second)}, true},
		{"stale", Device{MeasuredAt: now.Add(-2 * time.Minute)}, false},
		{"never read", Device{}, false},
		{"exactly at the limit", Device{MeasuredAt: now.Add(-time.Minute)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.device.Fresh(now, time.Minute); got != tt.want {
				t.Errorf("Fresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortedIsStable(t *testing.T) {
	var store Store
	for _, name := range []string{"water", "p1", "kwh", "battery"} {
		store.Update(name, func(d *Device) { d.Host = name })
	}

	want := []string{"battery", "kwh", "p1", "water"}
	for range 5 {
		got := store.Load().Sorted()
		if len(got) != len(want) {
			t.Fatalf("devices = %d, want %d", len(got), len(want))
		}
		for i, device := range got {
			if device.Name != want[i] {
				t.Fatalf("device %d = %q, want %q", i, device.Name, want[i])
			}
		}
	}

	if got := (*Snapshot)(nil).Sorted(); got != nil {
		t.Errorf("Sorted() on a nil snapshot = %v, want nil", got)
	}
}

func TestConcurrentUpdates(t *testing.T) {
	var store Store
	var wg sync.WaitGroup

	for _, name := range []string{"p1", "kwh", "socket", "water"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				store.Update(name, func(d *Device) {
					d.Host = name
					d.MeasuredAt = time.Now()
					d.Measurement = homewizard.Measurement{MeterModel: name}
				})
				_ = i
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			for _, device := range store.Load().Sorted() {
				if device.Measurement.MeterModel != "" && device.Measurement.MeterModel != device.Host {
					t.Errorf("torn read: model %q on host %q",
						device.Measurement.MeterModel, device.Host)
				}
			}
		}
	}()

	wg.Wait()

	if got := len(store.Load().Devices); got != 4 {
		t.Errorf("devices = %d, want 4", got)
	}
}
