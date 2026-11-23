// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package forkmerge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	machinectrl "github.com/agentrq/agentrq/backend/internal/controller/machine"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/daemon/wire"
	"github.com/mustafaturan/monoflake"
)

const (
	parentID  = int64(10)
	forkID    = int64(20)
	machineID = int64(11)
	sessionID = int64(500)
)

var userID = monoflake.ID(100).String()

// fakeCrud is a fork whose agent may be running, and records the order the
// merge does things in.
type fakeCrud struct {
	mu        sync.Mutex
	checkErr  error
	session   *entity.SessionView
	status    string // what the session row says now
	gone      bool   // the session row has been deleted
	sessErr   error
	mergeErr  error
	machineOK bool
	steps     []string
	folders   []string // machines that ran the fork
	foldErr   error
}

func (f *fakeCrud) step(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, s)
}

func (f *fakeCrud) got() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.steps, ",")
}

func (f *fakeCrud) CheckForkMerge(context.Context, entity.MergeForkRequest) error {
	f.step("check")
	return f.checkErr
}

func (f *fakeCrud) ActiveSessionForWorkspace(_ context.Context, req entity.ActiveSessionRequest) (*entity.SessionView, error) {
	if req.WorkspaceID != monoflake.ID(forkID).String() || req.UserID != userID {
		return nil, errors.New("asked about the wrong workspace")
	}
	return f.session, nil
}

func (f *fakeCrud) ForkFolderMachines(_ context.Context, req entity.ActiveSessionRequest) ([]string, error) {
	if req.WorkspaceID != monoflake.ID(forkID).String() || req.UserID != userID {
		return nil, errors.New("asked about the wrong workspace")
	}
	return f.folders, f.foldErr
}

func (f *fakeCrud) GetMachine(_ context.Context, req entity.GetMachineRequest) (*entity.GetMachineResponse, error) {
	if !f.machineOK {
		return nil, errors.New("not found")
	}
	return &entity.GetMachineResponse{Machine: entity.MachineView{ID: req.MachineID, Name: "laptop"}}, nil
}

