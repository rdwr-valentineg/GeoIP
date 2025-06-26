package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidate(t *testing.T) {
	tests := map[string]struct {
		config  *config
		wantErr string
	}{
		"valid config": {
			config: &config{
				DbPath:           "test.db",
				Port:             8080,
				IpHeader:         "some-header",
				CachePurgePeriod: 10,
			},
		},
		"empty db path": {
			config: &config{
				Port:             8080,
				IpHeader:         "some-header",
				CachePurgePeriod: 10,
			},
			wantErr: "both database path and Maxmind license key cannot be empty",
		},
		"invalid port": {
			config: &config{
				DbPath:           "test.db",
				Port:             65537, // Invalid port (greater than 65536)
				IpHeader:         "some-header",
				CachePurgePeriod: 10,
			},
			wantErr: "invalid port value, must be between 1 and 65536",
		},
		"missing port": {
			config: &config{
				DbPath:           "test.db",
				IpHeader:         "some-header",
				CachePurgePeriod: 10,
			},
			wantErr: "invalid port value, must be between 1 and 65536",
		},
		"missing cache purge period": {
			config: &config{
				DbPath:   "test.db",
				Port:     8080,
				IpHeader: "some-header",
			},
			wantErr: "cache purge interval must be greater than zero",
		},
		"good maxmind license key but missing account id": {
			config: &config{
				DbPath:            "test.db",
				Port:              8080,
				IpHeader:          "some-header",
				CachePurgePeriod:  10,
				MaxMindLicenseKey: "valid-key",
			},
			wantErr: "when maxmind license key provided, maxmind account id is required",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
				}
			} else if err == nil {
				t.Errorf("Validate() expected error but got nil")
			} else if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() got error [%v], while expected [%s]", err, tc.wantErr)
			}
		})
	}
}

func TestInitConfig(t *testing.T) {
	// Helper to reset flags between tests
	resetFlags := func() {
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	}

	tests := map[string]struct {
		name      string
		args      []string
		wantErr   bool
		wantCheck func(*config) error
	}{
		"custom values": {
			args: []string{
				"cmd",
				"-db=test.db",
				"-port=9090",
				"-exclude=1.2.3.4/32",
				"-allow=DE,FR",
				"-ip-header=Real-IP",
				"-log-level=debug",
				"-purge-interval=5m",
				"-maxmind-license-key=valid-key",
				"-maxmind-account-id=valid-id",
				"-maxmind-fetch-interval=1h",
			},
			wantErr: false,
			wantCheck: func(cfg *config) error {
				if cfg.DbPath != "test.db" {
					return errors.New("unexpected DbPath")
				}
				if cfg.Port != 9090 {
					return errors.New("unexpected Port")
				}
				if len(cfg.ExcludeCIDR) < 1 {
					return errors.New("unexpected ExcludeCIDR, expected at least one CIDR")
				}

				_, expectedNet, _ := net.ParseCIDR("1.2.3.4/32")
				if cfg.ExcludeCIDR[0] == nil ||
					!cfg.ExcludeCIDR[0].IP.Equal(expectedNet.IP) {
					return fmt.Errorf("unexpected ExcludeCIDR, expected to find [1.2.3.4/32], got [%s]",
						cfg.ExcludeCIDR[0].String())
				}
				if res, found := cfg.AllowedCodes["DE"]; !res || !found {
					return errors.New("unexpected AllowedCountryList - [DE] should be present")
				}
				if res, found := cfg.AllowedCodes["FR"]; !res || !found {
					return errors.New("unexpected AllowedCountryList, [FR] should be present")
				}
				if res, found := cfg.AllowedCodes["RU"]; res || found {
					return errors.New("unexpected AllowedCountryList, [RU] should not be present")
				}
				if cfg.IpHeader != "Real-IP" {
					return errors.New("unexpected IpHeader, expected [Real-IP]")
				}
				if cfg.LogLevelFlag != "debug" {
					return errors.New("unexpected LogLevelFlag, expected [debug]")
				}
				if cfg.CachePurgePeriod != 5*time.Minute {
					return errors.New("unexpected CachePurgePeriod, expected [5m]")
				}
				if cfg.MaxMindLicenseKey != "valid-key" {
					return errors.New("unexpected MaxMindLicenseKey, expected [valid-key]")
				}
				if cfg.MaxMindAccountId != "valid-id" {
					return errors.New("unexpected MaxMindAccountId, expected [valid-id]")
				}
				if cfg.MaxMindFetchInterval != time.Hour {
					return errors.New("unexpected MaxMindFetchInterval, expected [1h]")
				}
				return nil
			},
		},
		"invalid port": {
			args:    []string{"cmd", "-port=70000"},
			wantErr: true,
		},
		"empty db path": {
			args:    []string{"cmd", "-db="},
			wantErr: true,
		},
		"empty ip header": {
			args:    []string{"cmd", "-ip-header="},
			wantErr: true,
		},
		"zero purge interval": {
			args:    []string{"cmd", "-purge-interval=0s"},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			resetFlags()
			os.Args = tc.args
			cfg = nil // Reset global config before each test
			err := InitConfig()
			if tc.wantErr {
				if err == nil {
					t.Errorf("InitConfig() expected error, got nil, config: %+v", cfg)
				}
			} else {
				if err != nil {
					t.Errorf("InitConfig() unexpected error: %v, config: %+v", err, cfg)
				}
				if tc.wantCheck != nil {
					if checkErr := tc.wantCheck(cfg); checkErr != nil {
						t.Errorf("Config check failed: %v config: %+v", checkErr, cfg)
					}
				}
			}
		})
	}
}

