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
	DeletePanoDoc(ctx context.Context, req entity.DeletePanoDocRequest) error
	AddPanoComment(ctx context.Context, req entity.AddPanoCommentRequest) (*entity.AddPanoCommentResponse, error)
	AttachPanoFiles(ctx context.Context, req entity.AttachPanoFilesRequest) (*entity.AttachPanoFilesResponse, error)
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
)

// PanoMaxTTLDays is the longest a doc may live, from pano.maxTtlDays.
var PanoMaxTTLDays = DefaultPanoMaxTTLDays

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
	out := make([]entity.PanoDoc, len(docs))
	for i, d := range docs {
		out[i] = fromModelPanoDoc(d)
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
	doc.Markdown = d.Markdown
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
		if existing, err = c.repository.ListPanoFiles(ctx, d.ID); err != nil {
			return nil, err
		}
	} else {
		d.ID = c.idgen.NextID()
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

	d.Title = title
	d.Markdown = req.Markdown
	d.Excerpt = panoExcerpt(req.Markdown)
	d.ChecklistDone, d.ChecklistTotal = panoChecklist(req.Markdown)
	d.SizeBytes = len(req.Markdown)
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
	if err := c.repository.SavePanoDoc(ctx, d, create, add, drop); err != nil {
		c.purgePano(written)
		return nil, err
	}
	c.purgePano(dropped)

	action := entity.ActionPanoUpdate
	if create {
		action = entity.ActionPanoCreate
	}
	c.emitPanoEvent(ctx, action, uid, req.WorkspaceID, d.ID)
	doc := fromModelPanoDoc(d)
	doc.Markdown = req.Markdown
	doc.Files = []entity.PanoFile{}
	for _, f := range append(linked, add...) {
		doc.Files = append(doc.Files, c.fromModelPanoFile(f))
	}
	return &entity.SavePanoDocResponse{Doc: doc}, nil
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
	fence := ""
	for _, line := range strings.Split(markdown, "\n") {
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
		if m := panoTaskItem.FindStringSubmatch(line); m != nil {
			total++
			if m[1] != " " {
				done++
			}
		}
	}
	return done, total
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
