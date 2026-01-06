package collector

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/showwin/speedtest-go/speedtest"
)

const namespace = "speedtest"

type Config struct {
	ServerId int `env:"SERVER_ID" default:"0" help:"Speedtest.net server ID (0 = auto-select nearest server)"`
}

var (
	status = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "status"),
		"Whether the speedtest was successful (-1 = partiallysuccess, 0 = failure, 1 = success).",
		[]string{
			"test_uuid",
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
		[]string{
			"test_uuid",
		}, nil,
	)
	latency = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "latency_seconds"),
		"Network latency to the speedtest server in seconds.",
		[]string{
			"test_uuid",
		}, nil,
	)
	upload = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "upload_speed_Bps"),
		"Upload speed in bytes per second.",
		[]string{
			"test_uuid",
		}, nil,
	)
	download = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "download_speed_Bps"),
		"Download speed in bytes per second.",
		[]string{
			"test_uuid",
		}, nil,
	)
)

type Exporter struct {
	ServerId int
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
	testUUID := uuid.New().String()
	start := time.Now()
	var statusValue float64 = 0

	successCount, user, server := runSpeedTest(testUUID, e.ServerId, ch)
	// Calculate status based on successCount:
	// 0     	 - All tests failed                    	   -> Metric Value: 0
	// 1 or 2  - Some tests succeeded (partial success)  -> Metric Value: -1
	// 3     	 - All tests succeeded                 	   -> Metric Value: 1

	// If user and server are not nil, we check the successCount
	if user != nil && server != nil {
		if successCount > 0 && successCount < 3 {
			// Partially success
			statusValue = -1.0
		}
		if successCount == 3 {
			// All tests succeeded
			statusValue = 1
		}
		ch <- prometheus.MustNewConstMetric(
			status, prometheus.GaugeValue, statusValue,
			testUUID,
			user.Lat,
			user.Lon,
			user.IP,
			user.Isp,
			server.Lat,
			server.Lon,
			server.ID,
			server.Name,
			server.Country,
			fmt.Sprintf("%f", server.Distance),
		)
	} else {
		// If tests failed completely, send status with empty labels
		ch <- prometheus.MustNewConstMetric(
			status, prometheus.GaugeValue, statusValue,
			testUUID,
			"", "", "", "", "", "", "", "", "", "",
		)
	}

	ch <- prometheus.MustNewConstMetric(
		scrapeDurationSeconds, prometheus.GaugeValue, time.Since(start).Seconds(),
		testUUID,
	)
}

func runSpeedTest(uuid string, serverId int, ch chan<- prometheus.Metric) (int, *speedtest.User, *speedtest.Server) {
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

	slog.Debug("Starting speedtest...",
		"test_uuid", uuid,
		"server", targets[0].Host,
		"server_distance", targets[0].Distance,
		"server_country", targets[0].Country,
		"user_ip", user.IP,
		"user_isp", user.Isp,
	)

	if pingTest(uuid, targets[0], ch) {
		successCount++
	}
	if downloadTest(uuid, targets[0], ch) {
		successCount++
	}
	if uploadTest(uuid, targets[0], ch) {
		successCount++
	}

	return successCount, user, targets[0]
}

func pingTest(uuid string, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.PingTest(nil)
	if err != nil {
		slog.Error("Failed to run ping test", "error", err)
		return false
	}

	slog.Debug("Latency test completed",
		"test_uuid", uuid,
		"latency_seconds", server.Latency.Seconds(),
	)

	ch <- prometheus.MustNewConstMetric(
		latency, prometheus.GaugeValue, server.Latency.Seconds(),
		uuid,
	)

	return true
}

func downloadTest(uuid string, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.DownloadTest()
	if err != nil {
		slog.Error("Failed to run download test", "error", err)
		return false
	}

	slog.Debug("Download test completed",
		"test_uuid", uuid,
		"download_speed_bytes_per_sec", server.DLSpeed*1024*1024,
	)

	ch <- prometheus.MustNewConstMetric(
		download, prometheus.GaugeValue, float64(server.DLSpeed)*1024*1024,
		uuid,
	)

	return true
}

func uploadTest(uuid string, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
	err := server.UploadTest()
	if err != nil {
		slog.Error("Failed to run upload test", "error", err)
		return false
	}
	slog.Debug("Upload test completed",
		"test_uuid", uuid,
		"upload_speed_bytes_per_sec", server.ULSpeed*1024*1024,
	)

	ch <- prometheus.MustNewConstMetric(
		upload, prometheus.GaugeValue, float64(server.ULSpeed)*1024*1024,
		uuid,
	)

	return true
}

func New(cfg Config) *Exporter {
	return &Exporter{
		ServerId: cfg.ServerId,
	}
}
