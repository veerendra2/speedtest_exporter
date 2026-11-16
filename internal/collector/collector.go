package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/showwin/speedtest-go/speedtest"
)

const (
	namespace = "speedtest"

	// ProbeTimeout bounds one probe: the user/server lookup plus the three tests.
	ProbeTimeout = 2 * time.Minute

	// defaultTarget is the target label used when the scrape sends none.
	defaultTarget = "speedtest"

	// probeSlotWait bounds the wait for a free slot. It has to end well inside
	// ProbeTimeout, or the failure lands after the scraper has already given up.
	probeSlotWait = 30 * time.Second
)

// probeSlot serialises probes. Two speedtests at once steal bandwidth from each
// other and both report wrong speeds.
var probeSlot = make(chan struct{}, 1)

// withProbeSlot runs fn while holding the single probe slot, so only one
// speedtest runs at a time. It reports false if the slot did not free up within
// probeSlotWait, or if ctx ended first.
func withProbeSlot(ctx context.Context, fn func()) bool {
	wait, cancel := context.WithTimeout(ctx, probeSlotWait)
	defer cancel()

	select {
	case probeSlot <- struct{}{}:
		defer func() { <-probeSlot }()
		fn()
		return true
	case <-wait.Done():
		return false
	}
}

type Config struct {
	MaxConnections int `env:"MAX_CONNECTIONS" default:"4" help:"Number of parallel TCP streams for bandwidth test (1 = low data usage, 4–8 = accurate)"`
}

var (
	status = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "status"),
		"Whether the speedtest was successful (-1 = partial success, 0 = failure, 1 = success).",
		[]string{
			"target",
			"user_lat",
			"user_lon",
			"user_ip",
			"user_isp",
			"server_lat",
			"server_lon",
			"server_id",
			"server_name",
			"server_country",
			"distance",
		}, nil,
	)
	probeDurationSeconds = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "probe_duration_seconds"),
		"Total time taken to complete the speedtest.",
		[]string{"target"}, nil,
	)
	latency = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "latency_seconds"),
		"Network latency to the speedtest server in seconds.",
		[]string{"target"}, nil,
	)
	upload = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "upload_speed_bps"),
		"Upload speed in bits per second.",
		[]string{"target"}, nil,
	)
	download = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "download_speed_bps"),
		"Download speed in bits per second.",
		[]string{"target"}, nil,
	)
)

// probe is one Prometheus scrape: a single speedtest against one server,
// collected into a registry that lives only for that request.
type probe struct {
	// ctx is the request context, so a caller that goes away stops the test.
	ctx      context.Context
	target   string
	serverID int
	client   *speedtest.Speedtest
}

// Describe implements prometheus.Collector.
func (p *probe) Describe(ch chan<- *prometheus.Desc) {
	ch <- status
	ch <- probeDurationSeconds
	ch <- latency
	ch <- upload
	ch <- download
}

// statusValue maps the number of successful sub-tests (ping, download, upload)
// to a Prometheus status metric value.
func statusValue(successCount int) float64 {
	switch successCount {
	case 3:
		return 1.0
	case 0:
		return 0.0
	default:
		return -1.0
	}
}

// Collect implements prometheus.Collector.
func (p *probe) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(p.ctx, ProbeTimeout)
	defer cancel()

	successCount := 0
	var user *speedtest.User
	var server *speedtest.Server

	ran := withProbeSlot(ctx, func() {
		successCount, user, server = p.run(ctx, ch)
	})
	if !ran {
		slog.Warn("Another speedtest is still running, reporting this probe as failed", "target", p.target)
	}

	statVal := statusValue(successCount)

	labels := []string{
		p.target,
		"N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A",
	}
	if statVal != 0.0 {
		labels = []string{
			p.target,
			user.Lat, user.Lon, user.IP, user.Isp,
			server.Lat, server.Lon, server.ID, server.Host, server.Country,
			fmt.Sprintf("%f", server.Distance),
		}
	}

	ch <- prometheus.MustNewConstMetric(status, prometheus.GaugeValue, statVal, labels...)
	ch <- prometheus.MustNewConstMetric(probeDurationSeconds, prometheus.GaugeValue, time.Since(start).Seconds(), p.target)
}

