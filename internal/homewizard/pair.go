package homewizard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
)

// DefaultUserName used when pairing
const DefaultUserName = "local/homewizard_exporter"

var userNamePattern = regexp.MustCompile(`^local/[a-zA-Z0-9\-_/\\# ]{1,40}$`)

// PairOptions configures Pair.
type PairOptions struct {
	Host      string
	Name      string
	TLS       config.TLS
	Timeout   time.Duration
	Retry     time.Duration
	OnWaiting func(id IdentityHint)
}

// IdentityHint is what the certificate revealed about the device, so the
// caller can tell the user which box to walk over to.
type IdentityHint struct {
	ProductType string
	Serial      string
}

// PairResult is a successful pairing.
type PairResult struct {
	Name  string
	Token string
	Info  Info
}

// Pair creates a user on a device and returns its token.
func Pair(ctx context.Context, opts PairOptions) (PairResult, error) {
	if opts.Name == "" {
		opts.Name = DefaultUserName
	}
	if !userNamePattern.MatchString(opts.Name) {
		return PairResult{}, fmt.Errorf("user name %q is invalid: it must start with "+
			"local/ and be 1-40 characters of letters, digits, - _ / \\ # or spaces", opts.Name)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.Retry <= 0 {
		opts.Retry = time.Second
	}

	device := config.Device{Host: opts.Host, TLS: opts.TLS}
	if device.TLS.Mode == "" {
		device.TLS.Mode = config.TLSVerify
	}

	seen := &identityRecorder{}
	httpClient, err := newHTTPClient(device, opts.Timeout, seen)
	if err != nil {
		return PairResult{}, err
	}

	c := &Client{
		host:    opts.Host,
		version: config.APIv2,
		base:    baseURL(config.APIv2, opts.Host),
		http:    httpClient,
	}

	body, err := json.Marshal(map[string]string{"name": opts.Name})
	if err != nil {
		return PairResult{}, err
	}

	ticker := time.NewTicker(opts.Retry)
	defer ticker.Stop()

	announced := false
	for {
		token, err := c.createUser(ctx, body)
		switch {
		case err == nil:
			c.token = token
			info, err := c.fetchInfo(ctx)
			if err != nil {
				return PairResult{}, fmt.Errorf("the device issued a token but then "+
					"rejected it: %w", err)
			}
			return PairResult{Name: opts.Name, Token: token, Info: info}, nil

		case errors.Is(err, errButtonNotPressed):
			if !announced && opts.OnWaiting != nil {
				id := seen.get()
				opts.OnWaiting(IdentityHint{ProductType: id.ProductType, Serial: id.Serial})
				announced = true
			}

		default:
			return PairResult{}, err
		}

		select {
		case <-ctx.Done():
			return PairResult{}, fmt.Errorf("gave up waiting for the button on %s: %w",
				opts.Host, ctx.Err())
		case <-ticker.C:
		}
	}
}

var errButtonNotPressed = errors.New("waiting for the button to be pressed")

func (c *Client) createUser(ctx context.Context, body []byte) (string, error) {
	var token string

	err := c.do(ctx, http.MethodPost, "/api/user", body, func(r io.Reader) error {
		var resp struct {
			Name  string `json:"name"`
			Token string `json:"token"`
		}
		if err := json.NewDecoder(io.LimitReader(r, maxBody)).Decode(&resp); err != nil {
			return err
		}
		if resp.Token == "" {
			return fmt.Errorf("the device accepted the request but returned no token")
		}
		token = resp.Token
		return nil
	})

	if err != nil && errors.Is(err, ErrUnauthorized) {
		return "", errButtonNotPressed
	}
	return token, err
}
