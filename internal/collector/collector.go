package collector

import (
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/showwin/speedtest-go/speedtest"
)

const namespace = "speedtest"

type Config struct {
	ServerID int `env:"SERVER_ID" default:"0" help:"Speedtest.net server ID (0 = auto-select nearest server)"`
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

// Exporter implements prometheus.Collector for speedtest metrics.
// It owns a single *speedtest.Speedtest client that is reused across scrapes.
// SavingMode limits concurrent connections to 1, which significantly reduces
// peak memory usage during download/upload tests.
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

	successCount, user, server := e.runSpeedTest(ch)

	// Determine status based on successCount:
	// 0       - All tests failed                       -> Metric Value: 0
	// 1 or 2  - Some tests succeeded (partial success) -> Metric Value: -1
	// 3       - All tests succeeded                    -> Metric Value: 1
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

	// Reset the DataManager after each scrape to release RateSequence slices,
	// DataChunk snapshots, and Welford state accumulated during the test.
	e.client.Reset()

	// Request a GC cycle so that the heap freed above is returned to the OS
	// promptly rather than waiting for the next scheduled collection.
	runtime.GC()
}

func (e *Exporter) runSpeedTest(ch chan<- prometheus.Metric) (int, *speedtest.User, *speedtest.Server) {
	successCount := 0
	user, err := e.client.FetchUserInfo()
	if err != nil {
		slog.Error("Failed to fetch user info", "error", err)
		return successCount, nil, nil
	}

	serverList, err := e.client.FetchServers()
	if err != nil {
		slog.Error("Failed to fetch server", "error", err)
		return successCount, nil, nil
	}

	// NOTE: FindServer finds server by serverID in given server list.
	// If the id is not found in the given list, return the server
	// with the lowest latency.
	targets, err := serverList.FindServer([]int{e.serverID})
	if err != nil {
		slog.Error("Failed to find server", "error", err)
		return successCount, nil, nil
	}

	if len(targets) == 0 {
		return successCount, nil, nil
	}

	if e.serverID != 0 && targets[0].ID != fmt.Sprintf("%d", e.serverID) {
		slog.Warn("Requested server not found, using nearest server",
			"requested_id", e.serverID,
			"selected_id", targets[0].ID)
	}

	slog.Debug("Starting speedtest...",
		"server_id", targets[0].ID,
		"server", targets[0].Host,
		"server_distance", targets[0].Distance,
		"server_country", targets[0].Country,
		"user_ip", user.IP,
		"user_isp", user.Isp,
	)

	if pingTest(targets[0], ch) {
		successCount++
	}
	if downloadTest(targets[0], ch) {
		successCount++
	}
	if uploadTest(targets[0], ch) {
		successCount++
	}

	return successCount, user, targets[0]
}

func pingTest(server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.PingTest(nil)
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

func downloadTest(server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.DownloadTest()
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

func uploadTest(server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.UploadTest()
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

// New creates an Exporter with a dedicated speedtest client configured for
// minimal memory usage. SavingMode caps concurrent HTTP connections to 1,
// which avoids the multi-goroutine download/upload surge that is the primary
// driver of peak RSS. The client is reused across scrapes to avoid repeated
// allocation of the HTTP transport and DataManager.
func New(cfg Config) *Exporter {
	client := speedtest.New(speedtest.WithUserConfig(&speedtest.UserConfig{
		// SavingMode sets MaxConnections=1, so download and upload each use
		// a single goroutine instead of NumCPU goroutines. This is the
		// biggest lever for reducing peak memory during tests.
		SavingMode: true,
	}))
	return &Exporter{
		serverID: cfg.ServerID,
		client:   client,
	}
}
