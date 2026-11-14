// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package team

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/smtp"
	zlog "github.com/rs/zerolog/log"
)

const (
	// InvitationTTL is how long an invitation can be accepted.
	InvitationTTL = 7 * 24 * time.Hour
	// capWindow is the rolling window both invitation caps count in.
	capWindow = 24 * time.Hour
	// tokenBytes is an invitation token's randomness.
	tokenBytes = 32
	// maxEmail is the longest address an invitation goes to.
	maxEmail = 255
)

var errGone = teamErr(entity.TeamGone, "this invitation is no longer valid: ask for a new one")

// HashToken is how an invitation token is stored: its SHA-256, in hex.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newToken is 32 random bytes, base64url without padding.
func newToken() string {
	b := make([]byte, tokenBytes)
	_, _ = rand.Read(b) // crypto/rand never fails
	return base64.RawURLEncoding.EncodeToString(b)
}

// cleanEmail is a bare address, as typed ("a@b.c", never "Name <a@b.c>"),
// lowercased.
func cleanEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(email) > maxEmail {
		return "", teamErr(entity.TeamInvalid, "%q is not an email address", email)
	}
	return strings.ToLower(email), nil
}

// maskEmail keeps an address's first character and its domain:
// "bob@acme.com" is "b***@acme.com".
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 1 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}

// link is the page that accepts an invitation. The token is in the fragment,
// which browsers never send, so it stays out of every server's logs.
func (c *controller) link(token string) string {
	return c.baseURL + "/invite#" + token
}

// userName is the account's name, or "" when it cannot be read.
func (c *controller) userName(ctx context.Context, userID int64) string {
	u, err := c.repo.SystemGetUser(ctx, userID)
	if err != nil {
		return ""
	}
	return u.Name
}

// CreateInvitation leaves the caller's membership to the repository, which
// reads it under the team's lock.
func (c *controller) CreateInvitation(ctx context.Context, ch entity.TeamChange, email string) (entity.CreateTeamInvitationResponse, error) {
	if c.maxInvite <= 0 {
		return entity.CreateTeamInvitationResponse{}, teamErr(entity.TeamForbidden, "invitations are turned off on this server")
	}
	email, err := cleanEmail(email)
	if err != nil {
		return entity.CreateTeamInvitationResponse{}, err
	}
	if err := c.member(ctx, ch.UserID, ch.TeamID); err != nil {
		return entity.CreateTeamInvitationResponse{}, err
	}
	in, err := c.repo.TeamHasMemberWithEmail(ctx, ch.TeamID, email)
	if err != nil {
		return entity.CreateTeamInvitationResponse{}, err
	}
	if in {
		return entity.CreateTeamInvitationResponse{}, teamErr(entity.TeamConflict, "%s is already in this team", email)
	}
	team, err := c.repo.GetTeam(ctx, ch.TeamID)
	if err != nil {
		return entity.CreateTeamInvitationResponse{}, err
	}
	token, now := newToken(), c.now()
	inv := model.TeamInvitation{
		ID: c.ids.NextID(), TeamID: ch.TeamID, Email: email, InvitedBy: ch.UserID,
		TokenHash: HashToken(token), ExpiresAt: now.Add(InvitationTTL), CreatedAt: now,
	}
	next, err := c.repo.CreateTeamInvitation(ctx, inv, ch.Version, now.Add(-capWindow), int64(c.maxInvite), int64(c.maxByUser), c.activity())
	switch {
	case errors.Is(err, base.ErrInvitationCapReached):
		return entity.CreateTeamInvitationResponse{}, teamErr(entity.TeamThrottled, "this team has reached its limit of %d invitations per day", c.maxInvite)
	case errors.Is(err, base.ErrUserInvitationCapReached):
		return entity.CreateTeamInvitationResponse{}, teamErr(entity.TeamThrottled, "you have reached your limit of %d invitations per day across all your teams", c.maxByUser)
	case errors.Is(err, base.ErrInvitationPending):
		return entity.CreateTeamInvitationResponse{}, teamErr(entity.TeamConflict, "%s already has a pending invitation to this team", email)
	case err != nil:
		return entity.CreateTeamInvitationResponse{}, fromRepo(err)
	}
	c.emit(ctx, entity.ActionTeamInvite, ch.UserID, 0, ch.TeamID)
	inviter := c.userName(ctx, ch.UserID)
	rs := entity.CreateTeamInvitationResponse{Invitation: invitationEntity(inv, team.Name, inviter), Version: next}
	if c.mailer != nil {
		err := c.mailer.Send(ctx, smtp.SendRequest{To: []string{email}, Subject: inviteSubject(inviter, team.Name), Body: c.inviteBody(team.Name, token)})
		if err == nil {
			rs.EmailSent = true
			return rs, nil
		}
		zlog.Warn().Err(err).Int64("invitation", inv.ID).Msg("team invitation email not sent; the inviter is given the link instead")
	}
	rs.Link = c.link(token)
	return rs, nil
}

