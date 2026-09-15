// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/daemon/internal/pty"
	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/internal/update"
	"github.com/agentrq/agentrq/daemon/wire"
)

// feed serves a signed manifest and the binary it names.
type feed struct {
	manifest string
	artifact string
}

func (f *feed) Do(r *http.Request) (*http.Response, error) {
	body := f.artifact
	if strings.HasSuffix(r.URL.Path, ".json") {
		body = f.manifest
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

// signedFeed builds a release whose artefact is a stub reporting `reports`,
// and installs the signing key for the duration of the test.
func signedFeed(t *testing.T, version, reports string) *feed {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previous := update.ReleaseKey
	update.ReleaseKey = hex.EncodeToString(pub)
	t.Cleanup(func() { update.ReleaseKey = previous })

	var body string
	if runtime.GOOS == "windows" {
		body = "@echo off\r\necho agentrqd " + reports + "\r\n"
	} else {
		body = "#!/bin/sh\necho 'agentrqd " + reports + "'\n"
	}
	h := sha256.Sum256([]byte(body))

	type signed struct {
		Version   string                     `json:"version"`
		Artifacts map[string]update.Artifact `json:"artifacts"`
	}
	payload := signed{
		Version: version,
		Artifacts: map[string]update.Artifact{
			runtime.GOOS + "/" + runtime.GOARCH: {
				URL:    "https://releases.example/agentrqd",
				SHA256: hex.EncodeToString(h[:]),
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	m := update.Manifest{
		Version:   payload.Version,
		Artifacts: payload.Artifacts,
		Signature: hex.EncodeToString(ed25519.Sign(priv, raw)),
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return &feed{manifest: string(b), artifact: body}
}

// updaterHarness gives an updater a binary to replace, a supervisor with a
// live session, and a restart that records rather than happens.
type updaterHarness struct {
	u        *Updater
	dir      string
	state    string
	binary   string
	tty      *fakeTTY
	restarts []string
	restart  error
}

func newUpdater(t *testing.T, f *feed, current string) *updaterHarness {
	t.Helper()
	dir := t.TempDir()
	state := t.TempDir()
	binary := filepath.Join(dir, "agentrqd")
	if err := os.WriteFile(binary, []byte("v-old"), 0o755); err != nil {
		t.Fatal(err)
	}

	tty := newTTY()
	sup := supervisor.New(func(context.Context, pty.Spec) (pty.Session, error) { return tty, nil }, 0, 0)

	h := &updaterHarness{dir: dir, state: state, binary: binary, tty: tty}
	h.u = &Updater{
		BinaryPath:  binary,
		ManifestURL: "https://releases.example/agentrqd.json",
		Version:     current,
		StateDir:    state,
		Client:      f,
		Supervisor:  sup,
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Mode:        update.ModeReexec,
		Restart: func(_ update.Mode, path string, _ []string) error {
			h.restarts = append(h.restarts, path)
			return h.restart
		},
	}
	return h
}

func (h *updaterHarness) startSession(t *testing.T, id uint64) {
	t.Helper()
	if _, err := h.u.Supervisor.Start(context.Background(), "work", supervisor.Request{
		ID: id, Kind: supervisor.KindACPGateway, Dir: t.TempDir(),
		Params: supervisor.Params{Model: "m", Agent: "a"},
		Cols:   120, Rows: 40,
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
}

func TestApplyInstallsAndHandsOver(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")

	if err := h.u.Apply(context.Background(), "0.7.1"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if b, _ := os.ReadFile(h.binary); !strings.Contains(string(b), "0.7.1") {
		t.Errorf("the installed binary is %q", b)
	}
	// The previous one is kept, because an update that cannot be rolled back
	// takes a machine offline with no way in.
	if _, err := os.Stat(h.binary + update.OldSuffix); err != nil {
		t.Errorf("the previous binary was not kept: %v", err)
	}
	if len(h.restarts) != 1 {
		t.Errorf("handed over %d times", len(h.restarts))
	}
}

// The ordering the whole design turns on: the note is on disk before a single
// session is killed, because the thing that remembers it is the thing being
// replaced.
func TestTheNoteIsWrittenBeforeAnythingIsKilled(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.startSession(t, 9)

	// The restart is where the process would have gone. By the time it is
	// reached, the note must already describe what was running.
	var noteAtHandover restore.File
	h.u.Restart = func(update.Mode, string, []string) error {
		b, err := os.ReadFile(restore.Path(h.state))
		if err != nil {
			t.Errorf("no note on disk at handover: %v", err)
			return nil
		}
		if err := json.Unmarshal(b, &noteAtHandover); err != nil {
			t.Error(err)
		}
		return nil
	}

	if err := h.u.Apply(context.Background(), "0.7.1"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(noteAtHandover.Sessions) != 1 || noteAtHandover.Sessions[0].ID != 9 {
		t.Fatalf("the note said %+v", noteAtHandover.Sessions)
	}
	s := noteAtHandover.Sessions[0]
	if s.Kind != string(supervisor.KindACPGateway) || s.Model != "m" || s.Agent != "a" {
		t.Errorf("the note lost the session's arguments: %+v", s)
	}
	if s.Cols != 120 || s.Rows != 40 {
		t.Errorf("the note lost the terminal size: %+v", s)
	}
	if noteAtHandover.FromVersion != "0.7.0" {
		t.Errorf("fromVersion = %q", noteAtHandover.FromVersion)
	}
}

// Somebody agreed to lose their sessions for a particular version. If the feed
// has moved on, they did not agree to that.
func TestApplyRefusesAVersionNobodyApproved(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.8.0", "0.8.0"), "0.7.0")
	h.startSession(t, 9)

	err := h.u.Apply(context.Background(), "0.7.1")
	if !errors.Is(err, ErrNotWhatWasApproved) {
		t.Fatalf("error = %v, want ErrNotWhatWasApproved", err)
	}
	if b, _ := os.ReadFile(h.binary); string(b) != "v-old" {
		t.Errorf("the binary was replaced anyway: %q", b)
	}
	if _, err := os.Stat(restore.Path(h.state)); !os.IsNotExist(err) {
		t.Error("a refused update left a note that would restart sessions nobody stopped")
	}
	if state, _, _ := mustLive(t, h, 9).State(); state.Terminal() {
		t.Error("a refused update killed a session anyway")
	}
	entries, _ := os.ReadDir(h.dir)
	if len(entries) != 1 {
		t.Errorf("a refused update left %d files in the binary's directory", len(entries))
	}
}

func mustLive(t *testing.T, h *updaterHarness, id uint64) *supervisor.Session {
	t.Helper()
	s, err := h.u.Supervisor.Get(id)
	if err != nil {
		t.Fatalf("session %d: %v", id, err)
	}
	return s
}

// A binary that does not run here never reaches the swap, which is what makes
// "never auto-update on a failed start" enforceable rather than aspirational.
func TestApplyRefusesABinaryThatDoesNotRun(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.6.4"), "0.7.0")
	h.startSession(t, 9)

	if err := h.u.Apply(context.Background(), "0.7.1"); !errors.Is(err, update.ErrSelfTestFailed) {
		t.Fatalf("error = %v, want ErrSelfTestFailed", err)
	}
	if b, _ := os.ReadFile(h.binary); string(b) != "v-old" {
		t.Errorf("the binary was replaced anyway: %q", b)
	}
	if state, _, _ := mustLive(t, h, 9).State(); state.Terminal() {
		t.Error("a session was killed for an update that never happened")
	}
}

// The case the retained copy exists for.
func TestAHandoverThatFailsRollsBack(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.restart = errors.New("exec format error")

	if err := h.u.Apply(context.Background(), "0.7.1"); err == nil {
		t.Fatal("Apply reported success after the handover failed")
	}
	if b, _ := os.ReadFile(h.binary); string(b) != "v-old" {
		t.Errorf("after rollback the binary is %q, want the previous one", b)
	}
	// Kept for inspection: something that would not execute is the one thing
	// somebody will want to look at.
	if _, err := os.Stat(h.binary + ".failed"); err != nil {
		t.Errorf("the binary that would not run was not kept: %v", err)
	}
}

// An update that kills sessions it has not written down is an update that
// loses them.
func TestAnUnwritableNoteStopsTheUpdate(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.startSession(t, 9)

	// A state directory that is a file.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.u.StateDir = filepath.Join(blocked, "state")

	if err := h.u.Apply(context.Background(), "0.7.1"); err == nil {
		t.Fatal("Apply carried on without a note")
	}
	if state, _, _ := mustLive(t, h, 9).State(); state.Terminal() {
		t.Error("a session was killed with nothing recording it")
	}
	if b, _ := os.ReadFile(h.binary); string(b) != "v-old" {
		t.Errorf("the binary was replaced anyway: %q", b)
	}
}

func TestApplyWithNothingNewer(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.0", "0.7.0"), "0.7.0")
	if err := h.u.Apply(context.Background(), ""); !errors.Is(err, update.ErrAlreadyCurrent) {
		t.Errorf("error = %v, want ErrAlreadyCurrent", err)
	}
}

// Reporting is all Check does. The daemon never updates on its own initiative.
func TestCheckOffersAndDoesNotInstall(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	conn := &Conn{out: make(chan []byte, 4), done: make(chan struct{})}

	h.u.Check(context.Background(), conn)

	if h.u.Available() != "0.7.1" {
		t.Errorf("available = %q", h.u.Available())
	}
	// Reporting is all it does.
	if content, _ := os.ReadFile(h.binary); string(content) != "v-old" {
		t.Errorf("Check installed something: %q", content)
	}

	var offer wire.UpdateAvailable
	c := takeControl(t, conn, wire.OpUpdateAvailable)
	if err := json.Unmarshal(c.Body, &offer); err != nil {
		t.Fatal(err)
	}
	if offer.Version != "0.7.1" {
		t.Errorf("offered %q", offer.Version)
	}
}

// takeControl reads the next control message out of a connection's queue.
func takeControl(t *testing.T, c *Conn, op wire.Op) wire.Control {
	t.Helper()
	for {
		select {
		case b := <-c.out:
			f, err := wire.Decode(b)
			if err != nil {
				t.Fatal(err)
			}
			ctl, err := wire.ParseControl(f)
			if err != nil {
				t.Fatal(err)
			}
			if ctl.Op == op {
				return ctl
			}
		default:
			t.Fatalf("nothing on the connection with op %q", op)
			return wire.Control{}
		}
	}
}

func TestCheckSaysNothingWhenThereIsNothingNewer(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.0", "0.7.0"), "0.7.0")
	h.u.Check(context.Background(), &Conn{out: make(chan []byte, 4), done: make(chan struct{})})
	if h.u.Available() != "" {
		t.Errorf("available = %q, want nothing", h.u.Available())
	}
}

func TestCheckSurvivesAnUnreachableFeed(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.u.ManifestURL = "http://releases.example/x.json" // refused before any request
	h.u.Check(context.Background(), &Conn{out: make(chan []byte, 4), done: make(chan struct{})})
	if h.u.Available() != "" {
		t.Errorf("available = %q after an unusable feed", h.u.Available())
	}
}

// A build that cannot update itself says so rather than ignoring the request:
// somebody is watching a button they just pressed.
func TestABuildThatCannotUpdateSaysSo(t *testing.T) {
	b := newBackend(t)
	start(t, b)

	b.send(t, controlFrame(t, wire.OpUpdateNow, wire.UpdateNow{Version: "0.7.1"}))

	waitFor(t, func() bool { return len(b.controls(t, wire.OpError)) > 0 }, "the refusal never came back")
	var payload map[string]string
	if err := json.Unmarshal(b.controls(t, wire.OpError)[0].Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] == "" {
		t.Errorf("the refusal said %v", payload)
	}
}

// An unreadable approval is ignored rather than fatal — the connection carries
// every other session on this machine.
func TestAnUnreadableApprovalDoesNotDropTheConnection(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	before := b.connections()

	frame, err := wire.ControlFrame(wire.Control{Op: wire.OpUpdateNow, Body: []byte(`"not an object"`)})
	if err != nil {
		t.Fatal(err)
	}
	b.send(t, frame)

	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: t.TempDir(), Model: "m", Agent: "a",
	}))
	waitFor(t, func() bool { _, err := h.sup.Get(7); return err == nil }, "the connection did not survive")
	if b.connections() != before {
		t.Errorf("the connection was remade (%d → %d)", before, b.connections())
	}
}
