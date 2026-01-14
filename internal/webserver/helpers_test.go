package webserver

import (
	"net"
	"net/http"
	"testing"

	"github.com/rdwr-valentineg/GeoIP/internal/config"
)

func TestIsExcluded(t *testing.T) {
	tests := []struct {
		name     string
		ip       net.IP
		excluded []*net.IPNet
		expected bool
	}{
		{
			name: "IP in excluded subnet",
			ip:   net.ParseIP("10.20.0.1"),
			excluded: []*net.IPNet{
				{IP: net.ParseIP("10.10.0.0"), Mask: net.CIDRMask(24, 32)},
				{IP: net.ParseIP("10.20.0.0"), Mask: net.CIDRMask(24, 32)},
			},
			expected: true,
		}, {
			name: "IP not in excluded subnet",
			ip:   net.ParseIP("10.40.0.1"),
			excluded: []*net.IPNet{
				{IP: net.ParseIP("10.10.0.0"), Mask: net.CIDRMask(24, 32)},
				{IP: net.ParseIP("10.20.0.0"), Mask: net.CIDRMask(24, 32)},
				{IP: net.ParseIP("10.30.0.0"), Mask: net.CIDRMask(24, 32)},
			},
			expected: false,
		}, {
			name:     "Empty excluded list",
			ip:       net.ParseIP("1.2.3.4"),
			excluded: []*net.IPNet{},
			expected: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isExcluded(tc.ip, tc.excluded)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}

}

func TestGetIPFromRequest(t *testing.T) {
	config.InitConfig()
	tests := []struct {
		name       string
		request    *http.Request
		expectedIP net.IP
	}{
		{
			name: "IP from header",
			request: &http.Request{
				Header: http.Header{"X-Forwarded-For": []string{"1.2.3.4"}},
			},
			expectedIP: net.ParseIP("1.2.3.4"),
		}, {
			name: "Multiple IPs in header",
			request: &http.Request{
				Header: http.Header{"X-Forwarded-For": []string{"1.2.3.4,5.6.7.8"}},
			},
			expectedIP: net.ParseIP("1.2.3.4"),
		}, {
			name:       "IP from RemoteAddr",
			request:    &http.Request{RemoteAddr: "1.2.3.4:5678"},
			expectedIP: net.ParseIP("1.2.3.4"),
		}, {
			name:       "bad remote address value",
			request:    &http.Request{RemoteAddr: "bad:address"},
			expectedIP: nil,
		}, {
			name:       "SplitHostPort error",
			request:    &http.Request{RemoteAddr: "missingport"},
			expectedIP: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := getIPFromRequest(tc.request)
			if (ip == nil && tc.expectedIP != nil) ||
				(ip != nil && tc.expectedIP == nil) ||
				!ip.Equal(tc.expectedIP) {
				t.Errorf("Expected IP %s, got %s", tc.expectedIP.String(), ip.String())
			}
		})
	}
}