func inviteSubject(inviter, team string) string {
	if inviter == "" {
		inviter = "Someone"
	}
	return fmt.Sprintf("%s invited you to the %s team on AgentRQ", inviter, team)
}

func (c *controller) inviteBody(team, token string) string {
	return fmt.Sprintf("You are invited to join the %s team on AgentRQ.\n\n"+
		"Open this link and sign in with the account of this email address to accept:\n\n%s\n\n"+
		"The link works once and expires in 7 days.\n", team, c.link(token))
}

func invitationEntity(i model.TeamInvitation, team, inviter string) entity.TeamInvitation {
	return entity.TeamInvitation{
		ID: i.ID, TeamID: i.TeamID, TeamName: team, Email: i.Email,
		InvitedBy: i.InvitedBy, InvitedByName: inviter, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt,
	}
}

// invitationEntities names each invitation's team and inviter, reading each
// once.
func (c *controller) invitationEntities(ctx context.Context, rows []model.TeamInvitation) ([]entity.TeamInvitation, error) {
	teams, names := map[int64]string{}, map[int64]string{}
	out := make([]entity.TeamInvitation, len(rows))
	for i, inv := range rows {
		if _, ok := teams[inv.TeamID]; !ok {
			t, err := c.repo.GetTeam(ctx, inv.TeamID)
			if err != nil {
				return nil, err
			}
			teams[inv.TeamID] = t.Name
		}
		if _, ok := names[inv.InvitedBy]; !ok {
			names[inv.InvitedBy] = c.userName(ctx, inv.InvitedBy)
		}
		out[i] = invitationEntity(inv, teams[inv.TeamID], names[inv.InvitedBy])
	}
	return out, nil
}

func (c *controller) ListTeamInvitations(ctx context.Context, userID, teamID int64) (entity.TeamInvitations, error) {
	if err := c.member(ctx, userID, teamID); err != nil {
		return entity.TeamInvitations{}, err
	}
	rows, err := c.repo.ListTeamInvitations(ctx, teamID, c.now())
	if err != nil {
		return entity.TeamInvitations{}, err
	}
	invs, err := c.invitationEntities(ctx, rows)
	if err != nil {
		return entity.TeamInvitations{}, err
	}
	v, err := c.version(ctx, teamID)
	return entity.TeamInvitations{Invitations: invs, Version: v}, err
}

// RevokeInvitation leaves the caller's membership to the repository, which
// reads it under the team's lock.
func (c *controller) RevokeInvitation(ctx context.Context, ch entity.TeamChange, invitationID int64) (int64, error) {
	next, err := c.repo.RevokeTeamInvitation(ctx, ch.TeamID, ch.UserID, ch.Version, invitationID, c.activity())
	return next, fromRepo(err)
}

// myEmail is the account's email, lowercased.
func (c *controller) myEmail(ctx context.Context, userID int64) (string, error) {
	me, err := c.repo.SystemGetUser(ctx, userID)
	if err != nil {
		return "", err
	}
	return strings.ToLower(me.Email), nil
}

func (c *controller) ListMyInvitations(ctx context.Context, userID int64) ([]entity.TeamInvitation, error) {
	email, err := c.myEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	if email == "" {
		return []entity.TeamInvitation{}, nil
	}
	rows, err := c.repo.ListInvitationsForEmail(ctx, email, c.now())
	if err != nil {
		return nil, err
	}
	return c.invitationEntities(ctx, rows)
}

// open is nil when the invitation can still be accepted.
func (c *controller) open(inv model.TeamInvitation) error {
	if inv.AcceptedAt != nil || inv.RevokedAt != nil || !c.now().Before(inv.ExpiresAt) {
		return errGone
	}
	return nil
}

// inviterIsMember is errGone when the inviter is no longer in the team, as
// accepting would find.
func (c *controller) inviterIsMember(ctx context.Context, inv model.TeamInvitation) error {
	if err := c.member(ctx, inv.InvitedBy, inv.TeamID); !errors.Is(err, base.ErrNotFound) {
		return err
	}
	return errGone
}

