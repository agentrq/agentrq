// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_auth "github.com/agentrq/agentrq/backend/internal/service/mocks/auth"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

// errNotTheRealThing stands in for whatever a dependency says when it cannot
// answer — an in-memory session id is not a token, and a repository under test
// here is one that failed.
var errNotTheRealThing = errors.New("not the real thing")

// pushServer builds a workspace server with just enough wired up to run a poll
// and push a task down the channel.
func pushServer(t *testing.T) *WorkspaceServer {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	pubsubMock := mock_pubsub.NewMockService(ctrl)
	pubsubMock.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes()
	// SendChannelNotification logs each session's auth state, so the token
	// service is reached on every push even though nothing here asserts on it.
	tokenMock := mock_auth.NewMockTokenService(ctrl)
	tokenMock.EXPECT().ValidateToken(gomock.Any()).Return(nil, errNotTheRealThing).AnyTimes()

	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(100).String(),
		done:        make(chan struct{}),
		bus:         eventbus.New(),
		pubsub:      pubsubMock,
		tokenSvc:    tokenMock,
	}
	// Closed up front so clearContextFor's settle wait returns immediately: the
	// delay has its own test rather than being paid for in every poll here.
	close(ps.done)
	return ps
}

// countingClear wires the clear seam and reports how many times it was actually
// asked to clear — which is the number that has to stay at one however many
// times a task is pushed.
func countingClear(ps *WorkspaceServer, err error) *int {
	calls := 0
	ps.clearAgentContext = func(context.Context) error {
		calls++
		return err
	}
	return &calls
}

// listTasksFunc is a repository that answers the poller's questions from one
// underlying function. CountTasks reuses it rather than needing its own,
// since the fakes built on it already filter by the request's Status and
// Assignee — counting is just listing without a Limit and taking the length.
type listTasksFunc func(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error)

func (f listTasksFunc) ListTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error) {
	return f(ctx, req, userID)
}

func (f listTasksFunc) CountTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) (int64, error) {
	req.Limit = 0
	tasks, err := f(ctx, req, userID)
	if err != nil {
		return 0, err
	}
	return int64(len(tasks)), nil
}

// pendingTaskRepo stands in for the repository's WHERE/LIMIT, since pollOnce
// now makes two separate calls (one for "ongoing", one for "notstarted") and
// trusts each one to have already been filtered and capped — exactly what the
// real repository.ListTasks does.
func pendingTaskRepo(tasks ...model.Task) listTasksFunc {
	return func(_ context.Context, req entity.ListTasksRequest, _ int64) ([]model.Task, error) {
		wantStatus := make(map[string]bool, len(req.Status))
		for _, s := range req.Status {
			wantStatus[s] = true
		}
		var out []model.Task
		for _, t := range tasks {
			if len(wantStatus) > 0 && !wantStatus[t.Status] {
				continue
			}
			if req.Assignee != "" && t.Assignee != req.Assignee {
				continue
			}
			out = append(out, t)
			if req.Limit > 0 && len(out) >= req.Limit {
				break
			}
		}
		return out, nil
	}
}

// recordingTaskRepo wraps pendingTaskRepo and also records every request made
// to it, so a test can assert on the shape of pollOnce's separate queries.
func recordingTaskRepo(tasks ...model.Task) (listTasksFunc, *[]entity.ListTasksRequest) {
	var asked []entity.ListTasksRequest
	inner := pendingTaskRepo(tasks...)
	f := listTasksFunc(func(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error) {
		asked = append(asked, req)
		return inner(ctx, req, userID)
	})
	return f, &asked
}

// The reported bug, and the rule that fixes it: a task stands until the agent
// takes it. Nothing about a push is remembered, so a push made while a gateway
// was between connections — which reaches nobody, silently — is simply made
// again on the next tick. Suppressing the second one is what turned a task
// created at the wrong moment into a task that never arrived at all.
func TestPollOnceOffersAPendingTaskOnEveryTick(t *testing.T) {
	ps := pushServer(t)
	repo := pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent"})

	for tick := 1; tick <= 3; tick++ {
		if got := ps.pollOnce(repo); got != 7 {
			t.Fatalf("tick %d offered task %d, want 7 — a pending task stands until the agent takes it", tick, got)
		}
	}
}

// The other half, and the reason the two are tracked apart: the push repeats
// and the clear must not. A clear landing behind the second push would wipe the
// context that push had just put the task into.
func TestPollOnceClearsOnceHoweverManyTimesItPushes(t *testing.T) {
	ps := pushServer(t)
	clears := countingClear(ps, nil)
	repo := pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent", ClearContext: true})

	ps.pollOnce(repo)
	ps.pollOnce(repo)
	ps.pollOnce(repo)

	if *clears != 1 {
		t.Fatalf("cleared %d times across three pushes, want 1", *clears)
	}
}

