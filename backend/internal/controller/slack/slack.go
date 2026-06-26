// Package slack implements the Slack integration controller.
// It handles workspace channel provisioning, task thread creation,
// bidirectional message sync, and interactive button actions.
package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/security"
	slacksvc "github.com/agentrq/agentrq/backend/internal/service/slack"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// MCPManager is a minimal interface so the Slack controller can dispatch
// permission verdicts and channel notifications without importing the full mcp.Manager.
type MCPManager interface {
	SendPermissionVerdict(ctx context.Context, workspaceID int64, userID string, taskID int64, requestID, behavior string) error
	SendChannelNotification(ctx context.Context, workspaceID int64, userID string, taskID int64, content string)
}

// CRUDRespondToTask is a minimal interface for responding to tasks.
type CRUDRespondToTask interface {
	RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error)
	ReplyToTask(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error)
	CheckWorkspaceAccess(ctx context.Context, id int64, userID string) (bool, error)
	CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error)
}

// Params holds constructor dependencies for the Slack controller.
type Params struct {
	Repository base.Repository
	SlackSvc   slacksvc.Service
	Crud       CRUDRespondToTask
	MCPManager MCPManager
	PubSub     pubsub.Service
	TokenSvc   auth.TokenService
	TokenKey   string
	BaseURL    string
}

// Controller defines all Slack-related business operations.
type Controller interface {
	Start(ctx context.Context) error

	// Lifecycle hooks (called from notification controller)
	OnWorkspaceCreated(ctx context.Context, workspace entity.Workspace) error
	OnTaskCreated(ctx context.Context, task entity.Task) error
	OnMessageCreated(ctx context.Context, msg entity.Message, task entity.Task) error
	OnMessageUpdated(ctx context.Context, msg entity.Message, task entity.Task) error

	// Channel assignment (called from API handler)
	SetWorkspaceChannel(ctx context.Context, req entity.SetWorkspaceSlackChannelRequest) error
	RemoveWorkspaceChannel(ctx context.Context, req entity.RemoveWorkspaceSlackChannelRequest) error
	GetWorkspaceSlackConfig(ctx context.Context, workspaceID int64) (*entity.SlackConfig, error)

	// OAuth callback
	HandleOAuthCallback(ctx context.Context, workspaceID62 string, code string, redirectURI string) error

	// Inbound Slack events
	HandleSlackEvent(ctx context.Context, payload SlackEventPayload) error
	HandleSlashCommand(ctx context.Context, channelID string, text string) (responseMsg string, ephemeral bool, err error)

	// Interactive button actions
	HandleTaskApproval(ctx context.Context, action SlackBlockAction) error
	HandleMCPPermission(ctx context.Context, action SlackBlockAction) error
}

// SlackFile maps the file format from Slack's events payload.
type SlackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	MimeType           string `json:"mimetype"`
	URLPrivateDownload string `json:"url_private_download"`
}

// SlackEventPayload is the subset of Slack's Events API payload we need.
type SlackEventPayload struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Event     struct {
		Type        string      `json:"type"`
		User        string      `json:"user"`
		Text        string      `json:"text"`
		Channel     string      `json:"channel"`
		ThreadTS    string      `json:"thread_ts"`
		EventTS     string      `json:"event_ts"`
		ChannelType string      `json:"channel_type"`
		Files       []SlackFile `json:"files"`
	} `json:"event"`
}

// SlackBlockAction represents a single Slack Block Kit button click.
type SlackBlockAction struct {
	ActionID string
	// ResponseURL is used to update the Slack message in-place.
	ChannelID        string
	MessageTS        string
	UserName         string
	UserID           string
	WorkspaceOwnerID string // base62 user ID of the workspace owner (resolved from workspace)
}

type controller struct {
	repo     base.Repository
	slack    slacksvc.Service
	crud     CRUDRespondToTask
	mcp      MCPManager
	pubsub   pubsub.Service
	tokenSvc auth.TokenService
	tokenKey string
	baseURL  string
}

// New creates a new Slack controller.
func New(p Params) Controller {
	return &controller{
		repo:     p.Repository,
		slack:    p.SlackSvc,
		crud:     p.Crud,
		mcp:      p.MCPManager,
		pubsub:   p.PubSub,
		tokenSvc: p.TokenSvc,
		tokenKey: p.TokenKey,
		baseURL:  p.BaseURL,
	}
}

// ─── Lifecycle hooks ───────────────────────────────────────────────────────────

// OnWorkspaceCreated is called after a workspace is persisted.
// In multi-tenant mode, this is a no-op because Slack is authorized later by the user.
func (c *controller) OnWorkspaceCreated(ctx context.Context, workspace entity.Workspace) error {
	return nil
}

