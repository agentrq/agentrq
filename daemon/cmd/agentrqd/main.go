// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Command agentrqd is the AgentRQ machine daemon.
//
// This file is wiring: flags in, packages called, exit code out. Every rule it
// depends on lives in internal/, where it is tested without a process, a
// network or a terminal — the same split the desktop app's main process uses,
// and for the same reason.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/config"
	"github.com/agentrq/agentrq/daemon/internal/enrol"
	"github.com/agentrq/agentrq/daemon/internal/guard"
	"github.com/agentrq/agentrq/daemon/internal/localstatus"
	"github.com/agentrq/agentrq/daemon/internal/secret"
)

// version is stamped at build time with -ldflags "-X main.version=…".
var version = "dev"

const usage = `agentrqd — the AgentRQ machine daemon

  agentrqd enroll --server <url> --code <code> [--profile <id>] [--insecure]
  agentrqd serve [--profile <id>] [--verbose]
  agentrqd rollback
  agentrqd status
  agentrqd disable --profile <id>
  agentrqd version

Enrol a machine from the control panel: Machines → Add machine gives you a
short code, and the install steps. Run enroll on the machine itself; there is
no remote enrolment.

Enrolling lets anyone who can sign in to that AgentRQ account run commands on
this machine, as you. https://agentrq.com/docs/daemon
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "agentrqd: %v\n", err)
		if errors.Is(err, guard.ErrPrivileged) {
			fmt.Fprintf(os.Stderr, "\n%s\n", guard.PrivilegeAdvice)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	// Checked before any subcommand does work, including the read-only ones:
	// a daemon that reports happily as root and only refuses later has already
	// taught somebody that running it as root is fine.
	if err := guard.CheckPrivilege(runtime.GOOS, os.Geteuid(), isWindowsAdmin()); err != nil {
		return err
	}

	switch args[0] {
	case "enroll", "enrol":
		return cmdEnrol(ctx, args[1:])
	case "serve", "run":
		return cmdServe(ctx, args[1:])
	case "rollback":
		return cmdRollback()
	case "status":
		return cmdStatus()
	case "disable":
		return cmdDisable(args[1:])
	case "version":
		fmt.Printf("agentrqd %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return nil
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// configDir is where profiles and tokens live.
func configDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate a config directory: %w", err)
	}
	return filepath.Join(base, "agentrqd"), nil
}

func stores() (*store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	tokens, err := secret.NewFileStore(filepath.Join(dir, "tokens"))
	if err != nil {
		return nil, err
	}
	return &store{dir: dir, path: filepath.Join(dir, "config.json"), tokens: tokens}, nil
}

func cmdEnrol(ctx context.Context, args []string) error {
	fs := newFlags("enroll")
	server := fs.String("server", "", "AgentRQ server URL")
	code := fs.String("code", "", "one-time enrolment code from the control panel")
	profile := fs.String("profile", "default", "profile id (an account)")
	label := fs.String("label", "", "human-readable name for this profile")
	name := fs.String("name", "", "name for this machine (defaults to the hostname)")
	insecure := fs.Bool("insecure", false, "allow plain HTTP to a non-loopback server")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !config.ValidID(*profile) {
		return fmt.Errorf("%w: %q — lowercase letters, digits and hyphens", config.ErrBadID, *profile)
	}
	resolved, err := config.ResolveServer(*server, *insecure)
	if err != nil {
		return err
	}

	st, err := stores()
	if err != nil {
		return err
	}
	file, err := st.load()
	if err != nil {
		return err
	}

	hostname, _ := os.Hostname()
	machineName := *name
	if machineName == "" {
		machineName = hostname
	}

	client := &httpClient{timeout: enrol.DefaultTimeout}
	resp, err := enrol.Enrol(ctx, client, resolved.URL, *code, enrol.Machine{
		Name:     machineName,
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  version,
	})
	if err != nil {
		return err
	}

	p := config.Profile{
		ID:        *profile,
		Label:     *label,
		ServerURL: resolved.URL,
		MachineID: resp.MachineID,
		UserID:    resp.UserID,
		Insecure:  resolved.Insecure,
	}
	// Re-enrolment refreshes rather than duplicating: running this again on an
	// already-enrolled machine is a thing people do, and it should be harmless.
	if _, findErr := file.Find(p.ID); findErr == nil {
		file, err = file.Replace(p)
	} else {
		file, err = file.Add(p)
	}
	if err != nil {
		return err
	}

	if err := st.tokens.Set(p.ID, resp.MachineToken); err != nil {
		return err
	}
	if err := st.save(file); err != nil {
		return err
	}

	fmt.Printf("Enrolled %q as machine %s on %s\n", p.ID, resp.MachineID, resolved.URL)
	fmt.Printf("Token stored in %s\n", st.tokens.Describe())
	if w := config.InsecureWarning(p); w != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", w)
	}
	return nil
}

func cmdStatus() error {
	st, err := stores()
	if err != nil {
		return err
	}
	file, err := st.load()
	if err != nil {
		return err
	}
	if len(file.Profiles) == 0 {
		fmt.Println("No profiles. Run: agentrqd enroll --server <url> --code <code>")
		return nil
	}

	fmt.Printf("agentrqd %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Config: %s\n\n", st.path)

	printRunning(st.dir)

	for _, p := range file.Profiles {
		fmt.Printf("  %s (%s)\n", p.ID, p.Label)
		fmt.Printf("    server:  %s\n", p.ServerURL)
		fmt.Printf("    machine: %s\n", orNone(p.MachineID))
		if _, err := st.tokens.Get(p.ID); err != nil {
			fmt.Printf("    token:   MISSING — run enroll again\n")
		} else {
			fmt.Printf("    token:   present\n")
		}
		// Said on every run, not once at enrolment. A security decision that
		// becomes invisible after first boot has stopped being a decision.
		if w := config.InsecureWarning(p); w != "" {
			fmt.Printf("    %s\n", w)
		}
	}
	return nil
}

func cmdDisable(args []string) error {
	fs := newFlags("disable")
	profile := fs.String("profile", "", "profile id to disable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile == "" {
		return errors.New("disable needs --profile")
	}

	st, err := stores()
	if err != nil {
		return err
	}
	file, err := st.load()
	if err != nil {
		return err
	}
	if _, err := file.Find(*profile); err != nil {
		return err
	}

	// The token goes first. If saving the config then fails, the machine is
	// already unable to authenticate — which is the direction a disable should
	// fail in.
	if err := st.tokens.Delete(*profile); err != nil {
		return err
	}
	file, err = file.Remove(*profile)
	if err != nil {
		return err
	}
	if err := st.save(file); err != nil {
		return err
	}

	fmt.Printf("Disabled %q. Its token is gone from this machine.\n", *profile)
	fmt.Println("Remove the machine in the control panel too, to revoke it server-side.")
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "(not enrolled)"
	}
	return s
}

// printRunning says what is actually running on this machine right now.
//
// The point of `agentrqd status` is not to list configuration — it is to
// answer "is something driving my computer, and who is watching". That
// question has to be answerable by the person at the keyboard without asking
// the account that would be doing the driving.
func printRunning(dir string) {
	f, err := localstatus.Read(dir, time.Now())
	switch {
	case errors.Is(err, localstatus.ErrNoDaemon):
		fmt.Println("Not running. Start it with: agentrqd serve")
		fmt.Println()
		return
	case errors.Is(err, localstatus.ErrStale):
		// Shown rather than hidden: what a killed daemon was last doing is
		// exactly what somebody investigating wants to see.
		fmt.Printf("No recent report — the daemon may have been killed. Last seen %s:\n",
			f.UpdatedAt.Format(time.RFC3339))
	case err != nil:
		fmt.Printf("Cannot read the local status report: %v\n\n", err)
		return
	default:
		fmt.Printf("Running as pid %d since %s\n", f.PID, f.StartedAt.Format(time.RFC3339))
	}

	if len(f.Sessions) == 0 {
		fmt.Println("No agents are running on this machine.")
		fmt.Println()
		return
	}

	fmt.Printf("%d agent(s) running on this machine:\n", len(f.Sessions))
	for _, s := range f.Sessions {
		fmt.Printf("  %s in %s (profile %s)\n", s.Kind, s.Dir, s.Profile)
		if s.Workspace != "" {
			fmt.Printf("    workspace: %s\n", s.Workspace)
		}
		if s.Viewers > 0 {
			// The fact most worth surfacing locally: somebody is watching this
			// terminal right now.
			fmt.Printf("    WATCHED by %d viewer(s) right now\n", s.Viewers)
		}
	}
	fmt.Println()
	fmt.Println("Stop everything and refuse new work: agentrqd disable --profile <id>")
	fmt.Println()
}
