package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ManifestEntry struct {
	Key        string  `yaml:"key"`
	Type       string  `yaml:"type"` // "purchases" or "fees"
	Date       string  `yaml:"date"`
	PDF        string  `yaml:"pdf"`
	Total      float64 `yaml:"total"` // in kronor
	CostCenter string  `yaml:"cost_center"`
}

// readManifest loads the manifest from path and returns it keyed by report key.
// Returns an error if the file does not exist, so the caller can prompt the user
// to run a compile step first.
func readManifest(path string) (map[string]ManifestEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s not found — run without --upload first to generate it", path)
	}
	if err != nil {
		return nil, err
	}

	var entries []ManifestEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	m := make(map[string]ManifestEntry, len(entries))
	for _, e := range entries {
		m[e.Key] = e
	}
	return m, nil
}

// writeManifest writes entries to path. If a manifest already exists, any
// non-empty cost_center values for matching keys are preserved so that a
// re-compile does not erase the user's edits.
func writeManifest(path string, entries []ManifestEntry) error {
	existing := loadExistingManifest(path)
	for i, e := range entries {
		if ex, ok := existing[e.Key]; ok && ex.CostCenter != "" {
			entries[i].CostCenter = ex.CostCenter
		}
	}

	data, err := yaml.Marshal(entries)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadExistingManifest(path string) map[string]ManifestEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]ManifestEntry{}
	}
	var entries []ManifestEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return map[string]ManifestEntry{}
	}
	m := make(map[string]ManifestEntry, len(entries))
	for _, e := range entries {
		m[e.Key] = e
	}
	return m
}

func manifestFilePath() string {
	return "pending.yaml"
}

// filterManifest returns only the entries whose Type matches the given source name.
// "zettle" matches type "purchases"; "fees" matches type "fees".
func filterManifest(manifest map[string]ManifestEntry, sourceName string) map[string]ManifestEntry {
	wantType := sourceNameToType(sourceName)
	filtered := make(map[string]ManifestEntry, len(manifest))
	for k, e := range manifest {
		if e.Type == wantType {
			filtered[k] = e
		}
	}
	return filtered
}

func sourceNameToType(sourceName string) string {
	switch sourceName {
	case "zettle":
		return "purchases"
	case "fees":
		return "fees"
	default:
		return sourceName
	}
}
