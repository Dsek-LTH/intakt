package config

import (
	"encoding/json"
	"testing"
)

// The service decodes a Config from a request body that the caller built from
// its own database. Nothing checks that the two agree at compile time, so the
// round trip is worth asserting on directly — and one field in particular:
// SwishAmountFallbacks is keyed by int, which JSON cannot express. encoding/json
// renders those keys as strings and parses them back, but that is a property of
// the library rather than of anything visible here, and a silent failure would
// route unlabelled Swish payments to the wrong committee.
func TestConfigJSONRoundTrip(t *testing.T) {
	original := Config{
		Attestation: Attestation{Name: "Test Testsson", StilID: "te0000st-s"},
		AccountDescriptions: map[string]string{
			"3000": "Försäljning inom Sverige",
			"3011": "Öl",
		},
		PrefixMappings: map[string]PrefixAccount{
			"c":  {Name: "cafe", Account: 3000},
			"s1": {Name: "sex", Account: 3011},
		},
		Committees: map[string]CommitteeInfo{
			"cafe": {ProperName: "Cafémästeriet", Description: "Cafémästeriets verksamhet", DefaultCostCenter: "CAFE02"},
			"sex":  {ProperName: "Sexmästeriet", Description: "Sexmästeriets verksamhet"},
		},
		SwishAmountFallbacks: map[int]string{
			1300: "c-Cola",
			2200: "c-Monster",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshalling config: %v", err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshalling config: %v", err)
	}

	if err := decoded.Validate(); err != nil {
		t.Errorf("round-tripped config failed validation: %v", err)
	}

	if decoded.Attestation != original.Attestation {
		t.Errorf("attestation = %+v, want %+v", decoded.Attestation, original.Attestation)
	}

	for amount, want := range original.SwishAmountFallbacks {
		got, ok := decoded.SwishAmountFallbacks[amount]
		if !ok {
			t.Errorf("swish fallback for %d öre did not survive the round trip", amount)
			continue
		}
		if got != want {
			t.Errorf("swish fallback for %d öre = %q, want %q", amount, got, want)
		}
	}

	for prefix, want := range original.PrefixMappings {
		got, ok := decoded.PrefixMappings[prefix]
		if !ok {
			t.Errorf("prefix %q did not survive the round trip", prefix)
			continue
		}
		if got != want {
			t.Errorf("prefix %q = %+v, want %+v", prefix, got, want)
		}
	}

	for key, want := range original.Committees {
		got, ok := decoded.Committees[key]
		if !ok {
			t.Errorf("committee %q did not survive the round trip", key)
			continue
		}
		if got != want {
			t.Errorf("committee %q = %+v, want %+v", key, got, want)
		}
	}
}

// Validate is what stands between a malformed request body and a voucher booked
// against the wrong account, so each rule it enforces is worth a case.
func TestValidate(t *testing.T) {
	valid := func() Config {
		return Config{
			Attestation:    Attestation{Name: "Test Testsson", StilID: "te0000st-s"},
			PrefixMappings: map[string]PrefixAccount{"c": {Name: "cafe", Account: 3000}},
			Committees:     map[string]CommitteeInfo{"cafe": {ProperName: "Cafémästeriet"}},
		}
	}

	baseline := valid()
	if err := baseline.Validate(); err != nil {
		t.Fatalf("the valid fixture does not validate: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"no attestation name", func(c *Config) { c.Attestation.Name = "" }},
		{"no attestation stil id", func(c *Config) { c.Attestation.StilID = "" }},
		{"no prefix mappings", func(c *Config) { c.PrefixMappings = nil }},
		{"no committees", func(c *Config) { c.Committees = nil }},
		{
			// The one rule that is about the relationship between two fields
			// rather than about a field being present: a prefix routing to a
			// committee that does not exist would panic on the zero value at
			// render time, well after the request was accepted.
			"prefix routes to an undefined committee",
			func(c *Config) { c.PrefixMappings["c"] = PrefixAccount{Name: "nosuch", Account: 3000} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Error("expected a validation error, got nil")
			}
		})
	}
}

// Load applies validate(), which is Validate plus what only a config.yaml has
// to answer for: where the CLI writes, and which series it uploads into. A
// request body carries none of that, so a config with no out_path is exactly
// the case that must pass Validate and fail validate.
func TestLoadRequiresOutPathButValidateDoesNot(t *testing.T) {
	cfg := Config{
		Attestation:    Attestation{Name: "Test Testsson", StilID: "te0000st-s"},
		PrefixMappings: map[string]PrefixAccount{"c": {Name: "cafe", Account: 3000}},
		Committees:     map[string]CommitteeInfo{"cafe": {ProperName: "Cafémästeriet"}},
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate rejected a config with no out_path: %v", err)
	}
	if err := cfg.validate(); err == nil {
		t.Error("validate accepted a config with no out_path")
	}
}
