// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package stream

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

// render feeds a redraw into a fresh terminal and returns what it shows, which
// is what a reattaching client would actually see.
func render(t *testing.T, redraw []byte, cols, rows int) *Screen {
	t.Helper()
	s := NewScreen(cols, rows)
	if _, err := s.Write(redraw); err != nil {
		t.Fatalf("render: %v", err)
	}
	return s
}

// text returns a row's visible characters.
func (s *Screen) text(y int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for x := 0; x < s.cols; x++ {
		g := s.term.Cell(x, y)
		if g.Char == 0 {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(g.Char)
	}
	return strings.TrimRight(b.String(), " ")
}

// THE test for this milestone. A progress bar rewritten a thousand times is
// one line; a client attaching must see the bar as it looks now, not a
// thousand frames of how it got there.
func TestAttachingSeesTheProgressBarAsItIsNow(t *testing.T) {
	s := NewScreen(40, 10)

	// A thousand redraws of the same line, exactly what claude-code does.
	for i := 0; i <= 1000; i++ {
		fmt.Fprintf(newWriter(s), "\r[%-10s] %d%%", strings.Repeat("=", i/100), i/10)
	}

	redraw := s.Redraw()
	shown := render(t, redraw, 40, 10)

	if got := shown.text(0); got != "[==========] 100%" {
		t.Errorf("row 0 = %q, want the bar at its final state", got)
	}
	// And row 1 is empty: a thousand redraws produced one line, not a
	// thousand. This is the assertion a byte-history replay would fail.
	if got := shown.text(1); got != "" {
		t.Errorf("row 1 = %q, want nothing — the redraws were replayed as history", got)
	}

	// A redraw of one line must not be the size of the history that produced
	// it. Byte history here would be roughly 20 KB.
	if len(redraw) > 2048 {
		t.Errorf("redraw is %d bytes for a single line of content", len(redraw))
	}
}

func TestRedrawReproducesTheScreen(t *testing.T) {
	s := NewScreen(40, 6)
	w := newWriter(s)
	fmt.Fprint(w, "first line\r\n")
	fmt.Fprint(w, "second line\r\n")
	fmt.Fprint(w, "third")

	shown := render(t, s.Redraw(), 40, 6)
	for y, want := range []string{"first line", "second line", "third"} {
		if got := shown.text(y); got != want {
			t.Errorf("row %d = %q, want %q", y, got, want)
		}
	}
}

// A reattached terminal that loses every colour looks broken in a way that is
// hard to attribute — people reasonably assume the program changed.
func TestRedrawKeepsColour(t *testing.T) {
	s := NewScreen(20, 3)
	// Red foreground, then a reset.
	fmt.Fprint(newWriter(s), "\x1b[31mred\x1b[0m plain")

	redraw := s.Redraw()
	if !bytes.Contains(redraw, []byte("\x1b[")) {
		t.Fatal("the redraw carries no attributes at all")
	}

	shown := render(t, redraw, 20, 3)
	shown.mu.Lock()
	fg := shown.term.Cell(0, 0).FG
	plainFG := shown.term.Cell(4, 0).FG
	shown.mu.Unlock()

	if fg == plainFG {
		t.Error("the coloured and uncoloured cells came back identical")
	}
	if fg == vt10x.DefaultFG {
		t.Error("the coloured cell lost its colour")
	}
}

// Without the leading reset, whatever the viewer's terminal was last told
// about colour bleeds into the first cell of the redraw.
func TestRedrawStartsByResettingAttributes(t *testing.T) {
	s := NewScreen(20, 3)
	fmt.Fprint(newWriter(s), "hello")
	if got := s.Redraw(); !bytes.HasPrefix(got, []byte("\x1b[0m")) {
		t.Errorf("redraw does not begin with a reset: %q", got[:min(12, len(got))])
	}
}

// Anything typed after a reattach has to appear where the program thinks the
// cursor is.
func TestRedrawRestoresTheCursor(t *testing.T) {
	s := NewScreen(40, 6)
	fmt.Fprint(newWriter(s), "line one\r\nline two\r\nabc")

	redraw := s.Redraw()
	shown := render(t, redraw, 40, 6)

	shown.mu.Lock()
	cur := shown.term.Cursor()
	shown.mu.Unlock()
	if cur.Y != 2 || cur.X != 3 {
		t.Errorf("cursor at (%d,%d), want (3,2)", cur.X, cur.Y)
	}
}

// Emitting trailing blanks for every row triples the size of a redraw on a
// mostly-empty screen, which is the usual case.
func TestRedrawDoesNotPadEveryRow(t *testing.T) {
	big := NewScreen(200, 50)
	fmt.Fprint(newWriter(big), "one short line")

	if n := len(big.Redraw()); n > 400 {
		t.Errorf("redraw of a nearly empty 200x50 screen is %d bytes", n)
	}
}

func TestResize(t *testing.T) {
	s := NewScreen(80, 24)
	if c, r := s.Size(); c != 80 || r != 24 {
		t.Fatalf("size = %dx%d", c, r)
	}
	s.Resize(120, 40)
	if c, r := s.Size(); c != 120 || r != 40 {
		t.Errorf("size after resize = %dx%d, want 120x40", c, r)
	}
	// A zero dimension is not a window and must not be applied.
	s.Resize(0, 40)
	s.Resize(120, 0)
	if c, r := s.Size(); c != 120 || r != 40 {
		t.Errorf("a zero dimension changed the size to %dx%d", c, r)
	}
}

func TestNewScreenDefaultsToARealWindow(t *testing.T) {
	s := NewScreen(0, 0)
	if c, r := s.Size(); c != 80 || r != 24 {
		t.Errorf("default size = %dx%d, want 80x24", c, r)
	}
}

// The screen is fed while nobody is watching — that is the point. It has to be
// right the moment somebody attaches, and cannot be rebuilt later from bytes
// that were never kept.
func TestTheScreenIsMaintainedWithoutAViewer(t *testing.T) {
	s := NewScreen(40, 4)
	fmt.Fprint(newWriter(s), "written with nobody attached\r\n")
	if got := render(t, s.Redraw(), 40, 4).text(0); got != "written with nobody attached" {
		t.Errorf("row 0 = %q", got)
	}
}

// newWriter adapts a Screen for fmt.Fprintf.
func newWriter(s *Screen) *screenWriter { return &screenWriter{s} }

type screenWriter struct{ s *Screen }

func (w *screenWriter) Write(p []byte) (int, error) { return w.s.Write(p) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
