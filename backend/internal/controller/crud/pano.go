// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// PanoController is an account's Pano: markdown docs every workspace of the
// account can write, read, comment on and attach files to, and the person can
// too from the interface. A request's WorkspaceID is who is writing: the
// workspace (a fork's parent, for a fork), or 0 for the person.
type PanoController interface {
	ListPanoDocs(ctx context.Context, req entity.ListPanoDocsRequest) (*entity.ListPanoDocsResponse, error)
	// DeleteExpiredPanoDocs deletes every doc of any account that has
	// expired, and returns how many it deleted.
	DeleteExpiredPanoDocs(ctx context.Context) (int, error)
	GetPanoDoc(ctx context.Context, req entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error)
	SavePanoDoc(ctx context.Context, req entity.SavePanoDocRequest) (*entity.SavePanoDocResponse, error)
	// EditPanoDoc changes part of a doc's markdown, all or none of the ops.
	EditPanoDoc(ctx context.Context, req entity.EditPanoDocRequest) (*entity.SavePanoDocResponse, error)
	DeletePanoDoc(ctx context.Context, req entity.DeletePanoDocRequest) error
	AddPanoComment(ctx context.Context, req entity.AddPanoCommentRequest) (*entity.AddPanoCommentResponse, error)
	AttachPanoFiles(ctx context.Context, req entity.AttachPanoFilesRequest) (*entity.AttachPanoFilesResponse, error)
	// SyncPanoTask recounts the ticked items of the docs linked to a task
	// whose status changed, and returns the account and the ids of the docs
	// linked to it, whose items and tasks now read differently.
	SyncPanoTask(ctx context.Context, taskID int64) (userID int64, docIDs []int64, err error)
	// BackfillPanoDocTasks links every doc to the tasks its checklist items
	// were assigned to before links were stored, and returns how many it
	// added.
	BackfillPanoDocTasks(ctx context.Context) (int64, error)
}

// Limits on what a Pano doc holds. Anything over one is refused, never cut,
// so the caller can decide what to leave out. A file is capped so that it fits,
// base64, in one 4 MB request.
const (
	PanoMaxTitleChars    = 200
	PanoMaxMarkdownBytes = 256 << 10
	PanoMaxFileBytes     = 2 << 20
	PanoMaxFiles         = 100
	PanoMaxCommentBytes  = 16 << 10
	panoExcerptChars     = 280
	panoMaxFilenameBytes = 255
	// PanoDefaultTTLDays is how long a new doc lives when its writer does
	// not say, or the longest allowed when that is shorter.
	PanoDefaultTTLDays = 30
	// DefaultPanoMaxTTLDays is pano.maxTtlDays when it is not set.
	DefaultPanoMaxTTLDays = 365
	// panoSweepBatch is how many expired docs one sweep query takes.
	panoSweepBatch = 100
	// panoBackfillBatch is how many docs one backfill query takes.
	panoBackfillBatch = 100
)

// PanoMaxTTLDays is the longest a doc may live, from pano.maxTtlDays.
var PanoMaxTTLDays = DefaultPanoMaxTTLDays

// panoStale is the refusal of a write made without, or on a stale, version:
// it names the current one, and what to do about it.
func panoStale(sent, current int64) error {
	if sent == 0 {
		return &entity.PanoError{Conflict: true, Message: fmt.Sprintf("updating a Pano doc needs the version you read, and none was sent: read the doc, apply your change to it and save with its version (now %d)", current)}
	}
	return &entity.PanoError{Conflict: true, Message: fmt.Sprintf("the Pano doc changed since you read it (you sent version %d, it is now %d): read it again, apply your change to the new content and save again with the new version", sent, current)}
}

func panoRefuse(format string, a ...any) error {
	return &entity.PanoError{Message: fmt.Sprintf(format, a...)}
}

