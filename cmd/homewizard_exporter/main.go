// Command homewizard_exporter exports readings from HomeWizard Energy devices
// as Prometheus metrics.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/marcelblijleven/homewizard_exporter/internal/buildinfo"
	"github.com/marcelblijleven/homewizard_exporter/internal/config"
	"github.com/marcelblijleven/homewizard_exporter/internal/metrics"
	"github.com/marcelblijleven/homewizard_exporter/internal/poller"
	"github.com/marcelblijleven/homewizard_exporter/internal/snapshot"
	"github.com/marcelblijleven/homewizard_exporter/internal/web"
)

var (
	configPath  = flag.String("config", "", "path to the YAML config file")
	checkOnly   = flag.Bool("check", false, "validate the configuration and exit")
	showVersion = flag.Bool("version", false, "print version information and exit")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "homewizard_exporter: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	flag.Usage = usage

	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		switch cmd := os.Args[1]; cmd {
		case "pair":
			return runPair(os.Args[2:])
		case "discover":
			return runDiscover(os.Args[2:])
		case "capture":
			return runCapture(os.Args[2:])
		case "help":
			flag.Usage()
			return nil
		default:
			return fmt.Errorf("unknown command %q (try: homewizard_exporter help)", cmd)
		}
	}
	return runDaemon()
}

func runDaemon() error {
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Get())
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	if *checkOnly {
		printCheck(cfg, *configPath)
		return nil
	}

	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("starting",
		"version", buildinfo.Get().Version, "devices", len(cfg.Devices))

	// SIGINT/SIGTERM cancel the context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	registry := metrics.NewRegistry()
	store := &snapshot.Store{}

	// Devices are connected to by the poller rather than here, so that a meter
	// which is rebooting at the moment the exporter starts is retried instead
	// of taking the whole process down with it.
	poll := poller.New(poller.Options{
		Devices:  cfg.Devices,
		Store:    store,
		Poll:     cfg.Poll,
		Logger:   logger,
		Registry: registry,
	})

	registry.MustRegister(metrics.NewCollector(metrics.CollectorOptions{
		Store:      store,
		StaleAfter: cfg.Poll.StaleAfter.Duration(),
	}))

	server, err := web.New(web.Options{
		Server:     cfg.Server,
		Dashboard:  cfg.Dashboard,
		Registry:   registry,
		Logger:     logger,
		Healthz:    poll.Healthz,
		Store:      store,
		StaleAfter: cfg.Poll.StaleAfter.Duration(),
	})
	if err != nil {
		return err
	}

	// The poller stops with the context
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := poll.Run(ctx); err != nil {
			logger.Error("poller stopped", "error", err)
		}
	}()

	err = server.Run(ctx)
	wg.Wait()
	return err
}

func newLogger(cfg config.Log) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.Format == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, `%s

Usage: homewizard_exporter [flags]           run the exporter
       homewizard_exporter pair <host>       get a v2 API token from a device
       homewizard_exporter discover          find devices on the local network
       homewizard_exporter capture <host>    save device responses as test fixtures

Flags:
`, buildinfo.Get())
	flag.PrintDefaults()
	fmt.Fprintf(out, "\nEnvironment variables (override the config file):\n")
	for _, name := range config.EnvVarNames() {
		fmt.Fprintf(out, "  %s\n", name)
	}
}