// OnTaskCreated posts the task as a new Slack thread message using the workspace bot token.
// If the task is pending approval (assignee=agent, status=notstarted), approval buttons are included.
func (c *controller) OnTaskCreated(ctx context.Context, task entity.Task) error {
	if !c.slack.IsEnabled() {
		return nil
	}
	link, err := c.repo.GetSlackWorkspaceLink(ctx, task.WorkspaceID)
	if err != nil || link.AccessToken == "" {
		return nil // no channel configured or authorized — silently skip
	}

	decryptedToken, err := security.Decrypt(link.AccessToken, c.tokenKey, link.TokenNonce)
	if err != nil {
		zlog.Error().Err(err).Int64("workspaceID", task.WorkspaceID).Msg("[slack] failed to decrypt workspace bot token")
		return err
	}

	workspaceID62 := monoflake.ID(task.WorkspaceID).String()
	taskID62 := monoflake.ID(task.ID).String()
	needsApproval := task.Assignee == "agent" && task.Status == "notstarted" && task.CreatedBy != "human"

	blocks := slacksvc.BuildTaskBlocks(workspaceID62, taskID62, task.Title, task.Body, needsApproval)
	ts, err := c.slack.PostMessage(ctx, decryptedToken, link.SlackChannelID, blocks)
	if err != nil {
		zlog.Warn().Err(err).Int64("taskID", task.ID).Msg("[slack] failed to post task message")
		c.handleSlackError(ctx, task.WorkspaceID, err)
		return err
	}

	if err := c.repo.UpsertSlackTaskThread(ctx, model.SlackTaskThread{
		TaskID:         task.ID,
		WorkspaceID:    task.WorkspaceID,
		SlackChannelID: link.SlackChannelID,
		ThreadTS:       ts,
	}); err != nil {
		zlog.Error().Err(err).Int64("taskID", task.ID).Msg("[slack] failed to upsert task thread")
	} else {
		instruction := "💡 To send replies to this task, please use `@agentrq <message>` in this thread."
		if link.BotUserID != "" {
			instruction = fmt.Sprintf("💡 To send replies to this task, please use <@%s> <message> in this thread.", link.BotUserID)
		}
		sysBlocks := slacksvc.BuildSystemMessageBlocks(instruction)
		if _, err := c.slack.PostThreadReply(ctx, decryptedToken, link.SlackChannelID, ts, sysBlocks); err != nil {
			zlog.Warn().Err(err).Int64("taskID", task.ID).Msg("[slack] failed to post initial thread instruction reply")
			c.handleSlackError(ctx, task.WorkspaceID, err)
		}
	}
	return nil
}

// OnMessageCreated posts all messages to the Slack thread for the task using the workspace bot token.
// For pending MCP permission_request messages, it also posts interactive Allow/Deny buttons.
func (c *controller) OnMessageCreated(ctx context.Context, msg entity.Message, task entity.Task) error {
	if !c.slack.IsEnabled() {
		return nil
	}
	thread, err := c.repo.GetSlackTaskThreadByTask(ctx, task.ID)
	if err != nil {
		return nil // no thread — silently skip
	}

	link, err := c.repo.GetSlackWorkspaceLink(ctx, task.WorkspaceID)
	if err != nil || link.AccessToken == "" {
		return nil // not configured or authorized
	}

	decryptedToken, err := security.Decrypt(link.AccessToken, c.tokenKey, link.TokenNonce)
	if err != nil {
		zlog.Error().Err(err).Int64("workspaceID", task.WorkspaceID).Msg("[slack] failed to decrypt workspace bot token")
		return err
	}

	// Post the message text as a thread reply
	senderName := msg.Sender
	if msg.Sender == "human" && msg.UserID != 0 {
		u, err := c.repo.SystemGetUser(ctx, msg.UserID)
		if err == nil && u.Name != "" {
			senderName = u.Name
		}
	}

	blocks := slacksvc.BuildMessageBlocks(senderName, msg.Text)
	if _, err := c.slack.PostThreadReply(ctx, decryptedToken, thread.SlackChannelID, thread.ThreadTS, blocks); err != nil {
		zlog.Warn().Err(err).Int64("taskID", task.ID).Msg("[slack] failed to post thread reply")
		c.handleSlackError(ctx, task.WorkspaceID, err)
	}

	// If this is a pending MCP permission request, post interactive buttons too
	if isPermissionRequest(msg) {
		requestID, toolDesc := extractPermissionRequestFields(msg)
		if requestID != "" {
			workspaceID62 := monoflake.ID(task.WorkspaceID).String()
			taskID62 := monoflake.ID(task.ID).String()
			permBlocks := slacksvc.BuildPermissionRequestBlocks(workspaceID62, taskID62, requestID, toolDesc)
			ts, err := c.slack.PostThreadReply(ctx, decryptedToken, thread.SlackChannelID, thread.ThreadTS, permBlocks)
			if err != nil {
				zlog.Warn().Err(err).Msg("[slack] failed to post permission request buttons")
				c.handleSlackError(ctx, task.WorkspaceID, err)
			} else if ts != "" {
				// Save the channel ID and message timestamp of the permission request block back to metadata in GORM
				metaMap, _ := msg.Metadata.(map[string]any)
				if metaMap == nil {
					metaMap = make(map[string]any)
				}
				metaMap["slack_channel_id"] = thread.SlackChannelID
				metaMap["slack_message_ts"] = ts
				msg.Metadata = metaMap
				b, marshalErr := json.Marshal(metaMap)
				if marshalErr == nil {
					if updateErr := c.repo.UpdateMessageMetadata(ctx, task.ID, msg.ID, b); updateErr != nil {
						zlog.Error().Err(updateErr).Int64("messageID", msg.ID).Msg("[slack] failed to update message metadata with slack details")
					}
				}
			}
		}
	}
	return nil
}

