package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() *Config {
	return &Config{
		OutPath:     OutPath{Reports: "out/reports", Vouchers: "out/vouchers"},
		Attestation: Attestation{Name: "Test Person", StilID: "te1234ab"},
		PrefixMappings: map[string]PrefixAccount{
			"sk": {Name: "skatt", Account: 3000},
		},
		Committees: map[string]CommitteeInfo{
			"skatt": {ProperName: "Skattmästeriet", Description: "Kiosk"},
		},
		Fortnox: FortnoxConfig{ZettleAccount: "1580", SwishAccount: "1930", VoucherSeries: "IR"},
	}
}

func TestValidateAcceptsCompleteConfig(t *testing.T) {
	if err := validConfig().validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateRejectsMissingFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{"reports path", func(c *Config) { c.OutPath.Reports = "" }, "out_path.reports"},
		{"vouchers path", func(c *Config) { c.OutPath.Vouchers = "" }, "out_path.vouchers"},
		{"attestation name", func(c *Config) { c.Attestation.Name = "" }, "attestation.name"},
		{"stil id", func(c *Config) { c.Attestation.StilID = "" }, "attestation.stil_id"},
		{"prefixes", func(c *Config) { c.PrefixMappings = nil }, "prefix_mappings"},
		{
			"missing fallback prefix",
			func(c *Config) {
				delete(c.PrefixMappings, "sk")
				c.PrefixMappings["ca"] = PrefixAccount{Name: "skatt", Account: 3001}
			},
			`fallback prefix "sk"`,
		},
		{"committees", func(c *Config) { c.Committees = nil }, "committees"},
		{"zettle account", func(c *Config) { c.Fortnox.ZettleAccount = "" }, "zettle_account"},
		{"swish account", func(c *Config) { c.Fortnox.SwishAccount = "" }, "swish_account"},
		{"voucher series", func(c *Config) { c.Fortnox.VoucherSeries = "" }, "voucher_series"},
		{
			"prefix points at undefined committee",
			func(c *Config) { c.PrefixMappings["xx"] = PrefixAccount{Name: "ghost", Account: 3000} },
			"ghost",
		},
	}
	for _, tc := range cases {
		cfg := validConfig()
		tc.mutate(cfg)
		err := cfg.validate()
		if err == nil {
			t.Errorf("%s: expected validation error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestFeePrefixAccountPrefersZ(t *testing.T) {
	cfg := validConfig()
	if got := cfg.FeePrefixAccount(); got.Name != "skatt" {
		t.Errorf("without z mapping, fee account = %+v, want sk fallback", got)
	}

	cfg.PrefixMappings["z"] = PrefixAccount{Name: "zettle", Account: 6570}
	if got := cfg.FeePrefixAccount(); got.Name != "zettle" {
		t.Errorf("with z mapping, fee account = %+v, want z", got)
	}
}

func TestLoadReportsFileAndParseErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("expected an error for a missing file")
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("{not yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("expected an error for malformed YAML")
	}
}
