package homewizard

import "fmt"

// v2Measurement is the /api/measurement response. As with v1Data a single struct for all data
type v2Measurement struct {
	UniqueID        string   `json:"unique_id"`
	ProtocolVersion *float64 `json:"protocol_version"`
	MeterModel      string   `json:"meter_model"`
	Timestamp       string   `json:"timestamp"`
	Tariff          *float64 `json:"tariff"`

	EnergyImportKWh   *float64 `json:"energy_import_kwh"`
	EnergyImportT1KWh *float64 `json:"energy_import_t1_kwh"`
	EnergyImportT2KWh *float64 `json:"energy_import_t2_kwh"`
	EnergyImportT3KWh *float64 `json:"energy_import_t3_kwh"`
	EnergyImportT4KWh *float64 `json:"energy_import_t4_kwh"`
	EnergyExportKWh   *float64 `json:"energy_export_kwh"`
	EnergyExportT1KWh *float64 `json:"energy_export_t1_kwh"`
	EnergyExportT2KWh *float64 `json:"energy_export_t2_kwh"`
	EnergyExportT3KWh *float64 `json:"energy_export_t3_kwh"`
	EnergyExportT4KWh *float64 `json:"energy_export_t4_kwh"`

	PowerW   *float64 `json:"power_w"`
	PowerL1W *float64 `json:"power_l1_w"`
	PowerL2W *float64 `json:"power_l2_w"`
	PowerL3W *float64 `json:"power_l3_w"`

	VoltageV   *float64 `json:"voltage_v"`
	VoltageL1V *float64 `json:"voltage_l1_v"`
	VoltageL2V *float64 `json:"voltage_l2_v"`
	VoltageL3V *float64 `json:"voltage_l3_v"`

	CurrentA   *float64 `json:"current_a"`
	CurrentL1A *float64 `json:"current_l1_a"`
	CurrentL2A *float64 `json:"current_l2_a"`
	CurrentL3A *float64 `json:"current_l3_a"`

	ApparentCurrentA   *float64 `json:"apparent_current_a"`
	ApparentCurrentL1A *float64 `json:"apparent_current_l1_a"`
	ApparentCurrentL2A *float64 `json:"apparent_current_l2_a"`
	ApparentCurrentL3A *float64 `json:"apparent_current_l3_a"`

	ReactiveCurrentA   *float64 `json:"reactive_current_a"`
	ReactiveCurrentL1A *float64 `json:"reactive_current_l1_a"`
	ReactiveCurrentL2A *float64 `json:"reactive_current_l2_a"`
	ReactiveCurrentL3A *float64 `json:"reactive_current_l3_a"`

	ApparentPowerVA   *float64 `json:"apparent_power_va"`
	ApparentPowerL1VA *float64 `json:"apparent_power_l1_va"`
	ApparentPowerL2VA *float64 `json:"apparent_power_l2_va"`
	ApparentPowerL3VA *float64 `json:"apparent_power_l3_va"`

	ReactivePowerVAR   *float64 `json:"reactive_power_var"`
	ReactivePowerL1VAR *float64 `json:"reactive_power_l1_var"`
	ReactivePowerL2VAR *float64 `json:"reactive_power_l2_var"`
	ReactivePowerL3VAR *float64 `json:"reactive_power_l3_var"`

	PowerFactor   *float64 `json:"power_factor"`
	PowerFactorL1 *float64 `json:"power_factor_l1"`
	PowerFactorL2 *float64 `json:"power_factor_l2"`
	PowerFactorL3 *float64 `json:"power_factor_l3"`

	FrequencyHz *float64 `json:"frequency_hz"`

	VoltageSagL1Count   *float64 `json:"voltage_sag_l1_count"`
	VoltageSagL2Count   *float64 `json:"voltage_sag_l2_count"`
	VoltageSagL3Count   *float64 `json:"voltage_sag_l3_count"`
	VoltageSwellL1Count *float64 `json:"voltage_swell_l1_count"`
	VoltageSwellL2Count *float64 `json:"voltage_swell_l2_count"`
	VoltageSwellL3Count *float64 `json:"voltage_swell_l3_count"`
	AnyPowerFailCount   *float64 `json:"any_power_fail_count"`
	LongPowerFailCount  *float64 `json:"long_power_fail_count"`

	AveragePower15mW          *float64 `json:"average_power_15m_w"`
	MonthlyPowerPeakW         *float64 `json:"monthly_power_peak_w"`
	MonthlyPowerPeakTimestamp string   `json:"monthly_power_peak_timestamp"`

	StateOfChargePct *float64 `json:"state_of_charge_pct"`
	Cycles           *float64 `json:"cycles"`

	External []v2External `json:"external"`
}

