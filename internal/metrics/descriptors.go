package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
)

var phaseNames = [3]string{"l1", "l2", "l3"}

var tariffNames = [4]string{"1", "2", "3", "4"}

func desc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(
		Namespace+"_"+name, help, append([]string{"device"}, labels...), nil,
	)
}

var (
	descUp = desc("up",
		"1 when the last poll of this device succeeded and its readings are still fresh.")

	descDeviceInfo = desc("device_info",
		"Device identity as reported by /api. Always 1.",
		"product_type", "product_name", "serial", "firmware_version", "api", "api_version")

	descSmartMeterInfo = desc("smart_meter_info",
		"Identity of the smart meter behind a P1 Meter. Always 1.",
		"meter_model", "protocol_version", "unique_id")

	descWifiInfo = desc("wifi_info",
		"The Wi-Fi network the device is joined to. Always 1.", "ssid")

	descDevicesTotal = prometheus.NewDesc(Namespace+"_devices_total",
		"Devices this exporter is configured to poll.", nil, nil)

	descLastUpdate = desc("last_update_timestamp_seconds",
		"When this device was last read successfully, as a Unix timestamp.")

	descMeasurementTimestamp = desc("measurement_timestamp_seconds",
		"Timestamp the smart meter put on this measurement, as a Unix timestamp.")
)

var measurementGauges = []struct {
	desc  *prometheus.Desc
	value func(*homewizard.Measurement) *float64
}{
	{
		desc("active_power_watts",
			"Active power right now. Negative when exporting to the grid."),
		func(m *homewizard.Measurement) *float64 { return m.PowerW },
	},

	{
		desc("active_voltage_volts", "Active voltage."),
		func(m *homewizard.Measurement) *float64 { return m.VoltageV },
	},

	{
		desc("active_current_amperes",
			"Active current. On a multi-phase device this is the sum of the absolute "+
				"per-phase values, not their net."),
		func(m *homewizard.Measurement) *float64 { return m.CurrentA },
	},

	{
		desc("active_apparent_power_voltamperes", "Apparent power."),
		func(m *homewizard.Measurement) *float64 { return m.ApparentPowerVA },
	},

	{
		desc("active_reactive_power_voltamperes_reactive", "Reactive power."),
		func(m *homewizard.Measurement) *float64 { return m.ReactivePowerVAR },
	},

	{
		desc("active_apparent_current_amperes", "Apparent current."),
		func(m *homewizard.Measurement) *float64 { return m.ApparentCurrentA },
	},

	{
		desc("active_reactive_current_amperes", "Reactive current."),
		func(m *homewizard.Measurement) *float64 { return m.ReactiveCurrentA },
	},

	{
		desc("active_power_factor_ratio", "Power factor, between -1 and 1."),
		func(m *homewizard.Measurement) *float64 { return m.PowerFactor },
	},

	{
		desc("frequency_hertz", "Line frequency."),
		func(m *homewizard.Measurement) *float64 { return m.FrequencyHz },
	},

	{
		desc("active_tariff",
			"The tariff currently in effect, matching one of the per-tariff counters."),
		func(m *homewizard.Measurement) *float64 { return m.Tariff },
	},

	{
		desc("average_power_15m_watts",
			"Average demand over the current quarter hour. Only meters billed on a "+
				"capacity rate report this."),
		func(m *homewizard.Measurement) *float64 { return m.AveragePower15mW },
	},

	{
		desc("monthly_power_peak_watts",
			"Highest quarter-hour average demand so far this month, which is what a "+
				"capacity rate is billed on."),
		func(m *homewizard.Measurement) *float64 { return m.MonthlyPowerPeakW },
	},

	{
		desc("active_water_liters_per_minute", "Water flowing right now."),
		func(m *homewizard.Measurement) *float64 { return m.ActiveWaterLPM },
	},
}

