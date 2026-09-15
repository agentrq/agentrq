// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package pty

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The tests below that touch a real pseudo-terminal re-exec this test binary as
// the child process. It is the one program guaranteed to exist and behave
// identically on all three operating systems — a shell builtin, `echo` or
// `cmd /c` would each need a different spelling per platform, and the point of
// these tests is to compare platforms, not to write three different tests.
const helperEnv = "AGENTRQD_PTY_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(helperMain(mode))
	}
	os.Exit(m.Run())
}

// helperMain is the child process. Keep it trivial: anything clever here shows
// up as a flaky test on whichever platform buffers differently.
func helperMain(mode string) int {
	switch {
	case mode == "hello":
		fmt.Print("hello from the pty\n")
		return 0
	case strings.HasPrefix(mode, "exit:"):
		code := 0
		_, _ = fmt.Sscanf(mode, "exit:%d", &code)
		return code
	case mode == "echo":
		in := bufio.NewScanner(os.Stdin)
		for in.Scan() {
			line := strings.TrimRight(in.Text(), "\r")
			if line == "quit" {
				return 0
			}
			fmt.Printf("got:%s\n", line)
		}
		return 0
	case mode == "bytes":
		in := bufio.NewReader(os.Stdin)
		var hex strings.Builder
		for {
			b, err := in.ReadByte()
			if err != nil {
				return 1
			}
			// Either terminator ends the line. The line discipline rewrites
			// whichever one it does not use — Unix turns the CR that Enter
			// sends into NL before the child sees it — so a reader waiting for
			// one specific byte waits forever on the other platform.
			if b == '\r' || b == '\n' {
				break
			}
			fmt.Fprintf(&hex, "%02x", b)
		}
		fmt.Printf("hex:%s\n", hex.String())
		return 0
	case mode == "sleep":
		time.Sleep(time.Minute)
		return 0
	}
	return 99
}

// helperSpec builds a Spec that runs this test binary in the given mode.
func helperSpec(t *testing.T, mode string) Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return Spec{
		Argv: []string{exe},
		Dir:  t.TempDir(),
		Env:  append(os.Environ(), helperEnv+"="+mode),
	}
}

func TestStartValidatesTheSpecBeforeSpawningAnything(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tests := []struct {
		name string
		spec Spec
		want error
	}{
		{"no argv", Spec{Dir: t.TempDir()}, ErrNoArgv},
		{"no dir", Spec{Argv: []string{"anything"}}, ErrNoDir},
		{"dir missing", Spec{Argv: []string{"x"}, Dir: filepath.Join(t.TempDir(), "nope")}, ErrDirMissing},
		{"dir is a file", Spec{Argv: []string{"x"}, Dir: file}, ErrDirNotDir},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Start(t.Context(), tc.spec)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Start() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// The error a user actually sees when a workspace folder is wrong has to name
// the path. The server cannot check it — the agent runs on another machine — so
// if this message is vague, nobody can tell them what to fix.
func TestMissingDirErrorNamesThePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-checked-out")
	_, err := Start(t.Context(), Spec{Argv: []string{"x"}, Dir: missing})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error does not name the path: %v", err)
	}
}