// OnMessageUpdated processes updates to permission request messages (verdicts) and replaces Slack buttons with static text.
func (c *controller) OnMessageUpdated(ctx context.Context, msg entity.Message, task entity.Task) error {
	if !c.slack.IsEnabled() {
		return nil
	}

	if msg.Metadata == nil {
		return nil
	}

	metaMap, _ := msg.Metadata.(map[string]any)
	if metaMap == nil {
		return nil
	}

	mType, _ := metaMap["type"].(string)
	status, _ := metaMap["status"].(string)
	if mType != "permission_request" || (status != "allow" && status != "allow_always" && status != "deny") {
		return nil
	}

	// Skip if the decision was already executed/marked in Slack
	if decidedInSlack, _ := metaMap["decided_in_slack"].(bool); decidedInSlack {
		return nil
	}

	slackChannelID, _ := metaMap["slack_channel_id"].(string)
	slackMessageTS, _ := metaMap["slack_message_ts"].(string)
	if slackChannelID == "" || slackMessageTS == "" {
		zlog.Debug().Int64("messageID", msg.ID).Msg("[slack] skipping message update because slack coordinates are missing")
		return nil
	}

	link, err := c.repo.GetSlackWorkspaceLink(ctx, task.WorkspaceID)
	if err != nil || link.AccessToken == "" {
		return nil
	}

	decryptedToken, err := security.Decrypt(link.AccessToken, c.tokenKey, link.TokenNonce)
	if err != nil {
		zlog.Error().Err(err).Int64("workspaceID", task.WorkspaceID).Msg("[slack] failed to decrypt workspace bot token in OnMessageUpdated")
		return err
	}

	label := map[string]string{
		"allow":        "✅ Allowed by operator (via Web UI)",
		"allow_always": "✅ Allowed by operator (via Web UI)",
		"deny":         "❌ Denied by operator (via Web UI)",
	}[status]
	if label == "" {
		label = "Done"
	}

	updateErr := c.slack.UpdateMessage(ctx, decryptedToken, slackChannelID, slackMessageTS,
		slacksvc.BuildResultBlocks(label))
	c.handleSlackError(ctx, task.WorkspaceID, updateErr)
	return updateErr
}

// ─── Channel assignment ────────────────────────────────────────────────────────

func (c *controller) SetWorkspaceChannel(ctx context.Context, req entity.SetWorkspaceSlackChannelRequest) error {
	// Verify access
	if ok, err := c.crud.CheckWorkspaceAccess(ctx, req.WorkspaceID, req.UserID); err != nil || !ok {
		return fmt.Errorf("access denied")
	}
	link, err := c.repo.GetSlackWorkspaceLink(ctx, req.WorkspaceID)
	if err != nil {
		return fmt.Errorf("Slack integration must be authorized first")
	}
	link.SlackChannelID = req.ChannelID
	link.SlackChannelName = req.ChannelName
	link.AutoCreated = req.AutoCreated
	return c.repo.UpsertSlackWorkspaceLink(ctx, link)
}

func (c *controller) RemoveWorkspaceChannel(ctx context.Context, req entity.RemoveWorkspaceSlackChannelRequest) error {
	if ok, err := c.crud.CheckWorkspaceAccess(ctx, req.WorkspaceID, req.UserID); err != nil || !ok {
		return fmt.Errorf("access denied")
	}

	// Try to send a disconnect notification to the Slack channel
	link, err := c.repo.GetSlackWorkspaceLink(ctx, req.WorkspaceID)
	if err == nil && link.AccessToken != "" && link.SlackChannelID != "" {
		decryptedToken, decErr := security.Decrypt(link.AccessToken, c.tokenKey, link.TokenNonce)
		if decErr == nil {
			blocks := slacksvc.BuildResultBlocks("🔌 *AgentRQ has been unlinked from this channel.* Tasks will no longer be synchronized here.")
			_, postErr := c.slack.PostMessage(ctx, decryptedToken, link.SlackChannelID, blocks)
			if postErr != nil {
				zlog.Warn().Err(postErr).Int64("workspaceID", req.WorkspaceID).Msg("[slack] failed to send disconnect farewell message to channel")
			}
		} else {
			zlog.Warn().Err(decErr).Int64("workspaceID", req.WorkspaceID).Msg("[slack] failed to decrypt token during disconnect")
		}
	}

	return c.repo.DeleteSlackWorkspaceLink(ctx, req.WorkspaceID)
}