var measurementPhaseGauges = []struct {
	desc   *prometheus.Desc
	values func(*homewizard.Measurement) [3]*float64
}{
	{
		desc("active_power_phase_watts",
			"Active power on one phase. Negative when exporting to the grid.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.PowerPhaseW },
	},

	{
		desc("active_voltage_phase_volts", "Active voltage on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.VoltagePhaseV },
	},

	{
		desc("active_current_phase_amperes",
			"Active current on one phase. Negative when exporting to the grid.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.CurrentPhaseA },
	},

	{
		desc("active_apparent_power_phase_voltamperes", "Apparent power on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.ApparentPowerPhaseVA },
	},

	{
		desc("active_reactive_power_phase_voltamperes_reactive",
			"Reactive power on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.ReactivePowerPhaseVA },
	},

	{
		desc("active_apparent_current_phase_amperes", "Apparent current on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.ApparentCurrentPhaseA },
	},

	{
		desc("active_reactive_current_phase_amperes", "Reactive current on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.ReactiveCurrentPhaseA },
	},

	{
		desc("active_power_factor_phase_ratio", "Power factor on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.PowerFactorPhase },
	},
}

var measurementCounters = []struct {
	desc  *prometheus.Desc
	value func(*homewizard.Measurement) *float64
}{
	{
		desc("energy_import_kwh_total",
			"Energy taken from the grid since the meter was installed, all tariffs."),
		func(m *homewizard.Measurement) *float64 { return m.EnergyImportKWh },
	},

	{
		desc("energy_export_kwh_total",
			"Energy fed into the grid since the meter was installed, all tariffs."),
		func(m *homewizard.Measurement) *float64 { return m.EnergyExportKWh },
	},

	{
		desc("water_m3_total", "Water used since the meter was installed."),
		func(m *homewizard.Measurement) *float64 { return m.TotalWaterM3 },
	},

	{
		desc("battery_cycles_total", "Charge cycles this battery has completed."),
		func(m *homewizard.Measurement) *float64 { return m.Cycles },
	},

	{
		desc("power_fail_count_total", "Power failures the smart meter has recorded."),
		func(m *homewizard.Measurement) *float64 { return m.AnyPowerFailCount },
	},

	{
		desc("long_power_fail_count_total",
			"Long power failures the smart meter has recorded."),
		func(m *homewizard.Measurement) *float64 { return m.LongPowerFailCount },
	},
}

var measurementPhaseCounters = []struct {
	desc   *prometheus.Desc
	values func(*homewizard.Measurement) [3]*float64
}{
	{
		desc("voltage_sag_count_total",
			"Voltage sags the smart meter has recorded on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.VoltageSagPhaseCount },
	},

	{
		desc("voltage_swell_count_total",
			"Voltage swells the smart meter has recorded on one phase.", "phase"),
		func(m *homewizard.Measurement) [3]*float64 { return m.VoltageSwellPhaseCount },
	},
}

var (
	descEnergyImportTariff = desc("energy_import_tariff_kwh_total",
		"Energy taken from the grid on one tariff, since the meter was installed.", "tariff")
	descEnergyExportTariff = desc("energy_export_tariff_kwh_total",
		"Energy fed into the grid on one tariff, since the meter was installed.", "tariff")
)

var (
	descExternalValue = desc("external_meter_value",
		"Reading of a utility meter connected to the smart meter. The unit is a "+
			"label because it differs by meter: m3 for gas and water, GJ for heat.",
		"unique_id", "type", "unit")
	descExternalTimestamp = desc("external_meter_timestamp_seconds",
		"When this utility meter last reported, as a Unix timestamp. Gas meters "+
			"typically report hourly, not continuously.",
		"unique_id", "type")
)

var (
	descWifiRSSI     = desc("wifi_rssi_dbm", "Wi-Fi signal strength.")
	descWifiStrength = desc("wifi_strength_ratio",
		"Wi-Fi signal strength as a fraction of full, which is how the v1 API "+
			"reports it. The v2 API reports dBm instead.")
	descUptime       = desc("uptime_seconds", "How long the device has been running.")
	descCloudEnabled = desc("cloud_enabled",
		"1 when the device is allowed to talk to the HomeWizard cloud. A device "+
			"with this off is fully local and receives no firmware updates.")
	descLEDBrightness = desc("status_led_brightness_ratio",
		"Brightness of the status LED as a fraction of full.")
	descAPIv1Enabled = desc("api_v1_enabled", "1 when the v1 API is switched on.")

	descSocketPowerOn    = desc("socket_power_on", "1 when the Energy Socket's relay is closed.")
	descSocketSwitchLock = desc("socket_switch_lock",
		"1 when the Energy Socket is locked on and cannot be switched off.")
	descSocketBrightness = desc("socket_brightness_ratio",
		"Brightness of the Energy Socket's LED ring as a fraction of full.")
)

var (
	descBatteriesPower  = desc("batteries_power_watts", "Combined power of the controlled batteries. Negative when discharging into the house.")
	descBatteriesTarget = desc("batteries_target_power_watts",
		"Power the controller is aiming the batteries at.")
	descBatteriesMaxConsumption = desc("batteries_max_consumption_watts",
		"Highest charging power the controller will ask for.")
	descBatteriesMaxProduction = desc("batteries_max_production_watts",
		"Highest discharging power the controller will ask for.")
	descBatteriesCount        = desc("batteries_count", "Plug-In Batteries being controlled.")
	descBatteriesChargeToFull = desc("batteries_charge_to_full",
		"1 while the batteries are being charged to 100% regardless of household demand.")
	descBatteriesMode = desc("batteries_mode_info",
		"The control mode in effect. Always 1.", "mode")
	descBatteriesPermission = desc("batteries_permission",
		"1 for each action the batteries are currently permitted to take.", "permission")
)

var descBatteryStateOfCharge = desc("battery_state_of_charge_ratio",
	"Battery charge as a fraction of full.")

func allDescs() []*prometheus.Desc {
	descs := []*prometheus.Desc{
		descUp, descDeviceInfo, descSmartMeterInfo, descWifiInfo, descDevicesTotal,
		descLastUpdate, descMeasurementTimestamp,
		descEnergyImportTariff, descEnergyExportTariff,
		descExternalValue, descExternalTimestamp,
		descWifiRSSI, descWifiStrength, descUptime, descCloudEnabled,
		descLEDBrightness, descAPIv1Enabled,
		descSocketPowerOn, descSocketSwitchLock, descSocketBrightness,
		descBatteriesPower, descBatteriesTarget, descBatteriesMaxConsumption,
		descBatteriesMaxProduction, descBatteriesCount, descBatteriesChargeToFull,
		descBatteriesMode, descBatteriesPermission, descBatteryStateOfCharge,
		descMonthlyPeakTimestamp,
	}
	for _, g := range measurementGauges {
		descs = append(descs, g.desc)
	}
	for _, g := range measurementPhaseGauges {
		descs = append(descs, g.desc)
	}
	for _, c := range measurementCounters {
		descs = append(descs, c.desc)
	}
	for _, c := range measurementPhaseCounters {
		descs = append(descs, c.desc)
	}
	return descs
}

var descMonthlyPeakTimestamp = desc("monthly_power_peak_timestamp_seconds",
	"When this month's peak demand was recorded, as a Unix timestamp.")
