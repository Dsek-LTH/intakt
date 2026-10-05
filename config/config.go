package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// The json tags beside the yaml ones are not decoration. Under the CLI this
// struct is read from config.yaml; under the service it arrives in the request
// body, because the caller holds the routing table in its database and there is
// no file on this side to keep in step (server/api.go). Go would otherwise
// match `prefix_mappings` against `PrefixMappings` and fail — an underscore is
// not case — so the config would decode to an empty one and every product would
// route to nothing.
//
// The two spellings are the same string throughout, which is what lets a
// config.yaml and a request body be the same shape.

type Attestation struct {
	Name   string `yaml:"name" json:"name"`
	StilID string `yaml:"stil_id" json:"stil_id"`
}

type PrefixAccount struct {
	Name    string `yaml:"name" json:"name"`
	Account int    `yaml:"account" json:"account"`
}

type CommitteeInfo struct {
	ProperName        string `yaml:"proper_name" json:"proper_name"`
	Description       string `yaml:"description" json:"description"`
	DefaultCostCenter string `yaml:"default_cost_center" json:"default_cost_center"`
}

type OutPath struct {
	Reports  string `yaml:"reports" json:"reports"`
	Vouchers string `yaml:"vouchers" json:"vouchers"`
}

type FortnoxConfig struct {
	ZettleAccount string `yaml:"zettle_account" json:"zettle_account"`
	SwishAccount  string `yaml:"swish_account" json:"swish_account"`
	VoucherSeries string `yaml:"voucher_series" json:"voucher_series"`
}

type Config struct {
	// Where the CLI writes what it compiles. The service writes nothing — it
	// answers with bytes — so a request body leaves this empty.
	OutPath             OutPath                  `yaml:"out_path" json:"out_path"`
	Attestation         Attestation              `yaml:"attestation" json:"attestation"`
	AccountDescriptions map[string]string        `yaml:"account_descriptions" json:"account_descriptions"`
	PrefixMappings      map[string]PrefixAccount `yaml:"prefix_mappings" json:"prefix_mappings"`
	Committees          map[string]CommitteeInfo `yaml:"committees" json:"committees"`
	// Only the CLI uploads, so only the CLI needs a series to upload into.
	Fortnox              FortnoxConfig  `yaml:"fortnox" json:"fortnox"`
	SwishAmountFallbacks map[int]string `yaml:"swish_amount_fallbacks" json:"swish_amount_fallbacks"`
}

// fallbackPrefix routes products whose name matches no configured prefix; its
// mapping must exist (enforced by Validate). feePrefix, when configured, books
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

// Validate checks what any config must have, however it arrived: who attests,
// something to route with, and somewhere for each route to land.
//
// Split out from validate() below because the service is handed a config in a
// request body rather than reading config.yaml, and a body carries none of the
// file-only settings — it writes nothing, so it has no out_path, and it never
// uploads, so it has no series. Holding a request to those would refuse every
// one of them.
func (c *Config) Validate() error {
	if err := requireFields(
		field{c.Attestation.Name, "attestation.name"},
		field{c.Attestation.StilID, "attestation.stil_id"},
	); err != nil {
		return err
	}
	return c.validateMappings()
}

// validate is Validate plus what only a config.yaml has to answer for: where
// the CLI writes what it compiles, and which accounts and series it uploads to.
func (c *Config) validate() error {
	if err := requireFields(
		field{c.OutPath.Reports, "out_path.reports"},
		field{c.OutPath.Vouchers, "out_path.vouchers"},
		field{c.Fortnox.ZettleAccount, "fortnox.zettle_account"},
		field{c.Fortnox.SwishAccount, "fortnox.swish_account"},
		field{c.Fortnox.VoucherSeries, "fortnox.voucher_series"},
	); err != nil {
		return err
	}
	return c.Validate()
}

type field struct{ value, key string }

// requireFields reports the first field left empty.
func requireFields(fields ...field) error {
	for _, f := range fields {
		if f.value == "" {
			return fmt.Errorf("%s is required", f.key)
		}
	}
	return nil
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
