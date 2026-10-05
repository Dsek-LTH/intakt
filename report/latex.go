package report

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// A render is a subprocess doing filesystem work, so it gets a ceiling. Under
// the CLI a hung pdflatex was a person noticing and pressing ^C; under the
// service it is a request handler occupied forever.
const renderTimeout = 60 * time.Second

// The jobname inside the temp directory. Under the service it never reaches the
// caller — the return value is bytes, and what the file is eventually *called*
// is decided by whoever stores it; under the CLI compileTeX renames it.
const jobName = "report"

// renderTeX writes the skeleton and its \newcommand preamble into a private
// temporary directory, runs pdflatex there, and returns the resulting PDF.
//
// The directory is per-call, which is the whole point: rendering used to write
// <key>.tex into the assets directory and run pdflatex in it, so two renders of
// the same report raced over one set of filenames and every render mutated a
// directory that ought to be read-only. That was tolerable when one person ran
// one command; it is not when four requests arrive at once. Assets stay
// reachable through TEXINPUTS rather than being copied, so the skeleton's
// \includegraphics still resolves without anything being written next to them.
func renderTeX(assetsDir string, d texData) ([]byte, error) {
	// cmd.Dir is the temp directory, so a relative assets path would resolve
	// against the wrong place once TEXINPUTS is read.
	absAssets, err := filepath.Abs(assetsDir)
	if err != nil {
		return nil, fmt.Errorf("resolving assets directory: %w", err)
	}

	skeleton, err := os.ReadFile(filepath.Join(absAssets, d.Skeleton))
	if err != nil {
		return nil, fmt.Errorf("reading skeleton: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "intakt-render-")
	if err != nil {
		return nil, fmt.Errorf("creating render directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

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

	texPath := filepath.Join(tmpDir, jobName+".tex")
	if err := os.WriteFile(texPath, buf.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("writing tex file: %w", err)
	}

	if err := runPdflatex(tmpDir, absAssets); err != nil {
		return nil, err
	}

	pdf, err := os.ReadFile(filepath.Join(tmpDir, jobName+".pdf"))
	if err != nil {
		return nil, fmt.Errorf("reading rendered pdf: %w", err)
	}
	return pdf, nil
}

// compileTeX renders d and leaves the PDF in outputDir as <OutputBase>.pdf,
// which is what the CLI's compile step produces for the treasurer to look at.
//
// A wrapper over renderTeX rather than a pdflatex driver of its own. There were
// two for a while — one for the CLI, one for the service — and the differences
// between them were not deliberate: only one had the timeout, only one had the
// second longtable pass, and only one stopped writing into the assets
// directory. One driver, two ways of keeping the result.
func compileTeX(assetsDir, outputDir string, d texData) error {
	pdf, err := renderTeX(assetsDir, d)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	dst := filepath.Join(outputDir, d.OutputBase+".pdf")
	if err := os.WriteFile(dst, pdf, 0o644); err != nil {
		return fmt.Errorf("writing pdf: %w", err)
	}
	return nil
}

// The most passes runPdflatex will make. Two is enough for everything this
// document can contain: one to measure, one to apply.
const maxPasses = 2

// runPdflatex compiles jobName.tex in workDir, re-running only if the log asks
// for it.
//
// The items table is a longtable, which measures its column widths on one pass
// and applies them on the next, so a table spanning pages needs two. Most
// reports are a handful of product lines and settle on the first — measured
// against the real data, a second pass was byte-for-byte identical. So rather
// than pay double on every render or risk a misaligned table on the rare long
// one, this does what latexmk does and reads the log to decide.
func runPdflatex(workDir, assetsDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()

	for pass := 1; pass <= maxPasses; pass++ {
		cmd := exec.CommandContext(ctx, "pdflatex",
			// Without this pdflatex stops at the first error and waits on
			// stdin. Interactively that is a prompt; in a request handler it is
			// a hang, which is what the timeout above would otherwise be
			// covering for.
			"-interaction=nonstopmode",
			"-halt-on-error",
			// The skeleton is ours, but it is filled with values that came from
			// a Zettle product name or a Swish message. Nothing should be able
			// to reach a shell from there.
			"-no-shell-escape",
			jobName+".tex",
		)
		cmd.Dir = workDir
		// How the skeleton's \includegraphics finds D-symbol.pdf and sig.png
		// without anything being copied. The trailing separator keeps the
		// default search path in place.
		cmd.Env = append(os.Environ(), "TEXINPUTS="+assetsDir+string(os.PathListSeparator))

		out, err := cmd.CombinedOutput()
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("pdflatex timed out after %s", renderTimeout)
			}
			return fmt.Errorf("pdflatex (pass %d): %w\n%s", pass, err, lastLines(out, 40))
		}

		if !needsRerun(workDir) {
			break
		}
	}

	return nil
}

// needsRerun reports whether the last pass left the document unsettled. LaTeX
// says "Rerun to get cross-references right"; longtable says "Table widths have
// changed. Rerun LaTeX." Both contain the word, and nothing else in a clean log
// does — so matching on it covers both without tying this to either wording.
//
// A log that cannot be read is treated as settled: the pass itself succeeded,
// and a missing log is not a reason to compile the document twice.
func needsRerun(workDir string) bool {
	log, err := os.ReadFile(filepath.Join(workDir, jobName+".log"))
	if err != nil {
		return false
	}
	return bytes.Contains(bytes.ToLower(log), []byte("rerun"))
}

// pdflatex is voluminous and the interesting part of a failure is at the end.
// The whole log in an HTTP error body helps nobody.
func lastLines(out []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
