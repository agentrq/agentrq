package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	slackapi "github.com/slack-go/slack"
)

// Config holds Slack app credentials loaded from YAML config.
type Config struct {
	Enabled       bool   `yaml:"enabled"`
	SigningSecret string `yaml:"signingSecret"`
	AppID         string `yaml:"appId"`
	ClientID      string `yaml:"clientId"`
	ClientSecret  string `yaml:"clientSecret"`
}

// Service defines the Slack API operations needed by AgentRQ.
type Service interface {
	IsEnabled() bool
	ClientID() string

	// Channel management
	CreatePrivateChannel(ctx context.Context, token string, name string) (channelID string, err error)
	InviteUsersToChannel(ctx context.Context, token string, channelID string, userIDs []string) error

	// Messaging
	PostMessage(ctx context.Context, token string, channelID string, blocks []slackapi.Block) (ts string, err error)
	PostThreadReply(ctx context.Context, token string, channelID, threadTS string, blocks []slackapi.Block) (ts string, err error)
	UpdateMessage(ctx context.Context, token string, channelID, ts string, blocks []slackapi.Block) error

	// OAuth v2 Flow
	ExchangeCode(ctx context.Context, code string, redirectURI string) (token string, teamID string, botUserID string, authedUserID string, err error)

	// Security
	VerifyRequest(r *http.Request, body []byte) error
}

type service struct {
	cfg Config
}

// New returns a new Slack Service. When Slack is disabled the service
// still satisfies the interface but all operations are no-ops.
func New(cfg Config) Service {
	return &service{cfg: cfg}
}

func (s *service) IsEnabled() bool {
	return s.cfg.Enabled && s.cfg.ClientID != "" && s.cfg.ClientSecret != ""
}

func (s *service) ClientID() string {
	return s.cfg.ClientID
}

// CreatePrivateChannel creates a private Slack channel with the given name.
// If the channel name is already taken (e.g. from a previous install):
//  1. It tries to find and reuse the existing channel via conversations.list (requires groups:read).
//  2. If that fails (missing scope or bot not a member), it creates a new channel with a unique timestamp suffix.
func (s *service) CreatePrivateChannel(ctx context.Context, token, name string) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("slack: service disabled")
	}
	client := slackapi.New(token)
	return createPrivateChannelWithFallback(ctx, client, name)
}

// createPrivateChannelWithFallback handles name_taken gracefully.
func createPrivateChannelWithFallback(ctx context.Context, client *slackapi.Client, name string) (string, error) {
	ch, err := client.CreateConversationContext(ctx, slackapi.CreateConversationParams{
		ChannelName: name,
		IsPrivate:   true,
	})
	if err == nil {
		return ch.ID, nil
	}

	if !strings.Contains(err.Error(), "name_taken") {
		return "", fmt.Errorf("slack: create channel %q: %w", name, err)
	}

	// Channel already exists from a prior install — try to find and reuse it.
	if id, findErr := findChannelByName(ctx, client, name); findErr == nil {
		return id, nil
	}

	// Can't find the channel (missing groups:read scope or bot not a member).
	// Fall back to a unique name using a short timestamp suffix.
	uniqueName := fmt.Sprintf("%s-%d", name, time.Now().Unix()%100000)
	ch, err = client.CreateConversationContext(ctx, slackapi.CreateConversationParams{
		ChannelName: uniqueName,
		IsPrivate:   true,
	})
	if err != nil {
		return "", fmt.Errorf("slack: create channel %q (fallback %q): %w", name, uniqueName, err)
	}
	return ch.ID, nil
}

// findChannelByName iterates the bot's visible private channels to locate one by exact name.
// Requires the groups:read scope.
func findChannelByName(ctx context.Context, client *slackapi.Client, name string) (string, error) {
	params := &slackapi.GetConversationsParameters{
		Types:           []string{"private_channel"},
		ExcludeArchived: false,
	}
	for {
		channels, cursor, err := client.GetConversationsContext(ctx, params)
		if err != nil {
			return "", fmt.Errorf("slack: cannot list channels: %w", err)
		}
		for _, ch := range channels {
			if ch.Name == name {
				return ch.ID, nil
			}
		}
		if cursor == "" {
			break
		}
		params.Cursor = cursor
	}
	return "", fmt.Errorf("slack: channel %q not visible to bot", name)
}


