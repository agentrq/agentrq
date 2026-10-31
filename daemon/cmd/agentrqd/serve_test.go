// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/config"
	"github.com/agentrq/agentrq/daemon/internal/localstatus"
)

// The caps are flags with the constants as their defaults, so the numbers a
// machine runs with are a property of that machine rather than of the build.
func TestSessionCaps(t *testing.T) {
	t.Run("takes what was asked for", func(t *testing.T) {
		profile, machine, err := sessionCaps(4, 9)
		if err != nil {
			t.Fatalf("sessionCaps: %v", err)
		}
		if profile != 4 || machine != 9 {
			t.Errorf("caps = %d, %d; want 4, 9", profile, machine)
		}
	})

	// Zero is what the supervisor already reads as "no cap", and it is a real
	// answer for a machine somebody owns outright.
	t.Run("zero means no limit", func(t *testing.T) {
		profile, machine, err := sessionCaps(0, 0)
		if err != nil {
			t.Fatalf("sessionCaps: %v", err)
		}
		if profile != 0 || machine != 0 {
			t.Errorf("caps = %d, %d; want 0, 0", profile, machine)
		}
	})

	// A negative is not an answer to anything, and the supervisor's own test
	// for a cap is `> 0` — so it would quietly read as "unlimited", which is
	// the opposite of what somebody typing -1 is reaching for.
	t.Run("a negative is refused rather than read as unlimited", func(t *testing.T) {
		if _, _, err := sessionCaps(-1, 8); err == nil {
			t.Error("a negative per-profile cap was accepted")
		}
		if _, _, err := sessionCaps(8, -1); err == nil {
			t.Error("a negative machine cap was accepted")
		}
	})
}

// The defaults are what somebody gets without saying anything, and they are
// the numbers the docs and the usage text promise.
func TestDefaultCaps(t *testing.T) {
	if sessionsPerProfile != 8 || sessionsPerMachine != 16 {
		t.Errorf("defaults = %d, %d; want 8, 16", sessionsPerProfile, sessionsPerMachine)
	}
}

func TestShouldDetach(t *testing.T) {
	noEnv := func(string) string { return "" }

	t.Run("detaches by default at a terminal", func(t *testing.T) {
		if !shouldDetach(false, noEnv, "linux") {
			t.Error("shouldDetach returned false for a normal terminal start, want true")
		}
	})

	t.Run("stays in the foreground when asked", func(t *testing.T) {
		if shouldDetach(true, noEnv, "linux") {
			t.Error("shouldDetach returned true with --foreground set, want false")
		}
	})

	t.Run("stays in the foreground under systemd or launchd", func(t *testing.T) {
		systemd := func(k string) string {
			if k == "INVOCATION_ID" {
				return "unit-1"
			}
			return ""
		}
		if shouldDetach(false, systemd, "linux") {
			t.Error("shouldDetach returned true under systemd, want false")
		}
		launchd := func(k string) string {
			if k == "XPC_SERVICE_NAME" {
				return "com.agentrq.agentrqd"
			}
			return ""
		}
		if shouldDetach(false, launchd, "darwin") {
			t.Error("shouldDetach returned true under launchd, want false")
		}
	})
}

func TestBackgroundArgsPrependForegroundFlag(t *testing.T) {
	got := backgroundArgs([]string{"--profile", "work", "--verbose"})
	want := []string{"serve", "--foreground", "--profile", "work", "--verbose"}
	if !slices.Equal(got, want) {
		t.Errorf("backgroundArgs returned %v, want %v", got, want)
	}
}

