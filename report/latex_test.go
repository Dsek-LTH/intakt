package report

import (
	"bytes"
	"intakt/config"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// The assets shipped with the binary. Tests render against the real skeletons
// rather than a fixture, because the skeletons are the thing most likely to
// break — they reference D-symbol.pdf and sig.png, and whether those resolve is
// exactly what the TEXINPUTS change is responsible for.
const assetsDir = "../assets"

// pdflatex is a system dependency, present in the service image and on the
// treasurer's machine but not necessarily in every CI runner. Skipping is
// honest here in a way it would not be for the parsing tests: there is nothing
// this file can assert without it.
func requirePdflatex(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pdflatex"); err != nil {
		t.Skip("pdflatex is not installed; skipping render tests")
	}
}

// A report with two accounts, a variant, and a discount — enough that the
// distribution table, the longtable and the escaping all have something to do.
func sampleReport() *Report {
	return &Report{
		UtskottPrefix: "cafe",
		Date:          "2026-01-02",
		Total:         27500,
		Products: []Entry{
			{Name: "Cola", Account: "3000", Quantity: 10, UnitPrice: 1300, Total: 13000},
			{Name: "Läsk", Variant: "Stor", Account: "3000", Quantity: 5, UnitPrice: 1500, EffectiveDiscount: 500, Total: 7000},
			{Name: "Kaffe & bulle", Account: "3010", Quantity: 5, UnitPrice: 1500, Total: 7500},
		},
		AccountDistributions: []AccountDistribution{
			{Account: "3000", Description: "Försäljning inom Sverige", Total: 20000},
			{Account: "3010", Description: "Livsmedelsförsäljning", Total: 7500},
		},
	}
}

func renderTestConfig(t *testing.T) *config.Config {
	t.Helper()
	return loadTestConfig(t)
}

func assertIsPDF(t *testing.T, pdf []byte) {
	t.Helper()
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("output does not start with the PDF magic bytes (got %q)", pdf[:min(8, len(pdf))])
	}
	// A one-page PDF of this document is comfortably over 10 kB; anything
	// dramatically smaller means pdflatex produced a stub.
	if len(pdf) < 5000 {
		t.Errorf("PDF is only %d bytes, which is too small to be the rendered report", len(pdf))
	}
}

func TestRenderReportProducesPDF(t *testing.T) {
	requirePdflatex(t)

	pdf, err := RenderReport(renderTestConfig(t), assetsDir, sampleReport(), "Swish")
	if err != nil {
		t.Fatalf("RenderReport: %v", err)
	}
	assertIsPDF(t, pdf)
}

func TestRenderFeeReportProducesPDF(t *testing.T) {
	requirePdflatex(t)

	fees := &FeeReport{
		Date:  "2026-01-02",
		Total: 4200,
		Fees: []FeeEntry{
			{Name: "Zettle fee", Account: "6570", Quantity: 7, UnitPrice: 600, Total: 4200},
		},
	}

	pdf, err := RenderFeeReport(renderTestConfig(t), assetsDir, fees)
	if err != nil {
		t.Fatalf("RenderFeeReport: %v", err)
	}
	assertIsPDF(t, pdf)
}

func dirSnapshot(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// The regression this refactor exists for. Rendering used to write <key>.tex
// into the assets directory, compile it there, and delete the leftovers
// afterwards — so a render mutated a directory that should be read-only, and a
// failed render left .aux and .log files behind.
func TestRenderLeavesAssetsDirUntouched(t *testing.T) {
	requirePdflatex(t)

	before := dirSnapshot(t, assetsDir)

	if _, err := RenderReport(renderTestConfig(t), assetsDir, sampleReport(), "Swish"); err != nil {
		t.Fatalf("RenderReport: %v", err)
	}

	after := dirSnapshot(t, assetsDir)
	if len(before) != len(after) {
		t.Fatalf("assets directory changed: before %v, after %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("assets directory changed: before %v, after %v", before, after)
			break
		}
	}
}

// Two renders of the same report at the same time is the ordinary case for a
// service and was impossible before: both wrote the same filenames into the
// same directory, so one clobbered the other's .tex mid-compile.
func TestRenderConcurrently(t *testing.T) {
	requirePdflatex(t)

	cfg := renderTestConfig(t)
	const n = 6

	var wg sync.WaitGroup
	pdfs := make([][]byte, n)
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pdfs[i], errs[i] = RenderReport(cfg, assetsDir, sampleReport(), "Swish")
		}()
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Errorf("concurrent render %d failed: %v", i, errs[i])
			continue
		}
		assertIsPDF(t, pdfs[i])
	}
}

// Nothing should be left in the system temp directory once a render returns,
// successfully or otherwise. A service rendering a few hundred reports a term
// that leaked a directory each time would be a slow disk leak nobody looks for.
func TestRenderCleansUpTempDirs(t *testing.T) {
	requirePdflatex(t)

	pattern := filepath.Join(os.TempDir(), "intakt-render-*")
	before, _ := filepath.Glob(pattern)

	if _, err := RenderReport(renderTestConfig(t), assetsDir, sampleReport(), "Swish"); err != nil {
		t.Fatalf("RenderReport: %v", err)
	}

	after, _ := filepath.Glob(pattern)
	if len(after) != len(before) {
		t.Errorf("render left %d temp directories behind", len(after)-len(before))
	}
}

// A broken skeleton has to come back as an error rather than as a hung request.
// Without -interaction=nonstopmode this test would not fail — it would never
// finish, because pdflatex would sit waiting on stdin.
func TestRenderFailsOnBrokenSkeleton(t *testing.T) {
	requirePdflatex(t)

	broken := t.TempDir()
	skeleton := "\\documentclass{article}\n\\begin{document}\n\\thisisnotacommand\n\\end{document}\n"
	if err := os.WriteFile(filepath.Join(broken, "broken.tex"), []byte(skeleton), 0o644); err != nil {
		t.Fatalf("writing broken skeleton: %v", err)
	}

	_, err := renderTeX(broken, texData{Skeleton: "broken.tex"})
	if err == nil {
		t.Fatal("expected an error from a skeleton with an undefined control sequence")
	}
}

func TestRenderFailsOnMissingSkeleton(t *testing.T) {
	_, err := renderTeX(assetsDir, texData{Skeleton: "nosuch.tex"})
	if err == nil {
		t.Fatal("expected an error for a skeleton that does not exist")
	}
}