// InviteUsersToChannel invites one or more Slack users into a channel by their user IDs.
// It silently ignores "already_in_channel" errors.
func (s *service) InviteUsersToChannel(ctx context.Context, token, channelID string, userIDs []string) error {
	if !s.IsEnabled() {
		return fmt.Errorf("slack: service disabled")
	}
	client := slackapi.New(token)
	_, err := client.InviteUsersToConversationContext(ctx, channelID, userIDs...)
	if err != nil && !strings.Contains(err.Error(), "already_in_channel") {
		return fmt.Errorf("slack: invite users to channel %s: %w", channelID, err)
	}
	return nil
}

// PostMessage posts a Block Kit message to a channel and returns the message timestamp.
func (s *service) PostMessage(ctx context.Context, token, channelID string, blocks []slackapi.Block) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("slack: service disabled")
	}
	client := slackapi.New(token)
	_, ts, err := client.PostMessageContext(ctx, channelID,
		slackapi.MsgOptionBlocks(blocks...),
	)
	if err != nil {
		return "", fmt.Errorf("slack: post message to %s: %w", channelID, err)
	}
	return ts, nil
}

// PostThreadReply posts a Block Kit reply inside an existing thread.
func (s *service) PostThreadReply(ctx context.Context, token, channelID, threadTS string, blocks []slackapi.Block) (string, error) {
	if !s.IsEnabled() {
		return "", fmt.Errorf("slack: service disabled")
	}
	client := slackapi.New(token)
	_, ts, err := client.PostMessageContext(ctx, channelID,
		slackapi.MsgOptionBlocks(blocks...),
		slackapi.MsgOptionTS(threadTS),
	)
	if err != nil {
		return "", fmt.Errorf("slack: post thread reply to %s/%s: %w", channelID, threadTS, err)
	}
	return ts, nil
}

// UpdateMessage replaces the blocks of an existing message.
func (s *service) UpdateMessage(ctx context.Context, token, channelID, ts string, blocks []slackapi.Block) error {
	if !s.IsEnabled() {
		return fmt.Errorf("slack: service disabled")
	}
	client := slackapi.New(token)
	_, _, _, err := client.UpdateMessageContext(ctx, channelID, ts,
		slackapi.MsgOptionBlocks(blocks...),
	)
	if err != nil {
		return fmt.Errorf("slack: update message %s/%s: %w", channelID, ts, err)
	}
	return nil
}

// ExchangeCode exchanges a temporary OAuth code for a bot token, team ID, bot user ID,
// and the Slack user ID of the person who installed the app (authed user).
func (s *service) ExchangeCode(ctx context.Context, code, redirectURI string) (string, string, string, string, error) {
	if !s.IsEnabled() {
		return "", "", "", "", fmt.Errorf("slack: service disabled")
	}
	resp, err := slackapi.GetOAuthV2ResponseContext(ctx, &http.Client{}, s.cfg.ClientID, s.cfg.ClientSecret, code, redirectURI)
	if err != nil {
		return "", "", "", "", fmt.Errorf("slack: oauth exchange failed: %w", err)
	}
	if !resp.Ok {
		return "", "", "", "", fmt.Errorf("slack: oauth exchange response error: %s", resp.Error)
	}
	return resp.AccessToken, resp.Team.ID, resp.BotUserID, resp.AuthedUser.ID, nil
}

// VerifyRequest validates a Slack webhook request using HMAC-SHA256.
// It reads the X-Slack-Request-Timestamp and X-Slack-Signature headers.
func (s *service) VerifyRequest(r *http.Request, body []byte) error {
	if s.cfg.SigningSecret == "" {
		return fmt.Errorf("slack: signing secret not configured")
	}
	tsStr := r.Header.Get("X-Slack-Request-Timestamp")
	if tsStr == "" {
		return fmt.Errorf("slack: missing X-Slack-Request-Timestamp header")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("slack: invalid timestamp: %w", err)
	}
	// Reject replayed requests older than 5 minutes
	if time.Now().Unix()-ts > 300 {
		return fmt.Errorf("slack: request timestamp too old")
	}

	sigBase := "v0:" + tsStr + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(s.cfg.SigningSecret))
	mac.Write([]byte(sigBase))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	got := r.Header.Get("X-Slack-Signature")
	if !hmac.Equal([]byte(expected), []byte(got)) {
		return fmt.Errorf("slack: invalid signature")
	}
	return nil
}

// ─── Block Kit helpers ────────────────────────────────────────────────────────