type v2External struct {
	UniqueID  string   `json:"unique_id"`
	Type      string   `json:"type"`
	Timestamp string   `json:"timestamp"`
	Value     *float64 `json:"value"`
	Unit      string   `json:"unit"`
}

// measurement projects the v2 response onto the shared model.
func (d v2Measurement) measurement() (Measurement, error) {
	m := Measurement{
		UniqueID:        d.UniqueID,
		MeterModel:      d.MeterModel,
		ProtocolVersion: d.ProtocolVersion,

		Tariff:          d.Tariff,
		EnergyImportKWh: d.EnergyImportKWh,
		EnergyExportKWh: d.EnergyExportKWh,
		EnergyImportTariffKWh: [maxTariffs]*float64{
			d.EnergyImportT1KWh, d.EnergyImportT2KWh, d.EnergyImportT3KWh, d.EnergyImportT4KWh,
		},
		EnergyExportTariffKWh: [maxTariffs]*float64{
			d.EnergyExportT1KWh, d.EnergyExportT2KWh, d.EnergyExportT3KWh, d.EnergyExportT4KWh,
		},

		PowerW:      d.PowerW,
		PowerPhaseW: [phaseCount]*float64{d.PowerL1W, d.PowerL2W, d.PowerL3W},

		VoltageV:      d.VoltageV,
		VoltagePhaseV: [phaseCount]*float64{d.VoltageL1V, d.VoltageL2V, d.VoltageL3V},

		CurrentA:      d.CurrentA,
		CurrentPhaseA: [phaseCount]*float64{d.CurrentL1A, d.CurrentL2A, d.CurrentL3A},

		ApparentPowerVA: d.ApparentPowerVA,
		ApparentPowerPhaseVA: [phaseCount]*float64{
			d.ApparentPowerL1VA, d.ApparentPowerL2VA, d.ApparentPowerL3VA,
		},
		ReactivePowerVAR: d.ReactivePowerVAR,
		ReactivePowerPhaseVA: [phaseCount]*float64{
			d.ReactivePowerL1VAR, d.ReactivePowerL2VAR, d.ReactivePowerL3VAR,
		},

		ApparentCurrentA: d.ApparentCurrentA,
		ApparentCurrentPhaseA: [phaseCount]*float64{
			d.ApparentCurrentL1A, d.ApparentCurrentL2A, d.ApparentCurrentL3A,
		},
		ReactiveCurrentA: d.ReactiveCurrentA,
		ReactiveCurrentPhaseA: [phaseCount]*float64{
			d.ReactiveCurrentL1A, d.ReactiveCurrentL2A, d.ReactiveCurrentL3A,
		},

		PowerFactor:      d.PowerFactor,
		PowerFactorPhase: [phaseCount]*float64{d.PowerFactorL1, d.PowerFactorL2, d.PowerFactorL3},

		FrequencyHz: d.FrequencyHz,

		VoltageSagPhaseCount: [phaseCount]*float64{
			d.VoltageSagL1Count, d.VoltageSagL2Count, d.VoltageSagL3Count,
		},
		VoltageSwellPhaseCount: [phaseCount]*float64{
			d.VoltageSwellL1Count, d.VoltageSwellL2Count, d.VoltageSwellL3Count,
		},
		AnyPowerFailCount:  d.AnyPowerFailCount,
		LongPowerFailCount: d.LongPowerFailCount,

		AveragePower15mW:  d.AveragePower15mW,
		MonthlyPowerPeakW: d.MonthlyPowerPeakW,

		StateOfChargePct: d.StateOfChargePct,
		Cycles:           d.Cycles,
	}

	at, err := parseISOTimestamp(d.Timestamp)
	if err != nil {
		return Measurement{}, fmt.Errorf("timestamp: %w", err)
	}
	m.Timestamp = at

	peak, err := parseISOTimestamp(d.MonthlyPowerPeakTimestamp)
	if err != nil {
		return Measurement{}, fmt.Errorf("monthly_power_peak_timestamp: %w", err)
	}
	m.MonthlyPowerPeakAt = peak

	for i, e := range d.External {
		if e.Value == nil {
			continue
		}
		external := External{UniqueID: e.UniqueID, Type: e.Type, Unit: e.Unit, Value: *e.Value}
		if external.Timestamp, err = parseISOTimestamp(e.Timestamp); err != nil {
			return Measurement{}, fmt.Errorf("external[%d].timestamp: %w", i, err)
		}
		m.External = append(m.External, external)
	}

	return m, nil
}
