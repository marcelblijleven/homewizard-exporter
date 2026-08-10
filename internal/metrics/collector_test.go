package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

func ptr[T any](v T) *T { return &v }

var testNow = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

func newTestCollector(store *snapshot.Store) *Collector {
	return NewCollector(CollectorOptions{
		Store:      store,
		StaleAfter: time.Minute,
		Now:        func() time.Time { return testNow },
	})
}

func storeWith(name string, mutate func(*snapshot.Device)) *snapshot.Store {
	store := &snapshot.Store{}
	store.Update(name, func(d *snapshot.Device) {
		d.MeasuredAt = testNow.Add(-time.Second)
		mutate(d)
	})
	return store
}

func TestAbsentFieldsProduceNoSeries(t *testing.T) {
	store := storeWith("socket", func(d *snapshot.Device) {
		d.Info = homewizard.Info{ProductType: homewizard.ProductSocket}
		d.APIVersion = config.APIv1
		d.Measurement = homewizard.Measurement{
			PowerW:   ptr(543.312),
			VoltageV: ptr(231.539),
		}
	})

	got := gather(t, newTestCollector(store))

	mustContain(t, got, `homewizard_active_power_watts{device="socket"} 543.312`)
	mustContain(t, got, `homewizard_active_voltage_volts{device="socket"} 231.539`)

	mustNotContain(t, got, "homewizard_active_voltage_phase_volts")
	mustNotContain(t, got, "homewizard_active_power_phase_watts")
	mustNotContain(t, got, "homewizard_frequency_hertz")
	mustNotContain(t, got, "homewizard_energy_import_kwh_total")
}

func TestThreePhaseMeter(t *testing.T) {
	store := storeWith("p1", func(d *snapshot.Device) {
		d.Info = homewizard.Info{ProductType: homewizard.ProductP1, Serial: "aabb"}
		d.APIVersion = config.APIv2
		d.Measurement = homewizard.Measurement{
			PowerW:      ptr(-543.0),
			PowerPhaseW: [3]*float64{ptr(-676.0), ptr(133.0), ptr(0.0)},
			// Only two of the four tariffs exist on a Dutch meter.
			EnergyImportTariffKWh: [4]*float64{ptr(10830.511), ptr(2948.827), nil, nil},
			EnergyImportKWh:       ptr(13779.338),
		}
	})

	got := gather(t, newTestCollector(store))

	mustContain(t, got, `homewizard_active_power_watts{device="p1"} -543`)
	mustContain(t, got, `homewizard_active_power_phase_watts{device="p1",phase="l1"} -676`)
	mustContain(t, got, `homewizard_active_power_phase_watts{device="p1",phase="l3"} 0`)
	mustContain(t, got, `homewizard_energy_import_tariff_kwh_total{device="p1",tariff="1"} 10830.511`)
	mustContain(t, got, `homewizard_energy_import_tariff_kwh_total{device="p1",tariff="2"} 2948.827`)
	mustNotContain(t, got, `tariff="3"`)

	mustContain(t, got, `homewizard_energy_import_kwh_total{device="p1"} 13779.338`)
}

func TestPercentagesBecomeRatios(t *testing.T) {
	store := storeWith("battery", func(d *snapshot.Device) {
		d.Measurement = homewizard.Measurement{
			StateOfChargePct: ptr(50.0),
			WifiStrengthPct:  ptr(80.0),
		}
		d.StateAt = testNow
		d.State = homewizard.State{Brightness: ptr(255)}
		d.SystemAt = testNow
		d.System = homewizard.System{LEDBrightness: ptr(100.0)}
	})

	got := gather(t, newTestCollector(store))

	mustContain(t, got, `homewizard_battery_state_of_charge_ratio{device="battery"} 0.5`)
	mustContain(t, got, `homewizard_wifi_strength_ratio{device="battery"} 0.8`)
	mustContain(t, got, `homewizard_socket_brightness_ratio{device="battery"} 1`)
	mustContain(t, got, `homewizard_status_led_brightness_ratio{device="battery"} 1`)
}

func TestExternalMeters(t *testing.T) {
	store := storeWith("p1", func(d *snapshot.Device) {
		d.Measurement = homewizard.Measurement{
			External: []homewizard.External{
				{
					UniqueID: "GAS", Type: "gas_meter", Unit: "m3", Value: 2569.646,
					Timestamp: time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC),
				},
				{UniqueID: "WATER", Type: "water_meter", Unit: "m3", Value: 123.456},
			},
		}
	})

	got := gather(t, newTestCollector(store))

	mustContain(t, got,
		`homewizard_external_meter_value{device="p1",type="gas_meter",unique_id="GAS",unit="m3"} 2569.646`)
	mustContain(t, got,
		`homewizard_external_meter_value{device="p1",type="water_meter",unique_id="WATER",unit="m3"} 123.456`)
	mustContain(t, got, `homewizard_external_meter_timestamp_seconds{device="p1",type="gas_meter",unique_id="GAS"}`)
	mustNotContain(t, got, `homewizard_external_meter_timestamp_seconds{device="p1",type="water_meter"`)
}