func (c *controller) GetWorkspaceSlackConfig(ctx context.Context, workspaceID int64) (*entity.SlackConfig, error) {
	if !c.slack.IsEnabled() {
		return &entity.SlackConfig{Enabled: false}, nil
	}

	link, err := c.repo.GetSlackWorkspaceLink(ctx, workspaceID)
	installed := err == nil && link.AccessToken != ""

	workspaceID62 := monoflake.ID(workspaceID).String()
	state, err := c.tokenSvc.CreateOAuthStateToken(workspaceID62, "slack")
	if err != nil {
		zlog.Error().Err(err).Int64("workspaceID", workspaceID).Msg("[slack] failed to generate oauth state token")
		return nil, fmt.Errorf("failed to generate oauth state")
	}

	redirectURI := fmt.Sprintf("%s/slack/oauth/callback", c.baseURL)
	authURL := fmt.Sprintf(
		"https://slack.com/oauth/v2/authorize?client_id=%s&scope=groups:write,groups:read,chat:write,app_mentions:read,commands&redirect_uri=%s&state=%s",
		c.slack.ClientID(),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
	)

	cfg := &entity.SlackConfig{
		Enabled:   true,
		Installed: installed,
		ClientID:  c.slack.ClientID(),
		AuthURL:   authURL,
	}

	if installed {
		cfg.ChannelID = link.SlackChannelID
		cfg.ChannelName = link.SlackChannelName
		cfg.AutoCreated = link.AutoCreated
	}
	return cfg, nil
}

// HandleOAuthCallback handles the dynamic Slack OAuth v2 redirect code exchange.
// It exchanges the temporary code, encrypts the access token, auto-provisions a private channel,
// and saves the credentials into GORM.
func (c *controller) HandleOAuthCallback(ctx context.Context, workspaceID62 string, code string, redirectURI string) error {
	workspaceID := monoflake.IDFromBase62(workspaceID62).Int64()
	if workspaceID == 0 {
		return fmt.Errorf("slack: invalid workspace ID state: %s", workspaceID62)
	}

	ws, err := c.repo.SystemGetWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("slack: workspace not found: %w", err)
	}

	token, teamID, botUserID, authedUserID, err := c.slack.ExchangeCode(ctx, code, redirectURI)
	if err != nil {
		return fmt.Errorf("slack: oauth exchange failed: %w", err)
	}

	encToken, nonce, err := security.Encrypt(token, c.tokenKey)
	if err != nil {
		return fmt.Errorf("slack: failed to encrypt token: %w", err)
	}

	// Resolve existing link to preserve manual channel configurations if any
	link, err := c.repo.GetSlackWorkspaceLink(ctx, workspaceID)
	if err != nil {
		link = model.SlackWorkspaceLink{WorkspaceID: workspaceID}
	}

	link.AccessToken = encToken
	link.TokenNonce = nonce
	link.TeamID = teamID
	link.BotUserID = botUserID

	// Auto-create a private channel if not already linked
	if link.SlackChannelID == "" {
		channelName := slacksvc.BuildChannelNameFromWorkspace(ws.Name, workspaceID)
		channelID, err := c.slack.CreatePrivateChannel(ctx, token, channelName)
		if err != nil {
			return fmt.Errorf("slack: failed to auto-provision channel: %w", err)
		}
		link.SlackChannelID = channelID
		link.SlackChannelName = channelName
		link.AutoCreated = true

		// Invite the installing user so they can see the channel
		if authedUserID != "" {
			if invErr := c.slack.InviteUsersToChannel(ctx, token, channelID, []string{authedUserID}); invErr != nil {
				zlog.Warn().Err(invErr).Str("slackUserID", authedUserID).Msg("[slack] failed to invite installing user to channel")
			}
		}
	}

	if err := c.repo.UpsertSlackWorkspaceLink(ctx, link); err != nil {
		return fmt.Errorf("slack: failed to save workspace link: %w", err)
	}

	zlog.Info().Int64("workspaceID", workspaceID).Str("channel", link.SlackChannelName).Msg("[slack] successfully completed dynamic multi-tenant installation")
	return nil
}

// ─── Inbound Slack events ──────────────────────────────────────────────────────

