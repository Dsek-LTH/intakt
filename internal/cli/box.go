package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type rowState int

const (
	rowSpinning rowState = iota
	rowDone
	rowFailed
)

// boxRow is one line of work shown inside a liveBox.
type boxRow struct {
	label    string
	state    rowState
	detail   string // extra info after the label (counts, progress, …)
	errMsg   string // error text shown wrapped under the label when the row fails
	showTime bool   // show elapsed time once done
	start    time.Time
	elapsed  time.Duration

	// Progress rows (total > 0) embed a progress bar rendered by the
	// schollz/progressbar library into rendered.
	total    int
	rendered string
}

// boxTask is a unit of concurrent work for (*liveBox).runConcurrent.
type boxTask struct {
	label string
	fn    func() (detail string, err error)
}

// liveBox renders an inline, self-updating bordered box of task rows. Unlike an
// alt-screen program it stays in the terminal scrollback after it finishes.
type liveBox struct {
	title  string
	mu     sync.Mutex
	rows   []*boxRow
	frame  int
	height int // terminal lines the box occupied on the last draw
	stop   chan struct{}
	done   chan struct{}
}

func startBox(title string) *liveBox {
	b := &liveBox{
		title: title,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go b.loop()
	return b
}

func (b *liveBox) loop() {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-b.stop:
			b.draw()
			close(b.done)
			return
		case <-ticker.C:
			b.draw()
		}
	}
}

// finish stops the animation and leaves the final frame on screen.
func (b *liveBox) finish() {
	close(b.stop)
	<-b.done
}

func (b *liveBox) add(label string, showTime bool) *boxRow {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := &boxRow{label: label, state: rowSpinning, showTime: showTime, start: time.Now()}
	b.rows = append(b.rows, r)
	return r
}

func (b *liveBox) finishRow(r *boxRow, detail string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r.elapsed = time.Since(r.start)
	if detail != "" {
		r.detail = detail
	}
	if err != nil {
		r.state = rowFailed
		r.errMsg = err.Error()
	} else {
		r.state = rowDone
	}
}

// run adds a spinning row, runs fn to completion, then settles the row.
func (b *liveBox) run(label string, showTime bool, fn func() (string, error)) error {
	r := b.add(label, showTime)
	detail, err := fn()
	b.finishRow(r, detail, err)
	return err
}