func TestStartBackground(t *testing.T) {
	t.Run("starts a detached child with serve --foreground and logs to the config folder", func(t *testing.T) {
		dir := t.TempDir()
		var gotBin string
		var gotArgs []string
		var gotLog string
		previous := startDetached
		t.Cleanup(func() { startDetached = previous })
		startDetached = func(bin string, args []string, logFile *os.File) (int, error) {
			gotBin = bin
			gotArgs = args
			gotLog = logFile.Name()
			return 4242, nil
		}

		if err := startBackground(dir, []string{"--profile", "work"}, time.Now()); err != nil {
			t.Fatalf("startBackground returned error: %v", err)
		}
		if gotBin == "" {
			t.Error("startBackground did not resolve the executable path")
		}
		wantArgs := []string{"serve", "--foreground", "--profile", "work"}
		if !slices.Equal(gotArgs, wantArgs) {
			t.Errorf("detached child received args %v, want %v", gotArgs, wantArgs)
		}
		wantLog := filepath.Join(dir, "agentrqd.log")
		if gotLog != wantLog {
			t.Errorf("detached child log file was %q, want %q", gotLog, wantLog)
		}
	})

	t.Run("leaves a running daemon alone", func(t *testing.T) {
		dir := t.TempDir()
		if err := localstatus.Write(dir, localstatus.File{PID: 4242}); err != nil {
			t.Fatal(err)
		}
		called := false
		previous := startDetached
		t.Cleanup(func() { startDetached = previous })
		startDetached = func(string, []string, *os.File) (int, error) {
			called = true
			return 0, nil
		}

		if err := startBackground(dir, nil, time.Now()); err != nil {
			t.Fatalf("startBackground returned error: %v", err)
		}
		if called {
			t.Error("startBackground spawned a second daemon beside a running one")
		}
	})

	t.Run("reports when the binary path cannot be found", func(t *testing.T) {
		executable = func() (string, error) { return "", errors.New("no /proc here") }
		t.Cleanup(func() { executable = os.Executable })

		if err := startBackground(t.TempDir(), nil, time.Now()); err == nil {
			t.Error("startBackground succeeded without an executable path")
		}
	})

	t.Run("reports when the log file cannot be opened", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		if err := startBackground(missing, nil, time.Now()); err == nil {
			t.Error("startBackground succeeded when the log directory did not exist")
		}
	})

	t.Run("reports when starting the detached child fails", func(t *testing.T) {
		previous := startDetached
		t.Cleanup(func() { startDetached = previous })
		startDetached = func(string, []string, *os.File) (int, error) {
			return 0, errors.New("permission denied")
		}

		if err := startBackground(t.TempDir(), nil, time.Now()); err == nil {
			t.Error("startBackground succeeded when starting the child failed")
		}
	})
}

func TestRunDetached(t *testing.T) {
	t.Run("spawns a detached process and returns its pid", func(t *testing.T) {
		logFile, err := os.CreateTemp(t.TempDir(), "agentrqd-*.log")
		if err != nil {
			t.Fatal(err)
		}
		defer logFile.Close()

		pid, err := runDetached(os.Args[0], []string{"-test.run=^$"}, logFile)
		if err != nil {
			t.Fatalf("runDetached returned error: %v", err)
		}
		if pid <= 0 {
			t.Errorf("runDetached returned pid %d, want a positive pid", pid)
		}
	})

	t.Run("returns an error when the binary does not exist", func(t *testing.T) {
		logFile, err := os.CreateTemp(t.TempDir(), "agentrqd-*.log")
		if err != nil {
			t.Fatal(err)
		}
		defer logFile.Close()

		if _, err := runDetached(filepath.Join(t.TempDir(), "missing"), nil, logFile); err == nil {
			t.Error("runDetached succeeded for a missing binary")
		}
	})
}

func TestCmdServeDetachesByDefaultWhenEnrolled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("NOTIFY_SOCKET", "")
	t.Setenv("XPC_SERVICE_NAME", "")

	st, err := stores()
	if err != nil {
		t.Fatal(err)
	}
	f, err := (config.File{}).Add(config.Profile{
		ID:        "default",
		ServerURL: "https://app.agentrq.com",
		MachineID: "m1",
		UserID:    "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.save(f); err != nil {
		t.Fatal(err)
	}

	var gotArgs []string
	previous := startDetached
	t.Cleanup(func() { startDetached = previous })
	startDetached = func(_ string, args []string, _ *os.File) (int, error) {
		gotArgs = args
		return 5555, nil
	}

	if err := cmdServe(context.Background(), []string{"--profile", "default"}); err != nil {
		t.Fatalf("cmdServe returned error: %v", err)
	}
	want := []string{"serve", "--foreground", "--profile", "default"}
	if !slices.Equal(gotArgs, want) {
		t.Errorf("cmdServe detached with %v, want %v", gotArgs, want)
	}
}

