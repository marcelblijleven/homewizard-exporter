package homewizard

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/marcelblijleven/homewizard_exporter/internal/config"
)

// The v2 API is HTTPS, served by the device itself with a certificate signed by
// HomeWizard's own CA. The CA is published alongside the API documentation and
// is embedded here so that verification needs no files on disk. It expires in
// December 2031.
//
//go:embed homewizard-ca.pem
var homewizardCAPEM []byte

// certTypes maps the product identifier used in a device certificate to the
// product type the API reports. HomeWizard kept the older spelling in the
// certificates, so the two differ.
//
// energymeter covers both the 1-phase and the 3-phase kWh Meter, so the
// certificate narrows the device down to a family and no further. It is a hint
// for logging and for the pairing flow; /api is what the client believes.
var certTypes = map[string]string{
	"p1dongle":     ProductP1,
	"energysocket": ProductSocket,
	"energymeter":  unknownProductID,
	"watermeter":   ProductWater,
	"battery":      ProductBattery,
	"display":      ProductDisplay,
}

// identity is what a device's certificate claims about itself. The Common Name
// is `appliance/<certType>/<serial>`, which means a plain TLS handshake reveals
// the serial and the product family before any credential is presented.
type identity struct {
	CertType    string
	ProductType string
	Serial      string
}

func (i identity) valid() bool { return i.Serial != "" }

// tlsConfig builds the client TLS configuration and, when verification is on,
// records the identity of whatever device answered.
//
// Verification cannot use Go's built-in hostname check. The certificates
// identify a device by Common Name rather than by a Subject Alternative Name,
// and Go stopped honouring the Common Name as a hostname in 1.15. So the
// handshake is told to skip its own checks and VerifyPeerCertificate does the
// work: chain to the HomeWizard CA, then read the name.
func tlsConfig(cfg config.TLS, seen *identityRecorder) (*tls.Config, error) {
	if cfg.Mode == config.TLSInsecure {
		return &tls.Config{InsecureSkipVerify: true}, nil //nolint:gosec // explicitly requested
	}

	pem := homewizardCAPEM
	if cfg.CAFile != "" {
		b, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read tls.ca_file: %w", err)
		}
		pem = b
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("tls.ca_file %s contains no certificates", cfg.CAFile)
	}

	return &tls.Config{
		// Not actually insecure: it turns off the built-in verification so
		// that VerifyPeerCertificate below can do the equivalent by hand.
		InsecureSkipVerify: true, //nolint:gosec // replaced by VerifyPeerCertificate
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			id, err := verifyChain(rawCerts, pool)
			if err != nil {
				return err
			}
			seen.set(id)
			return nil
		},
	}, nil
}

func verifyChain(rawCerts [][]byte, pool *x509.CertPool) (identity, error) {
	if len(rawCerts) == 0 {
		return identity{}, fmt.Errorf("device presented no certificate")
	}

	certs := make([]*x509.Certificate, 0, len(rawCerts))
	for i, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return identity{}, fmt.Errorf("parse certificate %d: %w", i, err)
		}
		certs = append(certs, cert)
	}

	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}

	_, err := certs[0].Verify(x509.VerifyOptions{
		Roots:         pool,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return identity{}, fmt.Errorf("certificate is not signed by the HomeWizard CA "+
			"(set tls.mode: insecure to skip this check): %w", err)
	}

	id, err := parseCommonName(certs[0].Subject.CommonName)
	if err != nil {
		return identity{}, err
	}
	return id, nil
}

func parseCommonName(cn string) (identity, error) {
	parts := strings.Split(cn, "/")
	if len(parts) != 3 || parts[0] != "appliance" {
		return identity{}, fmt.Errorf(
			"certificate common name %q is not appliance/<type>/<serial>", cn,
		)
	}

	// An unrecognised cert type is not an error. HomeWizard ships new products
	// and the API tells us what this one is anyway.
	return identity{
		CertType:    parts[1],
		ProductType: certTypes[parts[1]],
		Serial:      parts[2],
	}, nil
}

type identityRecorder struct {
	mu sync.Mutex
	id identity
}

func (r *identityRecorder) set(id identity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.id = id
}

func (r *identityRecorder) get() identity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id
}
