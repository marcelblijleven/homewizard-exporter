package homewizard

import "fmt"

// v1Data is the /api/v1/data response. One struct covers every product
type v1Data struct {
	UniqueID     string   `json:"unique_id"`
	SMRVersion   *float64 `json:"smr_version"`
	MeterModel   string   `json:"meter_model"`
	WifiSSID     string   `json:"wifi_ssid"`
	WifiStrength *float64 `json:"wifi_strength"`

	ActiveTariff *float64 `json:"active_tariff"`

	TotalPowerImportKWh   *float64 `json:"total_power_import_kwh"`
	TotalPowerImportT1KWh *float64 `json:"total_power_import_t1_kwh"`
	TotalPowerImportT2KWh *float64 `json:"total_power_import_t2_kwh"`
	TotalPowerImportT3KWh *float64 `json:"total_power_import_t3_kwh"`
	TotalPowerImportT4KWh *float64 `json:"total_power_import_t4_kwh"`
	TotalPowerExportKWh   *float64 `json:"total_power_export_kwh"`
	TotalPowerExportT1KWh *float64 `json:"total_power_export_t1_kwh"`
	TotalPowerExportT2KWh *float64 `json:"total_power_export_t2_kwh"`
	TotalPowerExportT3KWh *float64 `json:"total_power_export_t3_kwh"`
	TotalPowerExportT4KWh *float64 `json:"total_power_export_t4_kwh"`

	ActivePowerW   *float64 `json:"active_power_w"`
	ActivePowerL1W *float64 `json:"active_power_l1_w"`
	ActivePowerL2W *float64 `json:"active_power_l2_w"`
	ActivePowerL3W *float64 `json:"active_power_l3_w"`

	ActiveVoltageV   *float64 `json:"active_voltage_v"`
	ActiveVoltageL1V *float64 `json:"active_voltage_l1_v"`
	ActiveVoltageL2V *float64 `json:"active_voltage_l2_v"`
	ActiveVoltageL3V *float64 `json:"active_voltage_l3_v"`

	ActiveCurrentA   *float64 `json:"active_current_a"`
	ActiveCurrentL1A *float64 `json:"active_current_l1_a"`
	ActiveCurrentL2A *float64 `json:"active_current_l2_a"`
	ActiveCurrentL3A *float64 `json:"active_current_l3_a"`

	ActiveApparentCurrentA   *float64 `json:"active_apparent_current_a"`
	ActiveApparentCurrentL1A *float64 `json:"active_apparent_current_l1_a"`
	ActiveApparentCurrentL2A *float64 `json:"active_apparent_current_l2_a"`
	ActiveApparentCurrentL3A *float64 `json:"active_apparent_current_l3_a"`

	ActiveReactiveCurrentA   *float64 `json:"active_reactive_current_a"`
	ActiveReactiveCurrentL1A *float64 `json:"active_reactive_current_l1_a"`
	ActiveReactiveCurrentL2A *float64 `json:"active_reactive_current_l2_a"`
	ActiveReactiveCurrentL3A *float64 `json:"active_reactive_current_l3_a"`

	ActiveApparentPowerVA   *float64 `json:"active_apparent_power_va"`
	ActiveApparentPowerL1VA *float64 `json:"active_apparent_power_l1_va"`
	ActiveApparentPowerL2VA *float64 `json:"active_apparent_power_l2_va"`
	ActiveApparentPowerL3VA *float64 `json:"active_apparent_power_l3_va"`

	ActiveReactivePowerVAR   *float64 `json:"active_reactive_power_var"`
	ActiveReactivePowerL1VAR *float64 `json:"active_reactive_power_l1_var"`
	ActiveReactivePowerL2VAR *float64 `json:"active_reactive_power_l2_var"`
	ActiveReactivePowerL3VAR *float64 `json:"active_reactive_power_l3_var"`

	ActivePowerFactor   *float64 `json:"active_power_factor"`
	ActivePowerFactorL1 *float64 `json:"active_power_factor_l1"`
	ActivePowerFactorL2 *float64 `json:"active_power_factor_l2"`
	ActivePowerFactorL3 *float64 `json:"active_power_factor_l3"`

	ActiveFrequencyHz *float64 `json:"active_frequency_hz"`

	VoltageSagL1Count   *float64 `json:"voltage_sag_l1_count"`
	VoltageSagL2Count   *float64 `json:"voltage_sag_l2_count"`
	VoltageSagL3Count   *float64 `json:"voltage_sag_l3_count"`
	VoltageSwellL1Count *float64 `json:"voltage_swell_l1_count"`
	VoltageSwellL2Count *float64 `json:"voltage_swell_l2_count"`
	VoltageSwellL3Count *float64 `json:"voltage_swell_l3_count"`
	AnyPowerFailCount   *float64 `json:"any_power_fail_count"`
	LongPowerFailCount  *float64 `json:"long_power_fail_count"`

	// "montly" is HomeWizard's own spelling mistake, documented as such and
	// kept for compatibility. It is fixed in v2.
	ActivePowerAverageW      *float64 `json:"active_power_average_w"`
	MontlyPowerPeakW         *float64 `json:"montly_power_peak_w"`
	MontlyPowerPeakTimestamp *float64 `json:"montly_power_peak_timestamp"`

	// The single-gas-meter fields predate `external` and are documented as
	// going away. The reference table calls the identifier unique_gas_id while
	// the worked example calls it gas_unique_id, so both are accepted.
	TotalGasM3   *float64 `json:"total_gas_m3"`
	GasTimestamp *float64 `json:"gas_timestamp"`
	UniqueGasID  string   `json:"unique_gas_id"`
	GasUniqueID  string   `json:"gas_unique_id"`

	External []v1External `json:"external"`

	ActiveLiterLPM     *float64 `json:"active_liter_lpm"`
	TotalLiterM3       *float64 `json:"total_liter_m3"`
	TotalLiterOffsetM3 *float64 `json:"total_liter_offset_m3"`
}

