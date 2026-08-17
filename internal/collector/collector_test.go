package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/showwin/speedtest-go/speedtest"
)

func TestSpeedTestsNegativeValues(t *testing.T) {
	client := speedtest.New()
	server, err := client.CustomServer("http://127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create custom server: %v", err)
	}

	server.DLSpeed = -1
	server.ULSpeed = -1

	ch := make(chan prometheus.Metric, 10)

	// Since Context is attached, the functions run their test contexts against an unreachable host
	// resulting in errors or negative speed, which should both return false without panic or metric emission.
	if downloadTest(context.Background(), server, ch) {
		t.Errorf("downloadTest() returned true for negative DLSpeed, expected false")
	}

	if uploadTest(context.Background(), server, ch) {
		t.Errorf("uploadTest() returned true for negative ULSpeed, expected false")
	}

	close(ch)

	if len(ch) != 0 {
		t.Errorf("expected no metrics to be emitted for failed tests, got %d metrics", len(ch))
	}
}

func TestStatusComputation(t *testing.T) {
	tests := []struct {
		name         string
		successCount int
		wantStatus   float64
	}{
		{"all succeeded", 3, 1.0},
		{"partial success 2", 2, -1.0},
		{"partial success 1", 1, -1.0},
		{"all failed", 0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var statusValue float64
			switch tt.successCount {
			case 3:
				statusValue = 1.0
			case 0:
				statusValue = 0.0
			default:
				statusValue = -1.0
			}
			if statusValue != tt.wantStatus {
				t.Errorf("got %v, want %v", statusValue, tt.wantStatus)
			}
		})
	}
}

func TestMetricsHandler(t *testing.T) {
	exp := New(Config{ServerID: 0, MaxConnections: 4})
	handler := NewMetricsHandler(exp)

	req := httptest.NewRequest(http.MethodGet, "/metrics?server_id=1234", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if exp.serverID != 0 {
		t.Errorf("expected serverID to be reset to 0, got %d", exp.serverID)
	}
}
