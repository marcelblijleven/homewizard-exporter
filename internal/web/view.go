package web

import (
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/homewizard"
	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
)

// View is what the dashboard renders and what /api/snapshot returns.
type View struct {
	Up        bool      `json:"up"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`

	Totals  Totals       `json:"totals"`
	Devices []DeviceView `json:"devices"`
}

// Totals summarises the household
type Totals struct {
	Devices  int     `json:"devices"`
	Online   int     `json:"online"`
	Watts    float64 `json:"watts"`
	WaterLPM float64 `json:"waterLpm"`
	HasWater bool    `json:"hasWater"`
}

// DeviceView is one device as the dashboard sees it
type DeviceView struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Product  string `json:"product"`
	Type     string `json:"type"`
	Serial   string `json:"serial"`
	Firmware string `json:"firmware"`
	API      string `json:"api"`

	Up        bool      `json:"up"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`

	Watts    *float64 `json:"watts,omitempty"`
	Phases   []Phase  `json:"phases,omitempty"`
	ImportKW *float64 `json:"importKwh,omitempty"`
	ExportKW *float64 `json:"exportKwh,omitempty"`

	WaterLPM   *float64 `json:"waterLpm,omitempty"`
	WaterTotal *float64 `json:"waterM3,omitempty"`

	BatteryPct *float64 `json:"batteryPct,omitempty"`

	SocketOn *bool `json:"socketOn,omitempty"`

	External []ExternalView `json:"external,omitempty"`
	WifiSSID string         `json:"wifiSsid,omitempty"`
}

// Phase is one phase' contribution.
type Phase struct {
	Name  string  `json:"name"`
	Watts float64 `json:"watts"`
}

// ExternalView is a gas, water or heat meter behind the smart meter.
type ExternalView struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

func buildView(s *snapshot.Snapshot, staleAfter time.Duration, now time.Time) View {
	view := View{Status: "waiting for the first poll"}
	if s == nil {
		return view
	}

	for _, device := range s.Sorted() {
		item := describe(device, staleAfter, now)
		view.Devices = append(view.Devices, item)

		view.Totals.Devices++
		if item.Up {
			view.Totals.Online++
		}
		if last := device.LastUpdate(); last.After(view.UpdatedAt) {
			view.UpdatedAt = last
		}

		if item.Up && device.Info.ProductType == homewizard.ProductP1 && item.Watts != nil {
			view.Totals.Watts += *item.Watts
		}
		if item.Up && item.WaterLPM != nil {
			view.Totals.WaterLPM += *item.WaterLPM
			view.Totals.HasWater = true
		}
	}

	switch {
	case view.UpdatedAt.IsZero():
		view.Status = "waiting for the first poll"
	case view.Totals.Online == 0:
		view.Status = "no device has fresh readings"
	case view.Totals.Online < view.Totals.Devices:
		view.Up = true
		view.Status = "some devices are not reporting"
	default:
		view.Up = true
		view.Status = "ok"
	}

	return view
}

func describe(d *snapshot.Device, staleAfter time.Duration, now time.Time) DeviceView {
	m := d.Measurement

	item := DeviceView{
		Name:     d.Name,
		Host:     d.Host,
		Product:  productName(d),
		Type:     d.Info.ProductType,
		Serial:   d.Info.Serial,
		Firmware: d.Info.FirmwareVersion,
		API:      string(d.APIVersion),

		Up:        d.Fresh(now, staleAfter),
		UpdatedAt: d.LastUpdate(),

		Watts:      m.PowerW,
		ImportKW:   m.EnergyImportKWh,
		ExportKW:   m.EnergyExportKWh,
		WaterLPM:   m.ActiveWaterLPM,
		WaterTotal: m.TotalWaterM3,
		BatteryPct: m.StateOfChargePct,

		SocketOn: d.State.PowerOn,
		WifiSSID: m.WifiSSID,
	}

	if d.System.WifiSSID != nil && item.WifiSSID == "" {
		item.WifiSSID = *d.System.WifiSSID
	}

	for i, watts := range m.PowerPhaseW {
		if watts != nil {
			item.Phases = append(item.Phases, Phase{
				Name:  [3]string{"L1", "L2", "L3"}[i],
				Watts: *watts,
			})
		}
	}

	for _, external := range m.External {
		item.External = append(item.External, ExternalView{
			Type:  external.Type,
			Value: external.Value,
			Unit:  external.Unit,
		})
	}

	switch last := item.UpdatedAt; {
	case last.IsZero():
		item.Status = "never reported"
	case !item.Up:
		item.Status = "stale, last read " + now.Sub(last).Truncate(time.Second).String() + " ago"
	default:
		item.Status = "ok"
	}

	return item
}

func productName(d *snapshot.Device) string {
	switch {
	case d.Info.ProductName != "":
		return d.Info.ProductName
	case d.Info.ProductType != "":
		return d.Info.ProductType
	default:
		return "unknown device"
	}
}
