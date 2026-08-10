package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

// Collector turns the current snapshot into metrics. It never blocks on a
// device: a scrape reads whatever the pollers last published.
type Collector struct {
	store      *snapshot.Store
	staleAfter time.Duration

	now func() time.Time
}

// CollectorOptions configures a Collector.
type CollectorOptions struct {
	Store      *snapshot.Store
	StaleAfter time.Duration
	Now        func() time.Time
}

// NewCollector builds a Collector.
func NewCollector(opts CollectorOptions) *Collector {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Collector{store: opts.Store, staleAfter: opts.StaleAfter, now: opts.Now}
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range allDescs() {
		ch <- d
	}
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	current := c.store.Load()
	if current == nil {
		return
	}

	e := emitter{ch: ch}
	devices := current.Sorted()
	e.plain(descDevicesTotal, prometheus.GaugeValue, float64(len(devices)))

	now := c.now()
	for _, device := range devices {
		c.collectDevice(e, device, now)
	}
}

func (c *Collector) collectDevice(e emitter, d *snapshot.Device, now time.Time) {
	e.gauge(descUp, d.Name, boolValue(d.Fresh(now, c.staleAfter)))

	e.gauge(descDeviceInfo, d.Name, 1,
		d.Info.ProductType, d.Info.ProductName, d.Info.Serial,
		d.Info.FirmwareVersion, string(d.APIVersion), d.Info.APIVersion)

	if last := d.LastUpdate(); !last.IsZero() {
		e.gauge(descLastUpdate, d.Name, float64(last.Unix()))
	}

	if !d.MeasuredAt.IsZero() {
		c.collectMeasurement(e, d)
	}
	if !d.SystemAt.IsZero() {
		c.collectSystem(e, d)
	}
	if !d.StateAt.IsZero() {
		c.collectState(e, d)
	}
	if !d.BatteriesAt.IsZero() {
		c.collectBatteries(e, d)
	}

	if ssid := wifiSSID(d); ssid != "" {
		e.gauge(descWifiInfo, d.Name, 1, ssid)
	}
}

func wifiSSID(d *snapshot.Device) string {
	if !d.SystemAt.IsZero() && d.System.WifiSSID != nil && *d.System.WifiSSID != "" {
		return *d.System.WifiSSID
	}
	if !d.MeasuredAt.IsZero() {
		return d.Measurement.WifiSSID
	}
	return ""
}

func (c *Collector) collectMeasurement(e emitter, d *snapshot.Device) {
	m := &d.Measurement

	for _, g := range measurementGauges {
		e.optional(g.desc, prometheus.GaugeValue, d.Name, g.value(m))
	}
	for _, counter := range measurementCounters {
		e.optional(counter.desc, prometheus.CounterValue, d.Name, counter.value(m))
	}
	for _, g := range measurementPhaseGauges {
		e.perPhase(g.desc, prometheus.GaugeValue, d.Name, g.values(m))
	}
	for _, counter := range measurementPhaseCounters {
		e.perPhase(counter.desc, prometheus.CounterValue, d.Name, counter.values(m))
	}

	for i, name := range tariffNames {
		e.optional(descEnergyImportTariff, prometheus.CounterValue, d.Name,
			m.EnergyImportTariffKWh[i], name)
		e.optional(descEnergyExportTariff, prometheus.CounterValue, d.Name,
			m.EnergyExportTariffKWh[i], name)
	}

	if m.StateOfChargePct != nil {
		e.gauge(descBatteryStateOfCharge, d.Name, *m.StateOfChargePct/100)
	}

	if !m.Timestamp.IsZero() {
		e.gauge(descMeasurementTimestamp, d.Name, float64(m.Timestamp.Unix()))
	}
	if !m.MonthlyPowerPeakAt.IsZero() {
		e.gauge(descMonthlyPeakTimestamp, d.Name, float64(m.MonthlyPowerPeakAt.Unix()))
	}

	if m.MeterModel != "" || m.UniqueID != "" || m.ProtocolVersion != nil {
		e.gauge(descSmartMeterInfo, d.Name, 1,
			m.MeterModel, formatOptional(m.ProtocolVersion), m.UniqueID)
	}

	if m.WifiStrengthPct != nil {
		e.gauge(descWifiStrength, d.Name, *m.WifiStrengthPct/100)
	}

	for _, external := range m.External {
		e.gauge(descExternalValue, d.Name, external.Value,
			external.UniqueID, external.Type, external.Unit)
		if !external.Timestamp.IsZero() {
			e.gauge(descExternalTimestamp, d.Name, float64(external.Timestamp.Unix()),
				external.UniqueID, external.Type)
		}
	}
}

