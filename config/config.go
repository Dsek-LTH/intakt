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
	Reports string `yaml:"reports"`
	SIE     string `yaml:"sie"`
	JSON    string `yaml:"json"`
}

type SIE4Config struct {
	Enabled bool `yaml:"enabled"`

	OrganizationID string `yaml:"organization_id"`
	CompanyName    string `yaml:"company_name"`

	FiscalYearStart string `yaml:"fiscal_year_start"` // YYYY-MM-DD
	FiscalYearEnd   string `yaml:"fiscal_year_end"`   // YYYY-MM-DD

	ZettleAccount     string `yaml:"zettle_account"`
	ZettleAccountName string `yaml:"zettle_account_name"`

	SwishAccount     string `yaml:"swish_account"`
	SwishAccountName string `yaml:"swish_account_name"`

	ProgramName    string `yaml:"program_name"`
	ProgramVersion string `yaml:"program_version"`

	VoucherSeries      string `yaml:"voucher_series"`
	VoucherNumberStart int    `yaml:"voucher_number_start"`
	VoucherText        string `yaml:"voucher_text"`
}

type Config struct {
	OutPath              OutPath                  `yaml:"out_path"`
	Attestation          Attestation              `yaml:"attestation"`
	AccountDescriptions  map[string]string        `yaml:"account_descriptions"`
	PrefixMappings       map[string]PrefixAccount `yaml:"prefix_mappings"`
	Committees           map[string]CommitteeInfo `yaml:"committees"`
	SIE4                 SIE4Config               `yaml:"sie4"`
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

	if c.OutPath.JSON == "" {
		return fmt.Errorf("out_path.json is required")
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

	if err := c.validateSIE4(); err != nil {
		return err
	}

	return nil
}

func (c *Config) validateSIE4() error {
	if !c.SIE4.Enabled {
		return nil
	}

	if c.SIE4.OrganizationID == "" {
		return fmt.Errorf("sie4.organization_id is required when sie4.enabled is true")
	}

	if c.SIE4.CompanyName == "" {
		return fmt.Errorf("sie4.company_name is required when sie4.enabled is true")
	}

	if c.SIE4.FiscalYearStart == "" {
		return fmt.Errorf("sie4.fiscal_year_start is required when sie4.enabled is true")
	}

	if c.SIE4.FiscalYearEnd == "" {
		return fmt.Errorf("sie4.fiscal_year_end is required when sie4.enabled is true")
	}

	if c.SIE4.ZettleAccount == "" {
		return fmt.Errorf("sie4.zettle_account is required when sie4.enabled is true")
	}

	if c.SIE4.SwishAccount == "" {
		return fmt.Errorf("sie4.zettle_account_name is required when sie4.enabled is true")
	}

	if c.OutPath.SIE == "" {
		return fmt.Errorf("out_path.sie is required when sie4.enabled is true")
	}

	return nil
}