// A clear that could not be sent is not a clear. The machine may be offline or
// the session gone — both ordinary — and remembering the failure as done would
// skip the clear for good the moment the machine came back, handing the agent a
// task on exactly the context the task asked to be rid of.
func TestPollOnceRetriesAClearThatNeverWentOut(t *testing.T) {
	ps := pushServer(t)
	clears := countingClear(ps, errNotTheRealThing)
	repo := pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent", ClearContext: true})

	ps.pollOnce(repo)
	ps.pollOnce(repo)

	if *clears != 2 {
		t.Fatalf("attempted %d clears, want 2 — a failed clear must be tried again", *clears)
	}
}

// Taking the task is what stops the pushes: the agent moves it to ongoing, and
// the tick after that offers nothing.
func TestPollOnceStopsOnceTheTaskIsOngoing(t *testing.T) {
	ps := pushServer(t)

	if got := ps.pollOnce(pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent"})); got != 7 {
		t.Fatalf("offered task %d, want 7", got)
	}
	if got := ps.pollOnce(pendingTaskRepo(model.Task{ID: 7, Status: "ongoing", Assignee: "agent"})); got != 0 {
		t.Fatalf("offered task %d while it was ongoing, want none", got)
	}
}

// A task handed back to an agent later is cleared afresh: the set is reconciled
// against what is still pending, so the record of the first handover's clear
// does not silently cover the second.
func TestPollOnceClearsAgainForALaterHandover(t *testing.T) {
	ps := pushServer(t)
	clears := countingClear(ps, nil)
	pending := pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent", ClearContext: true})

	ps.pollOnce(pending)
	// Taken, worked, and put back — the reconcile on this tick is what forgets
	// the first handover.
	ps.pollOnce(pendingTaskRepo(model.Task{ID: 7, Status: "completed", Assignee: "agent"}))
	ps.pollOnce(pending)

	if *clears != 2 {
		t.Fatalf("cleared %d times across two handovers, want 2", *clears)
	}
}

// A human putting a task back to notstarted hands it to the agent again. The
// status is the whole contract — nothing is remembered about the earlier push
// that could suppress this one, which is exactly what the old dedup got wrong:
// a task taken and put back inside one tick stayed marked as delivered and was
// never offered again.
func TestPollOnceOffersATaskPutBackToNotStarted(t *testing.T) {
	ps := pushServer(t)
	pending := pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent"})

	if got := ps.pollOnce(pending); got != 7 {
		t.Fatalf("offered task %d, want 7", got)
	}
	// The agent takes it.
	if got := ps.pollOnce(pendingTaskRepo(model.Task{ID: 7, Status: "ongoing", Assignee: "agent"})); got != 0 {
		t.Fatalf("offered task %d while it was ongoing, want none", got)
	}
	// The human puts it back.
	if got := ps.pollOnce(pending); got != 7 {
		t.Fatalf("offered task %d after it was put back to notstarted, want 7", got)
	}
}

// The same, with no tick in between — the agent took the task and the human put
// it back inside one interval, so the server never observed it as ongoing. The
// task must still be offered again.
func TestPollOnceOffersATaskPutBackWithinOneInterval(t *testing.T) {
	ps := pushServer(t)
	pending := pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent"})

	if got := ps.pollOnce(pending); got != 7 {
		t.Fatalf("offered task %d, want 7", got)
	}
	if got := ps.pollOnce(pending); got != 7 {
		t.Fatalf("offered task %d on the tick after, want 7", got)
	}
}

// The reported bug (task 0j1wvKhUB5V, reproduced live): a task the agent never
// picks up used to hide every task created after it. The poller always offered
// the oldest pending task and only that one, so a newer task behind it was
// never sent — not once, for as long as the older one sat there. Offering has
// to move on.
func TestPollOnceDoesNotStarveANewerTaskBehindAStuckOne(t *testing.T) {
	ps := pushServer(t)
	stuck := model.Task{ID: 7, Status: "notstarted", Assignee: "agent", SortOrder: 1}
	newer := model.Task{ID: 8, Status: "notstarted", Assignee: "agent", SortOrder: 2}
	repo := pendingTaskRepo(stuck, newer)

	// Four ticks, the way the live reproduction ran. The newer task has to be
	// offered in there somewhere.
	offered := map[int64]int{}
	for i := 0; i < 4; i++ {
		offered[ps.pollOnce(repo)]++
	}

	if offered[8] == 0 {
		t.Fatalf("the newer task was never offered across four ticks; offers were %v", offered)
	}
	if offered[7] == 0 {
		t.Errorf("the older task stopped being offered entirely; offers were %v", offered)
	}
}

