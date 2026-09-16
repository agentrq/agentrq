// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package stream

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/hinshun/vt10x"
)

// Screen is a session's terminal, as the daemon understands it.
//
// This exists because replay-on-attach cannot be a replay of bytes. Output
// that redraws in place — a progress bar, a spinner, any TUI — makes byte
// history useless: a buffer of it is mostly the same line written over and
// over, and feeding it to a fresh terminal shows a thousand frames of how
// something got to where it is instead of where it is.
//
// So the daemon keeps a terminal of its own, feeds it everything the process
// writes, and on attach synthesises a redraw of the current screen. That is
// what tmux does when a second client attaches, and it is why the progress bar
// arrives looking like a progress bar.
type Screen struct {
	mu   sync.Mutex
	term vt10x.Terminal
	cols int
	rows int
}

// NewScreen makes a terminal of the given size.
func NewScreen(cols, rows int) *Screen {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return &Screen{
		term: vt10x.New(vt10x.WithSize(cols, rows)),
		cols: cols,
		rows: rows,
	}
}

// Write feeds output to the screen.
//
// Everything the process writes goes here, including while nobody is watching.
// That is the point: the screen has to be right the moment somebody attaches,
// and it cannot be reconstructed later from bytes that were never kept.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.term.Write(p)
}

// Resize changes the terminal's dimensions.
func (s *Screen) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.term.Resize(cols, rows)
	s.cols, s.rows = cols, rows
}

// Size reports the current dimensions.
func (s *Screen) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// Redraw renders the current screen as the bytes that would produce it.
//
// Sent to a client on attach, and again after a resync. It is a picture of now
// rather than a history of how now happened.
//
// Colour is rebuilt from the cells rather than dropped, because a reattached
// terminal that loses every colour looks broken in a way that is hard to
// attribute — people reasonably assume the program changed, not the viewer.
func (s *Screen) Redraw() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	var b bytes.Buffer
	// Reset attributes, clear, home. Without the reset, whatever the viewer's
	// terminal was last told about colour bleeds into the first cell.
	b.WriteString("\x1b[0m\x1b[2J\x1b[H")

	lastFG, lastBG := vt10x.DefaultFG, vt10x.DefaultBG
	for y := 0; y < s.rows; y++ {
		if y > 0 {
			b.WriteString("\r\n")
		}
		// Trailing blanks are not written: they are what a cleared line
		// already is, and emitting them for every row triples the size of a
		// redraw on a mostly-empty screen.
		last := s.lastNonBlankLocked(y)
		for x := 0; x <= last; x++ {
			g := s.term.Cell(x, y)
			if g.FG != lastFG || g.BG != lastBG {
				b.WriteString(sgr(g.FG, g.BG))
				lastFG, lastBG = g.FG, g.BG
			}
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
	}

	b.WriteString("\x1b[0m")

	// Put the cursor back where the program thinks it is, so anything typed
	// next appears in the right place.
	cur := s.term.Cursor()
	fmt.Fprintf(&b, "\x1b[%d;%dH", cur.Y+1, cur.X+1)
	if !s.term.CursorVisible() {
		b.WriteString("\x1b[?25l")
	}
	return b.Bytes()
}

// lastNonBlankLocked is the rightmost column on a row worth writing.
func (s *Screen) lastNonBlankLocked(y int) int {
	for x := s.cols - 1; x >= 0; x-- {
		g := s.term.Cell(x, y)
		if g.Char != 0 && g.Char != ' ' {
			return x
		}
		// A cell that is blank but coloured still has to be written, or a
		// highlighted empty region disappears from the redraw.
		if g.BG != vt10x.DefaultBG {
			return x
		}
	}
	return -1
}

// sgr renders a colour pair as a select-graphic-rendition sequence.
func sgr(fg, bg vt10x.Color) string {
	var b bytes.Buffer
	b.WriteString("\x1b[0")
	if fg != vt10x.DefaultFG {
		writeColor(&b, fg, true)
	}
	if bg != vt10x.DefaultBG {
		writeColor(&b, bg, false)
	}
	b.WriteString("m")
	return b.String()
}

// writeColor appends one colour to an SGR sequence.
//
// vt10x reports the 256-colour palette as small integers and anything beyond
// it as packed RGB. Both are emitted in their extended form rather than mapped
// down, so a true-colour program looks the same after a reattach as before it.
func writeColor(b *bytes.Buffer, c vt10x.Color, foreground bool) {
	base := 48
	if foreground {
		base = 38
	}
	if c < 256 {
		fmt.Fprintf(b, ";%d;5;%d", base, c)
		return
	}
	r := (c >> 16) & 0xff
	g := (c >> 8) & 0xff
	bl := c & 0xff
	fmt.Fprintf(b, ";%d;2;%d;%d;%d", base, r, g, bl)
}
