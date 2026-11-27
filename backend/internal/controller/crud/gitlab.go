// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"errors"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/gitlabapp"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
	zlog "github.com/rs/zerolog/log"
)

// gitlabRenewBefore is how long before it lapses an access token is renewed,
// so that one never lapses during an import.
const gitlabRenewBefore = 5 * time.Minute

// GitLabController connects an account to the server's GitLab application,
// so that skills can be imported from its private projects.
type GitLabController interface {
	GetGitLabConnection(ctx context.Context, req entity.GetGitLabConnectionRequest) (*entity.GitLabConnectionResponse, error)
	ConnectGitLab(ctx context.Context, req entity.ConnectGitLabRequest) (*entity.GitLabConnectionResponse, error)
	DisconnectGitLab(ctx context.Context, req entity.DisconnectGitLabRequest) error
}

func (c *controller) gitlabEnabled() bool {
	return c.gitlabApp != nil && c.gitlabApp.Enabled()
}

// gitlabHost is the GitLab skills are imported from.
func (c *controller) gitlabHost() string {
	if c.gitlabApp == nil {
		return skillimport.DefaultGitLabHost
	}
	return c.gitlabApp.Host()
}

func (c *controller) GetGitLabConnection(ctx context.Context, req entity.GetGitLabConnectionRequest) (*entity.GitLabConnectionResponse, error) {
	uid, err := githubUser(req.UserID)
	if err != nil {
		return nil, err
	}
	return c.gitlabConnection(ctx, uid)
}

// gitlabConnection is the server's GitLab and, while connecting is on, the
// link to connect and who is connected. A connection made to another host,
// before the admin changed it, counts as none.
func (c *controller) gitlabConnection(ctx context.Context, uid int64) (*entity.GitLabConnectionResponse, error) {
	out := &entity.GitLabConnectionResponse{Host: c.gitlabHost()}
	if !c.gitlabEnabled() {
		return out, nil
	}
	out.Enabled = true
	state, err := c.gitlabApp.NewState(ctx, uid)
	if err != nil {
		return nil, err
	}
	if out.AuthorizeURL, err = c.gitlabApp.AuthorizeURL(ctx, state); err != nil {
		return nil, err
	}
	row, err := c.repository.GetGitLabConnection(ctx, uid)
	switch {
	case errors.Is(err, base.ErrNotFound):
	case err != nil:
		return nil, err
	case row.Host == out.Host:
		out.Username = row.Username
	}
	return out, nil
}

// ConnectGitLab keeps the token GitLab grants for the code, in place of the
// account's earlier connection. The state must be the one this account was
// given: otherwise someone could have the account's person finish a connect
// started for another account, and read their projects through it.
func (c *controller) ConnectGitLab(ctx context.Context, req entity.ConnectGitLabRequest) (*entity.GitLabConnectionResponse, error) {
	uid, err := githubUser(req.UserID)
	if err != nil {
		return nil, err
	}
	if !c.gitlabEnabled() {
		return nil, skillErr(SkillInvalid, "%v", gitlabapp.ErrDisabled)
	}
	if err := c.gitlabApp.CheckState(ctx, req.State, uid); err != nil {
		return nil, skillErr(SkillInvalid, "%v", err)
	}
	tok, err := c.gitlabApp.Exchange(ctx, req.Code)
	if err != nil {
		return nil, skillErr(SkillUpstream, "%v", err)
	}
	username, err := c.gitlabApp.Username(ctx, tok.Access)
	if err != nil {
		return nil, skillErr(SkillUpstream, "%v", err)
	}
	now := time.Now()
	row := model.GitLabConnection{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, Host: c.gitlabHost(), Username: username}
	if err := c.sealGitLabToken(&row, tok, now); err != nil {
		return nil, err
	}
	if err := c.repository.SaveGitLabConnection(ctx, row); err != nil {
		return nil, err
	}
	return c.gitlabConnection(ctx, uid)
}

