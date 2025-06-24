package config

import (
	"errors"
	"flag"
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
		"default values": {
			args:    []string{"cmd"},
			wantErr: false,
			wantCheck: func(cfg *config) error {
				if cfg.DbPath != "/mmdb/GeoLite2-Country.mmdb" {
					return errors.New("unexpected DbPath")
				}
				if cfg.Port != 8080 {
					return errors.New("unexpected Port")
				}
				if cfg.IpHeader != "X-Forwarded-For" {
					return errors.New("unexpected IpHeader")
				}
				if cfg.CachePurgePeriod != 2*time.Minute {
					return errors.New("unexpected CachePurgePeriod")
				}
				return nil
			},
		},
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
			},
			wantErr: false,
			wantCheck: func(cfg *config) error {
				if cfg.DbPath != "test.db" {
					return errors.New("unexpected DbPath")
				}
				if cfg.Port != 9090 {
					return errors.New("unexpected Port")
				}
				if cfg.ExcludeCIDR != "1.2.3.4/32" {
					return errors.New("unexpected ExcludeCIDR")
				}
				if cfg.AllowedCountryList != "DE,FR" {
					return errors.New("unexpected AllowedCountryList")
				}
				if cfg.IpHeader != "Real-IP" {
					return errors.New("unexpected IpHeader")
				}
				if cfg.LogLevelFlag != "debug" {
					return errors.New("unexpected LogLevelFlag")
				}
				if cfg.CachePurgePeriod != 5*time.Minute {
					return errors.New("unexpected CachePurgePeriod")
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()
			os.Args = tc.args
			Config = nil // Reset global config before each test
			err := InitConfig()
			if tc.wantErr {
				if err == nil {
					t.Errorf("InitConfig() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("InitConfig() unexpected error: %v", err)
				}
				if tc.wantCheck != nil {
					if checkErr := tc.wantCheck(Config); checkErr != nil {
						t.Errorf("Config check failed: %v", checkErr)
					}
				}
			}
		})
	}
}
