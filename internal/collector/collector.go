package collector

import (
	"fmt"
	"log/slog"
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

type Exporter struct {
	ServerID int
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

	successCount, user, server := runSpeedTest(e.ServerID, ch)

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

func runSpeedTest(serverId int, ch chan<- prometheus.Metric) (int, *speedtest.User, *speedtest.Server) {
	successCount := 0
	user, err := speedtest.FetchUserInfo()
	if err != nil {
		slog.Error("Failed to fetch user info", "error", err)
		return successCount, nil, nil
	}

	serverList, err := speedtest.FetchServers()
	if err != nil {
		slog.Error("Failed to fetch server", "error", err)
		return successCount, nil, nil
	}

	// NOTE: FindServer finds server by serverID in given server list.
	// If the id is not found in the given list, return the server
	// with the lowest latency.
	targets, err := serverList.FindServer([]int{serverId})
	if err != nil {
		slog.Error("Failed to find server", "error", err)
		return successCount, nil, nil
	}

	if len(targets) == 0 {
		return successCount, nil, nil
	}

	if serverId != 0 && targets[0].ID != fmt.Sprintf("%d", serverId) {
		slog.Warn("Requested server not found, using nearest server",
			"requested_id", serverId,
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

func New(cfg Config) *Exporter {
	return &Exporter{
		ServerID: cfg.ServerID,
	}
}
