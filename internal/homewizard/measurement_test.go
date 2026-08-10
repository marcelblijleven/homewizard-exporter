package homewizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func loadFixture[T any](t *testing.T, name string) T {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var dst T
	if err := json.Unmarshal(b, &dst); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return dst
}

func measurementFrom(t *testing.T, name string) Measurement {
	t.Helper()

	var (
		m   Measurement
		err error
	)
	if filepath.Base(name)[:2] == "v2" {
		m, err = loadFixture[v2Measurement](t, name).measurement()
	} else {
		m, err = loadFixture[v1Data](t, name).measurement()
	}
	if err != nil {
		t.Fatalf("map %s: %v", name, err)
	}
	return m
}

func wantFloat(t *testing.T, field string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: not reported, want %v", field, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %v, want %v", field, *got, want)
	}
}

func wantAbsent(t *testing.T, field string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want no value at all", field, *got)
	}
}

func TestMeasurementFixtures(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		check   func(*testing.T, Measurement)
	}{
		{
			name:    "v1 P1 three phase with gas and water",
			fixture: "v1_hwe-p1_measurement.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "power", m.PowerW, -543)
				wantFloat(t, "power l1", m.PowerPhaseW[0], -676)
				wantFloat(t, "power l2", m.PowerPhaseW[1], 133)
				wantFloat(t, "power l3", m.PowerPhaseW[2], 0)
				wantFloat(t, "import", m.EnergyImportKWh, 13779.338)
				wantFloat(t, "import t1", m.EnergyImportTariffKWh[0], 10830.511)
				wantFloat(t, "import t2", m.EnergyImportTariffKWh[1], 2948.827)
				wantFloat(t, "tariff", m.Tariff, 2)
				wantFloat(t, "sag l1", m.VoltageSagPhaseCount[0], 1)
				wantFloat(t, "peak", m.MonthlyPowerPeakW, 1111)
				wantFloat(t, "wifi", m.WifiStrengthPct, 100)

				// This meter reports current per phase but no voltage at all,
				// and no frequency. Those must not materialise.
				wantFloat(t, "current l1", m.CurrentPhaseA[0], -4)
				wantAbsent(t, "voltage", m.VoltageV)
				wantAbsent(t, "voltage l1", m.VoltagePhaseV[0])
				wantAbsent(t, "frequency", m.FrequencyHz)
				wantAbsent(t, "tariff 3", m.EnergyImportTariffKWh[2])
				wantAbsent(t, "power factor", m.PowerFactor)

				if m.MeterModel != "ISKRA  2M550T-101" {
					t.Errorf("meter model = %q", m.MeterModel)
				}
				wantFloat(t, "protocol version", m.ProtocolVersion, 50)

				if len(m.External) != 2 {
					t.Fatalf("external meters = %d, want 2", len(m.External))
				}
				if m.External[0].Type != "gas_meter" || m.External[0].Value != 2569.646 {
					t.Errorf("gas meter = %+v", m.External[0])
				}
				if m.External[1].Type != "water_meter" || m.External[1].Unit != "m3" {
					t.Errorf("water meter = %+v", m.External[1])
				}
			},
		},
		{
			name:    "v1 P1 one phase",
			fixture: "v1_hwe-p1_measurement_1phase.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "power", m.PowerW, -678)
				wantFloat(t, "power l1", m.PowerPhaseW[0], -676)

				// The whole point of a one-phase fixture.
				wantAbsent(t, "power l2", m.PowerPhaseW[1])
				wantAbsent(t, "power l3", m.PowerPhaseW[2])
				// Per-tariff totals without an all-tariff total is normal.
				wantAbsent(t, "import", m.EnergyImportKWh)
				wantFloat(t, "import t1", m.EnergyImportTariffKWh[0], 10830.511)
				wantAbsent(t, "external", nil)
				if len(m.External) != 0 {
					t.Errorf("external meters = %d, want none", len(m.External))
				}
			},
		},
		{
			name:    "v1 Energy Socket",
			fixture: "v1_hwe-skt_measurement.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "power", m.PowerW, 543.312)
				wantFloat(t, "voltage", m.VoltageV, 231.539)
				wantFloat(t, "current", m.CurrentA, 2.346)
				wantFloat(t, "power factor", m.PowerFactor, 0.81688)
				wantFloat(t, "apparent power", m.ApparentPowerVA, 666.768)
				wantFloat(t, "frequency", m.FrequencyHz, 50.005)

				// A socket has one phase and reports it unlabelled. It must not
				// masquerade as a per-phase voltage.
				wantAbsent(t, "voltage l1", m.VoltagePhaseV[0])
			},
		},
		{
			name:    "v1 kWh meter one phase",
			fixture: "v1_hwe-kwh1_measurement.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "power", m.PowerW, -45.252)
				wantFloat(t, "voltage", m.VoltageV, 228.472)
				wantFloat(t, "apparent current", m.ApparentCurrentA, 0.447)
				wantFloat(t, "reactive power", m.ReactivePowerVAR, -58.612)
				wantFloat(t, "export", m.EnergyExportKWh, 579.813)
				wantAbsent(t, "power factor l1", m.PowerFactorPhase[0])
			},
		},
		{
			name:    "v1 kWh meter three phase",
			fixture: "v1_hwe-kwh3_measurement.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "power", m.PowerW, 7100.278)
				wantFloat(t, "voltage l2", m.VoltagePhaseV[1], 228.391)
				wantFloat(t, "apparent power l3", m.ApparentPowerPhaseVA[2], 3563.414)
				wantFloat(t, "reactive power l2", m.ReactivePowerPhaseVA[1], -166.675)
				wantFloat(t, "power factor l1", m.PowerFactorPhase[0], 1)
				wantFloat(t, "current", m.CurrentA, 30.999)

				// The three-phase meter labels every voltage, so the unlabelled
				// one must be absent -- the mirror of the socket case above.
				wantAbsent(t, "voltage", m.VoltageV)
				wantAbsent(t, "power factor", m.PowerFactor)
			},
		},
		{
			name:    "v1 water meter",
			fixture: "v1_hwe-wtr_measurement.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "flow", m.ActiveWaterLPM, 7.2)
				wantFloat(t, "total", m.TotalWaterM3, 123.456)

				// A water meter measures no electricity at all.
				wantAbsent(t, "power", m.PowerW)
				wantAbsent(t, "import", m.EnergyImportKWh)
			},
		},
		{
			name:    "v2 P1 three phase with gas and water",
			fixture: "v2_hwe-p1_measurement.json",
			check: func(t *testing.T, m Measurement) {
				// Same house, same numbers, different field names: this is what
				// makes the metric names independent of the API generation.
				wantFloat(t, "power", m.PowerW, -543)
				wantFloat(t, "power l1", m.PowerPhaseW[0], -676)
				wantFloat(t, "import", m.EnergyImportKWh, 13779.338)
				wantFloat(t, "tariff", m.Tariff, 2)
				wantFloat(t, "current", m.CurrentA, 6)
				wantFloat(t, "average 15m", m.AveragePower15mW, 123)

				if len(m.External) != 2 {
					t.Fatalf("external meters = %d, want 2", len(m.External))
				}
				wantAbsent(t, "voltage", m.VoltageV)
				wantAbsent(t, "wifi", m.WifiStrengthPct)
			},
		},
		{
			name:    "v2 kWh meter three phase",
			fixture: "v2_hwe-kwh3_measurement.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "power", m.PowerW, 123)
				wantFloat(t, "power l3", m.PowerPhaseW[2], -30)
				wantFloat(t, "voltage l1", m.VoltagePhaseV[0], 230)
				wantFloat(t, "reactive current l1", m.ReactiveCurrentPhaseA[0], -0.2)
				wantFloat(t, "power factor l3", m.PowerFactorPhase[2], 0.85)
				wantAbsent(t, "power factor", m.PowerFactor)
			},
		},
		{
			name:    "v2 battery",
			fixture: "v2_hwe-bat_measurement.json",
			check: func(t *testing.T, m Measurement) {
				wantFloat(t, "state of charge", m.StateOfChargePct, 50)
				wantFloat(t, "cycles", m.Cycles, 123)
				wantFloat(t, "power", m.PowerW, 123)
				wantAbsent(t, "power l1", m.PowerPhaseW[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, measurementFrom(t, tt.fixture))
		})
	}
}

