package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A server with the memory tools wired to an in-memory store, so a save and the
// load that follows it exercise the same path a real agent takes.
type memoryStore struct {
	saved    map[string]string
	loadErr  error
	saveErr  error
	loadName string
	saveName string
}

func newMemoryServer(t *testing.T, store *memoryStore) *WorkspaceServer {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pubsubMock := mock_pubsub.NewMockService(ctrl)
	pubsubMock.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes()

	if store.saved == nil {
		store.saved = map[string]string{}
	}
	return &WorkspaceServer{
		workspaceID: 100,
		pubsub:      pubsubMock,
		loadMemory: func(ctx context.Context, name string) (string, bool, error) {
			store.loadName = name
			if store.loadErr != nil {
				return "", false, store.loadErr
			}
			content, ok := store.saved[name]
			return content, ok, nil
		},
		saveMemory: func(ctx context.Context, name, content string) error {
			store.saveName = name
			if store.saveErr != nil {
				return store.saveErr
			}
			store.saved[name] = content
			return nil
		},
	}
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result carried no content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, not text", res.Content[0])
	}
	return text.Text
}

func TestSaveThenLoadMemory(t *testing.T) {
	store := &memoryStore{}
	ps := newMemoryServer(t, store)
	ctx := context.Background()

	saved, _, err := ps.handleSaveMemory(ctx, &mcp.CallToolRequest{}, SaveMemoryParams{Content: "# Index\n- deploys.md"})
	if err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if saved.IsError {
		t.Fatalf("saveMemory reported an error: %s", resultText(t, saved))
	}

	loaded, _, err := ps.handleLoadMemory(ctx, &mcp.CallToolRequest{}, LoadMemoryParams{})
	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if got := resultText(t, loaded); got != "# Index\n- deploys.md" {
		t.Errorf("loaded %q", got)
	}
}

// With no name both tools work on the index, which is what makes a bare
// loadMemory() call the right first move in a task.
func TestMemoryToolsDefaultToTheIndex(t *testing.T) {
	store := &memoryStore{}
	ps := newMemoryServer(t, store)
	ctx := context.Background()

	if _, _, err := ps.handleSaveMemory(ctx, &mcp.CallToolRequest{}, SaveMemoryParams{Content: "x"}); err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if store.saveName != DefaultMemoryName {
		t.Errorf("saved under %q, want %q", store.saveName, DefaultMemoryName)
	}

	if _, _, err := ps.handleLoadMemory(ctx, &mcp.CallToolRequest{}, LoadMemoryParams{}); err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if store.loadName != DefaultMemoryName {
		t.Errorf("loaded %q, want %q", store.loadName, DefaultMemoryName)
	}
}

func TestSaveMemoryOverwrites(t *testing.T) {
	store := &memoryStore{}
	ps := newMemoryServer(t, store)
	ctx := context.Background()

	for _, content := range []string{"first", "second"} {
		if _, _, err := ps.handleSaveMemory(ctx, &mcp.CallToolRequest{}, SaveMemoryParams{Name: "notes.md", Content: content}); err != nil {
			t.Fatalf("saveMemory: %v", err)
		}
	}

	if store.saved["notes.md"] != "second" {
		t.Errorf("got %q, want the later write", store.saved["notes.md"])
	}
}

