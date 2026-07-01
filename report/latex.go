package report

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

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

func latexEscape(s string) string {
	return latexEscaper.Replace(s)
}

func crowns(amount int) string {
	return strconv.FormatFloat(float64(amount)/100.0, 'f', 2, 64)
}

func distributionToTex(d AccountDistribution) string {
	return fmt.Sprintf("%s & %s & %s", d.Account, latexEscape(d.Description), crowns(d.Total))
}

func distributionRowsToTex(distributions []AccountDistribution) []string {
	rows := make([]string, 0, len(distributions))
	for _, d := range distributions {
		rows = append(rows, distributionToTex(d))
	}
	return rows
}

// texData holds the rendered values for every \newcommand in the .tex skeleton.
// Both report types produce one of these; compileTeX turns it into a PDF.
type texData struct {
	Skeleton     string // skeleton .tex filename relative to assetsDir
	OutputBase   string // output filename without extension
	Created      string
	CostCenter   string
	Type         string
	Date         string
	Total        string
	Name         string
	StilID       string
	Description  string
	Distribution string
	Items        string
}

// compileTeX writes a .tex file from d and the named skeleton, runs pdflatex,
// moves the resulting PDF to outputDir, and removes build artefacts.
func compileTeX(assetsDir, outputDir string, d texData) error {
	skeleton, err := os.ReadFile(filepath.Join(assetsDir, d.Skeleton))
	if err != nil {
		return fmt.Errorf("reading skeleton: %w", err)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\\newcommand{\\skapad}{%s}\n", d.Created)
	fmt.Fprintf(&buf, "\\newcommand{\\verksamhet}{%s}\n", d.CostCenter)
	fmt.Fprintf(&buf, "\\newcommand{\\typ}{%s}\n", d.Type)
	fmt.Fprintf(&buf, "\\newcommand{\\datum}{%s}\n", d.Date)
	fmt.Fprintf(&buf, "\\newcommand{\\summa}{%s}\n", d.Total)
	fmt.Fprintf(&buf, "\\newcommand{\\namn}{%s}\n", d.Name)
	fmt.Fprintf(&buf, "\\newcommand{\\stilid}{%s}\n", d.StilID)
	fmt.Fprintf(&buf, "\\newcommand{\\beskrivning}{%s}\n", d.Description)
	fmt.Fprintf(&buf, "\\newcommand{\\fordelning}{%s}\n", d.Distribution)
	fmt.Fprintf(&buf, "\\newcommand{\\varor}{%s}\n", d.Items)
	buf.Write(skeleton)

	texPath := filepath.Join(assetsDir, d.OutputBase+".tex")
	if err := os.WriteFile(texPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing tex file: %w", err)
	}

	return runPdflatex(assetsDir, d.OutputBase, outputDir)
}

func runPdflatex(assetsDir, outputBase, outputDir string) error {
	cmd := exec.Command("pdflatex", "-interaction=nonstopmode", "-halt-on-error", outputBase+".tex")
	cmd.Dir = assetsDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdflatex: %w\n%s", err, out)
	}

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
