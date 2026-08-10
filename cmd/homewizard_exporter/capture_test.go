package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScrubJSON(t *testing.T) {
	body := []byte(`{
		"serial": "3c39e724c550",
		"wifi_ssid": "Marcels Wi-Fi",
		"unique_id": "4530303637303035363935373032333231",
		"gas_unique_id": "4730303339303031373030343630313137",
		"active_power_w": -1072,
		"external": [
			{"unique_id": "4730303339303031373030343630313137", "type": "gas_meter", "value": 2569.646},
			{"unique_id": "ABCDEF0123456789ABCDEF0123456789", "type": "water_meter", "value": 12.3}
		]
	}`)

	got := scrub(body)

	for _, secret := range []string{
		"3c39e724c550",
		"Marcels Wi-Fi",
		"4530303637303035363935373032333231",
		"4730303339303031373030343630313137",
		"ABCDEF0123456789ABCDEF0123456789",
	} {
		if strings.Contains(string(got), secret) {
			t.Errorf("%q survived scrubbing:\n%s", secret, got)
		}
	}

	// The measurements are the entire point of the fixture and must survive.
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("scrubbing produced invalid JSON: %v", err)
	}
	if doc["active_power_w"] != -1072.0 {
		t.Errorf("active_power_w = %v, want the reading to be untouched", doc["active_power_w"])
	}

	external, ok := doc["external"].([]any)
	if !ok || len(external) != 2 {
		t.Fatalf("external = %v, want two meters", doc["external"])
	}

	// Two meters must stay two distinguishable meters, or a fixture with a gas
	// and a water meter collapses into one and stops testing anything.
	first := external[0].(map[string]any)["unique_id"]
	second := external[1].(map[string]any)["unique_id"]
	if first == second {
		t.Errorf("both external meters ended up as %v", first)
	}
	if external[0].(map[string]any)["value"] != 2569.646 {
		t.Error("the gas reading was altered")
	}
}

func TestScrubTelegram(t *testing.T) {
	telegram := []byte(`/ISK5\2M550T-10111-

1-3:0.2.8(50)
0-0:1.0.0(181106140429W)
0-0:96.1.1(4530303637303035363935373032333231)
1-0:1.8.1(10830.511*kWh)
0-0:96.13.0(a message from the utility)
0-1:96.1.0(4730303339303031373030343630313137)
0-1:24.2.1(210606140010W)(02569.646*m3)
!1F28
`)

	got := string(scrub(telegram))

	for _, secret := range []string{
		"4530303637303035363935373032333231",
		"4730303339303031373030343630313137",
		"a message from the utility",
	} {
		if strings.Contains(got, secret) {
			t.Errorf("%q survived scrubbing:\n%s", secret, got)
		}
	}

	// The readings, and the structure around them, have to survive intact or
	// the fixture no longer describes a real telegram.
	for _, keep := range []string{
		`/ISK5\2M550T-10111-`,
		"1-0:1.8.1(10830.511*kWh)",
		"0-1:24.2.1(210606140010W)(02569.646*m3)",
		"0-0:1.0.0(181106140429W)",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q was lost:\n%s", keep, got)
		}
	}

	// The blanked value keeps its length, so the fixture still looks like a
	// telegram from a meter with a 34-character identifier.
	if !strings.Contains(got, "0-0:96.1.1(0000000000000000000000000000000000)") {
		t.Errorf("identifier was not blanked to the same length:\n%s", got)
	}
}

// TestScrubLeavesUnknownTextAlone: anything that is neither JSON nor a telegram
// is returned as-is rather than mangled.
func TestScrubHandlesEmptyBody(t *testing.T) {
	if got := scrub(nil); len(got) != 0 {
		t.Errorf("scrub(nil) = %q", got)
	}
	if got := scrub([]byte("not json, not a telegram")); string(got) != "not json, not a telegram" {
		t.Errorf("scrub mangled plain text: %q", got)
	}
}