// One task per tick, still. The fix for starvation must not turn into handing
// an agent its whole backlog at once, which is a different way to break it.
func TestPollOnceOffersOneTaskPerTick(t *testing.T) {
	ps := pushServer(t)

	got := ps.pollOnce(pendingTaskRepo(
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent", SortOrder: 1},
		model.Task{ID: 8, Status: "notstarted", Assignee: "agent", SortOrder: 2},
		model.Task{ID: 9, Status: "notstarted", Assignee: "agent", SortOrder: 3},
	))

	if got != 7 {
		t.Fatalf("the first tick offered task %d, want the oldest (7) — order still decides where it starts", got)
	}
}

// Rotation follows the queue order rather than jumping about, so the oldest
// still goes first and the rest follow it in turn.
func TestPollOnceRotatesInQueueOrder(t *testing.T) {
	ps := pushServer(t)
	repo := pendingTaskRepo(
		model.Task{ID: 9, Status: "notstarted", Assignee: "agent", SortOrder: 3},
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent", SortOrder: 1},
		model.Task{ID: 8, Status: "notstarted", Assignee: "agent", SortOrder: 2},
	)

	var got []int64
	for i := 0; i < 4; i++ {
		got = append(got, ps.pollOnce(repo))
	}

	want := []int64{7, 8, 9, 7}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("offers were %v, want %v", got, want)
		}
	}
}

// A task taken off the queue does not disturb the rotation of what is left.
func TestPollOnceKeepsRotatingWhenATaskIsTaken(t *testing.T) {
	ps := pushServer(t)
	all := pendingTaskRepo(
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent", SortOrder: 1},
		model.Task{ID: 8, Status: "notstarted", Assignee: "agent", SortOrder: 2},
	)

	if got := ps.pollOnce(all); got != 7 {
		t.Fatalf("first offer was %d, want 7", got)
	}
	// 7 is taken; only 8 is left, and it must be offered rather than the
	// rotation stalling on a task that is no longer pending.
	only8 := pendingTaskRepo(
		model.Task{ID: 7, Status: "ongoing", Assignee: "agent", SortOrder: 1},
		model.Task{ID: 8, Status: "notstarted", Assignee: "agent", SortOrder: 2},
	)
	if got := ps.pollOnce(only8); got != 0 {
		t.Fatalf("offered task %d while another was ongoing, want none", got)
	}
	done := pendingTaskRepo(
		model.Task{ID: 7, Status: "completed", Assignee: "agent", SortOrder: 1},
		model.Task{ID: 8, Status: "notstarted", Assignee: "agent", SortOrder: 2},
	)
	if got := ps.pollOnce(done); got != 8 {
		t.Fatalf("offered task %d, want the one still pending (8)", got)
	}
}

// The scan used to stop at the first ongoing task, so the set handed to
// reconcileClearedTaskIDs was truncated and silently dropped the record for
// every task listed after it — which would then be cleared a second time.
func TestPollOnceReconcilesEveryPendingTaskNotJustThoseBeforeAnOngoingOne(t *testing.T) {
	ps := pushServer(t)
	clears := countingClear(ps, nil)
	pending := model.Task{ID: 8, Status: "notstarted", Assignee: "agent", ClearContext: true}

	// Cleared once while nothing is ongoing.
	ps.pollOnce(pendingTaskRepo(pending))
	if *clears != 1 {
		t.Fatalf("cleared %d times, want 1", *clears)
	}

	// Now something is ongoing and is listed *before* the pending task. The
	// pending task is still pending, so its clear must still be remembered.
	ps.pollOnce(pendingTaskRepo(
		model.Task{ID: 1, Status: "ongoing", Assignee: "agent"},
		pending,
	))

	// Back to nothing ongoing: the task is offered again, and must not be
	// cleared again.
	ps.pollOnce(pendingTaskRepo(pending))
	if *clears != 1 {
		t.Fatalf("cleared %d times, want 1 — the ongoing task truncated the reconcile set", *clears)
	}
}

