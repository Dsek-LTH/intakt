package report

import (
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
		Items:        productRowsToTex(r.Products),
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

func productRowsToTex(products []Entry) string {
	productsByName := make(map[string][]Entry)
	for _, p := range products {
		name := strings.TrimSpace(p.Name)
		productsByName[name] = append(productsByName[name], p)
	}

	names := make([]string, 0, len(productsByName))
	for name := range productsByName {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		acctA := slices.MinFunc(productsByName[a], func(x, y Entry) int { return strings.Compare(x.Account, y.Account) }).Account
		acctB := slices.MinFunc(productsByName[b], func(x, y Entry) int { return strings.Compare(x.Account, y.Account) }).Account
		if c := strings.Compare(acctA, acctB); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})

	groups := make([]string, 0, len(names))
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

		rows := make([]string, 0, len(entries)+1)
		if hasMultipleVariants(entries) {
			rows = append(rows, fmt.Sprintf(`\textbf{%s:} & & & & &`, latexEscape(name)))
			for _, entry := range entries {
				rows = append(rows, entryToVariantTex(entry))
			}
		} else {
			for _, entry := range entries {
				rows = append(rows, entryToTex(entry))
			}
		}
		groups = append(groups, strings.Join(rows, "\\\\\n"))
	}

	// Groups are separated by \addlinespace so a wrapped product name is
	// visually distinct from the start of the next product.
	return strings.Join(groups, "\\\\\n\\addlinespace[4pt]\n")
}

func compileReports(cfg *config.Config, assetsDir, subdir, paymentType string, reports map[string]*Report, onProgress func()) error {
	outDir := filepath.Join(cfg.OutPath.Reports, subdir)
	return compileAll(reports, onProgress, func(key string, r *Report) error {
		d := reportToTexData(cfg, key, r)
		d.Type = paymentType
		return compileTeX(assetsDir, outDir, d)
	})
}

// Compile renders every report to a PDF via pdflatex.
// onProgress is called after each PDF is compiled; pass nil to skip.
func Compile(cfg *config.Config, assetsDir string, reports map[string]*Report, onProgress func()) error {
	return compileReports(cfg, assetsDir, "purchases", "Zettle", reports, onProgress)
}

// CompileSwish renders every Swish report to a PDF via pdflatex.
func CompileSwish(cfg *config.Config, assetsDir string, reports map[string]*Report, onProgress func()) error {
	return compileReports(cfg, assetsDir, "swish", "Swish", reports, onProgress)
}
