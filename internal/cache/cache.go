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

// dateFormat is the day-resolution format used in cache filenames.
const dateFormat = "2006-01-02"

// Dir returns the on-disk directory used to cache fetched Zettle data.
func Dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating cache directory: %w", err)
	}
	return filepath.Join(base, "intakt"), nil
}

// rangeFileName builds the canonical cache filename for prefix covering the
// inclusive day range [start, end]: "<prefix>-START--END.json".
func rangeFileName(prefix string, start, end time.Time) string {
	return prefix + "-" + start.Format(dateFormat) + "--" + end.Format(dateFormat) + ".json"
}

// parseRangeFileName is the inverse of rangeFileName. It reports ok=false for
// filenames that don't belong to prefix or don't follow the convention.
func parseRangeFileName(name, prefix string) (start, end time.Time, ok bool) {
	if !strings.HasPrefix(name, prefix+"-") || !strings.HasSuffix(name, ".json") {
		return time.Time{}, time.Time{}, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(name, prefix+"-"), ".json")
	parts := strings.SplitN(inner, "--", 2)
	if len(parts) != 2 {
		return time.Time{}, time.Time{}, false
	}
	start, err1 := time.Parse(dateFormat, parts[0])
	end, err2 := time.Parse(dateFormat, parts[1])
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// existingCache is the merged content and provenance of all cache files found
// for one prefix.
type existingCache[T any] struct {
	items      []T
	paths      []string  // files the items came from, removed after a rewrite
	start, end time.Time // union of the files' date ranges
}

// loadExisting reads every cache file for prefix in dir, starting from the
// seed range [start, end] and widening it to the union of all files found.
// Unreadable or misnamed files are skipped rather than failing the merge.
func loadExisting[T any](dir, prefix string, start, end time.Time) existingCache[T] {
	ex := existingCache[T]{start: start, end: end}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return ex
	}
	for _, e := range dirEntries {
		fileStart, fileEnd, ok := parseRangeFileName(e.Name(), prefix)
		if !ok {
			continue
		}
		path := filepath.Join(dir, e.Name())
		items, err := ReadFile[T](path)
		if err != nil {
			continue
		}
		ex.items = append(ex.items, items...)
		ex.paths = append(ex.paths, path)
		if fileStart.Before(ex.start) {
			ex.start = fileStart
		}
		if fileEnd.After(ex.end) {
			ex.end = fileEnd
		}
	}
	return ex
}

// dedupeByKey keeps the first occurrence of each key, preserving order.
func dedupeByKey[T any](items []T, key func(T) string) []T {
	seen := make(map[string]struct{}, len(items))
	result := make([]T, 0, len(items))
	for _, item := range items {
		k := key(item)
		if _, dup := seen[k]; !dup {
			seen[k] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

// MergeAndWrite loads all existing cache files for prefix, merges them with
// newItems (deduplicating by key), and writes a single file covering the union
// date range. Old files are deleted after the new one is safely written.
func MergeAndWrite[T any](dir, prefix string, start, end time.Time, newItems []T, key func(T) string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	existing := loadExisting[T](dir, prefix, dateOnly(start), dateOnly(end))
	// New items first so fresher data wins on duplicate keys.
	merged := dedupeByKey(append(newItems, existing.items...), key)

	outPath := filepath.Join(dir, rangeFileName(prefix, existing.start, existing.end))
	b, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("encoding cache: %w", err)
	}
	if err := os.WriteFile(outPath, b, 0644); err != nil {
		return "", err
	}

	for _, p := range existing.paths {
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
		cacheStart, cacheEnd, ok := parseRangeFileName(e.Name(), prefix)
		if !ok {
			continue
		}
		if !cacheStart.After(wantStart) && !cacheEnd.Before(wantEnd) {
			return filepath.Join(dir, e.Name()), true
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
