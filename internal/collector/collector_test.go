package collector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/showwin/speedtest-go/speedtest"
)

// TestSpeedTestsEmitNoMetricOnFailure points the SDK at a dead address, so the
// tests fail before any speed is measured. NOTE: the DLSpeed/ULSpeed < 0 guard
// (the SDK's "N/A" value) is not covered here — it needs a server that accepts
// connections and then fails every transfer.
func TestSpeedTestsEmitNoMetricOnFailure(t *testing.T) {
	client := speedtest.New()
	server, err := client.CustomServer("http://127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create custom server: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch := make(chan prometheus.Metric, 10)

	if downloadTest(ctx, server, "test", ch) {
		t.Errorf("downloadTest() returned true for a failing server, expected false")
	}

	if uploadTest(ctx, server, "test", ch) {
		t.Errorf("uploadTest() returned true for a failing server, expected false")
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
			got := statusValue(tt.successCount)
			if got != tt.wantStatus {
				t.Errorf("statusValue(%d) = %v, want %v", tt.successCount, got, tt.wantStatus)
			}
		})
	}
}

func TestProbeParams(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		wantTarget   string
		wantServerID int
		wantErr      bool
	}{
		{"defaults", "/probe", "speedtest", 0, false},
		{"target only", "/probe?target=berlin", "berlin", 0, false},
		{"server only", "/probe?server_id=1234", "speedtest", 1234, false},
		{"target and server", "/probe?target=berlin&server_id=1234", "berlin", 1234, false},
		{"invalid server id", "/probe?server_id=abc", "", 0, true},
		{"invalid utf-8 target", "/probe?target=%FF", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)

			target, serverID, err := probeParams(req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("probeParams() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if target != tt.wantTarget {
				t.Errorf("target = %q, want %q", target, tt.wantTarget)
			}
			if serverID != tt.wantServerID {
				t.Errorf("serverID = %d, want %d", serverID, tt.wantServerID)
			}
		})
	}
}

func TestWithProbeSlotSerialises(t *testing.T) {
	var inFlight, peak atomic.Int32
	var wg sync.WaitGroup

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			withProbeSlot(context.Background(), func() {
				now := inFlight.Add(1)
				if now > peak.Load() {
					peak.Store(now)
				}
				time.Sleep(10 * time.Millisecond)
				inFlight.Add(-1)
			})
		}()
	}
	wg.Wait()

	if got := peak.Load(); got != 1 {
		t.Errorf("peak concurrent probes = %d, want 1", got)
	}
}

func TestWithProbeSlotGivesUpOnTimeout(t *testing.T) {
	probeSlot <- struct{}{}
	defer func() { <-probeSlot }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if withProbeSlot(ctx, func() { t.Error("fn ran while the slot was taken") }) {
		t.Error("withProbeSlot() = true, want false when the slot stays busy")
	}
}