func TestGetStringGetters(t *testing.T) {
	// Save original cfg and restore after test
	origCfg := cfg
	defer func() { cfg = origCfg }()

	t.Run("cfg is nil", func(t *testing.T) {
		cfg = nil
		dbPath := GetDbPath()
		if dbPath != "" {
			t.Errorf("GetDbPath() with nil cfg = %q, want empty string", dbPath)
		}
		port := GetPort()
		if port != 0 {
			t.Errorf("GetPort() with nil cfg = %d, want 0", port)
		}
		ipHeader := GetIpHeader()
		if ipHeader != "" {
			t.Errorf("GetIpHeader() with nil cfg = %q, want empty string", ipHeader)
		}
		loglevel := GetLogLevel()
		if loglevel != "" {
			t.Errorf("GetLogLevelFlag() with nil cfg = %q, want empty string", loglevel)
		}
		key := GetMaxMindLicenseKey()
		if key != "" {
			t.Errorf("GetMaxMindLicenseKey() with nil cfg = %q, want empty string", key)
		}
		id := GetMaxMindAccountId()
		if id != "" {
			t.Errorf("GetMaxMindAccountId() with nil cfg = %q, want empty string", id)
		}
		mmInterval := GetMaxMindFetchInterval()
		if mmInterval != time.Duration(0) {
			t.Errorf("GetMaxMindAccountId() with nil cfg = %q, want empty string", mmInterval)
		}
		pInterval := GetCachePurgePeriod()
		if pInterval != time.Duration(0) {
			t.Errorf("GetMaxMindAccountId() with nil cfg = %q, want empty string", pInterval)
		}
		allowed := GetAllowedCodes()
		if allowed != nil {
			t.Errorf("GetMaxMindAccountId() with nil cfg = %v, want empty string", allowed)
		}
		excludes := GetExcludeCIDR()
		if excludes != nil {
			t.Errorf("GetMaxMindAccountId() with nil cfg = %q, want empty string", excludes)
		}
	})

	t.Run("cfg is set", func(t *testing.T) {
		cfg = &config{
			DbPath:               "test.db",
			Port:                 8080,
			IpHeader:             "X-Forwarded-For",
			LogLevelFlag:         "info",
			MaxMindLicenseKey:    "test-key",
			MaxMindAccountId:     "test-id",
			MaxMindFetchInterval: 30 * time.Minute,
			CachePurgePeriod:     10 * time.Minute,
			AllowedCodes:         map[string]bool{"US": true},
			ExcludeCIDR: []*net.IPNet{{
				IP:   net.ParseIP("1.2.3.4"),
				Mask: net.CIDRMask(32, 32),
			}},
		}
		dbPath := GetDbPath()
		if dbPath != "test.db" {
			t.Errorf("GetDbPath() = %q, want %q", dbPath, "test.db")
		}
		port := GetPort()
		if port != 8080 {
			t.Errorf("GetPort() = %d, want %d", port, 8080)
		}
		ipHeader := GetIpHeader()
		if ipHeader != "X-Forwarded-For" {
			t.Errorf("GetIpHeader() = %q, want %q", ipHeader, "X-Forwarded-For")
		}
		loglevel := GetLogLevel()
		if loglevel != "info" {
			t.Errorf("GetLogLevel() = %q, want %q", loglevel, "info")
		}
		key := GetMaxMindLicenseKey()
		if key != "test-key" {
			t.Errorf("GetMaxMindLicenseKey() = %q, want %q", key, "test-key")
		}
		id := GetMaxMindAccountId()
		if id != "test-id" {
			t.Errorf("GetMaxMindAccountId() = %q, want %q", id, "test-id")
		}
		mmInterval := GetMaxMindFetchInterval()
		if mmInterval != 30*time.Minute {
			t.Errorf("GetMaxMindFetchInterval() = %v, want %v", mmInterval, 30*time.Minute)
		}
		pInterval := GetCachePurgePeriod()
		if pInterval != 10*time.Minute {
			t.Errorf("GetCachePurgePeriod() = %v, want %v", pInterval, 10*time.Minute)
		}
		allowed := GetAllowedCodes()
		if allowed == nil || !allowed["US"] {
			t.Errorf("GetAllowedCodes() = %v, want map with 'US':true", allowed)
		}
		excludes := GetExcludeCIDR()
		if excludes == nil || excludes[0] == nil || !excludes[0].IP.Equal(net.ParseIP("1.2.3.4")) {
			t.Errorf("GetExcludeCIDR() = %v, want first IPNet with IP 1.2.3.4", excludes)
		}
	})
}
