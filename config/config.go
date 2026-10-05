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

// fallbackPrefix routes products whose name matches no configured prefix; its
// mapping must exist (enforced by validate). feePrefix, when configured, books
// Zettle fees on its own account instead of the fallback's.
const (
	fallbackPrefix = "sk"
	feePrefix      = "z"
)

// FallbackPrefixAccount returns the mapping used for products and payments
// whose name matches no configured prefix.
func (c *Config) FallbackPrefixAccount() PrefixAccount {
	return c.PrefixMappings[fallbackPrefix]
}

// FeePrefixAccount returns the prefix account used for Zettle fees.
// It prefers the fee prefix and falls back to the fallback prefix.
func (c *Config) FeePrefixAccount() PrefixAccount {
	if pa, ok := c.PrefixMappings[feePrefix]; ok {
		return pa
	}
	return c.FallbackPrefixAccount()
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
	required := []struct{ value, key string }{
		{c.OutPath.Reports, "out_path.reports"},
		{c.OutPath.Vouchers, "out_path.vouchers"},
		{c.Attestation.Name, "attestation.name"},
		{c.Attestation.StilID, "attestation.stil_id"},
		{c.Fortnox.ZettleAccount, "fortnox.zettle_account"},
		{c.Fortnox.SwishAccount, "fortnox.swish_account"},
		{c.Fortnox.VoucherSeries, "fortnox.voucher_series"},
	}
	for _, field := range required {
		if field.value == "" {
			return fmt.Errorf("%s is required", field.key)
		}
	}
	return c.validateMappings()
}

// validateMappings checks the routing tables: every prefix must point at a
// defined committee, and the fallback prefix must exist — without it,
// unmatched products would silently book to account 0.
func (c *Config) validateMappings() error {
	if len(c.PrefixMappings) == 0 {
		return fmt.Errorf("prefix_mappings is empty")
	}
	if _, ok := c.PrefixMappings[fallbackPrefix]; !ok {
		return fmt.Errorf("prefix_mappings must define the fallback prefix %q", fallbackPrefix)
	}
	if len(c.Committees) == 0 {
		return fmt.Errorf("committees is empty")
	}
	for prefix, pa := range c.PrefixMappings {
		if _, ok := c.Committees[pa.Name]; !ok {
			return fmt.Errorf("prefix_mappings[%q]: committee %q is not defined in committees", prefix, pa.Name)
		}
	}
	return nil
}
