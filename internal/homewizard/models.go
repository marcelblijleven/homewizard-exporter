// Package homewizard talks to HomeWizard Energy devices over their local API,
// handling both the v1 (HTTP) and v2 (HTTPS, bearer token) generations.
package homewizard

import (
	"fmt"
	"strconv"
	"time"
)

// Product types the local API reports
const (
	ProductP1        = "HWE-P1"
	ProductSocket    = "HWE-SKT"
	ProductWater     = "HWE-WTR"
	ProductKWh1      = "HWE-KWH1"
	ProductKWh3      = "HWE-KWH3"
	ProductSDM230    = "SDM230-wifi"
	ProductSDM630    = "SDM630-wifi"
	ProductBattery   = "HWE-BAT"
	ProductDisplay   = "HWE-DSP"
	phaseCount       = 3
	maxTariffs       = 4
	unknownProductID = ""
)

// Info is the device identity from /api (v1 and v2 return the same shape).
type Info struct {
	ProductType     string `json:"product_type"`
	ProductName     string `json:"product_name"`
	Serial          string `json:"serial"`
	FirmwareVersion string `json:"firmware_version"`
	APIVersion      string `json:"api_version"`
}

// Measurement is one reading from the API
type Measurement struct {
	// Smart meter identity, P1 only.
	MeterModel      string
	UniqueID        string
	ProtocolVersion *float64
	Timestamp       time.Time

	Tariff                *float64
	EnergyImportKWh       *float64
	EnergyExportKWh       *float64
	EnergyImportTariffKWh [maxTariffs]*float64
	EnergyExportTariffKWh [maxTariffs]*float64

	PowerW      *float64
	PowerPhaseW [phaseCount]*float64

	VoltageV      *float64
	VoltagePhaseV [phaseCount]*float64

	CurrentA      *float64
	CurrentPhaseA [phaseCount]*float64

	ApparentPowerVA      *float64
	ApparentPowerPhaseVA [phaseCount]*float64
	ReactivePowerVAR     *float64
	ReactivePowerPhaseVA [phaseCount]*float64

	ApparentCurrentA      *float64
	ApparentCurrentPhaseA [phaseCount]*float64
	ReactiveCurrentA      *float64
	ReactiveCurrentPhaseA [phaseCount]*float64

	PowerFactor      *float64
	PowerFactorPhase [phaseCount]*float64

	FrequencyHz *float64

	VoltageSagPhaseCount   [phaseCount]*float64
	VoltageSwellPhaseCount [phaseCount]*float64
	AnyPowerFailCount      *float64
	LongPowerFailCount     *float64

	// Capacity rate (capaciteitstarief), Belgian smart meters.
	AveragePower15mW   *float64
	MonthlyPowerPeakW  *float64
	MonthlyPowerPeakAt time.Time

	// Watermeter.
	ActiveWaterLPM *float64
	TotalWaterM3   *float64

	// Plug-In Battery.
	StateOfChargePct *float64
	Cycles           *float64

	// External utility meters hanging off the smart meter: gas, water, heat.
	External []External

	// Wi-Fi, which the v1 API reports as part of the measurement and the v2
	// API reports from /api/system instead.
	WifiSSID        string
	WifiStrengthPct *float64
}

// External is one utility meter connected to the smart meter.
type External struct {
	UniqueID  string
	Type      string
	Unit      string
	Value     float64
	Timestamp time.Time
}

// State is the Energy Socket's relay state (v1 /api/v1/state).
type State struct {
	PowerOn    *bool `json:"power_on"`
	SwitchLock *bool `json:"switch_lock"`
	Brightness *int  `json:"brightness"`
}

// System is the device's own health and settings. The v1 API reports only
// cloud_enabled. Te v2 API adds uptime, Wi-Fi and the LED.
type System struct {
	WifiSSID      *string  `json:"wifi_ssid"`
	WifiRSSIdB    *float64 `json:"wifi_rssi_db"`
	UptimeS       *float64 `json:"uptime_s"`
	CloudEnabled  *bool    `json:"cloud_enabled"`
	LEDBrightness *float64 `json:"status_led_brightness_pct"`
	APIv1Enabled  *bool    `json:"api_v1_enabled"`
}

// Batteries is the state of the Plug-In Battery group, reported by the P1 or
// kWh Meter that controls it rather than by the batteries themselves.
type Batteries struct {
	Mode            string   `json:"mode"`
	Permissions     []string `json:"permissions"`
	ChargeToFull    *bool    `json:"charge_to_full"`
	BatteryCount    *float64 `json:"battery_count"`
	PowerW          *float64 `json:"power_w"`
	TargetPowerW    *float64 `json:"target_power_w"`
	MaxConsumptionW *float64 `json:"max_consumption_w"`
	MaxProductionW  *float64 `json:"max_production_w"`
}

func parseISOTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05", s, time.Local)
}

func parseCompactTimestamp(n float64) (time.Time, error) {
	if n <= 0 {
		return time.Time{}, nil
	}
	s := strconv.FormatInt(int64(n), 10)
	for len(s) < 12 {
		s = "0" + s
	}
	if len(s) != 12 {
		return time.Time{}, fmt.Errorf("timestamp %s is not YYMMDDhhmmss", s)
	}
	return time.ParseInLocation("060102150405", s, time.Local)
}

func ptr[T any](v T) *T { return &v }
