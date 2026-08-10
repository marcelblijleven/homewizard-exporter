package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
)

// printCheck summarises what was actually resolved
func printCheck(cfg *config.Config, path string) {
	source := path
	if source == "" {
		source = "(defaults and environment only, no -config given)"
	}

	fmt.Println("config ok:", source)
	fmt.Println("  listen:          ", cfg.Server.Listen+cfg.Server.MetricsPath)
	fmt.Println("  poll interval:   ", cfg.Poll.Interval)
	fmt.Println("  system interval: ", cfg.Poll.SystemInterval)
	fmt.Println("  stale after:     ", cfg.Poll.StaleAfter)
	fmt.Println("  dashboard:       ", dashboardSummary(cfg.Dashboard))
	fmt.Println("  devices:         ", len(cfg.Devices))
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tHOST\tAPI\tTYPE\tCREDENTIALS\tINTERVAL")
	for _, d := range cfg.Devices {
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			d.Name, d.Host, d.APIVersion, orDetected(d.Type),
			credentialSummary(d), interval(d, cfg.Poll))
	}
	_ = w.Flush()

	for _, warning := range configWarnings(cfg) {
		fmt.Println("\nwarning:", warning)
	}
}

// configWarnings
func configWarnings(cfg *config.Config) []string {
	var warnings []string
	var tokenless, insecure []string

	for _, d := range cfg.Devices {
		if d.APIVersion != config.APIv1 && !d.HasToken() {
			tokenless = append(tokenless, d.Name)
		}
		if d.TLS.Mode == config.TLSInsecure {
			insecure = append(insecure, d.Name)
		}
	}

	if len(tokenless) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"no token for %s, so only the v1 API can be used.\n"+
				"         That works for a P1 Meter, kWh Meter, Energy Socket or Watermeter with\n"+
				"         Local API switched on in the HomeWizard app, but not for a Plug-In\n"+
				"         Battery. Run `homewizard_exporter pair <host>` to use the v2 API.",
			strings.Join(tokenless, ", "),
		))
	}
	if len(insecure) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"tls.mode is insecure for %s, so their certificates are not checked.\n"+
				"         Anything on the network can impersonate them.",
			strings.Join(insecure, ", "),
		))
	}

	return warnings
}

func credentialSummary(d config.Device) string {
	switch {
	case d.Token != "":
		return "token (supplied directly)"
	case d.HasToken():
		return "token in " + d.TokenFile
	default:
		return "none (v1 only)"
	}
}

func interval(d config.Device, poll config.Poll) string {
	if d.Interval > 0 {
		return d.Interval.String()
	}
	return poll.Interval.String()
}

func orDetected(s string) string {
	if s == "" {
		return "(detected)"
	}
	return s
}

func dashboardSummary(d config.Dashboard) string {
	if !d.Enabled {
		return "disabled"
	}
	return "enabled at " + d.Path
}
