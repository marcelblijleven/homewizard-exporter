package homewizard

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
)

// New connects to one device and finds out what it is.
func New(ctx context.Context, cfg config.Device, timeout time.Duration, logger *slog.Logger) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	token, err := readToken(cfg)
	if err != nil {
		return nil, fmt.Errorf("device %s: %w", cfg.Name, err)
	}

	seen := &identityRecorder{}
	httpClient, err := newHTTPClient(cfg, timeout, seen)
	if err != nil {
		return nil, fmt.Errorf("device %s: %w", cfg.Name, err)
	}

	c := &Client{
		name:   cfg.Name,
		host:   cfg.Host,
		http:   httpClient,
		token:  token,
		logger: logger,
	}

	var attempts []error
	for _, version := range probeOrder(cfg, token) {
		c.version = version
		c.base = baseURL(version, cfg.Host)

		info, err := c.fetchInfo(ctx)
		if err != nil {
			attempts = append(attempts, fmt.Errorf("%s: %w", version, err))
			continue
		}

		c.info = info
		c.cert = seen.get()
		if cfg.Type != "" {
			c.info.ProductType = cfg.Type
		}
		if c.info.Serial == "" && c.cert.valid() {
			c.info.Serial = c.cert.Serial
		}
		c.caps = capabilitiesFor(c.info, version)

		logger.Info(
			"connected to device",
			"device", cfg.Name,
			"url", c.base.String(),
			"product", c.info.ProductType,
			"serial", c.info.Serial,
			"firmware", c.info.FirmwareVersion,
			"api", version,
			"api_version", c.info.APIVersion,
			"capabilities", c.caps.String(),
		)
		return c, nil
	}

	return nil, connectError(cfg, token, seen.get(), attempts)
}

func (c *Client) fetchInfo(ctx context.Context) (Info, error) {
	var info Info
	if err := c.getJSON(ctx, "/api", &info); err != nil {
		return Info{}, err
	}
	if info.ProductType == "" && info.Serial == "" {
		return Info{}, fmt.Errorf("GET /api returned nothing that identifies a device")
	}
	return info, nil
}

func probeOrder(cfg config.Device, token string) []config.APIVersion {
	switch {
	case cfg.APIVersion == config.APIv1:
		return []config.APIVersion{config.APIv1}
	case cfg.APIVersion == config.APIv2:
		return []config.APIVersion{config.APIv2}
	case token != "":
		return []config.APIVersion{config.APIv2, config.APIv1}
	default:
		return []config.APIVersion{config.APIv1, config.APIv2}
	}
}

func baseURL(version config.APIVersion, host string) *url.URL {
	if version == config.APIv2 {
		return &url.URL{Scheme: "https", Host: host}
	}
	return &url.URL{Scheme: "http", Host: host}
}

func connectError(cfg config.Device, token string, cert identity, attempts []error) error {
	joined := errors.Join(attempts...)
	where := fmt.Sprintf("device %s (%s)", cfg.Name, cfg.Host)

	var unauthorized, apiDisabled bool
	for _, err := range attempts {
		unauthorized = unauthorized || errors.Is(err, ErrUnauthorized)
		apiDisabled = apiDisabled || errors.Is(err, ErrAPIDisabled)
	}

	switch {
	case token == "" && cert.valid():
		return fmt.Errorf("%s speaks the v2 API but no token is configured.\n\n"+
			"Run `homewizard_exporter pair %s` and press the button on the device.\n"+
			"The device identified itself as %s, serial %s.\n\n%w",
			where, cfg.Host, describeCert(cert), cert.Serial, joined)

	case token == "" && unauthorized:
		return fmt.Errorf("%s rejected an unauthenticated request.\n\n"+
			"Run `homewizard_exporter pair %s` to get a token.\n\n%w",
			where, cfg.Host, joined)

	case token != "" && unauthorized:
		return fmt.Errorf("%s rejected the token.\n\n"+
			"Tokens are invalidated when another application pairs under the same\n"+
			"name. Run `homewizard_exporter pair %s` again.\n\n%w",
			where, cfg.Host, joined)

	case apiDisabled:
		return fmt.Errorf("%s has the v1 API switched off.\n\n"+
			"Turn it on in the HomeWizard app under Settings > Meters > your meter >\n"+
			"Local API, or pair for the v2 API instead with\n"+
			"`homewizard_exporter pair %s`.\n\n%w",
			where, cfg.Host, joined)

	default:
		return fmt.Errorf("cannot reach %s: %w", where, joined)
	}
}

func describeCert(cert identity) string {
	if cert.ProductType != "" {
		return cert.ProductType
	}
	return "a " + cert.CertType
}