// HandleSlackEvent processes incoming Events API payloads.
// It handles app_mention events in threads, routing the message text to ReplyToTask.
func (c *controller) HandleSlackEvent(ctx context.Context, payload SlackEventPayload) error {
	ev := payload.Event
	if ev.Type != "app_mention" {
		return nil
	}
	// Mentions in threads have a thread_ts; top-level mentions are ignored.
	if ev.ThreadTS == "" {
		zlog.Debug().Msg("[slack] ignoring top-level app_mention (no thread_ts)")
		return nil
	}

	thread, err := c.repo.GetSlackTaskThreadByChannel(ctx, ev.Channel, ev.ThreadTS)
	if err != nil {
		zlog.Warn().Str("channel", ev.Channel).Str("threadTS", ev.ThreadTS).
			Msg("[slack] received mention in unknown thread")
		return nil
	}

	link, err := c.repo.GetSlackWorkspaceLink(ctx, thread.WorkspaceID)
	botUserID := ""
	decryptedToken := ""
	if err == nil {
		botUserID = link.BotUserID
		if link.AccessToken != "" {
			dec, decErr := security.Decrypt(link.AccessToken, c.tokenKey, link.TokenNonce)
			if decErr == nil {
				decryptedToken = dec
			} else {
				zlog.Warn().Err(decErr).Int64("workspaceID", thread.WorkspaceID).Msg("[slack] failed to decrypt token in event handler")
			}
		}
	}

	// Strip the bot mention (<@BOTID>) from the text
	text := stripBotMention(ev.Text, botUserID)
	if strings.TrimSpace(text) == "" && len(ev.Files) == 0 {
		return nil
	}

	// Download any attached files
	var attachments []entity.Attachment
	if decryptedToken != "" && len(ev.Files) > 0 {
		for _, f := range ev.Files {
			if f.URLPrivateDownload == "" {
				continue
			}
			dataBase64, downloadErr := downloadSlackFile(ctx, decryptedToken, f.URLPrivateDownload)
			if downloadErr != nil {
				zlog.Warn().Err(downloadErr).Str("fileID", f.ID).Msg("[slack] failed to download attachment from Slack")
				continue
			}
			attachments = append(attachments, entity.Attachment{
				Filename: f.Name,
				MimeType: f.MimeType,
				Data:     dataBase64,
			})
		}
	}

	// Resolve workspace owner to use as UserID for ReplyToTask
	ws, err := c.repo.SystemGetWorkspace(ctx, thread.WorkspaceID)
	if err != nil {
		return fmt.Errorf("[slack] failed to get workspace: %w", err)
	}
	ownerID := monoflake.ID(ws.UserID).String()

	ctx = entity.WithOrigin(ctx, entity.OriginSlack)
	_, err = c.crud.ReplyToTask(ctx, entity.ReplyToTaskRequest{
		WorkspaceID: thread.WorkspaceID,
		TaskID:      thread.TaskID,
		Text:        text,
		UserID:      ownerID,
		SlackUser:   ev.User,
		Attachments: attachments,
	})
	if err != nil {
		zlog.Error().Err(err).Int64("taskID", thread.TaskID).Msg("[slack] failed to reply to task from Slack")
		return err
	}

	if c.mcp != nil {
		content := fmt.Sprintf("[Reply to task %s] %s", monoflake.ID(thread.TaskID).String(), text)
		if atts := formatSlackAttachments(attachments); atts != "" {
			content += "\n" + atts
		}
		c.mcp.SendChannelNotification(ctx, thread.WorkspaceID, ownerID, thread.TaskID, content)
	}
	return nil
}

// HandleSlashCommand creates a new AgentRQ task inside the workspace connected to the Slack channel.
func (c *controller) HandleSlashCommand(ctx context.Context, channelID string, text string) (string, bool, error) {
	link, err := c.repo.GetSlackWorkspaceLinkByChannel(ctx, channelID)
	if err != nil {
		return "⚠️ This channel is not connected to any AgentRQ workspace. Please link it first in settings.", true, nil
	}

	// Normalize smart/curly double quotes to straight double quotes for iOS/macOS compatibility
	normalizedText := strings.ReplaceAll(text, "“", "\"")
	normalizedText = strings.ReplaceAll(normalizedText, "”", "\"")

	trimmedText := strings.TrimSpace(normalizedText)
	if trimmedText == "" {
		return "⚠️ Task description cannot be empty. Usage: `/t <task description>` or `/t \"<title>\" \"<description>\"`", true, nil
	}

	ws, err := c.repo.SystemGetWorkspace(ctx, link.WorkspaceID)
	if err != nil {
		return "⚠️ Failed to resolve linked workspace.", true, err
	}
	ownerID := monoflake.ID(ws.UserID).String()

	title := trimmedText
	body := trimmedText

	// Parse quoted parameters if present: /t "<title>" "<description>"
	if strings.HasPrefix(trimmedText, `"`) {
		endQuoteIdx := strings.Index(trimmedText[1:], `"`)
		if endQuoteIdx != -1 {
			titleVal := trimmedText[1 : endQuoteIdx+1]
			bodyVal := strings.TrimSpace(trimmedText[endQuoteIdx+2:])
			if bodyVal != "" {
				// Strip surrounding quotes from description if present
				if strings.HasPrefix(bodyVal, `"`) && strings.HasSuffix(bodyVal, `"`) && len(bodyVal) >= 2 {
					bodyVal = bodyVal[1 : len(bodyVal)-1]
				}
				title = titleVal
				body = bodyVal
			}
		}
	}

	if len(title) > 60 {
		title = title[:60] + "..."
	}

	ctx = entity.WithOrigin(ctx, entity.OriginSlack)
	resp, err := c.crud.CreateTask(ctx, entity.CreateTaskRequest{
		UserID: ownerID,
		Task: entity.Task{
			WorkspaceID: link.WorkspaceID,
			UserID:      ws.UserID,
			CreatedBy:   "human",
			Assignee:    "agent",
			Status:      "notstarted",
			Title:       title,
			Body:        body,
		},
	})
	if err != nil {
		return fmt.Sprintf("⚠️ Failed to create task: %s", err.Error()), true, err
	}

	if c.mcp != nil && resp != nil {
		shouldNotifyMCP := true
		listRs, listErr := c.repo.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: link.WorkspaceID}, ws.UserID)
		if listErr == nil {
			hasOngoing := false
			hasOtherNotStarted := false
			for _, t := range listRs {
				if t.ID == resp.Task.ID {
					continue // skip the newly created task itself
				}
				if t.Status == "ongoing" {
					hasOngoing = true
				}
				if t.Status == "notstarted" && t.Assignee == "agent" {
					hasOtherNotStarted = true
				}
			}
			if hasOngoing || hasOtherNotStarted {
				shouldNotifyMCP = false
			}
		}

		if shouldNotifyMCP {
			content := fmt.Sprintf("[Task %s] %s\n%s", monoflake.ID(resp.Task.ID).String(), resp.Task.Title, resp.Task.Body)
			if atts := formatSlackAttachments(resp.Task.Attachments); atts != "" {
				content += "\n" + atts
			}
			c.mcp.SendChannelNotification(ctx, link.WorkspaceID, ownerID, resp.Task.ID, content)
		}
	}

	return fmt.Sprintf("🚀 *Task created successfully:* %s", title), false, nil
}