// The poller reads the same limit the handler does: a gateway that will run
// four tasks at once and is running one has room for another. Gating on "is
// anything ongoing" made the concurrency control meaningless — the workspace
// would hand over one task and then wait for it, whatever the agent said it
// could take.
func TestPollOnceOffersWhileTheAgentHasRoomForMoreTasks(t *testing.T) {
	ps := pushServer(t)
	ps.agentConcurrency = map[string]agentlink.ConcurrencySnapshot{}
	sessID := connectedServer(t, ps, "acp-gateway")
	defer streamFor(ps, sessID)()
	ps.agentConcurrency[sessID] = agentlink.ConcurrencySnapshot{MaxConcurrency: 4, CanSet: true}

	got := ps.pollOnce(pendingTaskRepo(
		model.Task{ID: 1, Status: "ongoing", Assignee: "agent"},
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent"},
	))

	if got != 7 {
		t.Fatalf("offered task %d while three of four slots were free, want 7", got)
	}
}

// Full is full.
func TestPollOnceOffersNothingWhenEverySlotIsBusy(t *testing.T) {
	ps := pushServer(t)
	ps.agentConcurrency = map[string]agentlink.ConcurrencySnapshot{}
	sessID := connectedServer(t, ps, "acp-gateway")
	defer streamFor(ps, sessID)()
	ps.agentConcurrency[sessID] = agentlink.ConcurrencySnapshot{MaxConcurrency: 2, CanSet: true}

	got := ps.pollOnce(pendingTaskRepo(
		model.Task{ID: 1, Status: "ongoing", Assignee: "agent"},
		model.Task{ID: 2, Status: "ongoing", Assignee: "agent"},
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent"},
	))

	if got != 0 {
		t.Fatalf("offered task %d with both slots busy, want none", got)
	}
}

// A poll asks two separate questions — never a single query naming both
// statuses — and both ask only about the agent's own tasks. Unfiltered, a
// combined query returned the hundred most recent tasks of any status or
// assignee: a workspace with a hundred completed or human tasks could push
// the agent's pending ones out of the window, and a human's own ongoing task
// counted toward the agent's concurrency, blocking every push behind it.
func TestPollOnceAsksTwoSeparateQueriesForItsOwnTasks(t *testing.T) {
	ps := pushServer(t)
	repo, asked := recordingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent"})
	ps.pollOnce(repo)

	if len(*asked) != 2 {
		t.Fatalf("made %d queries, want exactly 2 (ongoing, then notstarted)", len(*asked))
	}
	if got := (*asked)[0].Status; len(got) != 1 || got[0] != "ongoing" {
		t.Fatalf("first query asked for statuses %v, want exactly [ongoing]", got)
	}
	if got := (*asked)[1].Status; len(got) != 1 || got[0] != "notstarted" {
		t.Fatalf("second query asked for statuses %v, want exactly [notstarted]", got)
	}
	for i, req := range *asked {
		if req.Assignee != "agent" {
			t.Fatalf("query %d asked for assignee %q, want \"agent\"", i, req.Assignee)
		}
	}
}

// The busy check never asks about the agent's pending backlog: no reason to
// list it when there is already no room to offer anything from it. (A full
// tick can still make a second call — fetching one ongoing row to name in the
// hourly status-check message — but that is never a "notstarted" query.)
func TestPollOnceSkipsThePendingQueryWhenFull(t *testing.T) {
	ps := pushServer(t)
	repo, asked := recordingTaskRepo(model.Task{ID: 1, Status: "ongoing", Assignee: "agent"})
	ps.pollOnce(repo)

	for _, req := range *asked {
		if len(req.Status) == 1 && req.Status[0] == "notstarted" {
			t.Fatalf("queried notstarted tasks while full: %+v", req)
		}
	}
}

// Nothing is offered while a task is ongoing: the agent already has work.
func TestPollOnceOffersNothingWhileATaskIsOngoing(t *testing.T) {
	ps := pushServer(t)

	got := ps.pollOnce(pendingTaskRepo(
		model.Task{ID: 1, Status: "ongoing", Assignee: "agent"},
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent"},
	))

	if got != 0 {
		t.Fatalf("offered task %d while another was ongoing, want none", got)
	}
}

// An archived workspace offers nothing at all.
func TestPollOnceSkipsAnArchivedWorkspace(t *testing.T) {
	ps := pushServer(t)
	now := time.Now()
	ps.archivedAt = &now

	if got := ps.pollOnce(pendingTaskRepo(model.Task{ID: 7, Status: "notstarted", Assignee: "agent"})); got != 0 {
		t.Fatalf("an archived workspace offered task %d", got)
	}
}

// A repository that cannot answer ends the tick rather than pushing on a guess.
func TestPollOnceSurvivesARepositoryError(t *testing.T) {
	ps := pushServer(t)

	got := ps.pollOnce(listTasksFunc(func(context.Context, entity.ListTasksRequest, int64) ([]model.Task, error) {
		return nil, errNotTheRealThing
	}))

	if got != 0 {
		t.Fatalf("a failed listing offered task %d", got)
	}
}

