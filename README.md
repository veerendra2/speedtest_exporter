# Speedtest Exporter

A Prometheus exporter for monitoring internet speed with [speedtest.net](https://www.speedtest.net/).

> _Inspired by [danopstech/speedtest_exporter](https://github.com/danopstech/speedtest_exporter) which is no longer actively maintained._

## Features

- Measures download/upload speeds and latency
- Exports metrics in Prometheus format
- Supports specific speedtest.net server selection (uses [speedtest.net API](https://www.speedtest.net/api/js/servers) via [speedtest-go](https://github.com/showwin/speedtest-go.git))
- Auto-selects nearest server when not specified or if requested server is unavailable
- Includes geographical metadata (user/server location)

## Quick Start

```bash
Usage: speedtest_exporter [flags]

A Prometheus exporter for monitoring internet speed with speedtest.net

Flags:
  -h, --help                    Show context-sensitive help.
      --address=":8080"         The address where the server should listen on ($ADDRESS).
      --server-id=0             Speedtest.net server ID (0 = auto-select nearest server) ($SERVER_ID)
      --log-format="console"    Set the output format of the logs. Must be "console" or "json" ($LOG_FORMAT).
      --log-level=INFO          Set the log level. Must be "DEBUG", "INFO", "WARN" or "ERROR" ($LOG_LEVEL).
      --log-add-source          Whether to add source file and line number to log records ($LOG_ADD_SOURCE).
      --version                 Print version information and exit
```

### Using Docker

```bash
docker run -d -p 8080:8080 ghcr.io/veerendra2/speedtest_exporter:latest
```

Docker Compose

```yaml
---
name: speedtest_exporter
services:
  speedtest_exporter:
    image: ghcr.io/veerendra2/speedtest_exporter:latest
    container_name: speedtest_exporter
    ports:
      - "8080:8080"
    restart: unless-stopped
    hostname: speedtest_exporter
```

## Metrics

```
# HELP speedtest_download_speed_Bps Download speed in bytes per second.
# TYPE speedtest_download_speed_Bps gauge
speedtest_download_speed_Bps{test_uuid="b37d2ad4-e382-452e-ba2f-53e6480dad39"} 3.060982156305362e+06
# HELP speedtest_latency_seconds Network latency to the speedtest server in seconds.
# TYPE speedtest_latency_seconds gauge
speedtest_latency_seconds{test_uuid="b37d2ad4-e382-452e-ba2f-53e6480dad39"} 0.005002029
# HELP speedtest_scrape_duration_seconds Total time taken to complete the speedtest.
# TYPE speedtest_scrape_duration_seconds gauge
speedtest_scrape_duration_seconds{test_uuid="b37d2ad4-e382-452e-ba2f-53e6480dad39"} 30.330574389
# HELP speedtest_status Whether the speedtest was successful (-1 = partial success, 0 = failure, 1 = success).
# TYPE speedtest_status gauge
speedtest_status{distance="4.885019",server_country="Germany",server_id="2495",server_lat="[REDACTED]",server_lon="[REDACTED]",server_name="[REDACTED]",test_uuid="b37d2ad4-e382-452e-ba2f-53e6480dad39",user_ip="[REDACTED]",user_isp="[REDACTED]",user_lat="[REDACTED]",user_lon="[REDACTED]"} 1
# HELP speedtest_upload_speed_Bps Upload speed in bytes per second.
# TYPE speedtest_upload_speed_Bps gauge
speedtest_upload_speed_Bps{test_uuid="b37d2ad4-e382-452e-ba2f-53e6480dad39"} 1.2263623242466215e+07
```

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
