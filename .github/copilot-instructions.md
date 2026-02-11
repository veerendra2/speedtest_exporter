# Speedtest Exporter - Copilot Instructions

## Project Overview

A Prometheus exporter for monitoring internet speed using speedtest.net. Built with Go, exposes metrics via HTTP `/metrics` endpoint for Prometheus scraping.

## Build, Test, and Lint Commands

This project uses [Taskfile](https://taskfile.dev/) for task automation:

```bash
# Build for current platform
task build

# Run all checks (format, lint, security, test)
task all

# Individual commands
task fmt                 # Format Go code
task lint                # Run golangci-lint (timeout: 3m)
task vet                 # Run go vet
task security            # Run govulncheck
task test                # Run all tests (currently no tests exist)
task run                 # Run locally with debug logging

# Multi-platform builds
task build-platforms     # Build for linux/darwin on amd64/arm64
goreleaser release --snapshot --clean  # Local release build
```

**Note**: No tests currently exist in the codebase (`**/*_test.go` returns empty).

## Architecture

### Core Components

**Main Application (`main.go`)**
- HTTP server with `/metrics` endpoint (promhttp.Handler)
- Graceful shutdown on SIGINT/SIGTERM with 10s timeout
- Uses Kong for CLI parsing with environment variable support
- Configures structured logging via `slogger.Config` from veerendra2/gopackages
- Server timeouts: ReadHeaderTimeout 10s, Read/Write 3m, Idle 2m

**Collector (`internal/collector/collector.go`)**
- Implements `prometheus.Collector` interface
- Each scrape runs three sequential tests: ping → download → upload
- Status metric carries all metadata as labels (user location, server info, distance)
- Status values: `1` = success, `-1` = partial success, `0` = failure
- Uses `showwin/speedtest-go` library for speedtest operations
- Server selection: If requested `SERVER_ID` not found, falls back to nearest server

### Metric Definitions

All metrics use namespace `speedtest`:

```
speedtest_status                    # Gauge with 10 labels (user/server metadata)
speedtest_scrape_duration_seconds   # Total test duration
speedtest_latency_seconds           # Ping latency
speedtest_download_speed_bps        # Download speed (bytes/s * 8)
speedtest_upload_speed_bps          # Upload speed (bytes/s * 8)
```

### Configuration

Configuration uses embedded structs with Kong tags:
- `cli.Address` - Server listen address (default: `:8080`)
- `cli.Collector` - Embeds `collector.Config` with `SERVER_ID` field
- `cli.Log` - Embeds `slogger.Config` with `log-` prefix for flags
- All flags support environment variables (e.g., `ADDRESS`, `SERVER_ID`, `LOG_LEVEL`)

## Key Conventions

### Version Information
- Version injected via ldflags at build time into `github.com/veerendra2/gopackages/version` package
- Variables: `Version`, `Revision`, `Branch`, `BuildUser`, `BuildDate`
- Logged at startup via `version.Info()` and `version.BuildContext()`

### Logging
- Uses structured logging (`log/slog`) throughout
- Log levels: DEBUG, INFO, WARN, ERROR
- Supports console/JSON formats via `LOG_FORMAT` env var
- Debug logs include speedtest progress and results

### Error Handling in Tests
- Each test (ping/download/upload) returns boolean success status
- Errors logged but don't abort the scrape
- `successCount` determines overall status metric value
- If all upstream API calls fail (user info, server list), returns early with status=0

### Prometheus Best Practices
- Collector pattern: implements `Describe()` and `Collect()`
- Metrics are const (created per-scrape, not stored)
- Label values default to "N/A" on failure
- `MustNewConstMetric` panics on invalid metrics (fail-fast)

### Docker & Deployment
- Uses distroless base image (gcr.io/distroless/static:nonroot)
- Built with `CGO_ENABLED=0` for static binaries
- Supports linux/darwin on amd64/arm64
- Recommended Prometheus scrape: interval 1h, timeout 2m

### Development Environment
- Go 1.25.5
- Dev stack available via `compose-dev.yml`
- Tools auto-installed via `task install`: golangci-lint, govulncheck
