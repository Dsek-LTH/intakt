package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Attestation struct {
	Name   string `yaml:"name"`
	StilID string `yaml:"stil_id"`
}

type PrefixAccount struct {
	Name    string `yaml:"name"`
	Account int    `yaml:"account"`
}

type CommitteeInfo struct {
	ProperName        string `yaml:"proper_name"`
	Description       string `yaml:"description"`
	DefaultCostCenter string `yaml:"default_cost_center"`
}

type OutPath struct {
	Reports  string `yaml:"reports"`
	Vouchers string `yaml:"vouchers"`
}

type FortnoxConfig struct {
	ZettleAccount string `yaml:"zettle_account"`
	SwishAccount  string `yaml:"swish_account"`
	VoucherSeries string `yaml:"voucher_series"`
}

type Config struct {
	OutPath              OutPath                  `yaml:"out_path"`
	Attestation          Attestation              `yaml:"attestation"`
	AccountDescriptions  map[string]string        `yaml:"account_descriptions"`
	PrefixMappings       map[string]PrefixAccount `yaml:"prefix_mappings"`
	Committees           map[string]CommitteeInfo `yaml:"committees"`
	Fortnox              FortnoxConfig            `yaml:"fortnox"`
	SwishAmountFallbacks map[int]string           `yaml:"swish_amount_fallbacks"`
}

// FeePrefixAccount returns the prefix account used for Zettle fees.
// It prefers the "z" prefix and falls back to "sk".
func (c *Config) FeePrefixAccount() PrefixAccount {
	if pa, ok := c.PrefixMappings["z"]; ok {
		return pa
	}
	return c.PrefixMappings["sk"]
}

// Load reads the YAML file at path and returns a validated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.OutPath.Reports == "" {
		return fmt.Errorf("out_path.reports is required")
	}

	if c.OutPath.Vouchers == "" {
		return fmt.Errorf("out_path.vouchers is required")
	}

	if c.Attestation.Name == "" {
		return fmt.Errorf("attestation.name is required")
	}

	if c.Attestation.StilID == "" {
		return fmt.Errorf("attestation.stil_id is required")
	}

	if len(c.PrefixMappings) == 0 {
		return fmt.Errorf("prefix_mappings is empty")
	}

	if len(c.Committees) == 0 {
		return fmt.Errorf("committees is empty")
	}

	for prefix, pa := range c.PrefixMappings {
		if _, ok := c.Committees[pa.Name]; !ok {
			return fmt.Errorf("prefix_mappings[%q]: committee %q is not defined in committees", prefix, pa.Name)
		}
	}

	if c.Fortnox.ZettleAccount == "" {
		return fmt.Errorf("fortnox.zettle_account is required")
	}

	if c.Fortnox.SwishAccount == "" {
		return fmt.Errorf("fortnox.swish_account is required")
	}

	if c.Fortnox.VoucherSeries == "" {
		return fmt.Errorf("fortnox.voucher_series is required")
	}

	return nil
}
