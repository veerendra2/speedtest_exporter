package collector

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/showwin/speedtest-go/speedtest"
)

const (
	namespace   = "speedtest"
	testTimeout = 2 * time.Minute
)

type Config struct {
	ServerID       int `env:"SERVER_ID"       default:"0" help:"Speedtest.net server ID (0 = auto-select nearest server)"`
	MaxConnections int `env:"MAX_CONNECTIONS" default:"4" help:"Number of parallel TCP streams for bandwidth test (1 = low data usage, 4–8 = accurate)"`
}

var (
	status = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "status"),
		"Whether the speedtest was successful (-1 = partial success, 0 = failure, 1 = success).",
		[]string{
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
	scrapeDurationSeconds = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"),
		"Total time taken to complete the speedtest.",
		[]string{}, nil,
	)
	latency = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "latency_seconds"),
		"Network latency to the speedtest server in seconds.",
		[]string{}, nil,
	)
	upload = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "upload_speed_bps"),
		"Upload speed in bits per second.",
		[]string{}, nil,
	)
	download = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "download_speed_bps"),
		"Download speed in bits per second.",
		[]string{}, nil,
	)
)

type Exporter struct {
	serverID int
	client   *speedtest.Speedtest
}

// Describe describes all the metrics. It implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- status
	ch <- scrapeDurationSeconds
	ch <- latency
	ch <- upload
	ch <- download
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	var statusValue float64

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	successCount, user, server := e.runSpeedTest(ctx, ch)

	// Determine status based on successCount:
	// 0     	 - All tests failed                    	  -> Metric Value: 0
	// 1 or 2  - Some tests succeeded (partial success) -> Metric Value: -1
	// 3     	 - All tests succeeded                 	  -> Metric Value: 1
	switch successCount {
	case 3:
		statusValue = 1.0
	case 0:
		statusValue = 0.0
	default:
		statusValue = -1.0
	}

	labels := []string{
		"N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A", "N/A",
	}
	if statusValue != 0.0 {
		labels = []string{
			user.Lat, user.Lon, user.IP, user.Isp,
			server.Lat, server.Lon, server.ID, server.Host, server.Country,
			fmt.Sprintf("%f", server.Distance),
		}
	}

	ch <- prometheus.MustNewConstMetric(status, prometheus.GaugeValue, statusValue, labels...)
	ch <- prometheus.MustNewConstMetric(scrapeDurationSeconds, prometheus.GaugeValue, time.Since(start).Seconds())
}

func (e *Exporter) runSpeedTest(ctx context.Context, ch chan<- prometheus.Metric) (int, *speedtest.User, *speedtest.Server) {
	successCount := 0

	user, err := e.client.FetchUserInfo()
	if err != nil {
		slog.Error("Failed to fetch user info", "error", err)
		return successCount, nil, nil
	}

	// When a specific server is configured, fetch it directly — avoids fetching and
	// pinging the full server list. Falls back to nearest on error.
	var server *speedtest.Server
	if e.serverID != 0 {
		server, err = e.client.FetchServerByID(fmt.Sprintf("%d", e.serverID))
		if err != nil {
			slog.Warn("Requested server not found, falling back to nearest", "server_id", e.serverID, "error", err)
		}
	}
	if server == nil {
		serverList, err := e.client.FetchServers()
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
		"server_id", server.ID,
		"server", server.Host,
		"server_distance", server.Distance,
		"server_country", server.Country,
		"user_ip", user.IP,
		"user_isp", user.Isp,
	)

	if pingTest(ctx, server, ch) {
		successCount++
	}
	if downloadTest(ctx, server, ch) {
		successCount++
	}
	if uploadTest(ctx, server, ch) {
		successCount++
	}

	// Reset clears DataChunks/RateSequence and archives the snapshot to a
	// 10-entry ring buffer. Clean() immediately drops that archive since the
	// exporter never reads historical snapshots, keeping memory truly flat.
	server.Context.Reset()
	server.Context.Snapshots().Clean()

	return successCount, user, server
}

func pingTest(ctx context.Context, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.PingTestContext(ctx, nil)
	if err != nil {
		slog.Error("Failed to run ping test", "error", err)
		return false
	}

	slog.Debug("Latency test completed",
		"latency_seconds", server.Latency.Seconds(),
	)

	ch <- prometheus.MustNewConstMetric(
		latency, prometheus.GaugeValue, server.Latency.Seconds(),
	)

	return true
}

func downloadTest(ctx context.Context, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.DownloadTestContext(ctx)
	if err != nil {
		slog.Error("Failed to run download test", "error", err)
		return false
	}

	slog.Debug("Download test completed",
		"download_speed", server.DLSpeed,
	)

	ch <- prometheus.MustNewConstMetric(
		download, prometheus.GaugeValue, float64(server.DLSpeed)*8,
	)

	return true
}

func uploadTest(ctx context.Context, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.UploadTestContext(ctx)
	if err != nil {
		slog.Error("Failed to run upload test", "error", err)
		return false
	}
	slog.Debug("Upload test completed",
		"upload_speed", server.ULSpeed,
	)

	ch <- prometheus.MustNewConstMetric(
		upload, prometheus.GaugeValue, float64(server.ULSpeed)*8,
	)

	return true
}

func NewMetricsHandler(exporter *Exporter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverIDParam := r.URL.Query().Get("server_id")
		if serverIDParam != "" {
			if id, err := strconv.Atoi(serverIDParam); err == nil {
				originalServerID := exporter.serverID
				exporter.serverID = id
				defer func() { exporter.serverID = originalServerID }()
			} else {
				slog.Warn("Invalid server_id parameter", "value", serverIDParam, "error", err)
			}
		}
		promhttp.Handler().ServeHTTP(w, r)
	})
}

func New(cfg Config) *Exporter {
	return &Exporter{
		serverID: cfg.ServerID,
		client: speedtest.New(speedtest.WithUserConfig(&speedtest.UserConfig{
			MaxConnections: cfg.MaxConnections,
			PingMode:       speedtest.TCP,
		})),
	}
}
