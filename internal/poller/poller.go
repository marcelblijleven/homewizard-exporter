// Package poller keeps the snapshot current by querying each device on a
// timer, independently of how often Prometheus scrapes.
package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
	"github.com/marcelblijleven/homewizard_exporter/internal/metrics"
	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

const (
	backoffMin = 5 * time.Second
	backoffMax = 2 * time.Minute
)

// Connector opens a connection to a device. It is a field rather than a direct
// call so that tests can drive the poller without a network.
type Connector func(ctx context.Context, device config.Device) (*homewizard.Client, error)

// Poller owns one polling loop per device.
type Poller struct {
	devices    []config.Device
	connect    Connector
	store      *snapshot.Store
	cfg        config.Poll
	logger     *slog.Logger
	metrics    *pollMetrics
	staleAfter time.Duration
	backoffMin time.Duration
	backoffMax time.Duration
}

// Options configures a Poller.
type Options struct {
	Devices  []config.Device
	Connect  Connector
	Store    *snapshot.Store
	Poll     config.Poll
	Logger   *slog.Logger
	Registry prometheus.Registerer

	BackoffMin time.Duration
	BackoffMax time.Duration
}

// New builds a Poller and registers its operational metrics.
func New(opts Options) *Poller {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Connect == nil {
		opts.Connect = func(ctx context.Context, device config.Device) (*homewizard.Client, error) {
			return homewizard.New(ctx, device, opts.Poll.Timeout.Duration(), opts.Logger)
		}
	}
	if opts.BackoffMin <= 0 {
		opts.BackoffMin = backoffMin
	}
	if opts.BackoffMax < opts.BackoffMin {
		opts.BackoffMax = max(backoffMax, opts.BackoffMin)
	}
	return &Poller{
		devices:    opts.Devices,
		connect:    opts.Connect,
		store:      opts.Store,
		cfg:        opts.Poll,
		logger:     opts.Logger,
		metrics:    newPollMetrics(opts.Registry),
		staleAfter: opts.Poll.StaleAfter.Duration(),
		backoffMin: opts.BackoffMin,
		backoffMax: opts.BackoffMax,
	}
}

// Run polls until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	for _, device := range p.devices {
		p.store.Update(device.Name, func(d *snapshot.Device) {
			d.Host = device.Host
		})

		wg.Add(1)
		go func() {
			defer wg.Done()
			p.loop(ctx, device)
		}()
	}

	wg.Wait()
	return nil
}

func (p *Poller) interval(device config.Device) time.Duration {
	if device.Interval > 0 {
		return device.Interval.Duration()
	}
	return p.cfg.Interval.Duration()
}

func (p *Poller) loop(ctx context.Context, device config.Device) {
	name := device.Name
	interval := p.interval(device)

	var client *homewizard.Client
	backoff := p.backoffMin
	timer := time.NewTimer(0) // first poll happens immediately
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		start := time.Now()

		var err error
		if client == nil {
			client, err = p.connect(ctx, device)
			if err == nil {
				p.publishIdentity(client)
			}
		}
		if err == nil {
			err = p.poll(ctx, client)
		}
		elapsed := time.Since(start)

		p.metrics.observe(name, elapsed, err)

		next := interval
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			if errors.Is(err, homewizard.ErrUnauthorized) || errors.Is(err, homewizard.ErrAPIDisabled) {
				client = nil
			}
			p.logger.Warn("poll failed", "device", name, "error", err, "retry_in", backoff)
			next = backoff
			if backoff *= 2; backoff > p.backoffMax {
				backoff = p.backoffMax
			}
		default:
			backoff = p.backoffMin
			p.logger.Debug("poll ok", "device", name, "duration", elapsed)
		}

		// A little jitter keeps several devices from lining up on every tick.
		timer.Reset(next + jitter(next))
	}
}

func (p *Poller) publishIdentity(client *homewizard.Client) {
	p.store.Update(client.Name(), func(d *snapshot.Device) {
		d.Host = client.Host()
		d.Info = client.Info()
		d.Caps = client.Capabilities()
		d.APIVersion = client.APIVersion()
	})
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)/50 + 1))
}