func TestTimestamps(t *testing.T) {
	v1 := measurementFrom(t, "v1_hwe-p1_measurement.json")
	want := time.Date(2023, 1, 1, 8, 0, 10, 0, time.Local)
	if !v1.MonthlyPowerPeakAt.Equal(want) {
		t.Errorf("v1 monthly peak at %s, want %s", v1.MonthlyPowerPeakAt, want)
	}

	wantGas := time.Date(2021, 6, 6, 14, 0, 10, 0, time.Local)
	if !v1.External[0].Timestamp.Equal(wantGas) {
		t.Errorf("v1 gas timestamp %s, want %s", v1.External[0].Timestamp, wantGas)
	}

	v2 := measurementFrom(t, "v2_hwe-p1_measurement.json")
	wantV2 := time.Date(2024, 6, 28, 14, 12, 34, 0, time.Local)
	if !v2.Timestamp.Equal(wantV2) {
		t.Errorf("v2 timestamp %s, want %s", v2.Timestamp, wantV2)
	}
	wantPeak := time.Date(2024, 6, 4, 10, 11, 22, 0, time.Local)
	if !v2.MonthlyPowerPeakAt.Equal(wantPeak) {
		t.Errorf("v2 monthly peak at %s, want %s", v2.MonthlyPowerPeakAt, wantPeak)
	}
}

