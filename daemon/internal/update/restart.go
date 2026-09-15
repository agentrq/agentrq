// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// Mode is how the daemon gets back after replacing itself.
type Mode string

const (
	// ModeSupervisor exits and lets systemd or launchd start the new binary.
	//
	// Correct wherever a supervisor exists, and not merely equivalent to
	// re-executing: a service manager knows about restart limits, logging and
	// ordering, and a process that re-executes itself underneath one hides
	// from all three.
	ModeSupervisor Mode = "supervisor"

	// ModeReexec replaces this process with the new binary.
	//
	// For a daemon somebody started by hand, where exiting would simply leave
	// the machine with no daemon.
	ModeReexec Mode = "reexec"
)

// DetectMode works out who is responsible for restarting this process.
//
// Read from the environment the supervisor itself sets, rather than from a
// setting somebody has to remember to change when they install it as a
// service. Getting this wrong in the safe direction — thinking there is no
// supervisor when there is — costs a re-exec that a supervisor would have done
// better. Getting it wrong the other way leaves the machine with no daemon,
// which is why an uncertain answer is ModeReexec.
func DetectMode(env func(string) string, goos string) Mode {
	// systemd sets both of these for every unit it starts.
	if env("INVOCATION_ID") != "" || env("NOTIFY_SOCKET") != "" {
		return ModeSupervisor
	}
	// launchd names the job in the environment of everything it starts.
	if goos == "darwin" && strings.TrimSpace(env("XPC_SERVICE_NAME")) != "" {
		return ModeSupervisor
	}
	return ModeReexec
}

// Restart hands over to the new binary.
//
// On the supervisor path it does not return: the process exits and the service
// manager starts the replacement. On the re-exec path it does not return
// either, unless the exec itself failed — and that failure is the caller's
// signal to roll back, because it means the binary just installed cannot be
// run.
func Restart(mode Mode, path string, args []string) error {
	if mode == ModeSupervisor {
		// Zero, not an error code: this is a deliberate stop, and a unit with
		// Restart=always treats a non-zero exit as a crash to be reported.
		os.Exit(0)
	}

	if runtime.GOOS == "windows" {
		// Windows has no exec that replaces the process, so the new binary is
		// started as a separate process and this one steps out of the way.
		cmd := exec.Command(path, args...)
		cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("update: cannot start the new binary: %w", err)
		}
		os.Exit(0)
	}

	// The process image is replaced, so the pid, the controlling terminal and
	// anything watching this process all stay as they were.
	argv := append([]string{path}, args...)
	if err := syscall.Exec(path, argv, os.Environ()); err != nil {
		return fmt.Errorf("update: cannot execute the new binary: %w", err)
	}
	return nil
}
