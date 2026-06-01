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
	ProperName  string `yaml:"proper_name"`
	Description string `yaml:"description"`
}

type Config struct {
	AssetsDir           string                   `yaml:"assets_dir"`
	Attestation         Attestation              `yaml:"attestation"`
	AccountDescriptions map[string]string        `yaml:"account_descriptions"`
	PrefixMappings      map[string]PrefixAccount `yaml:"prefix_mappings"`
	Committees          map[string]CommitteeInfo `yaml:"committees"`
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

	// Ensure every prefix mapping points to a known committee.
	for prefix, pa := range c.PrefixMappings {
		if _, ok := c.Committees[pa.Name]; !ok {
			return fmt.Errorf("prefix_mappings[%q]: committee %q is not defined in committees", prefix, pa.Name)
		}
	}

	return nil
}