func (c *Collector) collectSystem(e emitter, d *snapshot.Device) {
	s := d.System

	e.optional(descWifiRSSI, prometheus.GaugeValue, d.Name, s.WifiRSSIdB)
	e.optional(descUptime, prometheus.GaugeValue, d.Name, s.UptimeS)

	if s.CloudEnabled != nil {
		e.gauge(descCloudEnabled, d.Name, boolValue(*s.CloudEnabled))
	}
	if s.APIv1Enabled != nil {
		e.gauge(descAPIv1Enabled, d.Name, boolValue(*s.APIv1Enabled))
	}
	if s.LEDBrightness != nil {
		e.gauge(descLEDBrightness, d.Name, *s.LEDBrightness/100)
	}
}

func (c *Collector) collectState(e emitter, d *snapshot.Device) {
	if d.State.PowerOn != nil {
		e.gauge(descSocketPowerOn, d.Name, boolValue(*d.State.PowerOn))
	}
	if d.State.SwitchLock != nil {
		e.gauge(descSocketSwitchLock, d.Name, boolValue(*d.State.SwitchLock))
	}
	// The socket reports brightness on a 0-255 scale, which is an
	// implementation detail of the LED driver rather than something anyone
	// wants to graph.
	if d.State.Brightness != nil {
		e.gauge(descSocketBrightness, d.Name, float64(*d.State.Brightness)/255)
	}
}

func (c *Collector) collectBatteries(e emitter, d *snapshot.Device) {
	b := d.Batteries

	e.optional(descBatteriesPower, prometheus.GaugeValue, d.Name, b.PowerW)
	e.optional(descBatteriesTarget, prometheus.GaugeValue, d.Name, b.TargetPowerW)
	e.optional(descBatteriesMaxConsumption, prometheus.GaugeValue, d.Name, b.MaxConsumptionW)
	e.optional(descBatteriesMaxProduction, prometheus.GaugeValue, d.Name, b.MaxProductionW)
	e.optional(descBatteriesCount, prometheus.GaugeValue, d.Name, b.BatteryCount)

	if b.ChargeToFull != nil {
		e.gauge(descBatteriesChargeToFull, d.Name, boolValue(*b.ChargeToFull))
	}
	if b.Mode != "" {
		e.gauge(descBatteriesMode, d.Name, 1, b.Mode)
	}
	for _, permission := range b.Permissions {
		e.gauge(descBatteriesPermission, d.Name, 1, permission)
	}
}

type emitter struct {
	ch chan<- prometheus.Metric
}

func (e emitter) optional(
	d *prometheus.Desc,
	kind prometheus.ValueType,
	device string,
	value *float64,
	labels ...string,
) {
	if value == nil {
		return
	}
	e.plain(d, kind, *value, append([]string{device}, labels...)...)
}

func (e emitter) perPhase(
	d *prometheus.Desc,
	kind prometheus.ValueType,
	device string,
	values [3]*float64,
) {
	for i, value := range values {
		e.optional(d, kind, device, value, phaseNames[i])
	}
}

func (e emitter) gauge(d *prometheus.Desc, device string, value float64, labels ...string) {
	e.plain(d, prometheus.GaugeValue, value, append([]string{device}, labels...)...)
}

func (e emitter) plain(d *prometheus.Desc, kind prometheus.ValueType, value float64, labels ...string) {
	e.ch <- prometheus.MustNewConstMetric(d, kind, value, labels...)
}

func formatOptional(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

var _ prometheus.Collector = (*Collector)(nil)