// ─── Interactive button handlers ───────────────────────────────────────────────

// HandleTaskApproval processes block_actions with action_id prefix "task_respond:".
// action_id format: task_respond:<base62WorkspaceID>:<base62TaskID>:<action>
func (c *controller) HandleTaskApproval(ctx context.Context, action SlackBlockAction) error {
	parts := strings.SplitN(action.ActionID, ":", 4)
	if len(parts) != 4 {
		return fmt.Errorf("slack: invalid task_respond action_id: %s", action.ActionID)
	}
	workspaceID62, taskID62, taskAction := parts[1], parts[2], parts[3]
	workspaceID := monoflake.IDFromBase62(workspaceID62).Int64()
	taskID := monoflake.IDFromBase62(taskID62).Int64()

	if workspaceID == 0 || taskID == 0 {
		return fmt.Errorf("slack: invalid IDs in action_id")
	}

	ws, err := c.repo.SystemGetWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("slack: workspace not found: %w", err)
	}
	ownerID := monoflake.ID(ws.UserID).String()

	ctx = entity.WithOrigin(ctx, entity.OriginSlack)
	_, err = c.crud.RespondToTask(ctx, entity.RespondToTaskRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Action:      taskAction,
		UserID:      ownerID,
	})

	// Retrieve Slack Workspace bot credentials
	link, linkErr := c.repo.GetSlackWorkspaceLink(ctx, workspaceID)
	if linkErr != nil || link.AccessToken == "" {
		return fmt.Errorf("slack workspace credentials not found: %w", linkErr)
	}
	decryptedToken, decErr := security.Decrypt(link.AccessToken, c.tokenKey, link.TokenNonce)
	if decErr != nil {
		return fmt.Errorf("slack decryption failed: %w", decErr)
	}

	if err != nil {
		// Update the Slack message to show the error
		updateErr := c.slack.UpdateMessage(ctx, decryptedToken, action.ChannelID, action.MessageTS,
			slacksvc.BuildResultBlocks(fmt.Sprintf("⚠️ Action failed: %s", err.Error())))
		c.handleSlackError(ctx, workspaceID, updateErr)
		return err
	}

	actionLabel := map[string]string{
		"allow":     "✅ Approved",
		"allow_all": "🚀 Approved (Allow All Commands)",
		"reject":    "❌ Rejected",
	}[taskAction]
	if actionLabel == "" {
		actionLabel = "✅ Done"
	}
	label := fmt.Sprintf("%s by <@%s>", actionLabel, action.UserID)
	updateErr := c.slack.UpdateMessage(ctx, decryptedToken, action.ChannelID, action.MessageTS,
		slacksvc.BuildResultBlocks(label))
	c.handleSlackError(ctx, workspaceID, updateErr)
	return updateErr
}