func TestLegacyGasFieldsPromoted(t *testing.T) {
	data := v1Data{
		TotalGasM3:   ptr(2569.646),
		GasTimestamp: ptr(210606140010.0),
		UniqueGasID:  "FFEE",
	}

	m, err := data.measurement()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.External) != 1 {
		t.Fatalf("external meters = %d, want 1", len(m.External))
	}
	if m.External[0].Type != "gas_meter" || m.External[0].Value != 2569.646 {
		t.Errorf("promoted gas meter = %+v", m.External[0])
	}
	if m.External[0].Unit != "m3" {
		t.Errorf("promoted gas unit = %q, want m3", m.External[0].Unit)
	}
}

func TestLegacyGasFieldsNotDuplicated(t *testing.T) {
	m := measurementFrom(t, "v1_hwe-p1_measurement.json")

	gas := 0
	for _, external := range m.External {
		if external.Type == "gas_meter" {
			gas++
		}
	}
	if gas != 1 {
		t.Errorf("gas meters = %d, want exactly 1", gas)
	}
}

func TestGasIdentifierSpelling(t *testing.T) {
	for _, body := range []string{
		`{"total_gas_m3": 1, "unique_gas_id": "ABC"}`,
		`{"total_gas_m3": 1, "gas_unique_id": "ABC"}`,
	} {
		var data v1Data
		if err := json.Unmarshal([]byte(body), &data); err != nil {
			t.Fatal(err)
		}
		m, err := data.measurement()
		if err != nil {
			t.Fatal(err)
		}
		if len(m.External) != 1 || m.External[0].UniqueID != "ABC" {
			t.Errorf("%s did not yield the gas identifier: %+v", body, m.External)
		}
	}
}

func TestEmptyMeasurement(t *testing.T) {
	for _, tc := range []struct {
		name string
		get  func() (Measurement, error)
	}{
		{"v1", func() (Measurement, error) { return v1Data{}.measurement() }},
		{"v2", func() (Measurement, error) { return v2Measurement{}.measurement() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := tc.get()
			if err != nil {
				t.Fatal(err)
			}
			wantAbsent(t, "power", m.PowerW)
			wantAbsent(t, "import", m.EnergyImportKWh)
			wantAbsent(t, "frequency", m.FrequencyHz)
			if !m.Timestamp.IsZero() {
				t.Errorf("timestamp = %s, want zero", m.Timestamp)
			}
			if len(m.External) != 0 {
				t.Errorf("external = %+v, want none", m.External)
			}
		})
	}
}

func TestStateAndBatteriesFixtures(t *testing.T) {
	state := loadFixture[State](t, "v1_hwe-skt_state.json")
	if state.PowerOn == nil || !*state.PowerOn {
		t.Error("socket should be on")
	}
	if state.SwitchLock == nil || *state.SwitchLock {
		t.Error("switch lock should be off")
	}
	if state.Brightness == nil || *state.Brightness != 255 {
		t.Error("brightness should be 255")
	}

	batteries := loadFixture[Batteries](t, "v2_hwe-p1_batteries.json")
	if batteries.Mode != "zero" {
		t.Errorf("mode = %q", batteries.Mode)
	}
	wantFloat(t, "batteries power", batteries.PowerW, -404)
	wantFloat(t, "battery count", batteries.BatteryCount, 2)
	if len(batteries.Permissions) != 2 {
		t.Errorf("permissions = %v", batteries.Permissions)
	}

	system := loadFixture[System](t, "v2_hwe-p1_system.json")
	wantFloat(t, "rssi", system.WifiRSSIdB, -77)
	wantFloat(t, "uptime", system.UptimeS, 356)
	if system.CloudEnabled == nil || !*system.CloudEnabled {
		t.Error("cloud should be enabled")
	}

	// The v1 system endpoint reports only cloud_enabled. Everything else must
	// stay absent rather than reading as a device with no uptime.
	v1System := loadFixture[System](t, "v1_hwe-p1_system.json")
	wantAbsent(t, "v1 uptime", v1System.UptimeS)
	wantAbsent(t, "v1 rssi", v1System.WifiRSSIdB)
	if v1System.WifiSSID != nil {
		t.Errorf("v1 ssid = %v, want absent", *v1System.WifiSSID)
	}
}

func TestInfoFixtures(t *testing.T) {
	v1 := loadFixture[Info](t, "v1_hwe-p1_info.json")
	if v1.ProductType != "HWE-P1" || v1.APIVersion != "v1" {
		t.Errorf("v1 info = %+v", v1)
	}

	v2 := loadFixture[Info](t, "v2_hwe-p1_info.json")
	if v2.ProductType != "HWE-P1" || v2.APIVersion != "2.3.0" {
		t.Errorf("v2 info = %+v", v2)
	}
}