func (f *fakeCrud) GetSession(_ context.Context, req entity.GetSessionRequest) (*entity.GetSessionResponse, error) {
	if f.sessErr != nil {
		return nil, f.sessErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone {
		return nil, base.ErrNotFound
	}
	return &entity.GetSessionResponse{Session: entity.SessionView{ID: req.SessionID, Status: f.status}}, nil
}

func (f *fakeCrud) setStatus(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

func (f *fakeCrud) deleteSession() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gone = true
}

func (f *fakeCrud) MergeFork(context.Context, entity.MergeForkRequest) (*entity.MergeForkResponse, error) {
	f.step("merge")
	if f.mergeErr != nil {
		return nil, f.mergeErr
	}
	return &entity.MergeForkResponse{ParentID: monoflake.ID(parentID).String(), MovedTasks: 1,
		Tasks: []entity.Task{{ID: 7, WorkspaceID: parentID}}}, nil
}

// A daemon that reports the kill it is sent, as the real one does through the
// session row.
type killingDaemon struct {
	crud   *fakeCrud
	report string // the state the daemon reports; empty reports nothing
	delete bool   // the report deletes the session row, as a real ending does
	fail   bool
	frames []wire.Control
}

func (d *killingDaemon) Send(fr wire.Frame) error {
	if d.fail {
		return errors.New("socket closed")
	}
	c, err := wire.ParseControl(fr)
	if err != nil {
		return err
	}
	d.frames = append(d.frames, c)
	d.crud.step("kill")
	if d.delete {
		go d.crud.deleteSession()
	} else if d.report != "" {
		go d.crud.setStatus(d.report)
	}
	return nil
}
func (d *killingDaemon) Close() error { return nil }

type servers struct{ removed []int64 }

func (s *servers) Remove(id int64) { s.removed = append(s.removed, id) }

func forkSession() *entity.SessionView {
	return &entity.SessionView{
		ID:          monoflake.ID(sessionID).String(),
		MachineID:   monoflake.ID(machineID).String(),
		WorkspaceID: monoflake.ID(forkID).String(),
		Status:      machinectrl.SessionRunning,
	}
}

func merger(c *fakeCrud, reg *machinectrl.Registry, s *servers, wait time.Duration) *Merger {
	return &Merger{Crud: c, Machines: reg, Servers: s, Bus: eventbus.New(), StopWait: wait, StopPoll: 5 * time.Millisecond}
}

func request() entity.MergeForkRequest {
	return entity.MergeForkRequest{UserID: userID, WorkspaceID: forkID}
}

// The fork's agent is killed on its machine, the tasks move only once the
// daemon says it is dead, and the pages watching both workspaces hear of it.
func TestMerge_KillsTheAgentFirst(t *testing.T) {
	c := &fakeCrud{session: forkSession(), status: machinectrl.SessionRunning, machineOK: true}
	d := &killingDaemon{crud: c, report: machinectrl.SessionKilled}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, d)
	s := &servers{}
	m := merger(c, reg, s, time.Second)
	forkEvents := m.Bus.Subscribe(forkID, userID)
	parentEvents := m.Bus.Subscribe(parentID, userID)

	rs, err := m.Merge(context.Background(), request())
	if err != nil || rs.MovedTasks != 1 {
		t.Fatalf("rs = %+v, err = %v", rs, err)
	}
	if got := c.got(); got != "check,kill,merge" {
		t.Errorf("steps = %s, want the check, then the kill, then the merge", got)
	}
	if len(d.frames) != 1 || d.frames[0].Op != wire.OpKillSession || !strings.Contains(string(d.frames[0].Body), `500`) {
		t.Errorf("frames = %+v, want one kill of session 500", d.frames)
	}
	if len(s.removed) != 1 || s.removed[0] != forkID {
		t.Errorf("removed = %v", s.removed)
	}
	if ev := string(<-forkEvents); !strings.Contains(ev, "task.deleted") || !strings.Contains(ev, monoflake.ID(7).String()) {
		t.Errorf("fork event = %s", ev)
	}
	if ev := string(<-parentEvents); !strings.Contains(ev, "task.created") {
		t.Errorf("parent event = %s", ev)
	}
}

// The session row of an agent that ended is deleted, usually before the merge
// first looks: a row that is gone is an agent that is dead.
func TestMerge_SessionRowDeleted(t *testing.T) {
	c := &fakeCrud{session: forkSession(), status: machinectrl.SessionRunning, machineOK: true}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, &killingDaemon{crud: c, delete: true})

	rs, err := merger(c, reg, &servers{}, time.Second).Merge(context.Background(), request())
	if err != nil || rs.MovedTasks != 1 {
		t.Fatalf("rs = %+v, err = %v", rs, err)
	}
	if got := c.got(); got != "check,kill,merge" {
		t.Errorf("steps = %s", got)
	}
}

// With no agent running there is nothing to stop.
func TestMerge_NoAgent(t *testing.T) {
	c := &fakeCrud{}
	if _, err := merger(c, nil, &servers{}, 0).Merge(context.Background(), request()); err != nil || c.got() != "check,merge" {
		t.Errorf("err %v, steps %v", err, c.steps)
	}
}

// A merge that is going to be refused leaves the agent running.
func TestMerge_RefusedDoesNotKill(t *testing.T) {
	c := &fakeCrud{
		checkErr: entity.NewForkError(entity.ErrForkUnfinished, "1 task in this fork is not finished"),
		session:  forkSession(), status: machinectrl.SessionRunning, machineOK: true,
	}
	d := &killingDaemon{crud: c}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, d)
	s := &servers{}

	_, err := merger(c, reg, s, time.Second).Merge(context.Background(), request())
	if err == nil || !strings.Contains(err.Error(), "not finished") {
		t.Fatalf("err = %v", err)
	}
	if len(d.frames) != 0 || c.got() != "check" || len(s.removed) != 0 {
		t.Errorf("frames %v, steps %v, removed %v: a refused merge touched the agent", d.frames, c.steps, s.removed)
	}
}

