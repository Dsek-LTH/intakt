package cli

import (
	"context"
	"fmt"
	"intakt/config"
	"intakt/fortnox"
	"intakt/internal/voucher"
	"intakt/report"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// summaryChrome is the number of terminal rows reserved for the summary
// header and footer; the rest is given to the scrollable voucher list.
const summaryChrome = 9

// reviewEntry holds one pending voucher together with the user's decisions.
type reviewEntry struct {
	key        string
	pv         voucher.PendingVoucher
	costCenter string
	skip       bool
}

type tuiPhase int

const (
	phaseReview tuiPhase = iota
	phaseConfirm
	phaseUpload    // confirmation prompt before uploading
	phaseUploading // uploads in flight
	phaseResult    // final summary
)

// uploadResultMsg carries one finished upload back into the model.
type uploadResultMsg uploadResult

type tuiModel struct {
	entries  []reviewEntry
	cursor   int // entry being reviewed
	selected int // highlighted entry in the summary list
	phase    tuiPhase
	input    textinput.Model
	vp       viewport.Model
	ready    bool
	cfg      *config.Config

	// Upload machinery.
	ctx         context.Context
	fn          *fortnox.Client
	vouchersDir string
	dryRun      bool
	resultsCh   <-chan uploadResult
	results     []uploadResult
	toUpload    int // number of non-skipped entries being uploaded
	uploaded    int
	failed      int

	confirmed bool
	quitting  bool
}

func newTUIModel(ctx context.Context, fn *fortnox.Client, cfg *config.Config, vouchersDir string, entries []reviewEntry, dryRun bool) tuiModel {
	ti := textinput.New()
	ti.Placeholder = "e.g. ABC123"
	ti.CharLimit = 64
	ti.Focus()
	if len(entries) > 0 {
		ti.SetValue(entries[0].costCenter)
		ti.CursorEnd()
	}
	return tuiModel{
		entries:     entries,
		cursor:      0,
		phase:       phaseReview,
		input:       ti,
		cfg:         cfg,
		ctx:         ctx,
		fn:          fn,
		vouchersDir: vouchersDir,
		dryRun:      dryRun,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		vpHeight := max(3, ws.Height-summaryChrome)
		if !m.ready {
			m.vp = viewport.New(ws.Width, vpHeight)
			m.ready = true
		} else {
			m.vp.Width = ws.Width
			m.vp.Height = vpHeight
		}
		if m.phase == phaseConfirm {
			m.syncViewport()
		}
		return m, nil
	}
	switch m.phase {
	case phaseReview:
		return m.updateReview(msg)
	case phaseUpload:
		return m.updateUpload(msg)
	case phaseUploading:
		return m.updateUploading(msg)
	case phaseResult:
		return m.updateResult(msg)
	default:
		return m.updateConfirm(msg)
	}
}

// saveCurrent writes the text input value back into the current entry.
func (m *tuiModel) saveCurrent() {
	m.entries[m.cursor].costCenter = m.input.Value()
}

// jumpTo navigates to entry idx, saving the current input first.
func (m tuiModel) jumpTo(idx int) (tuiModel, tea.Cmd) {
	m.saveCurrent()
	m.cursor = idx
	m.input.SetValue(m.entries[idx].costCenter)
	m.input.CursorEnd()
	return m, textinput.Blink
}

// advance saves the current input and moves to the next entry, or to the
// summary if the current entry is the last one.
func (m tuiModel) advance() (tuiModel, tea.Cmd) {
	if m.cursor < len(m.entries)-1 {
		return m.jumpTo(m.cursor + 1)
	}
	m.enterConfirm()
	return m, nil
}

// enterConfirm switches to the summary, highlighting the entry that was just
// being reviewed.
func (m *tuiModel) enterConfirm() {
	m.saveCurrent()
	m.phase = phaseConfirm
	m.selected = m.cursor
	m.input.Blur()
	m.syncViewport()
}

// enterReview jumps from the summary into the review of entry idx.
func (m tuiModel) enterReview(idx int) (tuiModel, tea.Cmd) {
	m.cursor = idx
	m.phase = phaseReview
	m.input.SetValue(m.entries[idx].costCenter)
	m.input.CursorEnd()
	m.input.Focus()
	return m, textinput.Blink
}

// moveSelection moves the summary highlight, keeping it inside the viewport.
func (m *tuiModel) moveSelection(delta int) {
	m.selected = min(max(m.selected+delta, 0), len(m.entries)-1)
	m.syncViewport()
}

// syncViewport re-renders the summary list into the viewport and scrolls so the
// highlighted entry stays visible.
func (m *tuiModel) syncViewport() {
	if !m.ready {
		return
	}
	m.vp.SetContent(m.summaryRows())
	switch {
	case m.selected < m.vp.YOffset:
		m.vp.SetYOffset(m.selected)
	case m.selected >= m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(m.selected - m.vp.Height + 1)
	}
}

func (m tuiModel) updateReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch key.Type {
	case tea.KeyTab:
		m.saveCurrent()
		return m.advance()

	case tea.KeyShiftTab:
		if m.cursor > 0 {
			return m.jumpTo(m.cursor - 1)
		}
		return m, nil

	case tea.KeyCtrlS:
		m.entries[m.cursor].skip = !m.entries[m.cursor].skip
		m.saveCurrent()
		return m.advance()

	case tea.KeyCtrlD:
		m.enterConfirm()
		return m, nil

	case tea.KeyCtrlC, tea.KeyEsc:
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m tuiModel) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	switch key.Type {
	case tea.KeyUp:
		m.moveSelection(-1)
		return m, nil

	case tea.KeyDown:
		m.moveSelection(1)
		return m, nil

	case tea.KeyEnter:
		// Jump into the highlighted voucher to review/edit it.
		return m.enterReview(m.selected)

	case tea.KeyEsc:
		return m.enterReview(m.cursor)

	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	}
	switch key.String() {
	case "k":
		m.moveSelection(-1)
		return m, nil
	case "j":
		m.moveSelection(1)
		return m, nil
	case "u":
		if m.allValid() {
			m.phase = phaseUpload
		}
		return m, nil
	case "q":
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m tuiModel) updateUpload(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyEnter:
		return m.startUploading()

	case tea.KeyEsc:
		m.phase = phaseConfirm
		return m, nil

	case tea.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	}
	switch key.String() {
	case "y":
		return m.startUploading()
	case "n", "q":
		m.phase = phaseConfirm
		return m, nil
	}
	return m, nil
}