func TestStopDaemon(t *testing.T) {
	t.Run("signals the running daemon and waits for its status report to clear", func(t *testing.T) {
		dir := t.TempDir()
		if err := localstatus.Write(dir, localstatus.File{PID: 4242}); err != nil {
			t.Fatal(err)
		}

		var stoppedPID int
		previousSignal := signalStop
		previousWait := stopWait
		previousPoll := stopPoll
		t.Cleanup(func() {
			signalStop = previousSignal
			stopWait = previousWait
			stopPoll = previousPoll
		})
		stopWait = 50 * time.Millisecond
		stopPoll = 5 * time.Millisecond
		signalStop = func(pid int) error {
			stoppedPID = pid
			go func() {
				time.Sleep(10 * time.Millisecond)
				localstatus.Clear(dir)
			}()
			return nil
		}

		if err := stopDaemon(dir, time.Now()); err != nil {
			t.Fatalf("stopDaemon returned error: %v", err)
		}
		if stoppedPID != 4242 {
			t.Errorf("stopDaemon signalled pid %d, want 4242", stoppedPID)
		}
		if _, err := os.Stat(localstatus.Path(dir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("status file still exists after stopDaemon: %v", err)
		}
	})

	t.Run("clears the status file after the wait when the process exits without removing it", func(t *testing.T) {
		dir := t.TempDir()
		if err := localstatus.Write(dir, localstatus.File{PID: 4242}); err != nil {
			t.Fatal(err)
		}

		previousSignal := signalStop
		previousWait := stopWait
		previousPoll := stopPoll
		t.Cleanup(func() {
			signalStop = previousSignal
			stopWait = previousWait
			stopPoll = previousPoll
		})
		stopWait = 10 * time.Millisecond
		stopPoll = 5 * time.Millisecond
		signalStop = func(int) error { return nil }

		if err := stopDaemon(dir, time.Now()); err != nil {
			t.Fatalf("stopDaemon returned error: %v", err)
		}
		if _, err := os.Stat(localstatus.Path(dir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("status file still exists after stopDaemon: %v", err)
		}
	})

	t.Run("succeeds quietly when no daemon is running", func(t *testing.T) {
		if err := stopDaemon(t.TempDir(), time.Now()); err != nil {
			t.Fatalf("stopDaemon returned error when not running: %v", err)
		}
	})

	t.Run("clears a stale report when the daemon was already gone", func(t *testing.T) {
		dir := t.TempDir()
		if err := localstatus.Write(dir, localstatus.File{PID: 4242}); err != nil {
			t.Fatal(err)
		}
		if err := stopDaemon(dir, time.Now().Add(localstatus.Stale+time.Minute)); err != nil {
			t.Fatalf("stopDaemon returned error on a stale report: %v", err)
		}
		if _, err := os.Stat(localstatus.Path(dir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale status file still exists: %v", err)
		}
	})

	t.Run("reports an unreadable status file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(localstatus.Path(dir), []byte("{corrupt"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := stopDaemon(dir, time.Now()); err == nil {
			t.Error("stopDaemon succeeded with a corrupt status file")
		}
	})

	t.Run("reports when signalling the daemon fails", func(t *testing.T) {
		dir := t.TempDir()
		if err := localstatus.Write(dir, localstatus.File{PID: 4242}); err != nil {
			t.Fatal(err)
		}
		previous := signalStop
		t.Cleanup(func() { signalStop = previous })
		signalStop = func(int) error { return errors.New("operation not permitted") }

		if err := stopDaemon(dir, time.Now()); err == nil {
			t.Error("stopDaemon succeeded when signalStop failed")
		}
	})
}

func TestCmdStopAndRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)

	if err := run(context.Background(), []string{"stop"}); err != nil {
		t.Fatalf("run stop returned error: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv("USERPROFILE", "")
	if err := cmdStop(); err == nil {
		t.Error("cmdStop succeeded without a user config directory")
	}
}

func TestStopProcessReportsMissingPID(t *testing.T) {
	if err := stopProcess(1<<22 - 1); err == nil {
		t.Error("stopProcess succeeded for a non-existent pid")
	}
}
