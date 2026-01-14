package webserver

import (
	"errors"
	"flag"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/oschwald/maxminddb-golang"
	"github.com/rdwr-valentineg/GeoIP/internal/config"
	"github.com/rdwr-valentineg/GeoIP/internal/db"
	"github.com/rdwr-valentineg/GeoIP/internal/metrics"
)

type (
	mockGeoIPSource struct {
		db.GeoIPSource
		ready  bool
		lookup func(ip net.IP, record any) error
	}
	mockGeoIPReader struct {
		*maxminddb.Reader
		lookup func(ip net.IP, record any) error
	}
)

func (m *mockGeoIPSource) IsReady() bool {
	return m.ready
}

func (m *mockGeoIPSource) GetReader() db.ReaderInterface {
	return &mockGeoIPReader{lookup: m.lookup}
}

func (m *mockGeoIPReader) Lookup(ip net.IP, record any) error {
	return m.lookup(ip, record)
}

func (m *mockGeoIPReader) Close() error {
	return nil // No-op for mock
}

var (
	origGetIPFromRequest = getIPFromRequest
	origIsExcluded       = isExcluded
	origServeVerdict     = serveVerdict
	origRespondAllowed   = respondAllowed
	origArgs             = os.Args
)

func resetGlobals() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	os.Args = origArgs
	geoCache = make(map[string]cacheEntry)
	cacheMux = sync.RWMutex{}
	getIPFromRequest = origGetIPFromRequest
	isExcluded = origIsExcluded
	serveVerdict = origServeVerdict
	respondAllowed = origRespondAllowed
}

// --- Tests ---

func TestServeHTTP(t *testing.T) {
	metrics.InitMetrics()
	origGetIPFromRequest := getIPFromRequest
	originalIsExcluded := isExcluded
	originalServeVerdict := serveVerdict
	originalRespondAllowed := respondAllowed

	defer func() {
		resetGlobals()
		getIPFromRequest = origGetIPFromRequest
		isExcluded = originalIsExcluded
		serveVerdict = originalServeVerdict
		respondAllowed = originalRespondAllowed
	}()
	ip := net.ParseIP("1.2.3.4")
	excludedIp := net.ParseIP("10.0.0.1")

	tests := []struct {
		name             string
		handler          *mockGeoIPSource
		getIpFromReqFunc func(r *http.Request) net.IP
		isExcludedFunc   func(ip net.IP, excluded []*net.IPNet) bool
		cacheEntries     map[string]cacheEntry
		expectedStatus   int
		expectedBody     string
		expectedCountry  string
	}{
		{
			name:             "DB not ready",
			handler:          &mockGeoIPSource{ready: false},
			expectedStatus:   http.StatusServiceUnavailable,
			getIpFromReqFunc: origGetIPFromRequest,
			isExcludedFunc:   originalIsExcluded,
			expectedBody:     "GeoIP DB not ready",
		}, {
			name:             "IP is nil",
			handler:          &mockGeoIPSource{ready: true},
			getIpFromReqFunc: func(r *http.Request) net.IP { return nil },
			expectedStatus:   http.StatusBadRequest,
			isExcludedFunc:   originalIsExcluded,
			expectedBody:     "Unable to determine IP",
		}, {
			name:             "Cache hit",
			handler:          &mockGeoIPSource{ready: true},
			getIpFromReqFunc: func(r *http.Request) net.IP { return ip },
			isExcludedFunc:   originalIsExcluded,
			cacheEntries:     map[string]cacheEntry{ip.String(): {allowed: true, country: "US"}},
			expectedStatus:   200,
			expectedBody:     "",
			expectedCountry:  "US",
		}, {
			name:             "Excluded IP",
			handler:          &mockGeoIPSource{ready: true},
			getIpFromReqFunc: func(r *http.Request) net.IP { return excludedIp },
			isExcludedFunc:   func(ip net.IP, excluded []*net.IPNet) bool { return true },
			expectedStatus:   200,
			expectedBody:     "",
			expectedCountry:  "LAN",
		}, {
			name:             "GeoIP lookup error",
			handler:          &mockGeoIPSource{ready: true, lookup: func(ip net.IP, record any) error { return errors.New("fail") }},
			getIpFromReqFunc: func(r *http.Request) net.IP { return ip },
			isExcludedFunc:   originalIsExcluded,
			expectedStatus:   http.StatusInternalServerError,
			expectedBody:     "GeoIP lookup failed",
		}, {
			name: "Disallowed country",
			handler: &mockGeoIPSource{
				ready: true,
				lookup: func(ip net.IP, record any) error {
					rec := record.(*geoRecord)
					rec.Country.ISOCode = "ru"
					return nil
				},
			},
			getIpFromReqFunc: func(r *http.Request) net.IP { return ip },
			isExcludedFunc:   originalIsExcluded,
			expectedStatus:   403,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			getIPFromRequest = tc.getIpFromReqFunc
			isExcluded = tc.isExcludedFunc

			if len(tc.cacheEntries) > 0 {
				cacheMux.Lock()
				geoCache = tc.cacheEntries
				cacheMux.Unlock()
			} else {
				CacheCleanup()
			}
			handler := NewAuthHandler(tc.handler)
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)
			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.expectedBody) {
				t.Errorf("Expected body to contain '%s', got: %s", tc.expectedBody, w.Body.String())
			}
			if tc.expectedCountry != "" {
				country := w.Header().Get("X-Country")
				if country != tc.expectedCountry {
					t.Errorf("Expected country '%s', got '%s'", tc.expectedCountry, country)
				}
			}
		})
	}

}

func TestServeHTTP_AllowedCountry(t *testing.T) {
	defer resetGlobals()
	os.Args = []string{"cmd", "--allow=US", "--db=test.db"} // Simulate command line args for allowed countries,
	err := config.InitConfig()
	if err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	ip := net.ParseIP("8.8.8.8")
	handler := NewAuthHandler(&mockGeoIPSource{
		ready: true,
		lookup: func(ip net.IP, record any) error {
			rec := record.(*geoRecord)
			rec.Country.ISOCode = "us"
			return nil
		},
	})
	getIPFromRequest = func(r *http.Request) net.IP { return ip }
	isExcluded = func(ip net.IP, excluded []*net.IPNet) bool { return false }
	// config.GetAllowedCodes = func() map[string]bool { return map[string]bool{"US": true} }

	called := false
	serveVerdict = func(w http.ResponseWriter, allowed bool, country string) {
		called = true
		if !allowed || country != "US" {
			t.Errorf("Expected allowed=true, country='US', got allowed=%v, country='%s'", allowed, country)
		}
		w.WriteHeader(297)
		w.Write([]byte("allowed"))
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !called {
		t.Error("serveVerdict should have been called for allowed country")
	}
	if w.Code != 297 {
		t.Errorf("Expected status 297, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "allowed") {
		t.Errorf("Expected 'allowed' in response body, got: %s", w.Body.String())
	}
}