// startUploading kicks off the upload worker pool (or short-circuits to the
// result screen for a dry run) and starts listening for results.
func (m tuiModel) startUploading() (tuiModel, tea.Cmd) {
	m.confirmed = true

	toUpload := make([]reviewEntry, 0, len(m.entries))
	for _, e := range m.entries {
		if !e.skip {
			toUpload = append(toUpload, e)
		}
	}
	m.toUpload = len(toUpload)

	if m.dryRun || m.toUpload == 0 {
		m.phase = phaseResult
		return m, nil
	}

	m.phase = phaseUploading
	m.resultsCh = startUpload(m.ctx, m.fn, m.vouchersDir, toUpload)
	return m, waitForResult(m.resultsCh)
}

// waitForResult blocks in a command goroutine for the next upload result.
func waitForResult(ch <-chan uploadResult) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return nil
		}
		return uploadResultMsg(r)
	}
}

func (m tuiModel) updateUploading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case uploadResultMsg:
		m.results = append(m.results, uploadResult(msg))
		if msg.err != nil {
			m.failed++
		} else {
			m.uploaded++
		}
		if len(m.results) < m.toUpload {
			return m, waitForResult(m.resultsCh)
		}
		m.phase = phaseResult
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m tuiModel) updateResult(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) allValid() bool {
	for _, e := range m.entries {
		if !e.skip && e.costCenter == "" {
			return false
		}
	}
	return true
}