// HandleMCPPermission processes block_actions with action_id prefix "task_permission:".
// action_id format: task_permission:<base62WorkspaceID>:<base62TaskID>:<requestID>:<behavior>
func (c *controller) HandleMCPPermission(ctx context.Context, action SlackBlockAction) error {
	parts := strings.SplitN(action.ActionID, ":", 5)
	if len(parts) != 5 {
		return fmt.Errorf("slack: invalid task_permission action_id: %s", action.ActionID)
	}
	workspaceID62, taskID62, requestID, behavior := parts[1], parts[2], parts[3], parts[4]
	workspaceID := monoflake.IDFromBase62(workspaceID62).Int64()
	taskID := monoflake.IDFromBase62(taskID62).Int64()
	_ = taskID // used for context; MCP verdict is by requestID

	if workspaceID == 0 {
		return fmt.Errorf("slack: invalid workspaceID in action_id")
	}

	ws, err := c.repo.SystemGetWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("slack: workspace not found: %w", err)
	}
	ownerID := monoflake.ID(ws.UserID).String()

	// Retrieve Slack Workspace bot credentials
	link, linkErr := c.repo.GetSlackWorkspaceLink(ctx, workspaceID)
	if linkErr != nil || link.AccessToken == "" {
		return fmt.Errorf("slack workspace credentials not found: %w", linkErr)
	}
	decryptedToken, decErr := security.Decrypt(link.AccessToken, c.tokenKey, link.TokenNonce)
	if decErr != nil {
		return fmt.Errorf("slack decryption failed: %w", decErr)
	}

	if c.mcp == nil {
		return fmt.Errorf("slack: MCP manager not available")
	}

	// Try to find the permission request message and save the slack decision flag in its metadata first,
	// so that OnMessageUpdated can skip overwriting the slack-initiated UI update.
	messages, listErr := c.repo.ListMessages(ctx, taskID)
	if listErr == nil {
		for _, m := range messages {
			var metadata map[string]any
			if len(m.Metadata) > 0 {
				_ = json.Unmarshal(m.Metadata, &metadata)
			}
			if metadata != nil && metadata["type"] == "permission_request" {
				reqID, _ := metadata["request_id"].(string)
				if reqID == "" {
					reqID, _ = metadata["requestId"].(string)
				}
				if reqID == requestID {
					metadata["decided_in_slack"] = true
					metadata["slack_user_id"] = action.UserID
					metadata["slack_user_name"] = action.UserName
					b, marshalErr := json.Marshal(metadata)
					if marshalErr == nil {
						_ = c.repo.UpdateMessageMetadata(ctx, taskID, m.ID, b)
					}
					break
				}
			}
		}
	}

	if err := c.mcp.SendPermissionVerdict(ctx, workspaceID, ownerID, taskID, requestID, behavior); err != nil {
		errMsg := err.Error()
		var updateErr error
		if strings.Contains(errMsg, "expired") {
			updateErr = c.slack.UpdateMessage(ctx, decryptedToken, action.ChannelID, action.MessageTS,
				slacksvc.BuildResultBlocks("⚠️ This permission request has expired (agent was restarted)."))
		} else {
			updateErr = c.slack.UpdateMessage(ctx, decryptedToken, action.ChannelID, action.MessageTS,
				slacksvc.BuildResultBlocks(fmt.Sprintf("⚠️ Failed: %s", errMsg)))
		}
		c.handleSlackError(ctx, workspaceID, updateErr)
		return err
	}

	label := map[string]string{
		"allow": fmt.Sprintf("✅ Allowed by <@%s>", action.UserID),
		"deny":  fmt.Sprintf("❌ Denied by <@%s>", action.UserID),
	}[behavior]
	if label == "" {
		label = "Done"
	}
	updateErr := c.slack.UpdateMessage(ctx, decryptedToken, action.ChannelID, action.MessageTS,
		slacksvc.BuildResultBlocks(label))
	c.handleSlackError(ctx, workspaceID, updateErr)
	return updateErr
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

func stripBotMention(text, botUserID string) string {
	if botUserID == "" {
		return text
	}
	// Slack mentions look like <@UXXXXXXXXX>
	mention := "<@" + botUserID + ">"
	return strings.TrimSpace(strings.ReplaceAll(text, mention, ""))
}

func downloadSlackFile(ctx context.Context, token string, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(b), nil
}

// isPermissionRequest returns true if the message metadata indicates a pending MCP permission request.
func isPermissionRequest(msg entity.Message) bool {
	if msg.Metadata == nil {
		return false
	}
	b, err := json.Marshal(msg.Metadata)
	if err != nil {
		return false
	}
	var m struct {
		Type   string `json:"type"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	return m.Type == "permission_request" && m.Status == "pending"
}

// extractPermissionRequestFields extracts requestId and a description from
// the permission_request metadata.
func extractPermissionRequestFields(msg entity.Message) (requestID, toolDesc string) {
	b, err := json.Marshal(msg.Metadata)
	if err != nil {
		return "", ""
	}
	var m struct {
		RequestID  string `json:"requestId"`
		RequestID2 string `json:"request_id"`
		Tool       string `json:"tool"`
		ToolName   string `json:"tool_name"`
		Args       string `json:"args"`
		Desc       string `json:"description"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return "", ""
	}
	reqID := m.RequestID
	if reqID == "" {
		reqID = m.RequestID2
	}
	tool := m.Tool
	if tool == "" {
		tool = m.ToolName
	}
	args := m.Args
	if args == "" {
		args = m.Desc
	}
	desc := tool
	if args != "" {
		desc += " " + args
	}
	return reqID, desc
}

