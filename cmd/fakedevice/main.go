// Command fakedevice replays the captured fixtures as if it were a house full
// of HomeWizard hardware.
//
// It exists so the exporter can be run, and the dashboard looked at, without
// owning every product HomeWizard makes. The v2 devices are served over HTTPS
// with a certificate whose Common Name follows the real scheme
// (appliance/<type>/<serial>), signed by a CA this command writes out -- so
// pointing the exporter at it exercises the real certificate verification path
// rather than skipping it.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// device is one fake appliance.
type device struct {
	name     string
	product  string
	certType string
	serial   string
	v2       bool
	port     int
	// fixtures maps a request path to the file that answers it.
	fixtures map[string]string
}

// The set below covers one of each shape the collector has to handle: a
// three-phase P1 on v2 with gas and water behind it, a v1 socket with a relay,
// a v1 water meter, and a v2 battery.
func devices(dir string) []*device {
	return []*device{
		{
			name: "p1", product: "HWE-P1", certType: "p1dongle",
			serial: "aabbccddeeff", v2: true, port: 8443,
			fixtures: map[string]string{
				"/api":             "v2_hwe-p1_info.json",
				"/api/measurement": "v2_hwe-p1_measurement.json",
				"/api/system":      "v2_hwe-p1_system.json",
				"/api/batteries":   "v2_hwe-p1_batteries.json",
				"/api/telegram":    "v1_hwe-p1_telegram.txt",
			},
		},
		{
			name: "kwh", product: "HWE-KWH3", certType: "energymeter",
			serial: "aabbccddee01", v2: false, port: 8081,
			fixtures: map[string]string{
				"/api":           "v1_hwe-kwh3_info.json",
				"/api/v1/data":   "v1_hwe-kwh3_measurement.json",
				"/api/v1/system": "v1_hwe-p1_system.json",
			},
		},
		{
			name: "socket", product: "HWE-SKT", certType: "energysocket",
			serial: "aabbccddee02", v2: false, port: 8082,
			fixtures: map[string]string{
				"/api":           "v1_hwe-skt_info.json",
				"/api/v1/data":   "v1_hwe-skt_measurement.json",
				"/api/v1/state":  "v1_hwe-skt_state.json",
				"/api/v1/system": "v1_hwe-p1_system.json",
			},
		},
		{
			name: "water", product: "HWE-WTR", certType: "watermeter",
			serial: "aabbccddee03", v2: false, port: 8083,
			fixtures: map[string]string{
				"/api":           "v1_hwe-wtr_info.json",
				"/api/v1/data":   "v1_hwe-wtr_measurement.json",
				"/api/v1/system": "v1_hwe-p1_system.json",
			},
		},
		{
			name: "battery", product: "HWE-BAT", certType: "battery",
			serial: "aabbccddee04", v2: true, port: 8444,
			fixtures: map[string]string{
				"/api":             "v2_hwe-bat_info.json",
				"/api/measurement": "v2_hwe-bat_measurement.json",
				"/api/system":      "v2_hwe-p1_system.json",
			},
		},
	}
}

func main() {
	dir := flag.String("dir", "internal/homewizard/testdata", "where the fixtures live")
	host := flag.String("host", "127.0.0.1", "address to listen on")
	caOut := flag.String("ca", "dist/fake-ca.pem", "write the CA certificate here")
	token := flag.String("token", "0123456789ABCDEF0123456789ABCDEF",
		"token the v2 devices will accept")
	jitter := flag.Bool("jitter", true, "vary the readings slightly on each request")
	flag.Parse()

	ca, err := newCA()
	if err != nil {
		log.Fatal(err)
	}
	if err := writeCA(*caOut, ca); err != nil {
		log.Fatal(err)
	}
	// Stdout carries a config file and nothing else, so `fakedevice > fake.yaml`
	// produces something the exporter can be pointed straight at.
	fmt.Fprintf(os.Stderr, "CA certificate written to %s\n", *caOut)

	var wg sync.WaitGroup
	fmt.Println("devices:")
	for _, d := range devices(*dir) {
		fmt.Printf("  - name: %s\n    host: %s:%d\n", d.name, *host, d.port)
		if d.v2 {
			fmt.Printf("    token: %s\n    tls:\n      ca_file: %s\n", *token, *caOut)
		}
		fmt.Fprintf(os.Stderr, "%-8s %s  %s:%d\n", d.name, d.product, *host, d.port)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.serve(*dir, *host, ca, *token, *jitter); err != nil {
				log.Printf("%s: %v", d.name, err)
			}
		}()
	}
	fmt.Println()
	wg.Wait()
}

func (d *device) serve(dir, host string, ca *authority, token string, jitter bool) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// v2 devices demand a bearer token, and answering 401 without one is
		// the behaviour the exporter's error messages are written against.
		if d.v2 && r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":"user:unauthorized"}`)
			return
		}

		fixture, ok := d.fixtures[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		body, err := os.ReadFile(filepath.Join(dir, fixture))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if strings.HasSuffix(fixture, ".txt") {
			w.Header().Set("Content-Type", "text/plain")
			w.Write(body)
			return
		}

		if jitter && strings.Contains(fixture, "measurement") {
			body = vary(body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})

	addr := fmt.Sprintf("%s:%d", host, d.port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	if !d.v2 {
		return srv.ListenAndServe()
	}

	cert, err := ca.issue("appliance/" + d.certType + "/" + d.serial)
	if err != nil {
		return err
	}
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{*cert}}
	return srv.ListenAndServeTLS("", "")
}

// vary nudges the power readings so a dashboard left open does something. It
// only touches instantaneous values: moving a meter reading backwards would
// make a counter look like it had reset.
func vary(body []byte) []byte {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return body
	}

	// A signed swing so power crosses between import and export, which is what
	// the dashboard colours on.
	swing := float64(time.Now().Unix()%60) - 30
	for _, key := range []string{"power_w", "power_l1_w", "power_l2_w", "power_l3_w",
		"active_power_w", "active_power_l1_w", "active_power_l2_w", "active_power_l3_w"} {
		if value, ok := doc[key].(float64); ok {
			doc[key] = value + swing*2
		}
	}

	// Water only flows one way, and a tap running at minus fifty litres a
	// minute is the sort of nonsense that sends someone hunting a real bug.
	if value, ok := doc["active_liter_lpm"].(float64); ok {
		doc["active_liter_lpm"] = math.Round(max(0, value+swing/10)*10) / 10
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return body
	}
	return out
}

// authority is a throwaway CA standing in for HomeWizard's.
type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	der  []byte
}

func newCA() (*authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"fakedevice"}, CommonName: "Fake Appliance Access CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &authority{cert: cert, key: key, der: der}, nil
}

// issue mints a device certificate. The name goes in the Common Name and
// nowhere else, exactly as HomeWizard's do -- which is the whole reason the
// exporter cannot use Go's built-in hostname verification.
func (a *authority) issue(commonName string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{Organization: []string{"fakedevice"}, CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func writeCA(path string, ca *authority) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.der})
	return os.WriteFile(path, block, 0o644)
}