// DisconnectGitLab forgets the account's connection, then asks GitLab to
// revoke its token; a revoke GitLab refuses leaves it to lapse.
func (c *controller) DisconnectGitLab(ctx context.Context, req entity.DisconnectGitLabRequest) error {
	uid, err := githubUser(req.UserID)
	if err != nil {
		return err
	}
	row, err := c.repository.GetGitLabConnection(ctx, uid)
	if err != nil {
		return err
	}
	if err := c.repository.DeleteGitLabConnection(ctx, uid); err != nil {
		return err
	}
	if !c.gitlabEnabled() || row.Host != c.gitlabHost() {
		return nil
	}
	if refresh, err := security.Decrypt(row.RefreshToken, c.tokenKey, row.RefreshNonce); err == nil {
		if err := c.gitlabApp.Revoke(ctx, refresh); err != nil {
			zlog.Warn().Err(err).Msg("[skills] revoke GitLab token")
		}
	}
	return nil
}

// sealGitLabToken puts tok in row, encrypted.
func (c *controller) sealGitLabToken(row *model.GitLabConnection, tok gitlabapp.Token, now time.Time) error {
	var aerr, rerr error
	row.AccessToken, row.AccessNonce, aerr = security.Encrypt(tok.Access, c.tokenKey)
	row.RefreshToken, row.RefreshNonce, rerr = security.Encrypt(tok.Refresh, c.tokenKey)
	if err := errors.Join(aerr, rerr); err != nil {
		return err
	}
	row.UpdatedAt, row.ExpiresAt = now, tok.Expiry
	return nil
}

// gitlabToken is the access token an import by uid reads GitLab with: the
// account's connection's, renewed first when it lapses soon. Without one the
// import is anonymous, which a public project needs no more than.
func (c *controller) gitlabToken(ctx context.Context, uid int64) string {
	if !c.gitlabEnabled() {
		return ""
	}
	row, err := c.repository.GetGitLabConnection(ctx, uid)
	if err != nil {
		if !errors.Is(err, base.ErrNotFound) {
			zlog.Warn().Err(err).Msg("[skills] read GitLab connection")
		}
		return ""
	}
	if row.Host != c.gitlabHost() {
		return ""
	}
	if time.Until(row.ExpiresAt) > gitlabRenewBefore {
		return c.openGitLabAccess(row)
	}
	refresh, err := security.Decrypt(row.RefreshToken, c.tokenKey, row.RefreshNonce)
	if err != nil {
		zlog.Warn().Err(err).Msg("[skills] decrypt GitLab connection")
		return ""
	}
	tok, err := c.gitlabApp.Refresh(ctx, refresh)
	if err != nil {
		// GitLab spends a refresh token once: another import may have
		// renewed it a moment ago, and stored what it got.
		if now, rerr := c.repository.GetGitLabConnection(ctx, uid); rerr == nil && now.RefreshToken != row.RefreshToken {
			return c.openGitLabAccess(now)
		}
		if errors.Is(err, gitlabapp.ErrRevoked) {
			_ = c.repository.DeleteGitLabConnection(ctx, uid)
		} else {
			zlog.Warn().Err(err).Msg("[skills] renew GitLab connection")
		}
		return ""
	}
	renewed := row
	if err := c.sealGitLabToken(&renewed, tok, time.Now()); err != nil {
		zlog.Warn().Err(err).Msg("[skills] encrypt GitLab connection")
		return tok.Access
	}
	if _, err := c.repository.RenewGitLabConnection(ctx, renewed, row.RefreshToken); err != nil {
		zlog.Warn().Err(err).Msg("[skills] store renewed GitLab connection")
	}
	return tok.Access
}

func (c *controller) openGitLabAccess(row model.GitLabConnection) string {
	access, err := security.Decrypt(row.AccessToken, c.tokenKey, row.AccessNonce)
	if err != nil {
		zlog.Warn().Err(err).Msg("[skills] decrypt GitLab connection")
		return ""
	}
	return access
}
