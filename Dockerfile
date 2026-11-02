# syntax=docker/dockerfile:1

# ---- build ----
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev COMMIT=none DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w \
        -X github.com/titiabdelghafour8-lab/certwatch/internal/version.Version=${VERSION} \
        -X github.com/titiabdelghafour8-lab/certwatch/internal/version.Commit=${COMMIT} \
        -X github.com/titiabdelghafour8-lab/certwatch/internal/version.Date=${DATE}" \
      -o /out/certwatch-server ./cmd/certwatch-server

# ---- runtime ----
# distroless/static ships CA certificates and tzdata, runs as non-root and has
# no shell, which keeps the attack surface minimal.
FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/titiabdelghafour8-lab/certwatch" \
      org.opencontainers.image.description="certwatch server: TLS certificate and domain expiry monitoring" \
      org.opencontainers.image.licenses="MIT"

COPY --from=build /out/certwatch-server /usr/local/bin/certwatch-server
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/usr/local/bin/certwatch-server", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/certwatch-server"]
CMD ["all"]
