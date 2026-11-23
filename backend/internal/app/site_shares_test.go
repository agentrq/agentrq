// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/sitetools"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_repository "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
)

var errRepo = errors.New("database unavailable")

func newSiteShareStore(t *testing.T) (siteShareStore, *mock_repository.MockRepository, *mock_idgen.MockService) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mock_repository.NewMockRepository(ctrl)
	ids := mock_idgen.NewMockService(ctrl)
	return siteShareStore{repo: repo, ids: ids}, repo, ids
}

// expectWorkspace answers the lookup of workspace id, a fork of forkOf when
// that is not zero.
func expectWorkspace(repo *mock_repository.MockRepository, id, forkOf int64) {
	repo.EXPECT().SystemGetWorkspace(gomock.Any(), id).Return(model.Workspace{ID: id, UserID: 7, ForkOfID: forkOf}, nil)
}

func TestSiteShareStoreOwnsWorkspace(t *testing.T) {
	s, repo, _ := newSiteShareStore(t)
	repo.EXPECT().CheckWorkspaceAccess(gomock.Any(), int64(70), int64(7)).Return(true, nil)
	if ok, err := s.OwnsWorkspace(context.Background(), 7, 70); !ok || err != nil {
		t.Fatalf("OwnsWorkspace = %v, %v", ok, err)
	}
}

func TestSiteShareStoreUpsert(t *testing.T) {
	share := sitetools.Share{
		UserID: 7, WorkspaceID: 70, Origin: "https://a.com", BrowserID: "b1", InstanceID: "pod-a",
		LastURL: "https://a.com/x", Tools: []sitetools.Tool{{Name: "search"}},
	}
	for _, tc := range []struct {
		name     string
		sharedTo int64
		existing []model.SiteShare
		changed  bool
	}{
		{"new", 70, []model.SiteShare{{Origin: "https://b.com", WorkspaceID: 70}}, true},
		{"same workspace", 70, []model.SiteShare{{Origin: "https://a.com", WorkspaceID: 70}}, false},
		{"moved", 70, []model.SiteShare{{Origin: "https://a.com", WorkspaceID: 71}}, true},
		// A site shared with a fork is filed under its parent, where the fork reads it.
		{"to a fork", 72, []model.SiteShare{{Origin: "https://a.com", WorkspaceID: 70}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, ids := newSiteShareStore(t)
			if tc.sharedTo == 70 {
				expectWorkspace(repo, 70, 0)
			} else {
				expectWorkspace(repo, tc.sharedTo, 70)
			}
			share := share
			share.WorkspaceID = tc.sharedTo
			repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return(tc.existing, nil)
			ids.EXPECT().NextID().Return(int64(900))
			repo.EXPECT().UpsertSiteShare(gomock.Any(), model.SiteShare{
				ID: 900, UserID: 7, WorkspaceID: 70, Origin: "https://a.com", BrowserID: "b1", InstanceID: "pod-a",
				LastURL: "https://a.com/x", Tools: `[{"name":"search","description":""}]`, AlwaysAllow: "[]",
			}).Return(model.SiteShare{}, nil)
			changed, err := s.Upsert(context.Background(), share)
			if err != nil || changed != tc.changed {
				t.Fatalf("Upsert = %v, %v; want changed %v", changed, err, tc.changed)
			}
		})
	}
}

// No tools is an empty list in the row, not "null".
func TestSiteShareStoreUpsertNoTools(t *testing.T) {
	s, repo, ids := newSiteShareStore(t)
	expectWorkspace(repo, 70, 0)
	repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return(nil, nil)
	ids.EXPECT().NextID().Return(int64(900))
	repo.EXPECT().UpsertSiteShare(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m model.SiteShare) (model.SiteShare, error) {
			if m.Tools != "[]" {
				t.Errorf("tools = %q, want []", m.Tools)
			}
			return m, nil
		})
	if _, err := s.Upsert(context.Background(), sitetools.Share{UserID: 7, WorkspaceID: 70, Origin: "https://a.com"}); err != nil {
		t.Fatal(err)
	}
}

