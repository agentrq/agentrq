package api

import (
	"strings"
	"testing"
)

// isASCII reports whether every byte is one a header value may legally carry.
// This is the property that actually matters: the desktop client's HTTP stack
// refuses a header containing anything above 255 rather than mangling it, and
// refusing crashed its main process.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7e || s[i] < 0x20 {
			return false
		}
	}
	return true
}

func TestContentDisposition_PlainNameIsLeftAlone(t *testing.T) {
	got := contentDisposition("report.pdf")

	if want := `inline; filename="report.pdf"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "filename*") {
		t.Errorf("an ASCII name needs no extended form, got %q", got)
	}
}

func TestContentDisposition_MacOSScreenshotDoesNotEscapeIntoTheHeader(t *testing.T) {
	// The exact filename from the crash report. The space before PM is U+202F
	// NARROW NO-BREAK SPACE, which is what macOS puts there, and the old
	// fmt.Sprintf copied it straight into the header at index 50 -- the index
	// and the code point 8239 both named in the reported TypeError.
	name := "Screenshot 2026-08-30 at 5.51.27 PM.png"

	got := contentDisposition(name)

	if !isASCII(got) {
		t.Fatalf("header value is not ASCII: %q", got)
	}
	if !strings.Contains(got, `filename="Screenshot 2026-08-30 at 5.51.27_PM.png"`) {
		t.Errorf("expected a readable ASCII fallback, got %q", got)
	}
	// %E2%80%AF is U+202F encoded as UTF-8, so a client that understands the
	// extended form still recovers the exact name.
	if !strings.Contains(got, "filename*=UTF-8''Screenshot%202026-08-30%20at%205.51.27%E2%80%AFPM.png") {
		t.Errorf("expected the exact name in extended form, got %q", got)
	}
}

func TestContentDisposition_RejectsHeaderInjection(t *testing.T) {
	// A quote used to close the parameter early and let the rest of the
	// filename be read as parameters of its own.
	got := contentDisposition(`evil".png`)

	if strings.Contains(got, `evil".png`) {
		t.Errorf("quote survived into the header: %q", got)
	}
	if !strings.Contains(got, `filename="evil_.png"`) {
		t.Errorf("expected the quote replaced, got %q", got)
	}
}

func TestContentDisposition_RejectsHeaderSplitting(t *testing.T) {
	// A carriage return would end the header and start another.
	got := contentDisposition("a\r\nX-Injected: yes.png")

	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("header value contains a line break: %q", got)
	}
	if !isASCII(got) {
		t.Fatalf("header value is not ASCII: %q", got)
	}
}

func TestContentDisposition_BackslashIsNeutralised(t *testing.T) {
	// Inside a quoted string a backslash escapes whatever follows it, so a name
	// ending in one would escape the closing quote.
	got := contentDisposition(`back\slash.png`)

	if !strings.Contains(got, `filename="back_slash.png"`) {
		t.Errorf("expected the backslash replaced, got %q", got)
	}
}

func TestContentDisposition_NamesWithNothingUsableStillGetOne(t *testing.T) {
	// A name made entirely of characters that cannot appear in the header would
	// otherwise produce filename="", which some clients treat as no name at all.
	got := contentDisposition("日本語")

	if !strings.Contains(got, `filename="___"`) {
		t.Errorf("expected placeholder characters, got %q", got)
	}
	if !strings.Contains(got, "filename*=UTF-8''%E6%97%A5%E6%9C%AC%E8%AA%9E") {
		t.Errorf("expected the exact name in extended form, got %q", got)
	}
}

func TestContentDisposition_EmptyName(t *testing.T) {
	if want := `inline; filename="download"`; contentDisposition("") != want {
		t.Errorf("got %q, want %q", contentDisposition(""), want)
	}
}

func TestRFC5987Encode_LeavesOnlyAttrCharsAlone(t *testing.T) {
	// Percent-encoding more than necessary is harmless; encoding less is not.
	// ~ is an attr-char in RFC 5987 section 3.2.1, so it stays as it is.
	if got, want := rfc5987Encode("a-b_c.d~1"), "a-b_c.d~1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// ; and = would be read as parameter syntax, so they must not survive.
	if got, want := rfc5987Encode("a;b=c"), "a%3Bb%3Dc"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := rfc5987Encode("a b"), "a%20b"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