// runConcurrent runs all tasks at once, each as its own row, and returns the
// first error (after all have finished).
func (b *liveBox) runConcurrent(tasks ...boxTask) error {
	var wg sync.WaitGroup
	errs := make([]error, len(tasks))
	for i, t := range tasks {
		r := b.add(t.label, true)
		i, t := i, t
		wg.Add(1)
		go func() {
			defer wg.Done()
			detail, err := t.fn()
			errs[i] = err
			b.finishRow(r, detail, err)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// addProgress adds a progress-bar row whose bar is rendered by the caller via
// setRendered.
func (b *liveBox) addProgress(label string, total int) *boxRow {
	r := b.add(label, true)
	b.mu.Lock()
	r.total = total
	b.mu.Unlock()
	return r
}

// setRendered stores a pre-rendered progress-bar string for a progress row.
func (b *liveBox) setRendered(r *boxRow, s string) {
	b.mu.Lock()
	r.rendered = s
	b.mu.Unlock()
}

// hasFailure reports whether any row ended in failure, i.e. the box already
// displayed an error to the user.
func (b *liveBox) hasFailure() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.rows {
		if r.state == rowFailed {
			return true
		}
	}
	return false
}

func (b *liveBox) draw() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frame++

	// Don't render an empty box: if a command fails before adding any step
	// (e.g. a cache miss), the error is surfaced elsewhere instead.
	if len(b.rows) == 0 {
		return
	}

	lines := []string{""}
	for _, r := range b.rows {
		// A failed row may render its wrapped error across several lines.
		lines = append(lines, strings.Split(b.renderRow(r), "\n")...)
	}
	lines = append(lines, "")

	s := drawBox(b.title, lines)
	h := strings.Count(s, "\n") + 1

	var out bytes.Buffer
	if b.height > 0 {
		fmt.Fprintf(&out, "\033[%dA", b.height) // move up to the box top
	} else {
		// First render: a leading blank line so the box doesn't sit flush
		// against the shell prompt. It stays above the repainted region (which
		// only covers the box), and is skipped entirely when the box draws
		// nothing — e.g. a command that fails before adding any step.
		out.WriteByte('\n')
	}
	out.WriteString("\033[J") // clear from cursor to end of screen
	out.WriteString(s)
	out.WriteByte('\n')
	b.height = h
	_, _ = out.WriteTo(os.Stderr)
}

func (b *liveBox) renderRow(r *boxRow) string {
	const labelCol = 24

	// Progress rows embed the schollz/progressbar output (bar + percentage +
	// count), both while running and once complete.
	if r.total > 0 && r.state != rowFailed {
		icon := styleGreen.Render(spinnerFrames[b.frame%len(spinnerFrames)])
		tail := ""
		if r.state == rowDone {
			icon = styleGreen.Render("✓")
			tail = styleDim.Render(fmt.Sprintf("   %.1fs", r.elapsed.Seconds()))
		}
		return "  " + icon + " " + padDisplay(r.label, 14) + " " + r.rendered + tail
	}

	switch r.state {
	case rowDone:
		line := "  " + styleGreen.Render("✓") + " " + padDisplay(r.label, labelCol)
		tail := r.detail
		if r.showTime {
			if tail != "" {
				tail += "   "
			}
			tail += fmt.Sprintf("%.1fs", r.elapsed.Seconds())
		}
		return line + styleDim.Render(tail)
	case rowFailed:
		head := "  " + styleRed.Render("✗") + " " + r.label
		if r.errMsg == "" {
			return head
		}
		// Wrap the error under the label, indented past the ✗ marker.
		lines := []string{head}
		for _, w := range wrapWords(r.errMsg, cardWidth-6) {
			lines = append(lines, "    "+styleDim.Render(w))
		}
		return strings.Join(lines, "\n")
	default:
		icon := styleGreen.Render(spinnerFrames[b.frame%len(spinnerFrames)])
		line := "  " + icon + " " + r.label
		if r.detail != "" {
			line += "   " + styleDim.Render(r.detail)
		}
		return line
	}
}

// drawBox renders a bordered box with the title spliced into the top border.
// contentLines are padded to a fixed width so the borders line up.
func drawBox(title string, contentLines []string) string {
	return drawBoxStyled(title, styleBold, contentLines)
}

// drawBoxStyled is drawBox with a caller-chosen style for the title, used to
// render error boxes with a red title.
func drawBoxStyled(title string, titleStyle lipgloss.Style, contentLines []string) string {
	w := cardWidth
	// Keep room for the "─ " prefix and " " suffix around the title.
	title = ansi.Truncate(title, w-4, "…")
	dash := w - 3 - lipgloss.Width(title)
	if dash < 0 {
		dash = 0
	}
	var b strings.Builder
	b.WriteString(styleDim.Render("╭─ "))
	b.WriteString(titleStyle.Render(title))
	b.WriteString(styleDim.Render(" " + strings.Repeat("─", dash) + "╮"))
	b.WriteByte('\n')
	for _, line := range contentLines {
		line = ansi.Truncate(line, w, "…") // clamp overly-wide rows so borders line up
		b.WriteString(styleDim.Render("│"))
		b.WriteString(padDisplay(line, w))
		b.WriteString(styleDim.Render("│"))
		b.WriteByte('\n')
	}
	b.WriteString(styleDim.Render("╰" + strings.Repeat("─", w) + "╯"))
	return b.String()
}

// sanitizeBar flattens a progressbar render into a single embeddable line by
// removing the carriage returns / newlines the library uses to repaint.
func sanitizeBar(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return strings.TrimSpace(s)
}

// wrapWords word-wraps s to display width w, breaking on spaces and newlines and
// hard-splitting any single word wider than w. It returns the wrapped lines.
func wrapWords(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		cur := ""
		for _, word := range strings.Fields(para) {
			// A word wider than the line is split across lines by rune.
			for lipgloss.Width(word) > w {
				if cur != "" {
					lines = append(lines, cur)
					cur = ""
				}
				head, rest := splitRunes(word, w)
				lines = append(lines, head)
				word = rest
			}
			switch {
			case cur == "":
				cur = word
			case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
				cur += " " + word
			default:
				lines = append(lines, cur)
				cur = word
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}
	return lines
}

// splitRunes splits s after n runes, returning the head and remainder.
func splitRunes(s string, n int) (string, string) {
	r := []rune(s)
	if n >= len(r) {
		return s, ""
	}
	return string(r[:n]), string(r[n:])
}

// padDisplay right-pads s with spaces to display width w (ignoring ANSI codes).
func padDisplay(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}