func (p *Poller) poll(ctx context.Context, client *homewizard.Client) error {
	name := client.Name()
	caps := client.Capabilities()

	measurement, err := client.Measurement(ctx)
	if err != nil {
		return fmt.Errorf("measurement: %w", err)
	}

	now := time.Now()
	p.store.Update(name, func(d *snapshot.Device) {
		d.Measurement = measurement
		d.MeasuredAt = now
	})

	if caps.State {
		state, err := client.State(ctx)
		if err != nil {
			p.noteSecondary(name, "state", err, client)
		} else {
			at := time.Now()
			p.store.Update(name, func(d *snapshot.Device) {
				d.State = state
				d.StateAt = at
			})
		}
	}

	if p.due(name, p.cfg.SystemInterval.Duration()) {
		p.pollSlow(ctx, client)
	}
	return nil
}

// reports whether the slow endpoints are ready to be read again.
func (p *Poller) due(name string, every time.Duration) bool {
	device := p.store.Device(name)
	if device == nil || device.SystemAt.IsZero() {
		return true
	}
	return time.Since(device.SystemAt) >= every
}

func (p *Poller) pollSlow(ctx context.Context, client *homewizard.Client) {
	name := client.Name()
	caps := client.Capabilities()

	if caps.System {
		system, err := client.System(ctx)
		if err != nil {
			p.noteSecondary(name, "system", err, client)
		} else {
			at := time.Now()
			p.store.Update(name, func(d *snapshot.Device) {
				d.System = system
				d.SystemAt = at
			})
		}
	}

	if caps.Batteries {
		batteries, err := client.Batteries(ctx)
		if err != nil {
			p.noteSecondary(name, "batteries", err, client)
		} else {
			at := time.Now()
			p.store.Update(name, func(d *snapshot.Device) {
				d.Batteries = batteries
				d.BatteriesAt = at
			})
		}
	}
}

func (p *Poller) noteSecondary(name, endpoint string, err error, client *homewizard.Client) {
	if errors.Is(err, homewizard.ErrNotFound) {
		client.Disable(endpoint)
		caps := client.Capabilities()
		p.logger.Info("device does not serve this endpoint, will not ask again",
			"device", name, "endpoint", endpoint)
		p.store.Update(name, func(d *snapshot.Device) { d.Caps = caps })
		return
	}
	p.logger.Warn("secondary poll failed", "device", name, "endpoint", endpoint, "error", err)
}

// Healthz reports readiness for the HTTP health check.
func (p *Poller) Healthz() error {
	current := p.store.Load()
	if current == nil || len(current.Devices) == 0 {
		return fmt.Errorf("no devices")
	}

	now := time.Now()
	var stale []string
	fresh := 0

	for _, device := range current.Sorted() {
		if device.Fresh(now, p.staleAfter) {
			fresh++
			continue
		}
		switch last := device.LastUpdate(); {
		case last.IsZero():
			stale = append(stale, device.Name+": no successful poll yet")
		default:
			stale = append(stale, fmt.Sprintf("%s: %s old",
				device.Name, now.Sub(last).Truncate(time.Second)))
		}
	}

	if fresh == 0 {
		return fmt.Errorf("no device has fresh readings (stale after %s)\n%s",
			p.staleAfter, strings.Join(stale, "\n"))
	}
	return nil
}

type pollMetrics struct {
	duration    *prometheus.GaugeVec
	total       *prometheus.CounterVec
	errors      *prometheus.CounterVec
	lastSuccess *prometheus.GaugeVec
}

func newPollMetrics(reg prometheus.Registerer) *pollMetrics {
	labels := []string{"device"}
	m := &pollMetrics{
		duration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metrics.Namespace,
			Name:      "poll_duration_seconds",
			Help:      "Duration of the most recent poll.",
		}, labels),
		total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Name:      "poll_total",
			Help:      "Polls attempted.",
		}, labels),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: metrics.Namespace,
			Name:      "poll_errors_total",
			Help:      "Polls that failed.",
		}, labels),
		lastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metrics.Namespace,
			Name:      "poll_last_success_timestamp_seconds",
			Help:      "When each device was last polled successfully, as a Unix timestamp.",
		}, labels),
	}

	if reg != nil {
		reg.MustRegister(m.duration, m.total, m.errors, m.lastSuccess)
	}
	return m
}

func (m *pollMetrics) observe(name string, elapsed time.Duration, err error) {
	m.duration.WithLabelValues(name).Set(elapsed.Seconds())
	m.total.WithLabelValues(name).Inc()

	if err != nil {
		m.errors.WithLabelValues(name).Inc()
		return
	}
	m.lastSuccess.WithLabelValues(name).SetToCurrentTime()
}
