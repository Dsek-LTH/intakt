package report

import (
	"errors"
	"fmt"
	"intakt/config"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func reportToTexData(cfg *config.Config, key string, r *Report) texData {
	committee := cfg.Committees[r.UtskottPrefix]

	distributionRows := distributionRowsToTex(r.AccountDistributions)
	slices.Sort(distributionRows)

	return texData{
		Skeleton:     "intakt.tex",
		OutputBase:   key,
		Created:      time.Now().Format("2006-01-02"),
		CostCenter:   latexEscape(committee.ProperName),
		Type:         "Zettle",
		Date:         latexEscape(r.Date),
		Total:        latexEscape(crowns(r.Total) + " kr"),
		Name:         latexEscape(cfg.Attestation.Name),
		StilID:       latexEscape(cfg.Attestation.StilID),
		Description:  latexEscape(committee.Description),
		Distribution: strings.Join(distributionRows, "\\\\"),
		Items:        strings.Join(productRowsToTex(r.Products), "\\\\"),
	}
}

func hasMultipleVariants(entries []Entry) bool {
	variants := make(map[string]bool)
	for _, entry := range entries {
		if v := strings.TrimSpace(entry.Variant); v != "" {
			variants[strings.ToLower(v)] = true
		}
	}
	return len(variants) > 0
}

func (e Entry) displayName() string {
	if v := strings.TrimSpace(e.Variant); v != "" {
		return e.Name + " " + v
	}
	return e.Name
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

func entryToTex(e Entry) string {
	return entryToTexWithName(e, latexEscape(e.displayName()))
}

func entryToVariantTex(e Entry) string {
	if v := strings.TrimSpace(e.Variant); v != "" {
		return entryToTexWithName(e, `\quad \variantpil\ `+latexEscape(v))
	}
	return entryToTex(e)
}

func productRowsToTex(products []Entry) []string {
	productsByName := make(map[string][]Entry)
	for _, p := range products {
		name := strings.TrimSpace(p.Name)
		productsByName[name] = append(productsByName[name], p)
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

func compileReports(cfg *config.Config, assetsDir, subdir, paymentType string, reports map[string]*Report) error {
	var errs []error
	for key, r := range reports {
		d := reportToTexData(cfg, key, r)
		d.Type = paymentType
		if err := compileTeX(assetsDir, filepath.Join(cfg.OutPath.Reports, subdir), d); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}

// Compile renders every report to a PDF via pdflatex.
// All reports are attempted; any errors are collected and returned together.
func Compile(cfg *config.Config, assetsDir string, reports map[string]*Report) error {
	return compileReports(cfg, assetsDir, "purchases", "Zettle", reports)
}

// CompileSwish renders every Swish report to a PDF via pdflatex.
func CompileSwish(cfg *config.Config, assetsDir string, reports map[string]*Report) error {
	return compileReports(cfg, assetsDir, "swish", "Swish", reports)
}
