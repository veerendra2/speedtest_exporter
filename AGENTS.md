# AGENTS.md — Speedtest Exporter

Guidance for agentic coding agents operating in this repository.

---

## Project Overview

A Prometheus exporter for internet speed monitoring using speedtest.net. Written in Go, it follows the [multi-target exporter pattern](https://prometheus.io/docs/guides/multi-target-exporter/): `/probe?target=<name>` runs ping, download, and upload tests and returns the metrics for that one probe; `/metrics` serves only the exporter's own Go/process metrics.

---

## Build, Lint, and Test Commands

This project uses [Taskfile](https://taskfile.dev/). Install it with `go install github.com/go-task/task/v3/cmd/task@latest` if it is not available.

```bash
# Install required tools (golangci-lint, govulncheck) into $GOPATH/bin
task install

# Run all checks in order: fmt → lint → vet → security → test
task all

# Individual tasks
task fmt          # go fmt ./...
task vet          # go vet ./...
task lint         # golangci-lint run --timeout 3m
task security     # govulncheck ./...
task test         # go vet ./... && go test ./...

# Build
task build        # Build for host platform, output: dist/speedtest_exporter
task build-platforms  # Cross-compile linux/darwin × amd64/arm64

# Run locally with debug logging
task run
```

### Running a Single Test

Tests live next to the code in `internal/collector/collector_test.go`. Run a single test with:

```bash
# Run one test function in a specific package
go test ./internal/collector/ -run TestFunctionName -v

# Run all tests in one package
go test ./internal/collector/ -v

# Run with coverage
go test ./... -cover
```

Test files must end in `_test.go` and live alongside the package they test (e.g., `internal/collector/collector_test.go`).

### CI

- Pull requests run `golangci-lint` via GitHub Actions (`.github/workflows/ci.yml`).
- Releases are triggered by `v*.*.*` tags and use GoReleaser (`.github/workflows/release.yml`).
- Always run `task all` before pushing a branch.

---

## Code Style Guidelines

### Language and Toolchain

- **Go** — `go.mod` specifies `go 1.27.1`; Docker dev image uses `golang:1.27.3`.
- `CGO_ENABLED=0` is set globally (static binaries only).
- No linter config file exists; `golangci-lint` runs with its defaults.

### Import Organization

Use two groups separated by a blank line: stdlib first, then third-party. No internal group separator is needed given the project size.

```go
import (
    "context"
    "log/slog"
    "net/http"

    "github.com/prometheus/client_golang/prometheus"
    "github.com/showwin/speedtest-go/speedtest"
    "github.com/veerendra2/speedtest_exporter/internal/collector"
)
```

### Naming Conventions

| Construct | Style | Example |
|---|---|---|
| Packages | lowercase, single word | `collector`, `main` |
| Exported types/functions | PascalCase | `Exporter`, `Collect`, `New` |
| Unexported functions | camelCase | `runSpeedTest`, `pingTest` |
| Unexported variables/consts | camelCase | `namespace`, `statusValue` |
| Parameters | camelCase | `serverID`, `ch` |
| File names | snake_case | `collector.go`, `main.go` |
| Acronyms in identifiers | all-caps | `serverID` (not `serverId`) |

### Formatting

Run `task fmt` (`go fmt ./...`) before committing. No additional formatter (e.g., `goimports`) is configured, but import order must be consistent with the two-group convention above.

### Types

- Prefer explicit `float64` casts when assigning from library types (e.g., `speedtest.DLSpeed`).
- Package-level Prometheus `*prometheus.Desc` vars are declared in a `var ( ... )` block and initialized once at startup.
- Use anonymous struct literals for CLI config: `var cli struct { ... }` at package level in `main`.
- Kong struct tags follow the pattern: `env:"VAR_NAME" default:"value" help:"description"`.
- Embed sub-configs with `embed:""` and prefix them with `prefix:"flag-prefix-"` when needed.

### Error Handling

- Check every error immediately; do not assign and defer the check.
- Log errors with `slog.Error("message", "error", err)` and return early.
- Do **not** propagate errors up the stack inside the collector — log and continue.
- Use `successCount int` to track partial success across multiple sub-operations.
- Individual test helpers (`pingTest`, `downloadTest`, `uploadTest`) return `bool`; they log errors themselves.
- `prometheus.MustNewConstMetric` is intentional — panic on invalid metric construction (fail-fast).
- In `main`, fatal startup errors use `log.Fatalf` (stdlib `log`, not `slog`).

```go
// Correct pattern
result, err := someCall()
if err != nil {
    slog.Error("Failed to do X", "error", err)
    return 0, nil, nil
}
```

### Logging

- Use `log/slog` for all structured logging throughout the codebase.
- Log levels: `DEBUG`, `INFO`, `WARN`, `ERROR`.
- When logging an `error` value, always pass it as a structured key: `"error", err`.
- Debug logs should include progress and intermediate results (speeds, latency).
- Logger is configured at startup via `slogger.Config`; log format and level are controlled by `LOG_FORMAT` and `LOG_LEVEL` env vars.

### Comments

- Exported identifiers get a doc comment starting with the identifier name: `// Describe implements prometheus.Collector.`
- Use `NOTE:` prefix for important behavioral clarifications.
- Inline comments use `//` with a space and are written as full sentences.
- ASCII-art tables in comments are acceptable for explaining label/value semantics (see `collector.go`).

### Prometheus Collector Pattern

- Implement `Describe(ch chan<- *prometheus.Desc)` and `Collect(ch chan<- prometheus.Metric)`.
- Pass `chan<- prometheus.Metric` directly into helper functions rather than returning slices.
- Default label values to `"N/A"` when metadata is unavailable.
- All metrics share the namespace `speedtest`.
- Status metric encodes all run metadata as labels; value encodes overall result (`1` success, `-1` partial, `0` failure).

### speedtest-go SDK Usage Rules

- Always create a `*speedtest.Speedtest` client via `speedtest.New(...)` with `MaxConnections` and `PingMode: speedtest.TCP`; store it in `Exporter`. Never call package-level functions (`speedtest.FetchServers`, `speedtest.FetchUserInfo`) as they share a global client.
- When the probe asks for a specific `server_id`, use `client.FetchServerByIDContext(ctx, id)` to avoid fetching and pinging the entire server list. Fall back to `client.FetchServerListContext(ctx)` only when needed.
- Always use the `*Context` variants (`FetchUserInfoContext`, `FetchServerByIDContext`, `FetchServerListContext`). The non-context variants use `context.Background()` with `http.DefaultClient`, which has no timeout, so a stalled API hangs the probe forever.
- After each test round, always call **both**:
  ```go
  server.Context.Reset()            // clears DataChunks + RateSequence
  server.Context.Snapshots().Clean() // drops archived snapshots (ring buffer)
  ```
  Omitting either causes memory growth across scrapes.

---

## Architecture Reference

### Core Components

- **`main.go`** — HTTP server (`/probe`, `/metrics`), graceful shutdown (SIGINT/SIGTERM, `ProbeTimeout + 10 s` so an in-flight probe finishes), Kong CLI parsing, slogger setup, version logging at startup.
- **`internal/collector/collector.go`** — Prometheus collector; runs ping → download → upload on each scrape; handles server selection with fallback to nearest server. Key invariants:
  - `Exporter` holds a dedicated `*speedtest.Speedtest` client (never use package-level `speedtest.Fetch*` functions).
  - `server.Context.Reset()` followed by `server.Context.Snapshots().Clean()` **must** be called after every test run to prevent unbounded memory growth. NOTE: `server.Context` is the shared `*speedtest.Speedtest` client, not per-server state — this is why only one probe may run at a time.
  - Probes are serialised by `probeSlot` (a one-slot channel). Two speedtests at once steal bandwidth from each other and both report wrong speeds.
  - Each request gets its own `prometheus.Registry` and its own `probe` value. Never put per-scrape state on a shared struct; that was a data race and a way to pin the wrong server ID permanently.
  - All three test helpers (`pingTest`, `downloadTest`, `uploadTest`) accept a `context.Context` and call the `*Context` SDK variants — never the plain `PingTest`/`DownloadTest`/`UploadTest`.
  - `testTimeout = 2 * time.Minute` is applied per `Collect()` call to bound scrape duration.

### Metrics (namespace: `speedtest`)

| Metric | Type | Description |
|---|---|---|
| `speedtest_status` | Gauge | Overall result with 10 metadata labels |
| `speedtest_probe_duration_seconds` | Gauge | Total probe wall-clock duration |
| `speedtest_latency_seconds` | Gauge | Ping latency |
| `speedtest_download_speed_bps` | Gauge | Download speed (bytes/s × 8) |
| `speedtest_upload_speed_bps` | Gauge | Upload speed (bytes/s × 8) |

Every metric above carries a `target` label. The status metric also carries the user/server metadata labels and its value encodes the result (`1` success, `-1` partial, `0` failure).

### Configuration (environment variables / flags)

| Env Var | Default | Description |
|---|---|---|
| `ADDRESS` | `:8080` | HTTP listen address |
| `MAX_CONNECTIONS` | `4` | Parallel TCP streams for bandwidth test (1 = low data usage, 4–8 = accurate) |
| `LOG_LEVEL` | `info` | Logging level |
| `LOG_FORMAT` | `console` | Log format (`json` or `console`) |

### Query Parameters

`/probe` accepts two optional query parameters:

| Parameter   | Default          | Meaning                                                    |
| ----------- | ---------------- | ---------------------------------------------------------- |
| `target`    | `speedtest`      | Becomes the `target` label on every metric of this probe.   |
| `server_id` | `0` (nearest)    | speedtest.net server ID for this probe.                     |

```
GET /probe?target=berlin&server_id=1234
```

**Fallback behavior:** If the requested server ID is not found, the exporter automatically selects the nearest server. There is no global `SERVER_ID` setting: server selection is per scrape, which is what lets one exporter measure several servers.

**Invalid input:** A non-numeric `server_id` returns HTTP 400.

All probe metrics carry the `target` label, so one exporter can serve several targets without their series colliding. Prometheus scrape configs should relabel `instance` from `__param_target`, since every probe comes from the same exporter address.

### Version Injection (ldflags)

Build-time version info is injected into `github.com/veerendra2/gopackages/version`:
- `Version`, `Revision`, `Branch`, `BuildUser`, `BuildDate`

### Docker & Deployment

- Production images use `gcr.io/distroless/static:nonroot`; published to `ghcr.io/veerendra2/speedtest_exporter`.
- Dev image uses `alpine:3.23.3` (see `Dockerfile`).
- Dev stack: `task dev` (or `docker compose -f development/compose.yml up --build -d`) starts the exporter + VictoriaMetrics + Grafana with the dashboard provisioned. See `development/README.md`.
- Recommended Prometheus scrape config: `metrics_path: /probe`, `scrape_interval: 1h`, `scrape_timeout: 2m`, `params.target`, and `instance` relabelled from `__param_target`.