// A memory nobody has written is the ordinary state of a fresh workspace.
// Answering with an error would teach agents to stop asking.
func TestLoadMemoryMissIsNotAnError(t *testing.T) {
	ps := newMemoryServer(t, &memoryStore{})

	res, _, err := ps.handleLoadMemory(context.Background(), &mcp.CallToolRequest{}, LoadMemoryParams{Name: "never-written.md"})

	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if res.IsError {
		t.Fatalf("a miss must not be an error: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "never-written.md") || !strings.Contains(text, "saveMemory") {
		t.Errorf("a miss should name the memory and point at the fix; got %q", text)
	}
}

// Refused, not truncated: an agent told its memory is too large can decide what
// to drop, which is a judgement only it can make.
func TestSaveMemoryRefusesOversizedContent(t *testing.T) {
	store := &memoryStore{}
	ps := newMemoryServer(t, store)

	res, _, err := ps.handleSaveMemory(context.Background(), &mcp.CallToolRequest{}, SaveMemoryParams{
		Content: strings.Repeat("a", MaxMemoryBytes+1),
	})

	if err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if !res.IsError {
		t.Fatal("an oversized memory must be refused")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "16384") {
		t.Errorf("the refusal should name the limit; got %q", text)
	}
	if len(store.saved) != 0 {
		t.Error("nothing should have been stored")
	}
}

func TestSaveMemoryAcceptsContentExactlyAtTheLimit(t *testing.T) {
	store := &memoryStore{}
	ps := newMemoryServer(t, store)

	res, _, err := ps.handleSaveMemory(context.Background(), &mcp.CallToolRequest{}, SaveMemoryParams{
		Name: "big.md", Content: strings.Repeat("a", MaxMemoryBytes),
	})

	if err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if res.IsError {
		t.Fatalf("the limit itself must be allowed: %s", resultText(t, res))
	}
	if len(store.saved["big.md"]) != MaxMemoryBytes {
		t.Error("content at the limit should be stored whole")
	}
}

// The cap counts bytes, not characters: 16k multi-byte characters is far more
// than 16 KiB, and letting it through would overflow what the column holds.
func TestSaveMemoryCountsBytesNotCharacters(t *testing.T) {
	ps := newMemoryServer(t, &memoryStore{})

	res, _, err := ps.handleSaveMemory(context.Background(), &mcp.CallToolRequest{}, SaveMemoryParams{
		Content: strings.Repeat("é", MaxMemoryBytes-10),
	})

	if err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if !res.IsError {
		t.Error("content under the character count but over the byte cap must be refused")
	}
}

func TestMemoryNamesThatMustBeRefused(t *testing.T) {
	ps := newMemoryServer(t, &memoryStore{})
	ctx := context.Background()

	for _, tc := range []struct{ name, why string }{
		{strings.Repeat("n", MaxMemoryNameLength+1), "longer than the column"},
		{" MEMORY.md", "leading whitespace"},
		{"MEMORY.md ", "trailing whitespace"},
		{"notes/deploys.md", "a path separator"},
		{`notes\deploys.md`, "a windows path separator"},
		{"notes\x00.md", "a control character"},
	} {
		save, _, err := ps.handleSaveMemory(ctx, &mcp.CallToolRequest{}, SaveMemoryParams{Name: tc.name, Content: "x"})
		if err != nil {
			t.Fatalf("%s: %v", tc.why, err)
		}
		if !save.IsError {
			t.Errorf("saveMemory accepted a name with %s: %q", tc.why, tc.name)
		}

		// Refused on the way in as well, so a name that could never be written
		// is not silently a miss on read.
		load, _, err := ps.handleLoadMemory(ctx, &mcp.CallToolRequest{}, LoadMemoryParams{Name: tc.name})
		if err != nil {
			t.Fatalf("%s: %v", tc.why, err)
		}
		if !load.IsError {
			t.Errorf("loadMemory accepted a name with %s: %q", tc.why, tc.name)
		}
	}
}

func TestMemoryNameAtTheLimitIsAccepted(t *testing.T) {
	store := &memoryStore{}
	ps := newMemoryServer(t, store)
	name := strings.Repeat("n", MaxMemoryNameLength)

	res, _, err := ps.handleSaveMemory(context.Background(), &mcp.CallToolRequest{}, SaveMemoryParams{Name: name, Content: "x"})

	if err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if res.IsError {
		t.Fatalf("a name at the limit must be allowed: %s", resultText(t, res))
	}
}

// A name is counted in characters, as the column is, so an accented name of 32
// characters fits even though it is more than 32 bytes.
func TestMemoryNameIsCountedInCharacters(t *testing.T) {
	ps := newMemoryServer(t, &memoryStore{})

	res, _, err := ps.handleSaveMemory(context.Background(), &mcp.CallToolRequest{}, SaveMemoryParams{
		Name: strings.Repeat("é", MaxMemoryNameLength), Content: "x",
	})

	if err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if res.IsError {
		t.Errorf("32 characters must fit whatever they encode to: %s", resultText(t, res))
	}
}

func TestMemoryToolsReportStorageFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("save", func(t *testing.T) {
		ps := newMemoryServer(t, &memoryStore{saveErr: errors.New("disk on fire")})

		res, _, err := ps.handleSaveMemory(ctx, &mcp.CallToolRequest{}, SaveMemoryParams{Content: "x"})

		if err != nil {
			t.Fatalf("saveMemory: %v", err)
		}
		// The agent has to hear that its notes did not land; reporting success
		// would leave it trusting a memory that was never written.
		if !res.IsError || !strings.Contains(resultText(t, res), "disk on fire") {
			t.Errorf("got %q", resultText(t, res))
		}
	})

	t.Run("load", func(t *testing.T) {
		ps := newMemoryServer(t, &memoryStore{loadErr: errors.New("disk on fire")})

		res, _, err := ps.handleLoadMemory(ctx, &mcp.CallToolRequest{}, LoadMemoryParams{})

		if err != nil {
			t.Fatalf("loadMemory: %v", err)
		}
		// Distinct from a miss: "nothing saved yet" and "could not read" call
		// for different responses from the agent.
		if !res.IsError || !strings.Contains(resultText(t, res), "disk on fire") {
			t.Errorf("got %q", resultText(t, res))
		}
	})
}

// A server built without the memory funcs must say so rather than panic — the
// tools are registered unconditionally, so a server assembled without them is a
// wiring mistake that should surface as a message.
func TestMemoryToolsWithoutStorage(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pubsubMock := mock_pubsub.NewMockService(ctrl)
	pubsubMock.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes()
	ps := &WorkspaceServer{workspaceID: 100, pubsub: pubsubMock}
	ctx := context.Background()

	load, _, err := ps.handleLoadMemory(ctx, &mcp.CallToolRequest{}, LoadMemoryParams{})
	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if !load.IsError {
		t.Error("loadMemory should report that memory is unavailable")
	}

	save, _, err := ps.handleSaveMemory(ctx, &mcp.CallToolRequest{}, SaveMemoryParams{Content: "x"})
	if err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	if !save.IsError {
		t.Error("saveMemory should report that memory is unavailable")
	}
}
