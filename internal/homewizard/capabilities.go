package homewizard

import (
	"strconv"
	"strings"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
)

// Capabilities records which endpoints a device is expected to answer.
type Capabilities struct {
	Measurement bool
	System      bool
	// State is the Energy Socket's relay on v1 only
	State     bool
	Telegram  bool
	Batteries bool
}

// String renders the capabilities for a log line.
func (c Capabilities) String() string {
	var on []string
	for _, cap := range []struct {
		name string
		set  bool
	}{
		{"measurement", c.Measurement},
		{"system", c.System},
		{"state", c.State},
		{"telegram", c.Telegram},
		{"batteries", c.Batteries},
	} {
		if cap.set {
			on = append(on, cap.name)
		}
	}
	if len(on) == 0 {
		return "none"
	}
	return strings.Join(on, ",")
}

func capabilitiesFor(info Info, version config.APIVersion) Capabilities {
	caps := Capabilities{Measurement: true, System: true}
	v2 := version == config.APIv2

	switch info.ProductType {
	case ProductP1:
		caps.Telegram = true
		caps.Batteries = v2 && atLeastAPI(info.APIVersion, 2, 1)
	case ProductKWh1, ProductKWh3, ProductSDM230, ProductSDM630:
		caps.Batteries = v2 && atLeastAPI(info.APIVersion, 2, 1)
	case ProductSocket:
		caps.State = !v2
	}
	return caps
}

func atLeastAPI(version string, major, minor int) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")

	gotMajor, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	if gotMajor != major {
		return gotMajor > major
	}
	if len(parts) < 2 {
		return minor == 0
	}
	gotMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return gotMinor >= minor
}
