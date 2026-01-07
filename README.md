# Speedtest Exporter

A Prometheus exporter for monitoring internet speed with [speedtest.net](https://www.speedtest.net/).

> **Note:** Inspired by [danopstech/speedtest_exporter](https://github.com/danopstech/speedtest_exporter) which is no longer maintained.

## Features

- Measures download/upload speeds and latency
- Exports metrics in Prometheus format
- Supports specific speedtest.net server selection (uses [Speedtest.net API](https://www.speedtest.net/api/js/servers) via [speedtest-go](https://github.com/showwin/speedtest-go/blob/master/speedtest/server.go#L22))
- Auto-selects nearest server when not specified or if requested server is unavailable
- Includes geographical metadata (user/server location)

## Quick Start

```bash
./speedtest_exporter -h
Usage: speedtest_exporter [flags]

A Prometheus exporter for monitoring internet speed with speedtest.net

Flags:
  -h, --help                 Show context-sensitive help.
      --address=":8080"      The address where the server should listen on ($ADDRESS).
      --server-id=0          Speedtest.net server ID (0 = auto-select nearest server) ($SERVER_ID)
      --log-format="json"    Set the output format of the logs. Must be "console" or "json" ($LOG_FORMAT).
      --log-level=INFO       Set the log level. Must be "DEBUG", "INFO", "WARN" or "ERROR" ($LOG_LEVEL).
      --log-add-source       Whether to add source file and line number to log records ($LOG_ADD_SOURCE).
```

### Using Docker

```bash
docker run -p 8080:8080 ghcr.io/veerendra2/speedtest_exporter:latest
```

Docker Compose

```yaml
---
services:
  speedtest_exporter:
    image: ghcr.io/veerendra2/speedtest_exporter:latest
    hostname: speedtest_exporter
    container_name: speedtest_exporter
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      LOG_ADD_SOURCE: false
      LOG_FORMAT: console
```

### Using Binary

Download latest binary from [release page](https://github.com/veerendra2/speedtest_exporter/releases)

## Metrics

| Metric                              | Type  | Description                                                                                 |
| ----------------------------------- | ----- | ------------------------------------------------------------------------------------------- |
| `speedtest_status`                  | Gauge | Whether the last speedtest was successful (-1 = partiallysuccess, 0 = failure, 1 = success) |
| `speedtest_scrape_duration_seconds` | Gauge | Total time taken to complete the speedtest                                                  |
| `speedtest_latency_seconds`         | Gauge | Network latency to the speedtest server                                                     |
| `speedtest_download_speed_Bps`      | Gauge | Download speed in bytes per second                                                          |
| `speedtest_upload_speed_Bps`        | Gauge | Upload speed in bytes per second                                                            |

## Prometheus Configuration

```yaml
scrape_configs:
  - job_name: "speedtest"
    scrape_interval: 1h
    scrape_timeout: 2m
    static_configs:
      - targets: ["localhost:8080"]
```

## Build & Test

- Using [Taskfile](https://taskfile.dev/)

_Install Taskfile: [Installation Guide](https://taskfile.dev/docs/installation)_

```bash
# Available tasks
task --list
task: Available tasks for this project:
* all:                   Run comprehensive checks: format, lint, security and test
* build:                 Build the application binary for the current platform
* build-docker:          Build Docker image
* build-platforms:       Build the application binaries for multiple platforms and architectures
* fmt:                   Formats all Go source files
* install:               Install required tools and dependencies
* lint:                  Run static analysis and code linting using golangci-lint
* run:                   Runs the main application
* security:              Run security vulnerability scan
* test:                  Runs all tests in the project      (aliases: tests)
* vet:                   Examines Go source code and reports suspicious constructs
```

- Build with [goreleaser](https://goreleaser.com/)

_Install GoReleaser: [Installation Guide](https://goreleaser.com/install/)_

```bash
# Build locally
goreleaser release --snapshot --clean
...
```
