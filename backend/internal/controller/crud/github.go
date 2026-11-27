// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/githubapp"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// GitHubController connects an account to the server's GitHub App, so that
// skills can be imported from its private repositories.
type GitHubController interface {
	GetGitHubConnection(ctx context.Context, req entity.GetGitHubConnectionRequest) (*entity.GitHubConnectionResponse, error)
	ConnectGitHub(ctx context.Context, req entity.ConnectGitHubRequest) (*entity.GitHubConnectionResponse, error)
	DisconnectGitHub(ctx context.Context, req entity.DisconnectGitHubRequest) error
}

func (c *controller) githubEnabled() bool {
	return c.githubApp != nil && c.githubApp.Enabled()
}

func githubUser(userID string) (int64, error) {
	uid := monoflake.IDFromBase62(userID).Int64()
	if uid == 0 {
		return 0, fmt.Errorf("invalid request")
	}
	return uid, nil
}

func (c *controller) GetGitHubConnection(ctx context.Context, req entity.GetGitHubConnectionRequest) (*entity.GitHubConnectionResponse, error) {
	uid, err := githubUser(req.UserID)
	if err != nil {
		return nil, err
	}
	return c.githubConnection(ctx, uid)
}

func (c *controller) githubConnection(ctx context.Context, uid int64) (*entity.GitHubConnectionResponse, error) {
	out := &entity.GitHubConnectionResponse{Installations: []entity.GitHubInstallation{}}
	if !c.githubEnabled() {
		return out, nil
	}
	state, err := c.githubApp.NewState(ctx, uid)
	if err != nil {
		return nil, err
	}
	if out.InstallURL, err = c.githubApp.InstallURL(ctx, state); err != nil {
		return nil, err
	}
	rows, err := c.repository.ListGitHubInstallations(ctx, uid)
	if err != nil {
		return nil, err
	}
	out.Enabled = true
	for _, r := range rows {
		out.Installations = append(out.Installations, entity.GitHubInstallation{
			InstallationID: r.InstallationID, Account: r.Account, AccountType: r.AccountType, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// ConnectGitHub stores the installations GitHub lists for the person who
// authorised the code, in place of the account's earlier ones. The state
// must be the one this account was given: otherwise someone could have the
// account's person finish a connect started for another account, and read
// their repositories through it.
func (c *controller) ConnectGitHub(ctx context.Context, req entity.ConnectGitHubRequest) (*entity.GitHubConnectionResponse, error) {
	uid, err := githubUser(req.UserID)
	if err != nil {
		return nil, err
	}
	if !c.githubEnabled() {
		return nil, skillErr(SkillInvalid, "%v", githubapp.ErrDisabled)
	}
	if err := c.githubApp.CheckState(ctx, req.State, uid); err != nil {
		return nil, skillErr(SkillInvalid, "%v", err)
	}
	found, err := c.githubApp.UserInstallations(ctx, req.Code)
	if err != nil {
		return nil, skillErr(SkillUpstream, "%v", err)
	}
	now := time.Now()
	rows := make([]model.GitHubInstallation, len(found))
	for i, in := range found {
		rows[i] = model.GitHubInstallation{
			ID: c.idgen.NextID(), CreatedAt: now, UserID: uid,
			InstallationID: in.ID, Account: in.Account, AccountType: in.AccountType,
		}
	}
	if err := c.repository.ReplaceGitHubInstallations(ctx, uid, rows); err != nil {
		return nil, err
	}
	return c.githubConnection(ctx, uid)
}

// DisconnectGitHub forgets one installation. The app stays installed on
// GitHub, where only its owner can remove it.
func (c *controller) DisconnectGitHub(ctx context.Context, req entity.DisconnectGitHubRequest) error {
	uid, err := githubUser(req.UserID)
	if err != nil {
		return err
	}
	return c.repository.DeleteGitHubInstallation(ctx, uid, req.InstallationID)
}

// importToken is the token an import of rawURL by uid reads with: one from
// the account's installation on the repository's owner, reading that one
// repository. Without one the import is anonymous, which a public repository
// needs no more than.
func (c *controller) importToken(ctx context.Context, uid int64, rawURL string) string {
	if !c.githubEnabled() {
		return ""
	}
	src, err := skillimport.ParseURL(rawURL)
	if err != nil {
		return ""
	}
	rows, err := c.repository.ListGitHubInstallations(ctx, uid)
	if err != nil {
		zlog.Warn().Err(err).Msg("[skills] list GitHub installations")
		return ""
	}
	for _, r := range rows {
		if !strings.EqualFold(r.Account, src.Owner) {
			continue
		}
		token, err := c.githubApp.RepoToken(ctx, r.InstallationID, src.Repo)
		switch {
		case err == nil:
			return token
		case errors.Is(err, githubapp.ErrInstallationGone):
			_ = c.repository.DeleteGitHubInstallation(ctx, uid, r.InstallationID)
		case !errors.Is(err, githubapp.ErrRepoNotCovered):
			zlog.Warn().Err(err).Int64("installation", r.InstallationID).Msg("[skills] mint GitHub App token")
		}
	}
	return ""
}

// sourceToken is the token an import of rawURL by uid reads with, and
// whether the link is to GitLab: the account's GitHub App installation on the
// repository's owner, or its GitLab connection.
func (c *controller) sourceToken(ctx context.Context, uid int64, rawURL string) (string, bool) {
	src, err := c.skillImport.Parse(rawURL)
	switch {
	case err != nil:
		// Fetch refuses the link, with the reason.
		return "", false
	case src.GitLab:
		return c.gitlabToken(ctx, uid), true
	}
	return c.importToken(ctx, uid, rawURL), false
}

// importFetchErr is the refusal for an import the host would not serve. A
// repository not found anonymously may be private, which connecting GitHub
// or GitLab can fix when the server allows it.
func (c *controller) importFetchErr(err error, token string, gitlab bool) error {
	if errors.Is(err, skillimport.ErrNotFound) && token == "" && gitlab && c.gitlabEnabled() {
		return skillErr(SkillInvalid, "%v. If it is private, choose Connect GitLab beside the import", err)
	}
	if errors.Is(err, skillimport.ErrNotFound) && token == "" && !gitlab && c.githubEnabled() {
		return skillErr(SkillInvalid, "%v. If it is private, choose Connect GitHub beside the import and give the GitHub App access to it", err)
	}
	if errors.Is(err, skillimport.ErrInvalidURL) || errors.Is(err, skillimport.ErrNotFound) {
		return skillErr(SkillInvalid, "%v", err)
	}
	return skillErr(SkillUpstream, "%v", err)
}