// panoLinkedName is a linked file's name: one plain path segment, so a link
// to it in the markdown is the name itself.
var panoLinkedName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// The places a markdown doc names a file: an inline link or image, a
// reference definition, and an HTML src or href.
var (
	panoInlineLink    = regexp.MustCompile(`\]\(\s*<?([^)\s>]+)`)
	panoReferenceLink = regexp.MustCompile(`(?m)^ {0,3}\[[^\]]+\]:\s*<?([^\s>]+)`)
	panoHTMLLink      = regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*["']([^"']+)["']`)
)

// panoLinks is every target the markdown links to, as a file name: without
// a leading "./", a query or a fragment, and percent-decoded.
func panoLinks(markdown string) map[string]bool {
	links := map[string]bool{}
	for _, re := range []*regexp.Regexp{panoInlineLink, panoReferenceLink, panoHTMLLink} {
		for _, m := range re.FindAllStringSubmatch(markdown, -1) {
			target := strings.TrimPrefix(m[1], "./")
			if i := strings.IndexAny(target, "?#"); i >= 0 {
				target = target[:i]
			}
			if decoded, err := url.PathUnescape(target); err == nil {
				target = decoded
			}
			links[target] = true
		}
	}
	return links
}

// panoAuthor checks the writing workspace belongs to the account. The person
// (workspace 0) needs no check.
func (c *controller) panoAuthor(ctx context.Context, userID string, workspaceID int64) (int64, error) {
	uid := monoflake.IDFromBase62(userID).Int64()
	if uid == 0 {
		return 0, fmt.Errorf("invalid request")
	}
	if workspaceID == 0 {
		return uid, nil
	}
	ok, err := c.CheckWorkspaceAccess(ctx, workspaceID, userID)
	if err != nil {
		return 0, fmt.Errorf("check workspace access: %w", err)
	}
	if !ok {
		return 0, base.ErrNotFound
	}
	return uid, nil
}

// emitPanoEvent counts a Pano write, by the person or an agent, and is what
// tells open boards to read the Pano again.
func (c *controller) emitPanoEvent(ctx context.Context, action entity.Action, uid, workspaceID, docID int64) {
	actor := entity.ActorHuman
	if workspaceID != 0 {
		actor = entity.ActorAgent
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       action,
		UserID:       uid,
		WorkspaceID:  workspaceID,
		ResourceType: entity.ResourcePano,
		ResourceID:   docID,
		Actor:        actor,
	})
}

func (c *controller) ListPanoDocs(ctx context.Context, req entity.ListPanoDocsRequest) (*entity.ListPanoDocsResponse, error) {
	uid, err := c.panoAuthor(ctx, req.UserID, 0)
	if err != nil {
		return nil, err
	}
	docs, err := c.repository.ListPanoDocs(ctx, uid, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	// A card's excerpt shows its assigned items with their tasks' status
	// now, as a read of the doc does.
	ids := make([]int64, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	linked, err := c.repository.ListPanoDocTasks(ctx, ids)
	if err != nil {
		return nil, err
	}
	tasks := map[int64][]base.PanoTaskStatus{}
	for _, t := range linked {
		tasks[t.DocID] = append(tasks[t.DocID], t)
	}
	out := make([]entity.PanoDoc, len(docs))
	for i, d := range docs {
		out[i] = fromModelPanoDoc(d)
		out[i].Excerpt = panoOverlay(d.Excerpt, tasks[d.ID])
	}
	return &entity.ListPanoDocsResponse{Docs: out}, nil
}

func (c *controller) GetPanoDoc(ctx context.Context, req entity.GetPanoDocRequest) (*entity.GetPanoDocResponse, error) {
	uid, err := c.panoAuthor(ctx, req.UserID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	d, err := c.repository.GetPanoDoc(ctx, uid, req.ID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	doc := fromModelPanoDoc(d)
	tasks, err := c.repository.ListPanoDocTasks(ctx, []int64{d.ID})
	if err != nil {
		return nil, err
	}
	doc.Markdown = panoOverlay(d.Markdown, tasks)
	doc.Tasks = make([]entity.PanoDocTask, len(tasks))
	for i, t := range tasks {
		doc.Tasks[i] = entity.PanoDocTask{TaskID: t.TaskID, WorkspaceID: t.WorkspaceID, Status: t.Status}
	}
	files, err := c.repository.ListPanoFiles(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	doc.Files = []entity.PanoFile{}
	if req.IncludeAttachments {
		doc.Attachments = []entity.PanoFile{}
	}
	for _, f := range files {
		switch {
		case f.Linked:
			doc.Files = append(doc.Files, c.fromModelPanoFile(f))
		case req.IncludeAttachments:
			doc.Attachments = append(doc.Attachments, c.fromModelPanoFile(f))
		}
	}
	if req.IncludeComments {
		comments, err := c.repository.ListPanoComments(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		doc.Comments = make([]entity.PanoComment, len(comments))
		for i, cm := range comments {
			doc.Comments[i] = entity.PanoComment{ID: cm.ID, CreatedAt: cm.CreatedAt, WorkspaceID: cm.WorkspaceID, Body: cm.Body}
		}
	}
	return &entity.GetPanoDocResponse{Doc: doc}, nil
}

// SavePanoDoc writes a doc, and the linked files sent with it. Every file
// sent must be named by a link in the markdown, and a linked file the
// markdown no longer names is dropped: a Pano file is always reachable from
// its doc.
func (c *controller) SavePanoDoc(ctx context.Context, req entity.SavePanoDocRequest) (*entity.SavePanoDocResponse, error) {
	uid, err := c.panoAuthor(ctx, req.UserID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(req.Title)
	switch {
	case title == "":
		return nil, panoRefuse("a Pano doc needs a title")
	case utf8.RuneCountInString(title) > PanoMaxTitleChars:
		return nil, panoRefuse("the title is longer than %d characters", PanoMaxTitleChars)
	case strings.TrimSpace(req.Markdown) == "":
		return nil, panoRefuse("a Pano doc needs markdown")
	case len(req.Markdown) > PanoMaxMarkdownBytes:
		return nil, panoRefuse("the markdown is %d bytes, over the %d byte limit", len(req.Markdown), PanoMaxMarkdownBytes)
	case !utf8.ValidString(req.Markdown):
		return nil, panoRefuse("the markdown is not valid UTF-8")
	case req.TTLDays < 0 || req.TTLDays > PanoMaxTTLDays:
		return nil, panoRefuse("ttlDays must be between 1 and %d", PanoMaxTTLDays)
	}
	color, known := entity.ParsePanoColor(req.Color)
	if req.Color != "" && !known {
		return nil, panoRefuse("color must be one of %s", strings.Join(entity.PanoColors, ", "))
	}
	links := panoLinks(req.Markdown)
	uploads, err := decodePanoUploads(req.Files, true)
	if err != nil {
		return nil, err
	}
	sent := map[string]bool{}
	for _, u := range uploads {
		if sent[u.filename] {
			return nil, panoRefuse("%s is sent twice", u.filename)
		}
		sent[u.filename] = true
		if !links[u.filename] {
			return nil, panoRefuse("%s is not linked from the markdown: link it, as [%s](%s) or ![%s](%s), or send it with attach", u.filename, u.filename, u.filename, u.filename, u.filename)
		}
	}

	now := time.Now().UTC()
	create := req.ID == 0
	d := model.PanoDoc{ID: req.ID, CreatedAt: now, UserID: uid, CreatedByWorkspaceID: req.WorkspaceID}
	var existing []model.PanoFile
	if !create {
		if d, err = c.repository.GetPanoDoc(ctx, uid, req.ID, now); err != nil {
			return nil, err
		}
		if req.Version != d.Version {
			return nil, panoStale(req.Version, d.Version)
		}
		if existing, err = c.repository.ListPanoFiles(ctx, d.ID); err != nil {
			return nil, err
		}
	} else {
		d.ID = c.idgen.NextID()
		d.Version = 1
	}
	var drop []int64
	var dropped []string
	var linked []model.PanoFile
	kept := 0
	for _, f := range existing {
		if !f.Linked {
			kept++
			continue
		}
		if sent[f.Filename] || !links[f.Filename] {
			drop = append(drop, f.ID)
			dropped = append(dropped, f.StorageID)
			continue
		}
		linked = append(linked, f)
		kept++
	}
	if kept+len(uploads) > PanoMaxFiles {
		return nil, panoRefuse("a Pano doc holds at most %d files", PanoMaxFiles)
	}

	// The items assigned to tasks are written with each task's status now,
	// and the doc is linked to those tasks.
	assigned, err := c.repository.ListPanoTaskStatuses(ctx, uid, panoAssignedTaskIDs(req.Markdown))
	if err != nil {
		return nil, err
	}
	markdown := panoOverlay(req.Markdown, assigned)
	taskLinks := make([]model.PanoDocTask, len(assigned))
	for i, t := range assigned {
		taskLinks[i] = model.PanoDocTask{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, DocID: d.ID, TaskID: t.TaskID, WorkspaceID: t.WorkspaceID}
	}

	d.Title = title
	d.Markdown = markdown
	d.Excerpt = panoExcerpt(markdown)
	d.ChecklistDone, d.ChecklistTotal = panoChecklist(markdown)
	d.SizeBytes = len(markdown)
	d.UpdatedAt = now
	d.UpdatedByWorkspaceID = req.WorkspaceID
	// A save restarts the doc's clock, for as long as it asks or as long as
	// it already lived, capped by the configured maximum.
	d.TTLDays = min(cmp.Or(req.TTLDays, d.TTLDays, PanoDefaultTTLDays), PanoMaxTTLDays)
	d.ExpiresAt = now.AddDate(0, 0, d.TTLDays)
	if req.Color != "" {
		d.Color = uint8(color)
	}
	add, err := c.storePanoUploads(uid, d.ID, req.WorkspaceID, true, now, uploads)
	written := make([]string, len(add))
	for i, f := range add {
		written[i] = f.StorageID
	}
	if err != nil {
		c.purgePano(written)
		return nil, err
	}
	if err := c.repository.SavePanoDoc(ctx, d, create, add, drop, taskLinks); err != nil {
		c.purgePano(written)
		if errors.Is(err, base.ErrVersionConflict) {
			// Another write got in between the read and this one.
			current := d.Version
			if latest, gerr := c.repository.GetPanoDoc(ctx, uid, d.ID, now); gerr == nil {
				current = latest.Version
			}
			return nil, panoStale(req.Version, current)
		}
		return nil, err
	}
	if !create {
		d.Version++
	}
	c.purgePano(dropped)

	action := entity.ActionPanoUpdate
	if create {
		action = entity.ActionPanoCreate
	}
	c.emitPanoEvent(ctx, action, uid, req.WorkspaceID, d.ID)
	c.linkPanoDocToAgentTask(ctx, uid, d.ID, now)
	doc := fromModelPanoDoc(d)
	doc.Markdown = markdown
	doc.Files = []entity.PanoFile{}
	for _, f := range append(linked, add...) {
		doc.Files = append(doc.Files, c.fromModelPanoFile(f))
	}
	return &entity.SavePanoDocResponse{Doc: doc}, nil
}

// EditPanoDoc applies the ops to the doc's markdown in order, and saves the
// result as SavePanoDoc does: the same version check, limits and linked
// files. An op that cannot apply refuses the whole edit.
func (c *controller) EditPanoDoc(ctx context.Context, req entity.EditPanoDocRequest) (*entity.SavePanoDocResponse, error) {
	uid, err := c.panoAuthor(ctx, req.UserID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	d, err := c.repository.GetPanoDoc(ctx, uid, req.ID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	// Checked before the ops: against a stale doc they would fail with a
	// misleading "not found".
	if req.Version != d.Version {
		return nil, panoStale(req.Version, d.Version)
	}
	if len(req.Ops) == 0 {
		return nil, panoRefuse("edit needs at least one op")
	}
	// The ops apply to the markdown as a read returns it, with each assigned
	// item showing its task's status now.
	tasks, err := c.repository.ListPanoDocTasks(ctx, []int64{d.ID})
	if err != nil {
		return nil, err
	}
	markdown := panoOverlay(d.Markdown, tasks)
	for i, op := range req.Ops {
		switch op.Op {
		case "append":
			if op.Text == "" {
				return nil, panoRefuse("op %d: append needs text", i+1)
			}
			markdown += op.Text
		case "replace":
			if op.OldString == "" {
				return nil, panoRefuse("op %d: replace needs an oldString", i+1)
			}
			switch n := strings.Count(markdown, op.OldString); n {
			case 1:
				markdown = strings.Replace(markdown, op.OldString, op.NewString, 1)
			case 0:
				return nil, panoRefuse("op %d: oldString is not in the markdown, nothing was changed", i+1)
			default:
				return nil, panoRefuse("op %d: oldString occurs %d times, it must occur once: add the text around it, nothing was changed", i+1, n)
			}
		default:
			return nil, panoRefuse("op %d: op must be append or replace", i+1)
		}
	}
	return c.SavePanoDoc(ctx, entity.SavePanoDocRequest{UserID: req.UserID, WorkspaceID: req.WorkspaceID, ID: req.ID, Version: req.Version, Title: d.Title, Markdown: markdown})
}

func (c *controller) DeletePanoDoc(ctx context.Context, req entity.DeletePanoDocRequest) error {
	uid, err := c.panoAuthor(ctx, req.UserID, req.WorkspaceID)
	if err != nil {
		return err
	}
	storageIDs, err := c.repository.DeletePanoDoc(ctx, uid, req.ID)
	if err != nil {
		return err
	}
	c.purgePano(storageIDs)
	c.emitPanoEvent(ctx, entity.ActionPanoDelete, uid, req.WorkspaceID, req.ID)
	return nil
}

func (c *controller) AddPanoComment(ctx context.Context, req entity.AddPanoCommentRequest) (*entity.AddPanoCommentResponse, error) {
	uid, err := c.panoAuthor(ctx, req.UserID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(req.Body)
	switch {
	case body == "":
		return nil, panoRefuse("a comment needs text")
	case len(body) > PanoMaxCommentBytes:
		return nil, panoRefuse("the comment is %d bytes, over the %d byte limit", len(body), PanoMaxCommentBytes)
	case !utf8.ValidString(body):
		return nil, panoRefuse("the comment is not valid UTF-8")
	}
	cm := model.PanoComment{ID: c.idgen.NextID(), CreatedAt: time.Now().UTC(), DocID: req.ID, WorkspaceID: req.WorkspaceID, Body: body}
	if err := c.repository.AddPanoComment(ctx, uid, cm); err != nil {
		return nil, err
	}
	c.emitPanoEvent(ctx, entity.ActionPanoComment, uid, req.WorkspaceID, req.ID)
	return &entity.AddPanoCommentResponse{Comment: entity.PanoComment{ID: cm.ID, CreatedAt: cm.CreatedAt, WorkspaceID: cm.WorkspaceID, Body: cm.Body}}, nil
}

// AttachPanoFiles adds attachments to a doc: files listed with it that need
// no link, like a task's attachments.
func (c *controller) AttachPanoFiles(ctx context.Context, req entity.AttachPanoFilesRequest) (*entity.AttachPanoFilesResponse, error) {
	uid, err := c.panoAuthor(ctx, req.UserID, req.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if len(req.Files) == 0 {
		return nil, panoRefuse("attach needs at least one file")
	}
	uploads, err := decodePanoUploads(req.Files, false)
	if err != nil {
		return nil, err
	}
	if _, err := c.repository.GetPanoDoc(ctx, uid, req.ID, time.Now().UTC()); err != nil {
		return nil, err
	}
	existing, err := c.repository.ListPanoFiles(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if len(existing)+len(uploads) > PanoMaxFiles {
		return nil, panoRefuse("a Pano doc holds at most %d files", PanoMaxFiles)
	}
	add, err := c.storePanoUploads(uid, req.ID, req.WorkspaceID, false, time.Now().UTC(), uploads)
	written := make([]string, len(add))
	for i, f := range add {
		written[i] = f.StorageID
	}
	if err != nil {
		c.purgePano(written)
		return nil, err
	}
	if err := c.repository.AddPanoFiles(ctx, uid, req.ID, add); err != nil {
		c.purgePano(written)
		return nil, err
	}
	c.emitPanoEvent(ctx, entity.ActionPanoAttach, uid, req.WorkspaceID, req.ID)
	out := make([]entity.PanoFile, len(add))
	for i, f := range add {
		out[i] = c.fromModelPanoFile(f)
	}
	return &entity.AttachPanoFilesResponse{Attachments: out}, nil
}

// DeleteExpiredPanoDocs deletes expired docs a batch at a time, until none
// is left. Every backend instance sweeps: a doc another one deleted first is
// skipped, not an error.
func (c *controller) DeleteExpiredPanoDocs(ctx context.Context) (int, error) {
	deleted := 0
	for {
		docs, err := c.repository.ListExpiredPanoDocs(ctx, time.Now().UTC(), panoSweepBatch)
		if err != nil {
			return deleted, err
		}
		for _, d := range docs {
			storageIDs, err := c.repository.DeletePanoDoc(ctx, d.UserID, d.ID)
			if errors.Is(err, base.ErrNotFound) {
				continue
			}
			if err != nil {
				return deleted, err
			}
			c.purgePano(storageIDs)
			deleted++
		}
		if len(docs) < panoSweepBatch {
			return deleted, nil
		}
	}
}

// panoUpload is an upload checked and decoded.
type panoUpload struct {
	filename string
	mimeType string
	content  []byte
}

// decodePanoUploads checks each file's name and size and decodes it. A
// linked file's name must be one that a markdown link can name.
func decodePanoUploads(files []entity.PanoUpload, linked bool) ([]panoUpload, error) {
	out := make([]panoUpload, len(files))
	for i, f := range files {
		name := strings.TrimSpace(f.Filename)
		switch {
		case name == "":
			return nil, panoRefuse("every file needs a filename")
		case linked && !panoLinkedName.MatchString(name):
			return nil, panoRefuse("%q cannot be linked: a linked file's name is letters, digits, '.', '_' and '-', up to 128 characters", name)
		case len(name) > panoMaxFilenameBytes:
			return nil, panoRefuse("the filename %q is longer than %d bytes", name, panoMaxFilenameBytes)
		}
		content, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return nil, panoRefuse("%s: data is not base64", name)
		}
		if len(content) > PanoMaxFileBytes {
			return nil, panoRefuse("%s is %d bytes, over the %d byte limit", name, len(content), PanoMaxFileBytes)
		}
		mimeType := strings.TrimSpace(f.MimeType)
		if mimeType == "" {
			mimeType = mime.TypeByExtension(filepath.Ext(name))
		}
		if mimeType == "" {
			mimeType = http.DetectContentType(content)
		}
		out[i] = panoUpload{filename: name, mimeType: mimeType, content: content}
	}
	return out, nil
}

// storePanoUploads writes each upload to a fresh blob of the doc. On an error
// it returns the files it did write, for the caller to purge.
func (c *controller) storePanoUploads(uid, docID, workspaceID int64, linked bool, now time.Time, uploads []panoUpload) ([]model.PanoFile, error) {
	files := make([]model.PanoFile, 0, len(uploads))
	for _, u := range uploads {
		f := model.PanoFile{
			ID:          c.idgen.NextID(),
			CreatedAt:   now,
			DocID:       docID,
			WorkspaceID: workspaceID,
			Linked:      linked,
			Filename:    u.filename,
			MimeType:    u.mimeType,
			SizeBytes:   len(u.content),
			StorageID:   panoStorageID(uid, docID, c.idgen.NextID()),
		}
		if _, err := storage.SaveBlob(c.panoStorage, f.StorageID, base64.StdEncoding.EncodeToString(u.content), storage.SafeContentType(u.mimeType)); err != nil {
			return files, fmt.Errorf("store pano file %s: %w", u.filename, err)
		}
		files = append(files, f)
	}
	return files, nil
}

// panoStorageID keys a Pano blob as u-<owner>/pano-<doc>/<blob>. The blob id
// is fresh on every write, so a replaced blob is purged only after the new
// one is saved.
func panoStorageID(uid, docID, blobID int64) string {
	return "u-" + monoflake.ID(uid).String() + "/pano-" + monoflake.ID(docID).String() + "/" + monoflake.ID(blobID).String()
}

// purgePano deletes blobs whose rows are gone, or were never written. Best
// effort, like purge.
func (c *controller) purgePano(storageIDs []string) {
	for _, id := range storageIDs {
		if id != "" {
			_ = c.panoStorage.Delete(id)
		}
	}
}

// panoTaskItem is a task-list item: a list marker, then [ ] or [x].
var panoTaskItem = regexp.MustCompile(`^[ \t]*(?:[-*+]|\d{1,9}[.)])[ \t]+\[([ xX])\](?:[ \t]|$)`)

// panoChecklist counts the markdown's task-list items, and the checked ones,
// outside fenced code, where they render as text. The board mirrors this
// count when it checks one off, so the two must agree.
func panoChecklist(markdown string) (done, total int) {
	lines := strings.Split(markdown, "\n")
	panoItems(lines, func(i int, item []int) {
		total++
		if lines[i][item[2]:item[3]] != " " {
			done++
		}
	})
	return done, total
}

// panoAssignee ends a checklist item assigned to a task: a link to the task,
// titled with its status, as the web app writes it.
var panoAssignee = regexp.MustCompile(`\]\((/workspaces/([0-9A-Za-z]+)/tasks/([0-9A-Za-z]+))(?: "[a-z]*")?\)[ \t]*$`)

// panoItems calls fn with each checklist item of the lines, outside fenced
// code, and the index of its box in the item's match.
func panoItems(lines []string, fn func(i int, item []int)) {
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		if item := panoTaskItem.FindStringSubmatchIndex(line); item != nil {
			fn(i, item)
		}
	}
}

// panoAssignedTaskIDs is the ids of the tasks the markdown's checklist items
// are assigned to, once each.
func panoAssignedTaskIDs(markdown string) []int64 {
	seen := map[int64]bool{}
	var ids []int64
	lines := strings.Split(markdown, "\n")
	panoItems(lines, func(i int, _ []int) {
		m := panoAssignee.FindStringSubmatch(lines[i])
		if m == nil {
			return
		}
		if id := monoflake.IDFromBase62(m[3]).Int64(); id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	})
	return ids
}

// panoTaskPath is the web app's path of a task, which an assigned item links.
func panoTaskPath(workspaceID, taskID int64) string {
	return "/workspaces/" + monoflake.ID(workspaceID).String() + "/tasks/" + monoflake.ID(taskID).String()
}

// panoOverlay gives every checklist item assigned to one of tasks that task's
// status, ticked once it is completed. However many items one task has, all
// of them follow it. An item linking a task that is not in tasks is left as
// it is.
func panoOverlay(markdown string, tasks []base.PanoTaskStatus) string {
	if len(tasks) == 0 {
		return markdown
	}
	status := make(map[string]string, len(tasks))
	for _, t := range tasks {
		status[panoTaskPath(t.WorkspaceID, t.TaskID)] = t.Status
	}
	lines := strings.Split(markdown, "\n")
	panoItems(lines, func(i int, item []int) {
		line := lines[i]
		link := panoAssignee.FindStringSubmatchIndex(line)
		if link == nil {
			return
		}
		path := line[link[2]:link[3]]
		st, ok := status[path]
		if !ok {
			return
		}
		box := " "
		if st == "completed" || st == "done" {
			box = "x"
		}
		lines[i] = line[:item[2]] + box + line[item[3]:link[0]] + `](` + path + ` "` + st + `")`
	})
	return strings.Join(lines, "\n")
}

// linkPanoDocToAgentTask links a doc an agent wrote to the task it is on: the
// one ongoing task of the workspace that wrote it. With several ongoing it is
// not linked, as a missing link costs less than a wrong one. Best effort: the
// doc is saved either way.
func (c *controller) linkPanoDocToAgentTask(ctx context.Context, uid, docID int64, now time.Time) {
	workspaceID := entity.GetAgentWorkspace(ctx)
	if workspaceID == 0 {
		return
	}
	ids, err := c.repository.ListOngoingAgentTaskIDs(ctx, uid, workspaceID, 2)
	if err != nil || len(ids) != 1 {
		return
	}
	added, err := c.repository.LinkPanoDocTasks(ctx, []model.PanoDocTask{{ID: c.idgen.NextID(), CreatedAt: now, UserID: uid, DocID: docID, TaskID: ids[0], WorkspaceID: workspaceID}})
	if err == nil && added > 0 {
		c.emitPanoEvent(ctx, entity.ActionPanoLink, uid, workspaceID, docID)
	}
}

func (c *controller) SyncPanoTask(ctx context.Context, taskID int64) (int64, []int64, error) {
	t, err := c.repository.SystemGetTask(ctx, taskID)
	if err != nil {
		return 0, nil, err
	}
	ids, err := c.repository.ListPanoDocIDsOfTask(ctx, taskID)
	if err != nil {
		return t.UserID, nil, err
	}
	now := time.Now().UTC()
	var linked []int64
	for _, id := range ids {
		d, err := c.repository.GetPanoDoc(ctx, t.UserID, id, now)
		if errors.Is(err, base.ErrNotFound) {
			continue
		}
		if err != nil {
			return t.UserID, linked, err
		}
		tasks, err := c.repository.ListPanoDocTasks(ctx, []int64{id})
		if err != nil {
			return t.UserID, linked, err
		}
		if done, _ := panoChecklist(panoOverlay(d.Markdown, tasks)); done != d.ChecklistDone {
			if err := c.repository.SetPanoChecklistDone(ctx, t.UserID, id, done); err != nil {
				return t.UserID, linked, err
			}
		}
		linked = append(linked, id)
	}
	return t.UserID, linked, nil
}

// BackfillPanoDocTasks reads the docs a page at a time and links each to the
// account's tasks its checklist items are assigned to.
func (c *controller) BackfillPanoDocTasks(ctx context.Context) (int64, error) {
	var added int64
	var after int64
	for {
		docs, err := c.repository.ListPanoDocsNamingTasks(ctx, after, panoBackfillBatch)
		if err != nil {
			return added, err
		}
		for _, d := range docs {
			after = d.ID
			tasks, err := c.repository.ListPanoTaskStatuses(ctx, d.UserID, panoAssignedTaskIDs(d.Markdown))
			if err != nil {
				return added, err
			}
			links := make([]model.PanoDocTask, len(tasks))
			for i, t := range tasks {
				links[i] = model.PanoDocTask{ID: c.idgen.NextID(), CreatedAt: d.UpdatedAt, UserID: d.UserID, DocID: d.ID, TaskID: t.TaskID, WorkspaceID: t.WorkspaceID}
			}
			n, err := c.repository.LinkPanoDocTasks(ctx, links)
			if err != nil {
				return added, err
			}
			added += n
		}
		if len(docs) < panoBackfillBatch {
			return added, nil
		}
	}
}

// panoExcerpt is the opening of a doc's markdown, shown on its card.
func panoExcerpt(markdown string) string {
	s := strings.TrimSpace(markdown)
	if utf8.RuneCountInString(s) <= panoExcerptChars {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:panoExcerptChars])) + "…"
}

func fromModelPanoDoc(d model.PanoDoc) entity.PanoDoc {
	return entity.PanoDoc{
		ID:                   d.ID,
		CreatedAt:            d.CreatedAt,
		UpdatedAt:            d.UpdatedAt,
		Title:                d.Title,
		Excerpt:              d.Excerpt,
		CreatedByWorkspaceID: d.CreatedByWorkspaceID,
		UpdatedByWorkspaceID: d.UpdatedByWorkspaceID,
		TTLDays:              d.TTLDays,
		ExpiresAt:            d.ExpiresAt,
		Color:                entity.PanoColor(d.Color),
		ChecklistDone:        d.ChecklistDone,
		ChecklistTotal:       d.ChecklistTotal,
		Version:              d.Version,
	}
}

func (c *controller) fromModelPanoFile(f model.PanoFile) entity.PanoFile {
	return entity.PanoFile{
		ID:          f.ID,
		CreatedAt:   f.CreatedAt,
		WorkspaceID: f.WorkspaceID,
		Filename:    f.Filename,
		MimeType:    f.MimeType,
		SizeBytes:   f.SizeBytes,
		URL:         storage.PublicURL(c.panoStorage, f.StorageID),
	}
}