// BuildTaskBlocks builds the initial message blocks for a new task.
// If needsApproval is true (task assignee=agent, status=notstarted), it
// appends Allow / Allow All / Reject action buttons.
func BuildTaskBlocks(workspaceID, taskID, title, body string, needsApproval bool) []slackapi.Block {
	header := slackapi.NewSectionBlock(
		slackapi.NewTextBlockObject(slackapi.MarkdownType,
			fmt.Sprintf("*📋 New Task: %s*\n%s", escapeMarkdown(title), escapeMarkdown(body)), false, false),
		nil, nil,
	)
	blocks := []slackapi.Block{header, slackapi.NewDividerBlock()}

	if needsApproval {
		blocks = append(blocks, slackapi.NewActionBlock("",
			slackapi.NewButtonBlockElement(
				fmt.Sprintf("task_respond:%s:%s:allow", workspaceID, taskID),
				"allow",
				slackapi.NewTextBlockObject(slackapi.PlainTextType, "✅ Allow", true, false),
			),
			slackapi.NewButtonBlockElement(
				fmt.Sprintf("task_respond:%s:%s:allow_all", workspaceID, taskID),
				"allow_all",
				slackapi.NewTextBlockObject(slackapi.PlainTextType, "🚀 Allow All Commands", true, false),
			),
			slackapi.NewButtonBlockElement(
				fmt.Sprintf("task_respond:%s:%s:reject", workspaceID, taskID),
				"reject",
				slackapi.NewTextBlockObject(slackapi.PlainTextType, "❌ Reject", true, false),
			),
		))
	}
	return blocks
}

// BuildMessageBlocks builds a simple text thread-reply block.
func BuildMessageBlocks(sender, text string) []slackapi.Block {
	prefix := ""
	switch sender {
	case "agent":
		prefix = "🤖 *Agent:* "
	case "human":
		prefix = "👤 *Human:* "
	default:
		prefix = fmt.Sprintf("👤 *%s:* ", sender)
	}
	return []slackapi.Block{
		slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject(slackapi.MarkdownType,
				prefix+escapeMarkdown(text), false, false),
			nil, nil,
		),
	}
}

// BuildSystemMessageBlocks builds a system message reply block without escaping markdown so mentions render correctly.
func BuildSystemMessageBlocks(text string) []slackapi.Block {
	return []slackapi.Block{
		slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject(slackapi.MarkdownType,
				text, false, false),
			nil, nil,
		),
	}
}

// BuildPermissionRequestBlocks builds blocks for an MCP tool permission request
// with Allow / Deny buttons.
func BuildPermissionRequestBlocks(workspaceID, taskID, requestID, toolDesc string) []slackapi.Block {
	return []slackapi.Block{
		slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject(slackapi.MarkdownType,
				fmt.Sprintf("🔧 *Tool Call Request:* %s", escapeMarkdown(toolDesc)), false, false),
			nil, nil,
		),
		slackapi.NewActionBlock("",
			slackapi.NewButtonBlockElement(
				fmt.Sprintf("task_permission:%s:%s:%s:allow", workspaceID, taskID, requestID),
				"allow",
				slackapi.NewTextBlockObject(slackapi.PlainTextType, "✅ Allow", true, false),
			),
			slackapi.NewButtonBlockElement(
				fmt.Sprintf("task_permission:%s:%s:%s:deny", workspaceID, taskID, requestID),
				"deny",
				slackapi.NewTextBlockObject(slackapi.PlainTextType, "❌ Deny", true, false),
			),
		),
	}
}

// BuildResultBlocks builds a simple outcome block replacing interactive buttons.
func BuildResultBlocks(text string) []slackapi.Block {
	return []slackapi.Block{
		slackapi.NewSectionBlock(
			slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false),
			nil, nil,
		),
	}
}

// BuildChannelNameFromWorkspace generates a Slack-safe private channel name.
// Format: agentrq-<slug(name, 60 chars)>-<base36(workspaceID)>
// Total max length: 8 + 60 + 1 + ~13 = well under 80.
func BuildChannelNameFromWorkspace(name string, workspaceID int64) string {
	slug := slugify(name)
	if len(slug) > 60 {
		slug = slug[:60]
	}
	// Trim trailing hyphens from truncation
	slug = strings.TrimRight(slug, "-")
	id36 := strings.ToLower(strconv.FormatInt(workspaceID, 36))
	return fmt.Sprintf("agentrq-%s-%s", slug, id36)
}

// slugify converts a string to a Slack-safe slug (lowercase, alphanumeric + hyphens).
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func escapeMarkdown(s string) string {
	// Slack mrkdwn only needs minimal escaping
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
