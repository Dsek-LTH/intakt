package cli

import (
	"io"

	"github.com/schollz/progressbar/v3"
)

var barTheme = progressbar.Theme{
	Saucer:        "\033[92m━\033[0m", // bright green
	SaucerHead:    "\033[92m━\033[0m",
	SaucerPadding: "\033[90m─\033[0m", // dark gray
	BarStart:      "",
	BarEnd:        "",
}

// activeBox is the inline box that fetch/compile render into. The progress and
// spinner helpers below require it to be set; compile wires it around the source
// pipeline before source.go calls them.
var activeBox *liveBox

// boxProgress drives a real schollz/progressbar (rendered to a discarded writer)
// and mirrors its output into a liveBox progress row.
type boxProgress struct {
	b   *liveBox
	r   *boxRow
	bar *progressbar.ProgressBar
}

func newProgressBar(total int, description string) *boxProgress {
	bar := progressbar.NewOptions(total,
		progressbar.OptionSetWriter(io.Discard),
		progressbar.OptionSetWidth(24),
		progressbar.OptionShowCount(),
		progressbar.OptionSetPredictTime(false), // row shows its own elapsed time
		progressbar.OptionSetTheme(barTheme),
		progressbar.OptionEnableColorCodes(true),
	)
	p := &boxProgress{b: activeBox, r: activeBox.addProgress(description, total), bar: bar}
	_ = bar.RenderBlank()
	p.b.setRendered(p.r, sanitizeBar(bar.String()))
	return p
}

func (p *boxProgress) Add(n int) error {
	err := p.bar.Add(n)
	p.b.setRendered(p.r, sanitizeBar(p.bar.String()))
	return err
}

func (p *boxProgress) Finish() error {
	err := p.bar.Finish()
	p.b.setRendered(p.r, sanitizeBar(p.bar.String()))
	p.b.finishRow(p.r, "", nil)
	return err
}

// dimSpinWhile runs fn as a single spinner row in the active box.
func dimSpinWhile(description string, fn func() error) error {
	return activeBox.run(description, false, func() (string, error) { return "", fn() })
}
