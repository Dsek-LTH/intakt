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
	"strings"
	"time"
)

func feePrefixAccount(cfg *config.Config) config.PrefixAccount {
	if pa, ok := cfg.PrefixMappings["z"]; ok {
		return pa
	}

	return cfg.PrefixMappings["sk"]
}

func feeAccountDistributions(cfg *config.Config, fees []FeeEntry) []AccountDistribution {
	distributionsByAccount := make(map[string]int)

	for _, fee := range fees {
		distributionsByAccount[fee.Account] += fee.Total
	}

	distributions := make([]AccountDistribution, 0, len(distributionsByAccount))

	for account, total := range distributionsByAccount {
		distributions = append(distributions, AccountDistribution{
			Account:     account,
			Description: accountDescription(cfg, account),
			Total:       total,
		})
	}

	slices.SortFunc(distributions, func(a, b AccountDistribution) int {
		return strings.Compare(a.Account, b.Account)
	})

	return distributions
}

func feeDistributionRowsToTex(distributions []AccountDistribution) []string {
	rows := make([]string, 0, len(distributions))

	for _, d := range distributions {
		rows = append(rows, distributionToTex(d))
	}

	return rows
}

func feeEntryToTex(e FeeEntry) string {
	return fmt.Sprintf(
		"%s & %s & %s",
		latexEscape("Zettleavgift"),
		latexEscape(e.Account),
		crowns(e.Total),
	)
}

func feeRowsToTex(fees []FeeEntry) []string {
	total := 0

	for _, fee := range fees {
		total += fee.Total
	}

	if len(fees) == 0 {
		return nil
	}

	account := fees[0].Account
	for _, fee := range fees[1:] {
		if fee.Account != account {
			account = ""
			break
		}
	}

	return []string{
		fmt.Sprintf(
			"%s & %s & %s",
			latexEscape("Zettleavgifter"),
			latexEscape(account),
			crowns(total),
		),
	}
}

func writeFeeTexCommands(w io.Writer, cfg *config.Config, report *FeeReport) {
	pa := feePrefixAccount(cfg)
	committee := cfg.Committees[pa.Name]

	distributionRows := feeDistributionRowsToTex(feeAccountDistributions(cfg, report.Fees))
	feeRows := feeRowsToTex(report.Fees)

	fmt.Fprintf(w, "\\newcommand{\\skapad}{%s}\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(w, "\\newcommand{\\kostnadsstalle}{%s}\n", latexEscape(committee.ProperName))
	fmt.Fprintf(w, "\\newcommand{\\typ}{%s}\n", "Zettle")
	fmt.Fprintf(w, "\\newcommand{\\datum}{%s}\n", latexEscape(report.Date))
	fmt.Fprintf(w, "\\newcommand{\\summa}{%s}\n", latexEscape(crowns(report.Total)+" kr"))
	fmt.Fprintf(w, "\\newcommand{\\namn}{%s}\n", latexEscape(cfg.Attestation.Name))
	fmt.Fprintf(w, "\\newcommand{\\stilid}{%s}\n", latexEscape(cfg.Attestation.StilID))
	fmt.Fprintf(w, "\\newcommand{\\beskrivning}{%s}\n", latexEscape(committee.Description))
	fmt.Fprintf(w, "\\newcommand{\\fordelning}{%s}\n", strings.Join(distributionRows, "\\\\"))
	fmt.Fprintf(w, "\\newcommand{\\varor}{%s}\n", strings.Join(feeRows, "\\\\"))
}

func writeFeeTexFile(cfg *config.Config, report *FeeReport, skeletonPath, texFilePath string) error {
	skeleton, err := os.ReadFile(skeletonPath)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	writeFeeTexCommands(&buf, cfg, report)
	buf.Write(skeleton)

	return os.WriteFile(texFilePath, buf.Bytes(), 0644)
}

func compileFeeReport(cfg *config.Config, assetsDir string, outputBase string, report *FeeReport) error {
	texFilename := outputBase + ".tex"
	skeletonPath := filepath.Join(assetsDir, "avgift.tex")
	texFilePath := filepath.Join(assetsDir, texFilename)

	if err := writeFeeTexFile(cfg, report, skeletonPath, texFilePath); err != nil {
		return fmt.Errorf("writing fee tex file: %w", err)
	}

	cmd := exec.Command("pdflatex", texFilename)
	cmd.Dir = assetsDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdflatex fee report: %w\n%s", err, out)
	}

	outputDir := "output"

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	src := filepath.Join(assetsDir, outputBase+".pdf")
	dst := filepath.Join(outputDir, outputBase+".pdf")

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("moving fee pdf: %w", err)
	}

	for _, ext := range []string{".tex", ".aux", ".log"} {
		path := filepath.Join(assetsDir, outputBase+ext)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("could not remove %s: %v", path, err)
		}
	}

	return nil
}

// CompileFees renders every fee report to a PDF via pdflatex.
// Errors from individual reports are logged and do not abort the others.
func CompileFees(cfg *config.Config, assetsDir string, reports map[string]*FeeReport) {
	for key, report := range reports {
		outputBase := "avgift-" + key

		if err := compileFeeReport(cfg, assetsDir, outputBase, report); err != nil {
			log.Printf("compiling fee report %s: %v", key, err)
		}
	}
}
