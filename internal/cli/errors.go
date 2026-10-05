package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// usageError marks an error caused by how a command was invoked — bad arguments,
// an unknown source, a missing file. It carries the command's usage printer so
// the top-level handler can show usage plus a plain message, instead of the
// error box used for runtime failures.
type usageError struct {
	err   error
	usage func()
}

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// usage wraps err as a command-line usage error, attaching fs.Usage so the
// handler can print the command's usage text. nil stays nil.
func usage(fs *flag.FlagSet, err error) error {
	if err == nil {
		return nil
	}
	return usageError{err: err, usage: fs.Usage}
}

// HandleError presents err to the user according to its kind, and is a no-op for
// nil. Command-line/usage errors print the command usage followed by a plain
// message; failures already shown in a live box (reported) print nothing more;
// every other error is rendered in an error box.
func HandleError(err error) {
	if err == nil {
		return
	}
	var ue usageError
	if errors.As(err, &ue) {
		if ue.usage != nil {
			ue.usage()
		}
		fmt.Fprintf(os.Stderr, "\n%s\n", ue.err.Error())
		return
	}
	if IsReported(err) {
		return
	}
	PrintError(err)
}

// reportedError marks an error that has already been shown to the user (e.g. on
// a failed box row or in the upload summary), so main can exit non-zero without
// rendering it a second time.
type reportedError struct{ err error }

func (e reportedError) Error() string { return e.err.Error() }
func (e reportedError) Unwrap() error { return e.err }

// reported wraps err to signal it was already displayed. nil stays nil.
func reported(err error) error {
	if err == nil {
		return nil
	}
	return reportedError{err}
}

// IsReported reports whether err was already displayed to the user, so the
// top-level handler should not print it again.
func IsReported(err error) bool {
	var r reportedError
	return errors.As(err, &r)
}

// errLine is one failure shown in an error box: a bold label after the red ✗,
// with the detail wrapped underneath.
type errLine struct {
	label  string
	detail string
}

// renderErrorBox renders labelled failures in a bordered box with a red title,
// matching the command boxes. Long details wrap to the box width.
func renderErrorBox(title string, lines []errLine) string {
	content := []string{""}
	for i, el := range lines {
		// Wrap the label too so a long one never overflows or truncates; the
		// ✗ marks the first line, continuations align under the text.
		for j, ll := range wrapWords(el.label, cardWidth-6) {
			if j == 0 {
				content = append(content, "  "+styleRed.Render("✗")+" "+styleBold.Render(ll))
			} else {
				content = append(content, "    "+styleBold.Render(ll))
			}
		}
		for _, w := range wrapWords(el.detail, cardWidth-6) {
			content = append(content, "    "+styleDim.Render(w))
		}
		if i < len(lines)-1 {
			content = append(content, "")
		}
	}
	content = append(content, "")
	return drawBoxStyled(title, styleRed.Bold(true), content)
}

// PrintError renders err in a bordered "Error" box on stderr. It is for failures
// that surface outside a live box (bad flags, missing config, parse errors); box
// commands show their own failures inline and return a reported() error.
func PrintError(err error) {
	label, detail := splitError(err.Error())
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, renderErrorBox("Error", []errLine{{label, detail}}))
}

// splitError divides an error message into a label and a detail. Intentionally
// multi-line messages (a headline plus a hint) split on the first newline;
// otherwise it splits the outermost wrap ("operation: cause"), matching Go's %w
// convention but ignoring any ": " nested inside brackets so parentheticals like
// "(available: …)" stay intact. Messages with no split point become the label.
func splitError(s string) (label, detail string) {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimRight(s[:i], " "), strings.TrimSpace(s[i+1:])
	}
	depth := 0
	for i := 0; i+1 < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 && s[i+1] == ' ' {
				return s[:i], s[i+2:]
			}
		}
	}
	return s, ""
}
