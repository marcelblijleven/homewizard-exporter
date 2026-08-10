# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build

WORKDIR /src

# Dependencies first, so source edits do not invalidate the module cache.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG TARGETOS
ARG TARGETARCH

# CGO off and a static binary, so the result runs on distroless/static.
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
  go build -trimpath -ldflags "-s -w \
  -X github.com/marcelblijleven/homewizard_exporter/internal/buildinfo.Version=${VERSION} \
  -X github.com/marcelblijleven/homewizard_exporter/internal/buildinfo.Commit=${COMMIT}" \
  -o /out/homewizard_exporter ./cmd/homewizard_exporter

# An empty token directory, owned by the uid the final image runs as. Copying it
# into the image gives the volume mountpoint an owner; without it Docker creates
# the mountpoint root-owned and `pair -o` cannot write as nonroot.
RUN install -d -o 65532 -g 65532 /out/tokens

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source=https://github.com/marcelblijleven/homewizard_exporter
LABEL org.opencontainers.image.description="Prometheus exporter for HomeWizard Energy devices"
LABEL org.opencontainers.image.licenses=MIT

COPY --from=build /out/homewizard_exporter /usr/local/bin/homewizard_exporter

# v2 API tokens live here. Mount a volume to keep them across restarts.
COPY --from=build --chown=65532:65532 /out/tokens /var/lib/homewizard_exporter
VOLUME /var/lib/homewizard_exporter

USER nonroot:nonroot
EXPOSE 9833

ENTRYPOINT ["/usr/local/bin/homewizard_exporter"]