// An agent on a machine that cannot be reached cannot be stopped, so the merge
// is refused rather than leave it working for a workspace that is gone.
func TestMerge_MachineOffline(t *testing.T) {
	held := machinectrl.NewRegistry("pod-a")
	for name, tc := range map[string]struct {
		reg       *machinectrl.Registry
		daemon    bool
		machineOK bool
		want      string
	}{
		"not connected":       {machinectrl.NewRegistry("pod-a"), false, true, "the fork's agent is on laptop, which is offline"},
		"no machine name":     {machinectrl.NewRegistry("pod-a"), false, false, "the fork's agent is on its machine, which is offline"},
		"no machine registry": {nil, false, true, "the fork's agent is on laptop, which is offline"},
		"send fails":          {held, true, true, "the fork's agent is on laptop, which is offline"},
	} {
		c := &fakeCrud{session: forkSession(), status: machinectrl.SessionRunning, machineOK: tc.machineOK}
		if tc.daemon {
			tc.reg.Add(machineID, &killingDaemon{crud: c, fail: true})
		}
		s := &servers{}
		_, err := merger(c, tc.reg, s, time.Second).Merge(context.Background(), request())
		var fe *entity.ForkError
		if !errors.As(err, &fe) || fe.Kind != entity.ErrForkAgentRunning ||
			!strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "stop it from the machine page") {
			t.Errorf("%s: err = %v", name, err)
		}
		if c.got() != "check" || len(s.removed) != 0 {
			t.Errorf("%s: steps %v, removed %v: nothing may move", name, c.steps, s.removed)
		}
	}
}

// A daemon that takes the kill but never reports it dead is not merged past.
func TestMerge_AgentDoesNotStop(t *testing.T) {
	c := &fakeCrud{session: forkSession(), status: machinectrl.SessionRunning, machineOK: true}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, &killingDaemon{crud: c})

	_, err := merger(c, reg, &servers{}, 30*time.Millisecond).Merge(context.Background(), request())
	if err == nil || !strings.Contains(err.Error(), "has not stopped yet") {
		t.Fatalf("err = %v", err)
	}
	if c.got() != "check,kill" {
		t.Errorf("steps = %v", c.steps)
	}
}

// The session row cannot be read back: the merge stops there.
func TestMerge_SessionUnreadable(t *testing.T) {
	c := &fakeCrud{session: forkSession(), sessErr: errors.New("database unavailable"), machineOK: true}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, &killingDaemon{crud: c})
	if _, err := merger(c, reg, &servers{}, time.Second).Merge(context.Background(), request()); err == nil || err.Error() != "database unavailable" {
		t.Errorf("err = %v", err)
	}
	if c.got() != "check,kill" {
		t.Errorf("steps = %v", c.steps)
	}
}

// The request is gone while waiting: the wait ends with it.
func TestMerge_ContextCancelled(t *testing.T) {
	c := &fakeCrud{session: forkSession(), status: machinectrl.SessionRunning, machineOK: true}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, &killingDaemon{crud: c})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := merger(c, reg, &servers{}, time.Minute).Merge(ctx, request()); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

// The merge itself fails: the fork's server stays, and nobody is told the
// tasks moved.
func TestMerge_MergeFails(t *testing.T) {
	c := &fakeCrud{mergeErr: errors.New("merge transaction failed")}
	s := &servers{}
	if _, err := merger(c, nil, s, 0).Merge(context.Background(), request()); err == nil || len(s.removed) != 0 {
		t.Errorf("err %v, removed %v", err, s.removed)
	}
}

// Left at zero, the wait and the poll take their defaults.
func TestMerge_DefaultWait(t *testing.T) {
	c := &fakeCrud{session: forkSession(), status: machinectrl.SessionKilled, machineOK: true}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, &killingDaemon{crud: c})
	m := &Merger{Crud: c, Machines: reg, Servers: &servers{}, Bus: eventbus.New()}
	if _, err := m.Merge(context.Background(), request()); err != nil {
		t.Errorf("err = %v", err)
	}
}

func withFolders(c *fakeCrud) *fakeCrud {
	c.folders = []string{monoflake.ID(machineID).String()}
	c.machineOK = true
	return c
}

func deleteRequest() entity.MergeForkRequest {
	rq := request()
	rq.DeleteFolder = true
	return rq
}