func (c *controller) Start(ctx context.Context) error {
	if !c.slack.IsEnabled() {
		return nil
	}
	res, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD})
	if err != nil {
		return fmt.Errorf("failed to subscribe to CRUD topic: %w", err)
	}

	zlog.Info().Msg("[slack] started controller background loop")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-res.Events:
				if !ok {
					zlog.Warn().Msg("[slack] pubsub channel closed")
					return
				}

				event, ok := msg.(entity.CRUDEvent)
				if !ok {
					zlog.Error().Msg("[slack] received invalid event type")
					continue
				}

				c.processEvent(ctx, event)
			}
		}
	}()

	return nil
}

func (c *controller) processEvent(ctx context.Context, event entity.CRUDEvent) {
	if event.Origin == entity.OriginSlack && event.ResourceType == entity.ResourceMessage {
		zlog.Debug().Interface("event", event).Msg("[slack] skipping message event originating from slack to prevent echoing")
		return
	}
	switch event.ResourceType {
	case entity.ResourceTask:
		if event.Action == entity.ActionTaskCreate || event.Action == entity.ActionTaskFromScheduled {
			t, err := c.repo.SystemGetTask(ctx, event.ResourceID)
			if err == nil {
				task := c.fromModelTaskToEntity(t)
				go c.OnTaskCreated(ctx, task)
			}
		}
	case entity.ResourceWorkspace:
		if event.Action == entity.ActionWorkspaceCreate {
			w, err := c.repo.SystemGetWorkspace(ctx, event.ResourceID)
			if err == nil {
				workspace := c.fromModelWorkspaceToEntity(w)
				go c.OnWorkspaceCreated(ctx, workspace)
			}
		}
	case entity.ResourceMessage:
		if event.Action == entity.ActionMessageCreate {
			m, err := c.repo.SystemGetMessage(ctx, event.ResourceID)
			if err == nil {
				t, err := c.repo.SystemGetTask(ctx, m.TaskID)
				if err == nil {
					message := c.fromModelMessageToEntity(m)
					task := c.fromModelTaskToEntity(t)
					go c.OnMessageCreated(ctx, message, task)
				}
			}
		} else if event.Action == entity.ActionMessageUpdate {
			m, err := c.repo.SystemGetMessage(ctx, event.ResourceID)
			if err == nil {
				t, err := c.repo.SystemGetTask(ctx, m.TaskID)
				if err == nil {
					message := c.fromModelMessageToEntity(m)
					task := c.fromModelTaskToEntity(t)
					go c.OnMessageUpdated(ctx, message, task)
				}
			}
		}
	}
}

func (c *controller) fromModelWorkspaceToEntity(m model.Workspace) entity.Workspace {
	return entity.Workspace{
		ID:             m.ID,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		UserID:         m.UserID,
		Name:           m.Name,
		Description:    m.Description,
		Icon:           m.Icon,
		ArchivedAt:     m.ArchivedAt,
		TokenEncrypted: m.TokenEncrypted,
		TokenNonce:     m.TokenNonce,
	}
}

func (c *controller) fromModelTaskToEntity(m model.Task) entity.Task {
	return entity.Task{
		ID:               m.ID,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		WorkspaceID:      m.WorkspaceID,
		UserID:           m.UserID,
		CreatedBy:        m.CreatedBy,
		Assignee:         m.Assignee,
		Status:           m.Status,
		Title:            m.Title,
		Body:             m.Body,
		CronSchedule:     m.CronSchedule,
		AllowAllCommands: m.AllowAllCommands,
	}
}

func (c *controller) fromModelMessageToEntity(m model.Message) entity.Message {
	res := entity.Message{
		ID:        m.ID,
		CreatedAt: m.CreatedAt,
		TaskID:    m.TaskID,
		UserID:    m.UserID,
		Sender:    m.Sender,
		Text:      m.Text,
	}
	if len(m.Metadata) > 0 {
		var meta map[string]any
		if err := json.Unmarshal(m.Metadata, &meta); err == nil {
			res.Metadata = meta
		}
	}
	if res.Metadata == nil {
		res.Metadata = make(map[string]any)
	}
	return res
}

func (c *controller) handleSlackError(ctx context.Context, workspaceID int64, err error) {
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "channel_not_found") {
		zlog.Warn().Int64("workspaceID", workspaceID).Msg("[slack] channel_not_found received, deleting slack connection for workspace")
		if delErr := c.repo.DeleteSlackWorkspaceLink(ctx, workspaceID); delErr != nil {
			zlog.Error().Err(delErr).Int64("workspaceID", workspaceID).Msg("[slack] failed to delete slack workspace link after channel_not_found")
		}
	}
}

func formatSlackAttachments(atts []entity.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(atts))
	for _, a := range atts {
		if a.ID != "" {
			parts = append(parts, fmt.Sprintf("  - id=%s name=%s type=%s", a.ID, a.Filename, a.MimeType))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Attachments:\n" + strings.Join(parts, "\n")
}