// counts returns how many entries will be uploaded and how many are skipped.
func (m tuiModel) counts() (upload, skip int) {
	for _, e := range m.entries {
		if e.skip {
			skip++
		} else {
			upload++
		}
	}
	return upload, skip
}

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	cardWidth = 66

	styleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Width(cardWidth)

	styleCardSkipped = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238")).
				Width(cardWidth)

	// styleUploadCard adds inner padding so wrapped text keeps clear of the
	// border instead of touching it.
	styleUploadCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 2).
			Width(cardWidth)

	styleBold    = lipgloss.NewStyle().Bold(true)
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleGreen   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleRed     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleSkipped = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	styleDivider = strings.Repeat("─", cardWidth)
)

func hint(key, desc string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Render(key) +
		styleDim.Render(" "+desc)
}

// ── Views ─────────────────────────────────────────────────────────────────────

func (m tuiModel) View() string {
	if m.quitting {
		return ""
	}
	switch m.phase {
	case phaseConfirm:
		return m.confirmView()
	case phaseUpload:
		return m.uploadView()
	case phaseUploading:
		return m.uploadingView()
	case phaseResult:
		return m.resultView()
	default:
		return m.reviewView()
	}
}

func (m tuiModel) reviewView() string {
	e := m.entries[m.cursor]

	nav := styleDim.Render(fmt.Sprintf("Entry %d of %d", m.cursor+1, len(m.entries)))

	skipHint := hint("ctrl+s", "skip & next")
	if e.skip {
		skipHint = hint("ctrl+s", "include & next")
	}
	hints := strings.Join([]string{
		skipHint,
		hint("tab", "next"),
		hint("shift+tab", "back"),
		hint("ctrl+d", "summary"),
		hint("esc", "quit"),
	}, "   ")

	return "\n" + nav + "\n\n" + m.reviewCard(e) + "\n\n" + hints + "\n"
}

// reviewCard renders one voucher as a bordered card: title and meta rows, the
// account rows, and the editable cost-center row. Skipped entries render
// entirely in the muted "skipped" style.
func (m tuiModel) reviewCard(e reviewEntry) string {
	titleStyle, metaStyle := styleBold, styleDim
	if e.skip {
		titleStyle, metaStyle = styleSkipped, styleSkipped
	}

	titleRow := lipgloss.JoinHorizontal(lipgloss.Top,
		titleStyle.Width(cardWidth-12).Render(e.pv.Request.Voucher.Description),
		metaStyle.Render(fmt.Sprintf("%8.2f kr", voucherTotal(e.pv))),
	)
	metaRow := metaStyle.Render(e.pv.Request.Voucher.TransactionDate + "  ·  " + voucherSource(e.pv.PDFPath))

	ccRow := "  Cost center  " + m.input.View()
	if e.skip {
		ccRow = styleSkipped.Render("  Cost center  (skipped)")
	}

	body := strings.Join([]string{
		titleRow,
		metaRow,
		styleDim.Render(styleDivider),
		strings.Join(m.accountRows(e), "\n"),
		styleDim.Render(styleDivider),
		ccRow,
	}, "\n")

	if e.skip {
		return styleCardSkipped.Render(body)
	}
	return styleCard.Render(body)
}

// accountRows renders one "account description dr/cr amount" line per voucher row.
func (m tuiModel) accountRows(e reviewEntry) []string {
	var lines []string
	for _, row := range e.pv.Request.Voucher.VoucherRows {
		acctDesc := m.cfg.AccountDescriptions[fmt.Sprintf("%d", row.Account)]
		if acctDesc == "" {
			acctDesc = "–"
		}
		dir, amt := "dr", fmt.Sprintf("%9.2f", row.Debit)
		if row.Credit > 0 {
			dir, amt = "cr", fmt.Sprintf("%9.2f", row.Credit)
		}
		line := fmt.Sprintf("  %-5d  %-32s  %s  %s", row.Account, acctDesc, dir, amt)
		if e.skip {
			line = styleSkipped.Render(line)
		}
		lines = append(lines, line)
	}
	return lines
}