func (p *probe) run(ctx context.Context, ch chan<- prometheus.Metric) (int, *speedtest.User, *speedtest.Server) {
	successCount := 0

	user, err := p.client.FetchUserInfoContext(ctx)
	if err != nil {
		slog.Error("Failed to fetch user info", "error", err)
		return successCount, nil, nil
	}

	// When a specific server is configured, fetch it directly — avoids fetching and
	// pinging the full server list. Falls back to nearest on error.
	var server *speedtest.Server
	if p.serverID != 0 {
		server, err = p.client.FetchServerByIDContext(ctx, strconv.Itoa(p.serverID))
		if err != nil {
			slog.Warn("Requested server not found, falling back to nearest", "server_id", p.serverID, "error", err)
		}
	}
	if server == nil {
		serverList, err := p.client.FetchServerListContext(ctx)
		if err != nil {
			slog.Error("Failed to fetch servers", "error", err)
			return successCount, nil, nil
		}
		targets, err := serverList.FindServer([]int{})
		if err != nil {
			slog.Error("Failed to find server", "error", err)
			return successCount, nil, nil
		}
		if len(targets) == 0 {
			return successCount, nil, nil
		}
		server = targets[0]
	}

	slog.Debug("Starting speedtest...",
		"target", p.target,
		"server_id", server.ID,
		"server", server.Host,
		"server_distance", server.Distance,
		"server_country", server.Country,
		"user_ip", user.IP,
		"user_isp", user.Isp,
	)

	if pingTest(ctx, server, p.target, ch) {
		successCount++
	}
	if downloadTest(ctx, server, p.target, ch) {
		successCount++
	}
	if uploadTest(ctx, server, p.target, ch) {
		successCount++
	}

	// Reset clears DataChunks/RateSequence and archives the snapshot to a
	// 10-entry ring buffer. Clean() immediately drops that archive since the
	// exporter never reads historical snapshots, keeping memory truly flat.
	server.Context.Reset()
	server.Context.Snapshots().Clean()

	return successCount, user, server
}

func pingTest(ctx context.Context, server *speedtest.Server, target string, ch chan<- prometheus.Metric) bool {
	err := server.PingTestContext(ctx, nil)
	if err != nil {
		slog.Error("Failed to run ping test", "error", err)
		return false
	}

	slog.Debug("Latency test completed",
		"latency_seconds", server.Latency.Seconds(),
	)

	ch <- prometheus.MustNewConstMetric(
		latency, prometheus.GaugeValue, server.Latency.Seconds(), target,
	)

	return true
}

func downloadTest(ctx context.Context, server *speedtest.Server, target string, ch chan<- prometheus.Metric) bool {
	err := server.DownloadTestContext(ctx)
	if err != nil {
		slog.Error("Failed to run download test", "error", err)
		return false
	}
	if server.DLSpeed < 0 {
		slog.Error("Download test returned invalid speed", "download_speed", server.DLSpeed)
		return false
	}

	slog.Debug("Download test completed",
		"download_speed", server.DLSpeed,
	)

	ch <- prometheus.MustNewConstMetric(
		download, prometheus.GaugeValue, float64(server.DLSpeed)*8, target,
	)

	return true
}

func uploadTest(ctx context.Context, server *speedtest.Server, target string, ch chan<- prometheus.Metric) bool {
	err := server.UploadTestContext(ctx)
	if err != nil {
		slog.Error("Failed to run upload test", "error", err)
		return false
	}
	if server.ULSpeed < 0 {
		slog.Error("Upload test returned invalid speed", "upload_speed", server.ULSpeed)
		return false
	}

	slog.Debug("Upload test completed",
		"upload_speed", server.ULSpeed,
	)

	ch <- prometheus.MustNewConstMetric(
		upload, prometheus.GaugeValue, float64(server.ULSpeed)*8, target,
	)

	return true
}

// probeParams reads the per-scrape target name and server ID from the query
// string. server_id 0 means the SDK picks the nearest server.
func probeParams(r *http.Request) (string, int, error) {
	query := r.URL.Query()

	target := query.Get("target")
	if target == "" {
		target = defaultTarget
	}
	// A label value that is not valid UTF-8 panics the metric constructor, and
	// only after a full speedtest has run.
	if !utf8.ValidString(target) {
		return "", 0, fmt.Errorf("invalid target %q: must be valid UTF-8", target)
	}

	serverID := 0
	if param := query.Get("server_id"); param != "" {
		id, err := strconv.Atoi(param)
		if err != nil {
			return "", 0, fmt.Errorf("invalid server_id %q: must be a number", param)
		}
		serverID = id
	}

	return target, serverID, nil
}

// Handler returns the multi-target /probe endpoint. Each request runs one
// speedtest and serves the result from its own registry, so no probe can see
// or change another's state.
func Handler(cfg Config) http.Handler {
	client := speedtest.New(speedtest.WithUserConfig(&speedtest.UserConfig{
		MaxConnections: cfg.MaxConnections,
		PingMode:       speedtest.TCP,
	}))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, serverID, err := probeParams(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		registry := prometheus.NewRegistry()
		registry.MustRegister(&probe{ctx: r.Context(), target: target, serverID: serverID, client: client})
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	})
}
