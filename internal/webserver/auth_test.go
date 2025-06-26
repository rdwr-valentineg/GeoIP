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

// --- Mocks and helpers ---

type mockGeoIPSource struct {
	db.GeoIPSource
	ready  bool
	lookup func(ip net.IP, record any) error
}

func (m *mockGeoIPSource) IsReady() bool {
	return m.ready
}
func (m *mockGeoIPSource) GetReader() db.ReaderInterface {
	return &mockGeoIPReader{lookup: m.lookup}
}

type mockGeoIPReader struct {
	*maxminddb.Reader
	lookup func(ip net.IP, record any) error
}

func (m *mockGeoIPReader) Lookup(ip net.IP, record any) error {
	return m.lookup(ip, record)
}
func (m *mockGeoIPReader) Close() error {
	return nil // No-op for mock
}

// Patchable helpers
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

func TestServeHTTP_DBNotReady(t *testing.T) {
	metrics.InitMetrics()
	defer resetGlobals()
	handler := NewAuthHandler(&mockGeoIPSource{ready: false})
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
	if !strings.Contains(w.Body.String(), "GeoIP DB not ready") {
		t.Errorf("Expected 'GeoIP DB not ready' message, got: %s", w.Body.String())
	}
}

func TestServeHTTP_IPNil(t *testing.T) {
	metrics.InitMetrics()
	defer resetGlobals()
	handler := NewAuthHandler(&mockGeoIPSource{ready: true})
	getIPFromRequest = func(r *http.Request) net.IP { return nil }
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	if !strings.Contains(w.Body.String(), "Unable to determine IP") {
		t.Errorf("Expected 'Unable to determine IP' message, got: %s", w.Body.String())
	}
}

func TestServeHTTP_CacheHit(t *testing.T) {
	metrics.InitMetrics()
	defer resetGlobals()
	ip := net.ParseIP("1.2.3.4")
	geoCache[ip.String()] = cacheEntry{allowed: true, country: "US"}
	handler := NewAuthHandler(&mockGeoIPSource{ready: true})
	getIPFromRequest = func(r *http.Request) net.IP { return ip }

	called := false
	serveVerdict = func(w http.ResponseWriter, allowed bool, country string) {
		called = true
		w.WriteHeader(299)
		w.Write([]byte("cache hit"))
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !called {
		t.Error("serveVerdict should have been called for cache hit")
	}
	if w.Code != 299 {
		t.Errorf("Expected status 299, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "cache hit") {
		t.Errorf("Expected 'cache hit' in response body, got: %s", w.Body.String())
	}
}

func TestServeHTTP_ExcludedIP(t *testing.T) {
	metrics.InitMetrics()
	defer resetGlobals()
	ip := net.ParseIP("10.0.0.1")
	handler := NewAuthHandler(&mockGeoIPSource{ready: true})
	getIPFromRequest = func(r *http.Request) net.IP { return ip }
	isExcluded = func(ip net.IP, excluded []*net.IPNet) bool { return true }

	called := false
	respondAllowed = func(w http.ResponseWriter, country string) {
		called = true
		w.WriteHeader(298)
		w.Write([]byte("excluded"))
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !called {
		t.Error("respondAllowed should have been called for excluded IP")
	}
	if w.Code != 298 {
		t.Errorf("Expected status 298, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "excluded") {
		t.Errorf("Expected 'excluded' in response body, got: %s", w.Body.String())
	}
}

func TestServeHTTP_GeoIPLookupError(t *testing.T) {
	defer resetGlobals()
	ip := net.ParseIP("8.8.8.8")
	handler := NewAuthHandler(&mockGeoIPSource{
		ready: true,
		lookup: func(ip net.IP, record any) error {
			return errors.New("fail")
		},
	})
	getIPFromRequest = func(r *http.Request) net.IP { return ip }
	isExcluded = func(ip net.IP, excluded []*net.IPNet) bool { return false }

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
	if !strings.Contains(w.Body.String(), "GeoIP lookup failed") {
		t.Errorf("Expected 'GeoIP lookup failed' message, got: %s", w.Body.String())
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

func TestServeHTTP_DisallowedCountry(t *testing.T) {
	defer resetGlobals()
	ip := net.ParseIP("8.8.8.8")
	handler := NewAuthHandler(&mockGeoIPSource{
		ready: true,
		lookup: func(ip net.IP, record any) error {
			rec := record.(*geoRecord)
			rec.Country.ISOCode = "ru"
			return nil
		},
	})
	getIPFromRequest = func(r *http.Request) net.IP { return ip }
	isExcluded = func(ip net.IP, excluded []*net.IPNet) bool { return false }

	called := false
	serveVerdict = func(w http.ResponseWriter, allowed bool, country string) {
		called = true
		if allowed || country != "RU" {
			t.Errorf("Expected allowed=false, country='RU', got allowed=%v, country='%s'", allowed, country)
		}
		w.WriteHeader(296)
		w.Write([]byte("denied"))
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !called {
		t.Error("serveVerdict should have been called for disallowed country")
	}
	if w.Code != 296 {
		t.Errorf("Expected status 296, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "denied") {
		t.Errorf("Expected 'allowed' in response body, got: %s", w.Body.String())
	}
}