func TestRunsAProcessAndReadsItsOutput(t *testing.T) {
	s, err := Start(t.Context(), helperSpec(t, "hello"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if got := readUntil(t, s, "hello from the pty"); !got {
		t.Error("never saw the child's output")
	}
}

func TestReportsTheExitCode(t *testing.T) {
	s, err := Start(t.Context(), helperSpec(t, "exit:7"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	code, err := s.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

// Wait is called by the supervisor and again by whatever cleans up; os/exec
// refuses a second Wait, so the answer has to be remembered.
func TestWaitIsIdempotent(t *testing.T) {
	s, err := Start(t.Context(), helperSpec(t, "exit:3"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	first, err1 := s.Wait()
	second, err2 := s.Wait()
	if first != second || err1 != nil || err2 != nil {
		t.Errorf("Wait twice = (%d,%v) then (%d,%v)", first, err1, second, err2)
	}
}

// The keyboard half. Input is written as raw bytes and the child must see them.
//
// Note the CR. Pressing Enter on a real terminal sends carriage return (0x0d),
// not line feed — which is what xterm.js's onData yields, and therefore what
// will arrive from the browser. Writing "\n" here instead passed on Linux and
// macOS and failed on Windows: the ConPTY echoed the characters back but never
// treated the line as submitted, so the child sat in its scanner until the test
// timed out. Unix forgives it because the line discipline maps NL to a line
// ending; ConPTY does not.
//
// The daemon is right either way — it is a transparent byte pipe and translates
// nothing — but the test had encoded an assumption that only one platform
// shares, which is exactly the class of bug this suite exists to catch.
func TestWritingInputReachesTheProcess(t *testing.T) {
	s, err := Start(t.Context(), helperSpec(t, "echo"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.Write([]byte("ping\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !readUntil(t, s, "got:ping") {
		t.Error("the child never saw the input")
	}
}

// Esc has to arrive as the single byte 0x1b, through every layer, on every
// platform. It is the example in the brief and the thing most likely to be
// "helpfully" transformed by something in the middle.
func TestEscapeByteReachesTheProcessIntact(t *testing.T) {
	s, err := Start(t.Context(), helperSpec(t, "bytes"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	// Esc and the Enter that submits it go in SEPARATE writes with a gap
	// between them, because a terminal cannot tell a lone Escape key from the
	// start of an escape sequence except by timing.
	//
	// Sending {0x1b, '\r'} in one write passed on Unix and failed on Windows:
	// ConPTY's input parser saw ESC, began a sequence, and swallowed the CR as
	// part of it — the child reported a complete line containing zero bytes. A
	// human pressing Esc and then Enter supplies the gap naturally; a single
	// write manufactures an ambiguity that does not occur in practice.
	//
	// This is why input must never be coalesced on its way through the daemon.
	// Output is batched on a timer (see the plan, §10); doing the same to input
	// would merge a deliberate Esc with whatever key came next and turn it into
	// an escape sequence nobody typed.
	if _, err := s.Write([]byte{0x1b}); err != nil {
		t.Fatalf("Write esc: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := s.Write([]byte{'\r'}); err != nil {
		t.Fatalf("Write cr: %v", err)
	}
	// The helper reports what it received as hex, so the assertion does not
	// depend on the terminal echoing a control character legibly.
	if !readUntil(t, s, "hex:1b") {
		t.Error("Esc did not arrive as 0x1b")
	}
}

func TestResize(t *testing.T) {
	s, err := Start(t.Context(), helperSpec(t, "sleep"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Resize(120, 40); err != nil {
		t.Errorf("Resize: %v", err)
	}
	// A zero dimension is not a window, and passing it through would have the
	// platform decide what it means.
	if err := s.Resize(0, 40); err == nil {
		t.Error("Resize(0, 40) should fail")
	}
	if err := s.Resize(120, 0); err == nil {
		t.Error("Resize(120, 0) should fail")
	}
}

func TestCloseKillsTheProcessAndIsIdempotent(t *testing.T) {
	s, err := Start(t.Context(), helperSpec(t, "sleep"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Called again by a deferred cleanup somewhere else; must not report a
	// second, different failure.
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	done := make(chan struct{})
	go func() { _, _ = s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("process still running 10s after Close")
	}
}

func TestDefaultWindowSizeIsApplied(t *testing.T) {
	// Not observable from outside the child without a terminal-size syscall in
	// the helper, which differs per platform. What is checked here is that a
	// Spec with no size starts at all — the defaults exist so a process is
	// never told nothing.
	s, err := Start(t.Context(), helperSpec(t, "sleep"))
	if err != nil {
		t.Fatalf("Start with no size: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if DefaultCols == 0 || DefaultRows == 0 {
		t.Fatal("defaults must be a real window")
	}
}

func TestStartFailsForAMissingExecutable(t *testing.T) {
	spec := helperSpec(t, "hello")
	spec.Argv = []string{filepath.Join(t.TempDir(), "definitely-not-here")}
	if _, err := Start(t.Context(), spec); err == nil {
		t.Error("expected Start to fail for a missing executable")
	}
}

// A cancelled context must not leave the process running.
func TestContextCancellationStopsTheProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	s, err := Start(ctx, helperSpec(t, "sleep"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	cancel()
	done := make(chan struct{})
	go func() { _, _ = s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("process survived context cancellation")
	}
}

// readUntil scans the pty for a line containing want.
//
// A pty read returns an error rather than EOF when the child exits (EIO on
// Linux), so the loop treats any read error as "no more output" rather than a
// test failure — the assertion is whether the text was seen, not how the stream
// ended.
func readUntil(t *testing.T, s Session, want string) bool {
	t.Helper()
	type result struct{ found bool }
	ch := make(chan result, 1)

	go func() {
		buf := make([]byte, 1024)
		var seen strings.Builder
		for {
			n, err := s.Read(buf)
			if n > 0 {
				seen.Write(buf[:n])
				if strings.Contains(seen.String(), want) {
					ch <- result{true}
					return
				}
			}
			if err != nil {
				t.Logf("read ended: %v (saw %q)", err, seen.String())
				ch <- result{false}
				return
			}
		}
	}()

	select {
	case r := <-ch:
		return r.found
	case <-time.After(15 * time.Second):
		t.Error("timed out waiting for output")
		return false
	}
}
