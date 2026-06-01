package report

import (
	"bytes"
	"fmt"
	"intakt/config"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func latexEscape(s string) string {
	var latexEscaper = strings.NewReplacer(
		`\`, `\textbackslash{}`,
		`&`, `\&`,
		`%`, `\%`,
		`$`, `\$`,
		`#`, `\#`,
		`_`, `\_`,
		`{`, `\{`,
		`}`, `\}`,
		`~`, `\textasciitilde{}`,
		`^`, `\textasciicircum{}`,
	)
	return latexEscaper.Replace(s)
}
func writeTexCommands(w io.Writer, cfg *config.Config, report *Report) {
	committee := cfg.Committees[report.UtskottPrefix]

	distributionRows := make([]string, 0, len(report.AccountDistributions))
	for _, d := range report.AccountDistributions {
		distributionRows = append(distributionRows, distributionToTex(d))
	}
	slices.Sort(distributionRows)

	productRows := productRowsToTex(report.Products)

	fmt.Fprintf(w, "\\newcommand{\\skapad}{%s}\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(w, "\\newcommand{\\kostnadsstalle}{%s}\n", latexEscape(committee.ProperName))
	fmt.Fprintf(w, "\\newcommand{\\typ}{%s}\n", "Zettle")
	fmt.Fprintf(w, "\\newcommand{\\datum}{%s}\n", latexEscape(report.Date))
	fmt.Fprintf(w, "\\newcommand{\\summa}{%s}\n", latexEscape(crowns(report.Total)+" kr"))
	fmt.Fprintf(w, "\\newcommand{\\namn}{%s}\n", latexEscape(cfg.Attestation.Name))
	fmt.Fprintf(w, "\\newcommand{\\stilid}{%s}\n", latexEscape(cfg.Attestation.StilID))
	fmt.Fprintf(w, "\\newcommand{\\beskrivning}{%s}\n", latexEscape(committee.Description))
	fmt.Fprintf(w, "\\newcommand{\\fordelning}{%s}\n", strings.Join(distributionRows, "\\\\"))
	fmt.Fprintf(w, "\\newcommand{\\varor}{%s}\n", strings.Join(productRows, "\\\\"))
}

func crowns(amount int) string {
	return strconv.FormatFloat(float64(amount)/100.0, 'f', 2, 64)
}

func hasMultipleVariants(entries []Entry) bool {
	variants := make(map[string]bool)

	for _, entry := range entries {
		variant := strings.TrimSpace(entry.Variant)
		if variant == "" {
			continue
		}

		variants[strings.ToLower(variant)] = true
	}

	return len(variants) > 0
}

func entryToTex(e Entry) string {
	return entryToTexWithName(e, latexEscape(e.displayName()))
}

func entryToVariantTex(e Entry) string {
	variant := strings.TrimSpace(e.Variant)

	if variant == "" {
		return entryToTex(e)
	}

	return entryToTexWithName(e, `\quad \variantpil\ `+latexEscape(variant))
}

func (e Entry) displayName() string {
	variant := strings.TrimSpace(e.Variant)

	if variant == "" {
		return e.Name
	}

	return latexEscape(e.Name + " " + variant)
}

func entryToTexWithName(e Entry, name string) string {
	return fmt.Sprintf(
		"%s & %s & %d & %s & %s & %s",
		name,
		e.Account,
		e.Quantity,
		crowns(e.UnitPrice),
		crowns(e.EffectiveDiscount),
		crowns(e.Total),
	)
}

func distributionToTex(d AccountDistribution) string {
	return fmt.Sprintf(
		"%s & %s & %s",
		d.Account,
		latexEscape(d.Description),
		crowns(d.Total),
	)
}

func productRowsToTex(products []Entry) []string {
	productsByName := make(map[string][]Entry)

	for _, product := range products {
		name := strings.TrimSpace(product.Name)
		productsByName[name] = append(productsByName[name], product)
	}

	names := make([]string, 0, len(productsByName))
	for name := range productsByName {
		names = append(names, name)
	}
	slices.Sort(names)

	rows := make([]string, 0, len(products))

	for _, name := range names {
		entries := productsByName[name]

		slices.SortFunc(entries, func(a, b Entry) int {
			if c := strings.Compare(strings.ToLower(a.Variant), strings.ToLower(b.Variant)); c != 0 {
				return c
			}

			if c := strings.Compare(a.Account, b.Account); c != 0 {
				return c
			}

			return a.UnitPrice - b.UnitPrice
		})

		if hasMultipleVariants(entries) {
			rows = append(rows, fmt.Sprintf(`\textbf{%s:} & & & & &`, latexEscape(name)))

			for _, entry := range entries {
				rows = append(rows, entryToVariantTex(entry))
			}

			continue
		}

		for _, entry := range entries {
			rows = append(rows, entryToTex(entry))
		}
	}

	return rows
}

func writeTexFile(cfg *config.Config, report *Report, skeletonPath, texFilePath string) error {
	skeleton, err := os.ReadFile(skeletonPath)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	writeTexCommands(&buf, cfg, report)
	buf.Write(skeleton)

	return os.WriteFile(texFilePath, buf.Bytes(), 0644)
}

func compileReport(cfg *config.Config, assetsDir string, outputBase string, report *Report) error {
	// Write the .tex file into the assets directory so pdflatex can find
	// intakt.tex, D-symbol.pdf, and sig.png via relative paths.
	texFilename := outputBase + ".tex"
	skeletonPath := filepath.Join(assetsDir, "intakt.tex")
	texFilePath := filepath.Join(assetsDir, texFilename)

	if err := writeTexFile(cfg, report, skeletonPath, texFilePath); err != nil {
		return fmt.Errorf("writing tex file: %w", err)
	}

	// Run pdflatex from the assets directory.
	cmd := exec.Command("pdflatex", texFilename)
	cmd.Dir = assetsDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdflatex: %w\n%s", err, out)
	}

	// Move the finished PDF up to the working directory.
	outputDir := "output"

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	src := filepath.Join(assetsDir, outputBase+".pdf")
	dst := filepath.Join(outputDir, outputBase+".pdf")

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("moving pdf: %w", err)
	}

	for _, ext := range []string{".tex", ".aux", ".log"} {
		path := filepath.Join(assetsDir, outputBase+ext)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("could not remove %s: %v", path, err)
		}
	}

	return nil
}

// Compile renders every report to a PDF via pdflatex.
// Errors from individual reports are logged and do not abort the others.
func Compile(cfg *config.Config, assetsDir string, reports map[string]*Report) {
	for key, report := range reports {
		if err := compileReport(cfg, assetsDir, key, report); err != nil {
			log.Printf("compiling %s: %v", key, err)
		}
	}
}
