package cache

import (
	"encoding/json"
	"fmt"
	"intakt/zettle"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir returns the on-disk directory used to cache fetched Zettle data.
func Dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating cache directory: %w", err)
	}
	return filepath.Join(base, "intakt"), nil
}

// MergeAndWrite loads all existing cache files for prefix, merges them with
// newItems (deduplicating by key), and writes a single file covering the union
// date range. Old files are deleted after the new one is safely written.
func MergeAndWrite[T any](dir, prefix string, start, end time.Time, newItems []T, key func(T) string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	unionStart := dateOnly(start)
	unionEnd := dateOnly(end)

	var existing []T
	var oldPaths []string

	if dirEntries, err := os.ReadDir(dir); err == nil {
		for _, e := range dirEntries {
			name := e.Name()
			if !strings.HasPrefix(name, prefix+"-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(name, prefix+"-"), ".json")
			parts := strings.SplitN(inner, "--", 2)
			if len(parts) != 2 {
				continue
			}
			fileStart, err1 := time.Parse("2006-01-02", parts[0])
			fileEnd, err2 := time.Parse("2006-01-02", parts[1])
			if err1 != nil || err2 != nil {
				continue
			}
			path := filepath.Join(dir, name)
			items, err := ReadFile[T](path)
			if err != nil {
				continue
			}
			existing = append(existing, items...)
			oldPaths = append(oldPaths, path)
			if fileStart.Before(unionStart) {
				unionStart = fileStart
			}
			if fileEnd.After(unionEnd) {
				unionEnd = fileEnd
			}
		}
	}

	// Merge: new items first so fresher data wins on duplicate keys.
	seen := make(map[string]struct{}, len(existing)+len(newItems))
	merged := make([]T, 0, len(existing)+len(newItems))
	for _, item := range append(newItems, existing...) {
		k := key(item)
		if _, dup := seen[k]; !dup {
			seen[k] = struct{}{}
			merged = append(merged, item)
		}
	}

	outName := prefix + "-" + unionStart.Format("2006-01-02") + "--" + unionEnd.Format("2006-01-02") + ".json"
	outPath := filepath.Join(dir, outName)
	b, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("encoding cache: %w", err)
	}
	if err := os.WriteFile(outPath, b, 0644); err != nil {
		return "", err
	}

	for _, p := range oldPaths {
		if p != outPath {
			_ = os.Remove(p)
		}
	}

	return outPath, nil
}

func ReadFile[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no cache found at %s", path)
	}
	if err != nil {
		return nil, err
	}
	var result []T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("corrupt cache at %s: %w", path, err)
	}
	return result, nil
}

// FindCovering scans dir for a cache file named "<prefix>-START--END.json"
// whose date range fully covers [start, end]. Returns the path if found.
func FindCovering(dir, prefix string, start, end time.Time) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	wantStart := dateOnly(start)
	wantEnd := dateOnly(end)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix+"-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(name, prefix+"-"), ".json")
		parts := strings.SplitN(inner, "--", 2)
		if len(parts) != 2 {
			continue
		}
		cacheStart, err1 := time.Parse("2006-01-02", parts[0])
		cacheEnd, err2 := time.Parse("2006-01-02", parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		if !cacheStart.After(wantStart) && !cacheEnd.Before(wantEnd) {
			return filepath.Join(dir, name), true
		}
	}
	return "", false
}

// FilterByDate keeps only items whose Zettle timestamp falls within [start, end].
func FilterByDate[T any](items []T, start, end time.Time, getTS func(T) string) []T {
	wantStart := dateOnly(start)
	wantEnd := dateOnly(end)
	var result []T
	for _, item := range items {
		d, ok := zettle.ParseDate(getTS(item))
		if !ok {
			continue
		}
		if !d.Before(wantStart) && !d.After(wantEnd) {
			result = append(result, item)
		}
	}
	return result
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
