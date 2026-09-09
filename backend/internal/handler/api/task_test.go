package api

import (
	"strings"
	"testing"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/mustafaturan/monoflake"
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

// A slash command only reaches an ACP agent if it leads the prompt text, so the
// envelope that gives an ordinary reply its context is precisely what stops a
// command being one. These pin which replies lose it.
func TestReplyChannelContent(t *testing.T) {
	const taskID = int64(1234)
	envelope := "[Reply to task " + monoflake.ID(taskID).String() + "] "

	advertised := &mcpctrl.AgentCommandsSnapshot{
		Commands: []mcpctrl.AgentCommand{
			{Name: "compact", Description: "Shorten the context"},
			{Name: "web", Hint: "query"},
		},
	}

	t.Run("delivers an advertised command bare", func(t *testing.T) {
		if got := replyChannelContent(taskID, "/compact", nil, advertised); got != "/compact" {
			t.Errorf("got %q, want the command alone", got)
		}
	})

	t.Run("keeps the command's argument with it", func(t *testing.T) {
		const text = "/web agent client protocol"
		if got := replyChannelContent(taskID, text, nil, advertised); got != text {
			t.Errorf("got %q, want %q", got, text)
		}
	})

	t.Run("trims leading whitespace so the command still leads the prompt", func(t *testing.T) {
		if got := replyChannelContent(taskID, "  /compact", nil, advertised); got != "/compact" {
			t.Errorf("got %q, want the command at the start", got)
		}
	})

	t.Run("keeps the envelope for a path that only looks like a command", func(t *testing.T) {
		// The guard that matters: these are ordinary sentences, and delivering
		// them stripped of their task context would be a silent loss.
		for _, text := range []string{
			"/Users/mt/thing is broken",
			"/etc/hosts looks wrong",
			"/compactify the logs",
			"/",
			"/ compact",
		} {
			want := envelope + text
			if got := replyChannelContent(taskID, text, nil, advertised); got != want {
				t.Errorf("%q: got %q, want the envelope kept", text, got)
			}
		}
	})

	t.Run("keeps the envelope for ordinary text", func(t *testing.T) {
		const text = "please compact the context"
		want := envelope + text
		if got := replyChannelContent(taskID, text, nil, advertised); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("keeps the envelope when no agent has advertised anything", func(t *testing.T) {
		// Anything that is not an ACP agent reports nothing, so nothing about
		// its behaviour changes.
		want := envelope + "/compact"
		if got := replyChannelContent(taskID, "/compact", nil, nil); got != want {
			t.Errorf("got %q, want the envelope kept", got)
		}
		empty := &mcpctrl.AgentCommandsSnapshot{}
		if got := replyChannelContent(taskID, "/compact", nil, empty); got != want {
			t.Errorf("with an empty list: got %q, want the envelope kept", got)
		}
	})

	t.Run("matches the advertised name exactly", func(t *testing.T) {
		// The agent matches what it advertised; a near miss is not a command.
		for _, text := range []string{"/Compact", "/COMPACT", "/compac"} {
			want := envelope + text
			if got := replyChannelContent(taskID, text, nil, advertised); got != want {
				t.Errorf("%q: got %q, want the envelope kept", text, got)
			}
		}
	})

	t.Run("lists attachments after the message in both branches", func(t *testing.T) {
		atts := []entity.Attachment{{ID: "att-1", Filename: "log.txt", MimeType: "text/plain"}}
		listed := formatAttachments(atts)
		if listed == "" {
			t.Fatal("expected the attachment to be listed")
		}

		// After a bare command, the listing is on its own line, so it never
		// comes between the command and the start of the prompt.
		got := replyChannelContent(taskID, "/compact", atts, advertised)
		if got != "/compact\n"+listed {
			t.Errorf("command branch: got %q", got)
		}

		got = replyChannelContent(taskID, "look at this", atts, advertised)
		if got != envelope+"look at this\n"+listed {
			t.Errorf("envelope branch: got %q", got)
		}
	})
}

// A task body that *is* a slash command has the same problem a reply did: the
// agent matches the command at the start of what it is given. It also has one a
// reply does not — the naming line is the only place the agent learns the task
// ID, so it cannot simply be dropped.
func TestTaskChannelContent(t *testing.T) {
	const taskID = int64(1234)
	naming := "[Task " + monoflake.ID(taskID).String() + "] Fix the flake"

	advertised := &mcpctrl.AgentCommandsSnapshot{
		Commands: []mcpctrl.AgentCommand{{Name: "review"}, {Name: "compact"}},
	}

	t.Run("names the task first for an ordinary body", func(t *testing.T) {
		got := taskChannelContent(taskID, "Fix the flake", "the retry test is flaky", advertised)
		if got != naming+"\nthe retry test is flaky" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("puts an advertised command first, and the naming line after it", func(t *testing.T) {
		got := taskChannelContent(taskID, "Fix the flake", "/review", advertised)
		if got != "/review\n\n"+naming {
			t.Errorf("got %q", got)
		}
	})

	t.Run("keeps the task ID reachable even then", func(t *testing.T) {
		// Dropping it would leave the agent unable to move the task on or reply
		// to it, which is worse than a noisy argument.
		got := taskChannelContent(taskID, "Fix the flake", "/review the auth changes", advertised)
		if !strings.Contains(got, monoflake.ID(taskID).String()) {
			t.Errorf("the task ID is missing from %q", got)
		}
		if !strings.HasPrefix(got, "/review the auth changes") {
			t.Errorf("the command must lead: %q", got)
		}
	})

	t.Run("leaves a body that only looks like a command alone", func(t *testing.T) {
		for _, body := range []string{"/Users/mt/thing is broken", "/reviewer notes", "/"} {
			got := taskChannelContent(taskID, "Fix the flake", body, advertised)
			if got != naming+"\n"+body {
				t.Errorf("%q: got %q", body, got)
			}
		}
	})

	t.Run("names the task first when no agent has advertised anything", func(t *testing.T) {
		got := taskChannelContent(taskID, "Fix the flake", "/review", nil)
		if got != naming+"\n/review" {
			t.Errorf("got %q", got)
		}
	})
}
