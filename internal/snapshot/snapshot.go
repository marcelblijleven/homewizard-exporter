// Package snapshot holds the most recent readings from every device.
package snapshot

import (
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
)

// Device is one consistent view of one device. Treat it as immutable once
type Device struct {
	Name       string
	Host       string
	APIVersion config.APIVersion
	Info       homewizard.Info
	Caps       homewizard.Capabilities

	Measurement homewizard.Measurement
	System      homewizard.System
	State       homewizard.State
	Batteries   homewizard.Batteries

	MeasuredAt  time.Time
	SystemAt    time.Time
	StateAt     time.Time
	BatteriesAt time.Time
}

// LastUpdate reports the most recent successful read of any kind.
func (d Device) LastUpdate() time.Time {
	last := d.MeasuredAt
	for _, at := range []time.Time{d.SystemAt, d.StateAt, d.BatteriesAt} {
		if at.After(last) {
			last = at
		}
	}
	return last
}

// Fresh reports whether the readings are recent enough to trust.
func (d Device) Fresh(now time.Time, staleAfter time.Duration) bool {
	last := d.LastUpdate()
	return !last.IsZero() && now.Sub(last) < staleAfter
}

// Snapshot is the state of every configured device at one moment.
type Snapshot struct {
	Devices map[string]*Device
}

// Sorted returns the devices in name order, so that exposition and the
// dashboard are stable between scrapes rather than following map iteration.
func (s *Snapshot) Sorted() []*Device {
	if s == nil {
		return nil
	}
	names := slices.Sorted(maps.Keys(s.Devices))
	devices := make([]*Device, 0, len(names))
	for _, name := range names {
		devices = append(devices, s.Devices[name])
	}
	return devices
}

// Store publishes snapshots to readers without locking them.
type Store struct {
	// writers serialises the read-modify-write below; readers never take it.
	writers sync.Mutex
	current atomic.Pointer[Snapshot]
}

// Load returns the current snapshot, or nil before anything has been published.
func (s *Store) Load() *Snapshot { return s.current.Load() }

// Device returns one device's current view, or nil if it is not known.
func (s *Store) Device(name string) *Device {
	current := s.Load()
	if current == nil {
		return nil
	}
	return current.Devices[name]
}

// Update applies mutate to a copy of one device and publishes a new snapshot.
func (s *Store) Update(name string, mutate func(*Device)) {
	s.writers.Lock()
	defer s.writers.Unlock()

	next := &Snapshot{Devices: make(map[string]*Device)}
	if current := s.current.Load(); current != nil {
		maps.Copy(next.Devices, current.Devices)
	}

	device := &Device{Name: name}
	if previous, ok := next.Devices[name]; ok {
		copied := *previous
		device = &copied
	}

	mutate(device)
	next.Devices[name] = device
	s.current.Store(next)
}