func TestStaleness(t *testing.T) {
	store := &snapshot.Store{}
	store.Update("p1", func(d *snapshot.Device) {
		d.MeasuredAt = testNow.Add(-5 * time.Minute)
		d.Measurement = homewizard.Measurement{PowerW: ptr(100.0)}
	})

	got := gather(t, newTestCollector(store))

	mustContain(t, got, `homewizard_up{device="p1"} 0`)
	mustContain(t, got, `homewizard_active_power_watts{device="p1"} 100`)
	mustContain(t, got, `homewizard_last_update_timestamp_seconds{device="p1"}`)
}

func TestNeverPolled(t *testing.T) {
	store := &snapshot.Store{}
	store.Update("p1", func(d *snapshot.Device) { d.Host = "192.168.1.10" })

	got := gather(t, newTestCollector(store))

	mustContain(t, got, `homewizard_up{device="p1"} 0`)
	mustContain(t, got, `homewizard_devices_total 1`)
	mustNotContain(t, got, "homewizard_active_power_watts")
	mustNotContain(t, got, "homewizard_last_update_timestamp_seconds")
}

func TestNothingPolledYet(t *testing.T) {
	got := gather(t, newTestCollector(&snapshot.Store{}))

	if strings.Contains(got, "homewizard_up") {
		t.Errorf("expected no series at all before the first poll, got:\n%s", got)
	}
}

func TestBatteriesGroup(t *testing.T) {
	store := storeWith("p1", func(d *snapshot.Device) {
		d.BatteriesAt = testNow
		d.Batteries = homewizard.Batteries{
			Mode:         "zero",
			Permissions:  []string{"charge_allowed", "discharge_allowed"},
			ChargeToFull: ptr(false),
			BatteryCount: ptr(2.0),
			PowerW:       ptr(-404.0),
		}
	})

	got := gather(t, newTestCollector(store))

	mustContain(t, got, `homewizard_batteries_power_watts{device="p1"} -404`)
	mustContain(t, got, `homewizard_batteries_count{device="p1"} 2`)
	mustContain(t, got, `homewizard_batteries_mode_info{device="p1",mode="zero"} 1`)
	mustContain(t, got, `homewizard_batteries_permission{device="p1",permission="charge_allowed"} 1`)
	mustContain(t, got, `homewizard_batteries_charge_to_full{device="p1"} 0`)
}

func TestMultipleDevices(t *testing.T) {
	store := &snapshot.Store{}
	for _, name := range []string{"socket_kitchen", "socket_shed"} {
		store.Update(name, func(d *snapshot.Device) {
			d.MeasuredAt = testNow
			d.Info = homewizard.Info{ProductType: homewizard.ProductSocket}
			d.Measurement = homewizard.Measurement{PowerW: ptr(10.0)}
		})
	}

	got := gather(t, newTestCollector(store))

	mustContain(t, got, `homewizard_active_power_watts{device="socket_kitchen"} 10`)
	mustContain(t, got, `homewizard_active_power_watts{device="socket_shed"} 10`)
	mustContain(t, got, `homewizard_devices_total 2`)
}

func TestNoDuplicateSeries(t *testing.T) {
	store := storeWith("p1", func(d *snapshot.Device) {
		d.Info = homewizard.Info{ProductType: homewizard.ProductP1}
		d.MeasuredAt = testNow
		d.SystemAt = testNow
		d.StateAt = testNow
		d.BatteriesAt = testNow
		d.Measurement = homewizard.Measurement{
			PowerW:                 ptr(1.0),
			PowerPhaseW:            [3]*float64{ptr(1.0), ptr(2.0), ptr(3.0)},
			VoltageV:               ptr(230.0),
			VoltagePhaseV:          [3]*float64{ptr(230.0), ptr(231.0), ptr(232.0)},
			EnergyImportKWh:        ptr(1.0),
			EnergyImportTariffKWh:  [4]*float64{ptr(1.0), ptr(2.0), ptr(3.0), ptr(4.0)},
			VoltageSagPhaseCount:   [3]*float64{ptr(1.0), ptr(1.0), ptr(1.0)},
			VoltageSwellPhaseCount: [3]*float64{ptr(1.0), ptr(1.0), ptr(1.0)},
			MeterModel:             "ISKRA",
			WifiSSID:               "My Wi-Fi",
			External: []homewizard.External{
				{UniqueID: "A", Type: "gas_meter", Unit: "m3", Value: 1},
				{UniqueID: "B", Type: "water_meter", Unit: "m3", Value: 2},
			},
		}
		d.System = homewizard.System{WifiSSID: ptr("My Wi-Fi")}
	})

	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(newTestCollector(store))

	if _, err := registry.Gather(); err != nil {
		t.Fatalf("gathering produced an inconsistent exposition: %v", err)
	}
}

func TestDescribeCoversCollect(t *testing.T) {
	described := make(chan *prometheus.Desc, 256)
	newTestCollector(&snapshot.Store{}).Describe(described)
	close(described)

	seen := make(map[string]bool)
	for d := range described {
		seen[d.String()] = true
	}

	for _, d := range allDescs() {
		if !seen[d.String()] {
			t.Errorf("descriptor not described: %s", d)
		}
	}
}

func gather(t *testing.T, collector prometheus.Collector) string {
	t.Helper()

	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(collector)

	var sb strings.Builder
	if err := testutil.CollectAndCompare(collector, strings.NewReader("")); err != nil {
		sb.WriteString(err.Error())
	}
	return sb.String()
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("missing series:\n  %s\n\ngot:\n%s", want, got)
	}
}

func mustNotContain(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Errorf("unexpected series %q in:\n%s", unwanted, got)
	}
}
