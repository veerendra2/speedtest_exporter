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
	up = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "up"),
		"Whether the speedtest was successful (1 = success, 0 = failure).",
		[]string{
			"test_uuid",
		}, nil,
	)
	scrapeDurationSeconds = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"),
		"Total time taken to complete the speedtest (ping + download + upload).",
		[]string{
			"test_uuid",
		}, nil,
	)
	latency = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "latency_seconds"),
		"Network latency to the speedtest server in seconds.",
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
	upload = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "upload_speed_Bps"),
		"Upload speed in bytes per second.",
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
	download = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "download_speed_Bps"),
		"Download speed in bytes per second.",
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
)

type Exporter struct {
	ServerId int
}

// Describe describes all the metrics. It implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- up
	ch <- scrapeDurationSeconds
	ch <- latency
	ch <- upload
	ch <- download
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	testUUID := uuid.New().String()
	start := time.Now()

	ok := runSpeedTest(testUUID, e.ServerId, ch)

	if ok {
		ch <- prometheus.MustNewConstMetric(
			up, prometheus.GaugeValue, 1.0,
			testUUID,
		)
		ch <- prometheus.MustNewConstMetric(
			scrapeDurationSeconds, prometheus.GaugeValue, time.Since(start).Seconds(),
			testUUID,
		)
	} else {
		ch <- prometheus.MustNewConstMetric(
			up, prometheus.GaugeValue, 0.0,
			testUUID,
		)
	}
}

func runSpeedTest(uuid string, serverId int, ch chan<- prometheus.Metric) bool {
	user, err := speedtest.FetchUserInfo()
	if err != nil {
		slog.Error("Failed to fetch user info", "error", err)
		return false
	}

	serverList, err := speedtest.FetchServers()
	if err != nil {
		slog.Error("Failed to fetch server", "error", err)
		return false
	}

	// NOTE: FindServer finds server by serverID in given server list.
	// If the id is not found in the given list, return the server
	// with the lowest latency.
	targets, err := serverList.FindServer([]int{serverId})
	if err != nil {
		slog.Error("Failed to find server", "error", err)
		return false
	}

	if len(targets) == 0 {
		return false
	}

	slog.Debug("Starting speedtest...",
		"test_uuid", uuid,
		"server", targets[0].Host,
		"server_distance", targets[0].Distance,
		"server_country", targets[0].Country,
		"user_ip", user.IP,
		"user_isp", user.Isp,
	)

	ok := pingTest(uuid, user, targets[0], ch)
	ok = downloadTest(uuid, user, targets[0], ch) && ok
	ok = uploadTest(uuid, user, targets[0], ch) && ok

	return ok
}

func pingTest(uuid string, user *speedtest.User, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
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

	return true
}

func downloadTest(uuid string, user *speedtest.User, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
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

	return true
}

func uploadTest(uuid string, user *speedtest.User, server *speedtest.Server, ch chan<- prometheus.Metric) bool {
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

	return true
}

func New(cfg Config) *Exporter {
	return &Exporter{
		ServerId: cfg.ServerId,
	}
}
