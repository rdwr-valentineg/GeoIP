package webserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		source         *mockGeoIPSource
		expectedStatus int
	}{
		{
			name:           "Healthz endpoint",
			url:            "/healthz",
			source:         &mockGeoIPSource{ready: true},
			expectedStatus: http.StatusOK,
		}, {
			name:           "Ready endpoint",
			url:            "/ready",
			source:         &mockGeoIPSource{ready: true},
			expectedStatus: http.StatusOK,
		}, {
			name:           "Not Ready endpoint",
			url:            "/ready",
			source:         &mockGeoIPSource{ready: false},
			expectedStatus: http.StatusServiceUnavailable,
		}, {
			name:           "Metrics endpoint",
			url:            "/metrics",
			source:         &mockGeoIPSource{ready: true},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := Run(tc.source, make(chan error, 1))
			defer server.Srv.Close()

			req := httptest.NewRequest("GET", tc.url, nil)
			w := httptest.NewRecorder()
			server.Srv.Handler.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}
		})
	}

	// Simulate ListenAndServe error by passing a faulty address or handler
	t.Run("ListenAndServe error", func(t *testing.T) {
		config.InitConfig()
		badSource := &mockGeoIPSource{ready: true}
		errCh := make(chan error, 1)
		Run(badSource, errCh)
		time.Sleep(100 * time.Millisecond) // Allow server to start
		Run(badSource, errCh)
		time.Sleep(100 * time.Millisecond) // Allow server to start

		err := <-errCh
		if err == nil {
			t.Errorf("Expected error from Run with invalid address, got nil")
		}
	})
}