// The daemon is told to delete the folder once the fork is merged, and only by
// the fork's id.
func TestMerge_DeleteFolder(t *testing.T) {
	c := withFolders(&fakeCrud{})
	d := &killingDaemon{crud: c}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, d)
	reg.SetCapabilities(machineID, d, []string{wire.CapabilityForkCleanup})

	if _, err := merger(c, reg, &servers{}, time.Second).Merge(context.Background(), deleteRequest()); err != nil {
		t.Fatal(err)
	}
	if len(d.frames) != 1 || d.frames[0].Op != wire.OpRemoveForkDir ||
		!strings.Contains(string(d.frames[0].Body), monoflake.ID(forkID).String()) {
		t.Errorf("frames = %+v, want one removeForkDir naming the fork", d.frames)
	}
}

// Without the flag the folder is left, as before.
func TestMerge_LeavesTheFolderByDefault(t *testing.T) {
	c := withFolders(&fakeCrud{})
	d := &killingDaemon{crud: c}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, d)
	reg.SetCapabilities(machineID, d, []string{wire.CapabilityForkCleanup})

	if _, err := merger(c, reg, &servers{}, time.Second).Merge(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
	if len(d.frames) != 0 {
		t.Errorf("frames = %+v, want none", d.frames)
	}
}

// A fork that never ran anywhere has no folder to delete.
func TestMerge_DeleteFolderWithNoMachines(t *testing.T) {
	c := &fakeCrud{}
	if _, err := merger(c, nil, &servers{}, 0).Merge(context.Background(), deleteRequest()); err != nil || c.got() != "check,merge" {
		t.Errorf("err %v, steps %v", err, c.steps)
	}
}

// Once merged nothing says where the folder was, so a machine that cannot be
// told stops the merge, and nothing has moved.
func TestMerge_DeleteFolderRefusedWhenAMachineCannotBeTold(t *testing.T) {
	for name, tc := range map[string]struct {
		connect bool
		caps    []string
		named   bool
		want    string
	}{
		"offline":         {false, nil, true, "laptop"},
		"old daemon":      {true, nil, true, "laptop"},
		"machine unknown": {false, nil, false, "a machine that ran it"},
	} {
		t.Run(name, func(t *testing.T) {
			c := withFolders(&fakeCrud{})
			c.machineOK = tc.named
			reg := machinectrl.NewRegistry("pod-a")
			if tc.connect {
				d := &killingDaemon{crud: c}
				reg.Add(machineID, d)
				reg.SetCapabilities(machineID, d, tc.caps)
			}
			_, err := merger(c, reg, &servers{}, time.Second).Merge(context.Background(), deleteRequest())
			var fe *entity.ForkError
			if !errors.As(err, &fe) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a fork error naming %q", err, tc.want)
			}
			if c.got() != "check" {
				t.Errorf("steps = %s, want the check only", c.got())
			}
		})
	}
	if machinectrl.CanRemoveForkDir(nil, machineID) {
		t.Error("a nil registry can remove a folder")
	}
}

func TestMerge_DeleteFolderRefusedOnALookupError(t *testing.T) {
	c := withFolders(&fakeCrud{foldErr: errors.New("database unavailable")})
	if _, err := merger(c, nil, &servers{}, 0).Merge(context.Background(), deleteRequest()); err == nil || c.got() != "check" {
		t.Errorf("err = %v steps = %s", err, c.got())
	}
}

// A machine that drops off after the merge only keeps its folder.
func TestMerge_DeleteFolderThatCannotBeSentStillMerges(t *testing.T) {
	c := withFolders(&fakeCrud{})
	d := &killingDaemon{crud: c, fail: true}
	reg := machinectrl.NewRegistry("pod-a")
	reg.Add(machineID, d)
	reg.SetCapabilities(machineID, d, []string{wire.CapabilityForkCleanup})

	if _, err := merger(c, reg, &servers{}, time.Second).Merge(context.Background(), deleteRequest()); err != nil {
		t.Errorf("err = %v, want the merge to stand", err)
	}
	if c.got() != "check,merge" {
		t.Errorf("steps = %s", c.got())
	}
}