// summaryRows renders one line per entry, marking the highlighted one.
func (m tuiModel) summaryRows() string {
	rows := make([]string, len(m.entries))
	for i, e := range m.entries {
		marker := "  "
		if i == m.selected {
			marker = "▸ "
		}
		date := e.pv.Request.Voucher.TransactionDate
		desc := truncate(e.pv.Request.Voucher.Description, 38)
		total := voucherTotal(e.pv)
		if e.skip {
			line := fmt.Sprintf("–  %-12s  %-38s  %8.2f kr  (skipped)", date, desc, total)
			rows[i] = marker + styleSkipped.Render(line)
		} else {
			cc := styleMuted.Render(e.costCenter)
			if e.costCenter == "" {
				cc = styleRed.Render("missing!")
			}
			line := fmt.Sprintf("%s  %-12s  %-38s  %8.2f kr  %s",
				styleGreen.Render("✓"), date, desc, total, cc)
			rows[i] = marker + line
		}
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) confirmView() string {
	uploadCount, skipCount := m.counts()

	title := styleBold.Render("Summary")
	sub := styleDim.Render(fmt.Sprintf("%d will be uploaded, %d will be skipped", uploadCount, skipCount))

	list := m.summaryRows()
	var scroll string
	if m.ready {
		m.vp.SetContent(list)
		list = m.vp.View()
		if m.vp.TotalLineCount() > m.vp.Height {
			scroll = styleDim.Render(fmt.Sprintf("  %d/%d  ·  %3.0f%%",
				m.selected+1, len(m.entries), m.vp.ScrollPercent()*100))
		}
	}

	var warning string
	if !m.allValid() {
		warning = "\n  " + styleRed.Render("⚠  Some entries have no cost center — go back and fill them in.") + "\n"
	}

	uploadHint := hint("u", "upload")
	if !m.allValid() {
		uploadHint = styleDim.Render("u  upload")
	}
	hints := strings.Join([]string{
		hint("↑/↓", "move"),
		hint("enter", "review"),
		uploadHint,
		hint("esc", "back"),
		hint("q", "quit"),
	}, "   ")

	return "\n" + title + "\n" + sub + "\n\n" +
		list + "\n" + scroll +
		warning +
		"\n\n" + hints + "\n"
}

func (m tuiModel) uploadView() string {
	uploadCount, skipCount := m.counts()

	series := m.cfg.Fortnox.VoucherSeries
	if series == "" {
		series = "?"
	}

	title := styleBold.Render("Confirm upload")
	lines := []string{
		fmt.Sprintf("%s vouchers will be uploaded to Fortnox in series %s.",
			styleBold.Render(fmt.Sprintf("%d", uploadCount)), styleBold.Render(series)),
	}
	if skipCount > 0 {
		lines = append(lines, styleDim.Render(fmt.Sprintf("%d voucher(s) will be skipped.", skipCount)))
	}
	lines = append(lines, "", styleDim.Render("PDF receipts are attached and each uploaded voucher is removed locally."))

	body := title + "\n\n" + strings.Join(lines, "\n")
	card := styleUploadCard.Render(body)

	prompt := styleBold.Render("Do you want to continue?")
	yes := styleGreen.Bold(true).Render("enter") + styleDim.Render(" yes, upload")
	no := styleRed.Bold(true).Render("esc") + styleDim.Render(" no, back")

	return "\n" + card + "\n\n  " + prompt + "\n\n  " + yes + "   " + no + "\n"
}

// progressBar renders a fixed-width text progress bar.
func progressBar(done, total, width int) string {
	if total <= 0 {
		total = 1
	}
	filled := width * done / total
	if filled > width {
		filled = width
	}
	return styleGreen.Render(strings.Repeat("█", filled)) +
		styleDim.Render(strings.Repeat("░", width-filled))
}

func (m tuiModel) uploadingView() string {
	done := len(m.results)
	title := styleBold.Render("Uploading to Fortnox")
	bar := progressBar(done, m.toUpload, cardWidth-4)
	status := styleDim.Render(fmt.Sprintf("%d / %d uploaded", done, m.toUpload))
	if m.failed > 0 {
		status += styleRed.Render(fmt.Sprintf("   ✗ %d failed", m.failed))
	}

	body := title + "\n\n" + bar + "\n\n" + status
	card := styleUploadCard.Render(body)

	return "\n" + card + "\n\n  " + styleDim.Render("please wait…") + "\n"
}

func (m tuiModel) resultView() string {
	title, lines := m.resultContent()
	body := title + "\n\n" + strings.Join(lines, "\n")
	card := styleUploadCard.Render(body)

	return "\n" + card + "\n\n  " + hint("any key", "quit") + "\n"
}

// resultContent builds the final card's title and body lines for either a dry
// run or a real upload, including per-voucher failure lines.
func (m tuiModel) resultContent() (title string, lines []string) {
	series := m.cfg.Fortnox.VoucherSeries
	if series == "" {
		series = "?"
	}

	if m.dryRun {
		return styleBold.Render("Dry run"), []string{
			fmt.Sprintf("%s voucher(s) would be uploaded to Fortnox in series %s.",
				styleBold.Render(fmt.Sprintf("%d", m.toUpload)), styleBold.Render(series)),
			styleDim.Render("No vouchers were sent."),
		}
	}

	title = styleGreen.Bold(true).Render("Upload complete")
	if m.failed > 0 {
		title = styleRed.Bold(true).Render("Upload finished with errors")
	}
	lines = []string{styleGreen.Render(fmt.Sprintf("✓ %d uploaded", m.uploaded))}
	if m.failed > 0 {
		lines = append(lines,
			styleRed.Render(fmt.Sprintf("✗ %d failed — left on disk to retry:", m.failed)),
		)
		for _, r := range m.results {
			if r.err != nil {
				lines = append(lines, styleDim.Render("  "+r.key+": ")+styleRed.Render(truncate(r.err.Error(), cardWidth-10)))
			}
		}
	}
	return title, lines
}

// truncate shortens s to at most n runes, adding an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// voucherSource returns a human-readable source label derived from the PDF's
// report subdirectory.
func voucherSource(pdfPath string) string {
	switch filepath.Base(filepath.Dir(pdfPath)) {
	case report.SubdirPurchases:
		return "Zettle · purchases"
	case report.SubdirFees:
		return "Zettle · fees"
	case report.SubdirSwish:
		return "Swish"
	default:
		return "unknown"
	}
}

// voucherTotal returns the sum of all debit amounts in the voucher rows.
func voucherTotal(pv voucher.PendingVoucher) float64 {
	var total float64
	for _, row := range pv.Request.Voucher.VoucherRows {
		total += row.Debit
	}
	return total
}

// runTUI launches the review TUI, which drives the whole review-and-upload
// flow, and returns a summary of what happened. On dry runs the fortnox client
// may be nil.
func runTUI(ctx context.Context, fn *fortnox.Client, cfg *config.Config, vouchersDir string, entries []reviewEntry, dryRun bool) (uploadSummary, error) {
	m := newTUIModel(ctx, fn, cfg, vouchersDir, entries, dryRun)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return uploadSummary{}, err
	}
	fm := final.(tuiModel)

	var failures []uploadResult
	for _, r := range fm.results {
		if r.err != nil {
			failures = append(failures, r)
		}
	}
	return uploadSummary{
		dryRun:    fm.dryRun,
		confirmed: fm.confirmed,
		total:     fm.toUpload,
		uploaded:  fm.uploaded,
		failures:  failures,
	}, nil
}
