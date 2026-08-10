BINARY  := homewizard_exporter
PKG     := github.com/marcelblijleven/homewizard_exporter

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Stamped into the binary so `homewizard_exporter -version` and
# homewizard_build_info report something more useful than "dev".
LDFLAGS := -s -w \
	-X $(PKG)/internal/buildinfo.Version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(PKG)/internal/buildinfo.Date=$(DATE)

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the binary into ./homewizard_exporter
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/homewizard_exporter

.PHONY: install
install: ## Install homewizard_exporter into GOBIN
	go install -ldflags '$(LDFLAGS)' ./cmd/homewizard_exporter

.PHONY: test
test: ## Run all tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run tests and open a coverage report
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: lint
lint: ## Vet, check formatting and run golangci-lint if installed
	go vet ./...
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	@if command -v golangci-lint >/dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

.PHONY: check
check: lint test ## Everything CI runs

.PHONY: run
run: config.yaml ## Run against real devices
	go run ./cmd/homewizard_exporter -config config.yaml

config.yaml: ## Create a local config from the example
	@cp config.example.yaml $@
	@echo "created $@ from the example -- list your devices before running"

.PHONY: fake
fake: ## Run against the captured fixtures, with the dashboard on :9833
	@mkdir -p dist
	@go build -o dist/fakedevice ./cmd/fakedevice
	@go build -ldflags '$(LDFLAGS)' -o dist/$(BINARY) ./cmd/homewizard_exporter
	@./dist/fakedevice > dist/fake-devices.yaml 2> dist/fakedevice.log & \
	FAKE=$$!; \
	trap 'kill $$FAKE 2>/dev/null; wait $$FAKE 2>/dev/null' EXIT INT TERM HUP; \
	sleep 1; \
	{ cat dist/fake-devices.yaml; echo "dashboard:"; echo "  enabled: true"; } > dist/fake.yaml; \
	sed -n 's/^/  /p' dist/fakedevice.log; \
	echo "  dashboard: http://localhost:9833/"; \
	./dist/$(BINARY) -config dist/fake.yaml

.PHONY: capture
capture: ## Refresh the test fixtures from a real device (make capture HOST=192.168.1.10)
	go run ./cmd/homewizard_exporter capture $(HOST)

.PHONY: discover
discover: ## Find HomeWizard devices on the local network
	go run ./cmd/homewizard_exporter discover

.PHONY: docker
docker: ## Build the container image
	docker build -t $(BINARY):$(VERSION) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) .

.PHONY: clean
clean:
	rm -f $(BINARY) coverage.out
	rm -rf dist/

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