type v1External struct {
	UniqueID  string   `json:"unique_id"`
	Type      string   `json:"type"`
	Timestamp *float64 `json:"timestamp"`
	Value     *float64 `json:"value"`
	Unit      string   `json:"unit"`
}

// measurement projects the v1 response onto the shared model.
func (d v1Data) measurement() (Measurement, error) {
	m := Measurement{
		UniqueID:        d.UniqueID,
		MeterModel:      d.MeterModel,
		ProtocolVersion: d.SMRVersion,
		WifiSSID:        d.WifiSSID,
		WifiStrengthPct: d.WifiStrength,

		Tariff:          d.ActiveTariff,
		EnergyImportKWh: d.TotalPowerImportKWh,
		EnergyExportKWh: d.TotalPowerExportKWh,

		PowerW:      d.ActivePowerW,
		PowerPhaseW: [phaseCount]*float64{d.ActivePowerL1W, d.ActivePowerL2W, d.ActivePowerL3W},

		VoltageV:      d.ActiveVoltageV,
		VoltagePhaseV: [phaseCount]*float64{d.ActiveVoltageL1V, d.ActiveVoltageL2V, d.ActiveVoltageL3V},

		CurrentA:      d.ActiveCurrentA,
		CurrentPhaseA: [phaseCount]*float64{d.ActiveCurrentL1A, d.ActiveCurrentL2A, d.ActiveCurrentL3A},

		ApparentPowerVA: d.ActiveApparentPowerVA,
		ApparentPowerPhaseVA: [phaseCount]*float64{
			d.ActiveApparentPowerL1VA, d.ActiveApparentPowerL2VA, d.ActiveApparentPowerL3VA,
		},
		ReactivePowerVAR: d.ActiveReactivePowerVAR,
		ReactivePowerPhaseVA: [phaseCount]*float64{
			d.ActiveReactivePowerL1VAR, d.ActiveReactivePowerL2VAR, d.ActiveReactivePowerL3VAR,
		},

		ApparentCurrentA: d.ActiveApparentCurrentA,
		ApparentCurrentPhaseA: [phaseCount]*float64{
			d.ActiveApparentCurrentL1A, d.ActiveApparentCurrentL2A, d.ActiveApparentCurrentL3A,
		},
		ReactiveCurrentA: d.ActiveReactiveCurrentA,
		ReactiveCurrentPhaseA: [phaseCount]*float64{
			d.ActiveReactiveCurrentL1A, d.ActiveReactiveCurrentL2A, d.ActiveReactiveCurrentL3A,
		},

		PowerFactor: d.ActivePowerFactor,
		PowerFactorPhase: [phaseCount]*float64{
			d.ActivePowerFactorL1, d.ActivePowerFactorL2, d.ActivePowerFactorL3,
		},

		FrequencyHz: d.ActiveFrequencyHz,

		VoltageSagPhaseCount: [phaseCount]*float64{
			d.VoltageSagL1Count, d.VoltageSagL2Count, d.VoltageSagL3Count,
		},
		VoltageSwellPhaseCount: [phaseCount]*float64{
			d.VoltageSwellL1Count, d.VoltageSwellL2Count, d.VoltageSwellL3Count,
		},
		AnyPowerFailCount:  d.AnyPowerFailCount,
		LongPowerFailCount: d.LongPowerFailCount,

		AveragePower15mW:  d.ActivePowerAverageW,
		MonthlyPowerPeakW: d.MontlyPowerPeakW,

		ActiveWaterLPM: d.ActiveLiterLPM,
		TotalWaterM3:   d.TotalLiterM3,

		EnergyImportTariffKWh: [maxTariffs]*float64{
			d.TotalPowerImportT1KWh, d.TotalPowerImportT2KWh,
			d.TotalPowerImportT3KWh, d.TotalPowerImportT4KWh,
		},
		EnergyExportTariffKWh: [maxTariffs]*float64{
			d.TotalPowerExportT1KWh, d.TotalPowerExportT2KWh,
			d.TotalPowerExportT3KWh, d.TotalPowerExportT4KWh,
		},
	}

	if d.MontlyPowerPeakTimestamp != nil {
		at, err := parseCompactTimestamp(*d.MontlyPowerPeakTimestamp)
		if err != nil {
			return Measurement{}, fmt.Errorf("montly_power_peak_timestamp: %w", err)
		}
		m.MonthlyPowerPeakAt = at
	}

	for i, e := range d.External {
		if e.Value == nil {
			continue
		}
		external := External{UniqueID: e.UniqueID, Type: e.Type, Unit: e.Unit, Value: *e.Value}
		if e.Timestamp != nil {
			at, err := parseCompactTimestamp(*e.Timestamp)
			if err != nil {
				return Measurement{}, fmt.Errorf("external[%d].timestamp: %w", i, err)
			}
			external.Timestamp = at
		}
		m.External = append(m.External, external)
	}

	// Older firmware reports its gas meter only through the flat fields. They
	// describe the same meter the `external` list would, so promoting them
	// keeps one shape downstream -- but only when the list is absent, or the
	// gas meter would appear twice.
	if len(m.External) == 0 && d.TotalGasM3 != nil {
		gas := External{
			UniqueID: firstNonEmpty(d.UniqueGasID, d.GasUniqueID),
			Type:     "gas_meter",
			Unit:     "m3",
			Value:    *d.TotalGasM3,
		}
		if d.GasTimestamp != nil {
			at, err := parseCompactTimestamp(*d.GasTimestamp)
			if err != nil {
				return Measurement{}, fmt.Errorf("gas_timestamp: %w", err)
			}
			gas.Timestamp = at
		}
		m.External = append(m.External, gas)
	}

	return m, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
