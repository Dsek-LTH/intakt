package report

import (
	"regexp"
	"testing"
)

func TestDeconstructSplitsPrefixAndName(t *testing.T) {
	re := regexp.MustCompile(buildRegexFromValidPrefixes(testConfig()))
	cases := []struct {
		in         string
		wantPrefix string
		wantName   string
	}{
		{"SK - Godis", "sk", "Godis"},
		{"sk – Godis", "sk", "Godis"}, // en dash separator, lower case
		{"CA Kaffe", "ca", "Kaffe"},   // space separator
		{"No prefix", "", "No prefix"},
		{"  SK - padded  ", "sk", "padded"},
	}
	for _, tc := range cases {
		prefix, name := deconstruct(re, tc.in)
		if prefix != tc.wantPrefix || name != tc.wantName {
			t.Errorf("deconstruct(%q) = (%q, %q), want (%q, %q)",
				tc.in, prefix, name, tc.wantPrefix, tc.wantName)
		}
	}
}

func TestLatexEscape(t *testing.T) {
	got := latexEscape(`50% & $2 #1 _x {y} ~z`)
	want := `50\% \& \$2 \#1 \_x \{y\} \textasciitilde{}z`
	if got != want {
		t.Errorf("latexEscape = %q, want %q", got, want)
	}
}

func TestCrownsFormatsOreAsKronor(t *testing.T) {
	cases := []struct {
		öre  int
		want string
	}{
		{12345, "123.45"},
		{100, "1.00"},
		{-250, "-2.50"},
	}
	for _, tc := range cases {
		if got := crowns(tc.öre); got != tc.want {
			t.Errorf("crowns(%d) = %q, want %q", tc.öre, got, tc.want)
		}
	}
}
