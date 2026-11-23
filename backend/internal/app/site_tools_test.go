// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"gorm.io/gorm"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/agentrq/agentrq/backend/internal/controller/sitetools"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
)

// browserConn answers every call frame with reply, as the extension would.
type browserConn struct {
	hub   *sitetools.Hub
	reply sitetools.Frame
	sent  []sitetools.Frame
}

func (c *browserConn) Send(f sitetools.Frame) error {
	c.sent = append(c.sent, f)
	r := c.reply
	r.Type, r.CallID = sitetools.FrameResult, f.CallID
	go c.hub.Deliver(r)
	return nil
}

func (c *browserConn) Close() error { return nil }

func newSiteToolsBackend(t *testing.T) (siteToolsBackend, *mock_repository.MockRepository, *sitetools.Hub) {
	t.Helper()
	repo := mock_repository.NewMockRepository(gomock.NewController(t))
	hub := sitetools.NewHub("here", func() string { return "call-1" })
	return siteToolsBackend{repo: repo, hub: hub}, repo, hub
}

func TestSiteToolsBackendList(t *testing.T) {
	b, repo, hub := newSiteToolsBackend(t)
	hub.Add(7, "b1", &browserConn{hub: hub})
	hub.Add(7, "b3", &browserConn{hub: hub})
	repo.EXPECT().ListSiteSharesForWorkspace(gomock.Any(), int64(70), int64(7)).Return([]model.SiteShare{
		{Origin: "https://a.test", BrowserID: "b1", InstanceID: "here", Tools: `[{"name":"search"}]`, AlwaysAllow: `["search"]`},
		{Origin: "https://b.test", BrowserID: "b2", InstanceID: "here", Tools: `[]`, AlwaysAllow: `[]`},
		{Origin: "https://c.test", BrowserID: "b3", InstanceID: "elsewhere", Tools: `[]`, AlwaysAllow: `[]`},
		{Origin: "https://d.test", BrowserID: "b1", InstanceID: "here"},
	}, nil)

	got, err := b.List(context.Background(), 70, 7)
	if err != nil || len(got) != 4 {
		t.Fatalf("List = %+v, %v", got, err)
	}
	if !got[0].Online || got[0].Tools[0].Name != "search" || got[0].AlwaysAllow[0] != "search" || got[0].BrowserID != "b1" {
		t.Errorf("connected share = %+v", got[0])
	}
	if got[1].Online {
		t.Error("a browser with no socket here is online")
	}
	if got[2].Online {
		t.Error("a share recorded on another instance is online")
	}
	if got[3].Tools == nil {
		t.Error("unreadable tools came back as null")
	}
}

func TestSiteToolsBackendListError(t *testing.T) {
	b, repo, _ := newSiteToolsBackend(t)
	repo.EXPECT().ListSiteSharesForWorkspace(gomock.Any(), int64(70), int64(7)).Return(nil, errRepo)
	if _, err := b.List(context.Background(), 70, 7); !errors.Is(err, errRepo) {
		t.Fatalf("err = %v", err)
	}
}

func TestSiteToolsBackendGet(t *testing.T) {
	b, repo, _ := newSiteToolsBackend(t)
	repo.EXPECT().GetSiteShare(gomock.Any(), int64(70), int64(7), "https://a.test").
		Return(model.SiteShare{Origin: "https://a.test", Tools: `[{"name":"search"}]`}, nil)
	repo.EXPECT().GetSiteShare(gomock.Any(), int64(70), int64(7), "https://none.test").
		Return(model.SiteShare{}, gorm.ErrRecordNotFound)
	repo.EXPECT().GetSiteShare(gomock.Any(), int64(70), int64(7), "https://down.test").
		Return(model.SiteShare{}, errRepo)

	if v, ok, err := b.Get(context.Background(), 70, 7, "https://a.test"); !ok || err != nil || v.Tools[0].Name != "search" {
		t.Errorf("found = %+v, %v, %v", v, ok, err)
	}
	if _, ok, err := b.Get(context.Background(), 70, 7, "https://none.test"); ok || err != nil {
		t.Errorf("missing = %v, %v", ok, err)
	}
	if _, _, err := b.Get(context.Background(), 70, 7, "https://down.test"); !errors.Is(err, errRepo) {
		t.Errorf("error = %v", err)
	}
}

func TestSiteToolsBackendAllowAlways(t *testing.T) {
	b, repo, _ := newSiteToolsBackend(t)
	repo.EXPECT().GetSiteShare(gomock.Any(), int64(70), int64(7), "https://a.test").
		Return(model.SiteShare{ID: 9, AlwaysAllow: `["fork"]`}, nil).Times(2)
	repo.EXPECT().SetSiteShareAlwaysAllow(gomock.Any(), int64(9), []string{"fork", "star"}).Return(nil)

	if err := b.AllowAlways(context.Background(), 70, 7, "https://a.test", "star"); err != nil {
		t.Fatal(err)
	}
	// Already allowed: nothing to write.
	if err := b.AllowAlways(context.Background(), 70, 7, "https://a.test", "fork"); err != nil {
		t.Fatal(err)
	}
}

func TestSiteToolsBackendAllowAlwaysLookupFails(t *testing.T) {
	b, repo, _ := newSiteToolsBackend(t)
	repo.EXPECT().GetSiteShare(gomock.Any(), int64(70), int64(7), "https://a.test").Return(model.SiteShare{}, errRepo)
	if err := b.AllowAlways(context.Background(), 70, 7, "https://a.test", "star"); !errors.Is(err, errRepo) {
		t.Fatalf("err = %v", err)
	}
}

func TestSiteToolsBackendCall(t *testing.T) {
	b, _, hub := newSiteToolsBackend(t)
	conn := &browserConn{hub: hub, reply: sitetools.Frame{Text: "3 results"}}
	hub.Add(7, "b1", conn)
	share := mcp.SiteShareView{Site: "https://a.test", BrowserID: "b1", InstanceID: "here"}

	text, err := b.Call(context.Background(), 7, share, "search", json.RawMessage(`{"q":"x"}`))
	if err != nil || text != "3 results" {
		t.Fatalf("Call = %q, %v", text, err)
	}
	if f := conn.sent[0]; f.Origin != "https://a.test" || f.Tool != "search" || string(f.Arguments) != `{"q":"x"}` {
		t.Errorf("sent %+v", f)
	}

	conn.reply = sitetools.Frame{Error: "not signed in"}
	var failed *mcp.SiteToolFailedError
	if _, err := b.Call(context.Background(), 7, share, "search", nil); !errors.As(err, &failed) || failed.Message != "not signed in" {
		t.Errorf("page error = %v", err)
	}

	// The socket moved here since the share was recorded: still reachable.
	share.InstanceID = "elsewhere"
	conn.reply = sitetools.Frame{Text: "ok"}
	if text, err := b.Call(context.Background(), 7, share, "search", nil); err != nil || text != "ok" {
		t.Errorf("moved socket = %q, %v", text, err)
	}
}

func TestSiteToolsBackendCallUnreachable(t *testing.T) {
	b, _, _ := newSiteToolsBackend(t)
	here := mcp.SiteShareView{Site: "https://a.test", BrowserID: "b1", InstanceID: "here"}
	if _, err := b.Call(context.Background(), 7, here, "search", nil); !errors.Is(err, sitetools.ErrOffline) {
		t.Errorf("offline here = %v", err)
	}
	there := here
	there.InstanceID = "elsewhere"
	if _, err := b.Call(context.Background(), 7, there, "search", nil); !errors.Is(err, mcp.ErrSiteOtherInstance) {
		t.Errorf("another instance = %v", err)
	}
}