// Two tasks waiting means the order they were put in decides, not the order the
// repository happened to list them in — and one tick hands over one task.
func TestPollOnceOffersThePendingTasksInOrder(t *testing.T) {
	ps := pushServer(t)

	got := ps.pollOnce(pendingTaskRepo(
		model.Task{ID: 8, Status: "notstarted", Assignee: "agent", SortOrder: 2},
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent", SortOrder: 1},
	))

	if got != 7 {
		t.Fatalf("offered task %d, want the one sorted first", got)
	}
}

// An unordered pair falls back to when each was created, and then to its ID —
// two tasks made in the same millisecond still have to have an order.
func TestPollOnceFallsBackToCreationTimeThenID(t *testing.T) {
	ps := pushServer(t)
	made := time.Now()

	got := ps.pollOnce(pendingTaskRepo(
		model.Task{ID: 9, Status: "notstarted", Assignee: "agent", CreatedAt: made},
		model.Task{ID: 7, Status: "notstarted", Assignee: "agent", CreatedAt: made},
		model.Task{ID: 8, Status: "notstarted", Assignee: "agent", CreatedAt: made.Add(-time.Hour)},
	))

	if got != 8 {
		t.Fatalf("offered task %d, want the oldest", got)
	}
}

// A task's attachments travel with it, so the agent can fetch them.
func TestPollOnceCarriesTheTaskAttachments(t *testing.T) {
	ps := pushServer(t)

	got := ps.pollOnce(pendingTaskRepo(model.Task{
		ID: 7, Status: "notstarted", Assignee: "agent",
		Attachments: []byte(`[{"id":"a1","filename":"trace.log","mimeType":"text/plain"}]`),
	}))

	if got != 7 {
		t.Fatalf("offered task %d, want 7", got)
	}
}

// A push reaches a gateway that is really there. Nothing is reported back about
// it — the retry above is what covers a push that lands nowhere — so this is
// here to prove the reflection into the SDK's private conn still works at all.
func TestSendChannelNotificationReachesALiveSession(t *testing.T) {
	ps := pushServer(t)
	sessID := connectedServer(t, ps, "acp-gateway")
	defer streamFor(ps, sessID)()

	ps.SendChannelNotification(context.Background(), 7, "next task")
}

// A workspace whose MCP server has not been built has nothing to notify, and
// must say so rather than panicking on a nil server's session list.
func TestSendChannelNotificationWithNoServerDoesNotPanic(t *testing.T) {
	ps := pushServer(t)

	ps.SendChannelNotification(context.Background(), 7, "next task")
}

// pollRepo is a repository that answers only ListTasks, so StartPoller's own
// loop can be driven without standing up the real one.
type pollRepo struct {
	base.Repository
	list listTasksFunc
}

func (r pollRepo) ListTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error) {
	return r.list(ctx, req, userID)
}

func (r pollRepo) CountTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) (int64, error) {
	return listTasksFunc(r.list).CountTasks(ctx, req, userID)
}

// The loop itself: it ticks until the server is closed, and closing it is what
// stops the goroutine rather than leaking it for the life of the process.
func TestStartPollerTicksAndStopsWhenTheServerCloses(t *testing.T) {
	previous := pollInterval
	pollInterval = time.Millisecond
	t.Cleanup(func() { pollInterval = previous })

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pubsubMock := mock_pubsub.NewMockService(ctrl)
	pubsubMock.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes()
	tokenMock := mock_auth.NewMockTokenService(ctrl)
	tokenMock.EXPECT().ValidateToken(gomock.Any()).Return(nil, errNotTheRealThing).AnyTimes()

	// Not pushServer: this one needs `done` still open, since closing it is the
	// thing under test.
	ps := &WorkspaceServer{
		workspaceID: 100,
		userID:      monoflake.ID(100).String(),
		done:        make(chan struct{}),
		bus:         eventbus.New(),
		pubsub:      pubsubMock,
		tokenSvc:    tokenMock,
	}

	ticked := make(chan struct{})
	var once sync.Once
	ps.StartPoller(pollRepo{list: func(context.Context, entity.ListTasksRequest, int64) ([]model.Task, error) {
		once.Do(func() { close(ticked) })
		return []model.Task{{ID: 7, Status: "notstarted", Assignee: "agent"}}, nil
	}})

	select {
	case <-ticked:
	case <-time.After(5 * time.Second):
		t.Fatal("the poller never ticked")
	}

	close(ps.done)
	// Nothing to assert beyond not hanging: the goroutine's only exit is the
	// done channel, so a poller that ignored it would go on ticking here.
}
