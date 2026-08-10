package homewizard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
)

var (
	// ErrNotFound means the device does not implement the endpoint.
	ErrNotFound = errors.New("endpoint not found")
	// ErrUnauthorized means the token was missing or rejected.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrAPIDisabled means the v1 API is switched off for this device.
	ErrAPIDisabled = errors.New("v1 API is disabled")
	// ErrNoTelegram means the P1 Meter has not received a telegram yet.
	ErrNoTelegram = errors.New("no telegram received")
)

// maxBody caps how much of a response is read. Even a P1 telegram is a couple
// of kilobytes; anything larger is a device behaving weird
const maxBody = 1 << 20

// Client is a connection to one HomeWizard device, pinned to the API version
// the device has
type Client struct {
	name    string
	host    string
	version config.APIVersion
	base    *url.URL
	http    *http.Client
	token   string
	logger  *slog.Logger

	info Info
	cert identity

	mu   sync.Mutex
	caps Capabilities
}

// Name returns the configured device name, which labels its metrics.
func (c *Client) Name() string { return c.name }

// Host returns the address the device was reached at.
func (c *Client) Host() string { return c.host }

// Info returns the device identity read at connection time.
func (c *Client) Info() Info { return c.info }

// Capabilities returns which endpoints this device is expected to serve.
func (c *Client) Capabilities() Capabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps
}

// Disable disables a capability on thr client
func (c *Client) Disable(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch endpoint {
	case "system":
		c.caps.System = false
	case "state":
		c.caps.State = false
	case "telegram":
		c.caps.Telegram = false
	case "batteries":
		c.caps.Batteries = false
	}
}

// APIVersion returns the API generation in use.
func (c *Client) APIVersion() config.APIVersion { return c.version }

// BaseURL returns the URL the client settled on.
func (c *Client) BaseURL() string { return c.base.String() }

// Measurement fetches the most recent reading.
func (c *Client) Measurement(ctx context.Context) (Measurement, error) {
	if c.version == config.APIv2 {
		var raw v2Measurement
		if err := c.getJSON(ctx, "/api/measurement", &raw); err != nil {
			return Measurement{}, err
		}
		return raw.measurement()
	}

	var raw v1Data
	if err := c.getJSON(ctx, "/api/v1/data", &raw); err != nil {
		return Measurement{}, err
	}
	return raw.measurement()
}

// System fetches the device's own health and settings.
func (c *Client) System(ctx context.Context) (System, error) {
	path := "/api/v1/system"
	if c.version == config.APIv2 {
		path = "/api/system"
	}

	var system System
	err := c.getJSON(ctx, path, &system)
	return system, err
}

// State fetches the Energy Socket's relay state. It exists on v1 only.
func (c *Client) State(ctx context.Context) (State, error) {
	var state State
	err := c.getJSON(ctx, "/api/v1/state", &state)
	return state, err
}

// Batteries fetches the state of the Plug-In Battery group. It is served by the
// P1 or kWh Meter that controls the batteries, not by a battery itself.
func (c *Client) Batteries(ctx context.Context) (Batteries, error) {
	var batteries Batteries
	err := c.getJSON(ctx, "/api/batteries", &batteries)
	return batteries, err
}

// GetRaw fetches path and returns the undecoded body. It exists for capturing
// fixtures, where the point is to preserve exactly what the device sent.
func (c *Client) GetRaw(ctx context.Context, path string) ([]byte, error) {
	var body []byte
	err := c.do(ctx, http.MethodGet, path, nil, func(r io.Reader) error {
		var err error
		body, err = io.ReadAll(io.LimitReader(r, maxBody))
		return err
	})
	return body, err
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	return c.do(ctx, http.MethodGet, path, nil, func(r io.Reader) error {
		return json.NewDecoder(io.LimitReader(r, maxBody)).Decode(dst)
	})
}

func (c *Client) do(
	ctx context.Context,
	method, path string,
	body []byte,
	decode func(io.Reader) error,
) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16)) // drain so the connection can be reused
		resp.Body.Close()
	}()

	if err := statusError(method, path, resp); err != nil {
		return err
	}
	if decode == nil {
		return nil
	}
	if err := decode(resp.Body); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	ref, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("bad endpoint %q: %w", path, err)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base.ResolveReference(ref).String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.version == config.APIv2 {
		req.Header.Set("X-Api-Version", "2")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	}
	return req, nil
}

func statusError(method, path string, resp *http.Response) error {
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	code, description := decodeAPIError(snippet)

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s %s: %w", method, path, ErrNotFound)

	case code == "202":
		return fmt.Errorf("%s %s: %w -- enable it in the HomeWizard app "+
			"under Settings > Meters > your meter > Local API", method, path, ErrAPIDisabled)

	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s %s: %w (%s)", method, path, ErrUnauthorized, resp.Status)

	case resp.StatusCode == http.StatusServiceUnavailable &&
		strings.Contains(string(snippet), "telegram"):
		return fmt.Errorf("%s %s: %w", method, path, ErrNoTelegram)
	}

	if description != "" {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, description)
	}
	if msg := strings.TrimSpace(string(snippet)); msg != "" {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, msg)
	}
	return fmt.Errorf("%s %s: %s", method, path, resp.Status)
}

func decodeAPIError(body []byte) (code, description string) {
	// v2: {"error": "user:unauthorized"}
	var flat struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &flat); err == nil && flat.Error != "" {
		return flat.Error, flat.Error
	}

	// v1: {"error": {"id": 202, "description": "API not enabled"}}
	var nested struct {
		Error struct {
			ID          json.Number `json:"id"`
			Description string      `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &nested); err == nil && nested.Error.ID != "" {
		return nested.Error.ID.String(), nested.Error.Description
	}
	return "", ""
}

// newHTTPClient builds the transport for one device.
func newHTTPClient(cfg config.Device, timeout time.Duration, seen *identityRecorder) (*http.Client, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("http.DefaultTransport is not an *http.Transport")
	}

	transport := base.Clone()
	// One device, polled on a timer: a large connection pool is pointless.
	transport.MaxIdleConnsPerHost = 2

	tlsCfg, err := tlsConfig(cfg.TLS, seen)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = tlsCfg

	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func readToken(cfg config.Device) (string, error) {
	if cfg.Token != "" {
		return cfg.Token, nil
	}
	if cfg.TokenFile == "" {
		return "", nil
	}

	b, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read token_file %s: %w", cfg.TokenFile, err)
	}
	return strings.TrimSpace(string(b)), nil
}
