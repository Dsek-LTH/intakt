package cli

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"testing"
)

func TestSplitError(t *testing.T) {
	cases := []struct {
		in         string
		wantLabel  string
		wantDetail string
	}{
		{"creating voucher: 400 Bad Request", "creating voucher", "400 Bad Request"},
		{"no colon here", "no colon here", ""},
		// A ": " inside brackets is not a wrap boundary.
		{`unknown source "x" (available: zettle, fees)`, `unknown source "x" (available: zettle, fees)`, ""},
		// Intentional multi-line messages split on the newline.
		{"no local cache found — run first:\n  intakt fetch 2025-01-01 2025-01-31",
			"no local cache found — run first:", "intakt fetch 2025-01-01 2025-01-31"},
	}
	for _, tc := range cases {
		label, detail := splitError(tc.in)
		if label != tc.wantLabel || detail != tc.wantDetail {
			t.Errorf("splitError(%q) = (%q, %q), want (%q, %q)",
				tc.in, label, detail, tc.wantLabel, tc.wantDetail)
		}
	}
}

func TestWrapWords(t *testing.T) {
	got := wrapWords("alpha beta gamma", 10)
	want := []string{"alpha beta", "gamma"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("wrapWords = %v, want %v", got, want)
	}

	// A single word wider than the width is hard-split, never dropped.
	long := strings.Repeat("x", 25)
	for _, line := range wrapWords(long, 10) {
		if len(line) > 10 {
			t.Errorf("line %q exceeds width 10", line)
		}
	}
	if joined := strings.Join(wrapWords(long, 10), ""); joined != long {
		t.Errorf("hard split lost characters: %q", joined)
	}

	// Explicit newlines start new lines.
	if got := wrapWords("a\nb", 10); len(got) != 2 {
		t.Errorf("wrapWords with newline = %v, want two lines", got)
	}
}

func TestReportedAndUsageErrorsUnwrap(t *testing.T) {
	base := errors.New("boom")

	r := reported(fmt.Errorf("wrapped: %w", base))
	if !IsReported(r) {
		t.Error("reported error not detected")
	}
	if !errors.Is(r, base) {
		t.Error("reported error must unwrap to its cause")
	}
	if reported(nil) != nil {
		t.Error("reported(nil) must stay nil")
	}
	if IsReported(base) {
		t.Error("plain error misdetected as reported")
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	u := usage(fs, base)
	var ue usageError
	if !errors.As(u, &ue) || !errors.Is(u, base) {
		t.Error("usage error must wrap its cause")
	}
	if usage(fs, nil) != nil {
		t.Error("usage(fs, nil) must stay nil")
	}
}

func TestParseDate(t *testing.T) {
	if _, err := parseDate("2025-01-15"); err != nil {
		t.Errorf("valid date rejected: %v", err)
	}
	for _, bad := range []string{"15/01/2025", "2025-13-01", "yesterday"} {
		if _, err := parseDate(bad); err == nil {
			t.Errorf("parseDate(%q) accepted invalid input", bad)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short string changed: %q", got)
	}
	if got := truncate("hello world", 8); got != "hello w…" {
		t.Errorf("truncate = %q, want %q", got, "hello w…")
	}
}