func TestSiteShareStoreUpsertErrors(t *testing.T) {
	t.Run("workspace", func(t *testing.T) {
		s, repo, _ := newSiteShareStore(t)
		repo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(70)).Return(model.Workspace{}, errRepo)
		if _, err := s.Upsert(context.Background(), sitetools.Share{UserID: 7, WorkspaceID: 70}); !errors.Is(err, errRepo) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("list", func(t *testing.T) {
		s, repo, _ := newSiteShareStore(t)
		expectWorkspace(repo, 0, 0)
		repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return(nil, errRepo)
		if _, err := s.Upsert(context.Background(), sitetools.Share{UserID: 7}); !errors.Is(err, errRepo) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("upsert", func(t *testing.T) {
		s, repo, ids := newSiteShareStore(t)
		expectWorkspace(repo, 0, 0)
		repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return(nil, nil)
		ids.EXPECT().NextID().Return(int64(900))
		repo.EXPECT().UpsertSiteShare(gomock.Any(), gomock.Any()).Return(model.SiteShare{}, errRepo)
		if _, err := s.Upsert(context.Background(), sitetools.Share{UserID: 7}); !errors.Is(err, errRepo) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestSiteShareStoreDelete(t *testing.T) {
	t.Run("shared", func(t *testing.T) {
		s, repo, _ := newSiteShareStore(t)
		repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return([]model.SiteShare{{Origin: "https://a.com", WorkspaceID: 70}}, nil)
		repo.EXPECT().DeleteSiteShare(gomock.Any(), int64(7), "https://a.com").Return(true, nil)
		ws, deleted, err := s.Delete(context.Background(), 7, "https://a.com")
		if ws != 70 || !deleted || err != nil {
			t.Fatalf("Delete = %d, %v, %v", ws, deleted, err)
		}
	})
	t.Run("not shared", func(t *testing.T) {
		s, repo, _ := newSiteShareStore(t)
		repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return(nil, nil)
		repo.EXPECT().DeleteSiteShare(gomock.Any(), int64(7), "https://a.com").Return(false, nil)
		if _, deleted, err := s.Delete(context.Background(), 7, "https://a.com"); deleted || err != nil {
			t.Fatalf("Delete = %v, %v", deleted, err)
		}
	})
	t.Run("list error", func(t *testing.T) {
		s, repo, _ := newSiteShareStore(t)
		repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return(nil, errRepo)
		if _, _, err := s.Delete(context.Background(), 7, "https://a.com"); !errors.Is(err, errRepo) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestSiteShareStoreListForUser(t *testing.T) {
	s, repo, _ := newSiteShareStore(t)
	repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return([]model.SiteShare{
		{UserID: 7, WorkspaceID: 70, Origin: "https://a.com", BrowserID: "b1", InstanceID: "pod-a", LastURL: "https://a.com/x",
			Tools: `[{"name":"search","description":"d"}]`, AlwaysAllow: `["search"]`},
	}, nil)
	got, err := s.ListForUser(context.Background(), 7)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListForUser = %+v, %v", got, err)
	}
	g := got[0]
	if g.UserID != 7 || g.WorkspaceID != 70 || g.Origin != "https://a.com" || g.BrowserID != "b1" || g.InstanceID != "pod-a" ||
		g.LastURL != "https://a.com/x" || len(g.Tools) != 1 || g.Tools[0].Description != "d" || len(g.AlwaysAllow) != 1 {
		t.Errorf("share = %+v", g)
	}

	s, repo, _ = newSiteShareStore(t)
	repo.EXPECT().ListSiteSharesForUser(gomock.Any(), int64(7)).Return(nil, errRepo)
	if _, err := s.ListForUser(context.Background(), 7); !errors.Is(err, errRepo) {
		t.Fatalf("err = %v", err)
	}
}

func TestBrowserTicketAuth(t *testing.T) {
	tokens := newTestTokenService(t)
	authFn := browserTicketAuth(tokens)
	user := monoflake.ID(7).String()

	ticket, _ := tokens.CreateBrowserTicket(user)
	if id, err := authFn(httptest.NewRequest("GET", "/api/v1/browser/connect?ticket="+ticket, nil)); id != 7 || err != nil {
		t.Fatalf("auth = %d, %v", id, err)
	}

	terminal, _ := tokens.CreateTerminalTicket(user, "500")
	unreadable, _ := tokens.CreateBrowserTicket("")
	for name, q := range map[string]string{"none": "", "terminal ticket": terminal, "no subject": unreadable} {
		if _, err := authFn(httptest.NewRequest("GET", "/api/v1/browser/connect?ticket="+q, nil)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

type siteShareRecorder struct {
	got []entity.RecordSiteShareRequest
}

func (r *siteShareRecorder) RecordSiteShare(_ context.Context, req entity.RecordSiteShareRequest) {
	r.got = append(r.got, req)
}

func TestCountSiteShare(t *testing.T) {
	rec := &siteShareRecorder{}
	countSiteShare(rec, true)(7, 70, "https://a.com")
	countSiteShare(rec, false)(7, 71, "https://a.com")
	want := []entity.RecordSiteShareRequest{{UserID: 7, WorkspaceID: 70, Shared: true}, {UserID: 7, WorkspaceID: 71}}
	if len(rec.got) != 2 || rec.got[0] != want[0] || rec.got[1] != want[1] {
		t.Fatalf("recorded %+v, want %+v", rec.got, want)
	}
}