// byToken is the invitation the token belongs to; base.ErrNotFound when none
// does.
func (c *controller) byToken(ctx context.Context, token string) (model.TeamInvitation, error) {
	if token == "" {
		return model.TeamInvitation{}, base.ErrNotFound
	}
	return c.repo.GetTeamInvitationByTokenHash(ctx, HashToken(token))
}

func (c *controller) PreviewInvitation(ctx context.Context, userID int64, token string) (entity.TeamInvitationPreview, error) {
	inv, err := c.byToken(ctx, token)
	if err != nil {
		return entity.TeamInvitationPreview{}, err
	}
	if err := c.open(inv); err != nil {
		return entity.TeamInvitationPreview{}, err
	}
	if err := c.inviterIsMember(ctx, inv); err != nil {
		return entity.TeamInvitationPreview{}, err
	}
	team, err := c.repo.GetTeam(ctx, inv.TeamID)
	if err != nil {
		return entity.TeamInvitationPreview{}, err
	}
	email, err := c.myEmail(ctx, userID)
	if err != nil {
		return entity.TeamInvitationPreview{}, err
	}
	return entity.TeamInvitationPreview{
		TeamID: team.ID, TeamName: team.Name, InvitedByName: c.userName(ctx, inv.InvitedBy),
		Email: maskEmail(inv.Email), EmailMatches: email == inv.Email, ExpiresAt: inv.ExpiresAt,
	}, nil
}

func (c *controller) AcceptInvitation(ctx context.Context, userID int64, token string) (entity.Team, error) {
	inv, err := c.byToken(ctx, token)
	if err != nil {
		return entity.Team{}, err
	}
	if err := c.open(inv); err != nil {
		return entity.Team{}, err
	}
	email, err := c.myEmail(ctx, userID)
	if err != nil {
		return entity.Team{}, err
	}
	if email != inv.Email {
		return entity.Team{}, teamErr(entity.TeamForbidden, "this invitation is for %s: sign in with that account", maskEmail(inv.Email))
	}
	return c.accept(ctx, userID, inv)
}

// mine is the invitation to the account's email; base.ErrNotFound when it is
// someone else's, or there is none.
func (c *controller) mine(ctx context.Context, userID, invitationID int64) (model.TeamInvitation, error) {
	inv, err := c.repo.GetTeamInvitation(ctx, invitationID)
	if err != nil {
		return inv, err
	}
	email, err := c.myEmail(ctx, userID)
	if err != nil {
		return inv, err
	}
	if email != inv.Email {
		return inv, base.ErrNotFound
	}
	return inv, nil
}

func (c *controller) AcceptInvitationByID(ctx context.Context, userID, invitationID int64) (entity.Team, error) {
	inv, err := c.mine(ctx, userID, invitationID)
	if err != nil {
		return entity.Team{}, err
	}
	if err := c.open(inv); err != nil {
		return entity.Team{}, err
	}
	return c.accept(ctx, userID, inv)
}

// accept joins the account to the invitation's team, unless already in it.
func (c *controller) accept(ctx context.Context, userID int64, inv model.TeamInvitation) (entity.Team, error) {
	act := c.activity()
	member := model.TeamMember{ID: c.ids.NextID(), TeamID: inv.TeamID, UserID: userID, CreatedAt: act.CreatedAt, UpdatedAt: act.CreatedAt}
	if err := c.repo.AcceptTeamInvitation(ctx, inv.ID, member, act.CreatedAt, act); err != nil {
		if errors.Is(err, base.ErrInvitationNotPending) {
			return entity.Team{}, errGone
		}
		return entity.Team{}, err
	}
	c.emit(ctx, entity.ActionTeamJoin, userID, 0, inv.TeamID)
	return c.GetTeam(ctx, userID, inv.TeamID)
}

func (c *controller) DeclineInvitation(ctx context.Context, userID, invitationID int64) error {
	inv, err := c.mine(ctx, userID, invitationID)
	if err != nil {
		return err
	}
	if inv.AcceptedAt != nil || inv.RevokedAt != nil {
		return errGone
	}
	act := c.activity()
	act.ActorUserID = userID
	err = c.repo.DeclineTeamInvitation(ctx, inv.TeamID, inv.ID, act)
	if errors.Is(err, base.ErrInvitationNotPending) {
		return errGone
	}
	return err
}
