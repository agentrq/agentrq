// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import "time"

// Pano is the account's board of markdown docs, written and read by every
// workspace of the account. A WorkspaceID of 0 on any of these is the person,
// from the interface; otherwise it is the workspace that wrote it, the parent
// for a fork.
type (
	// PanoDoc is one doc. Markdown, Files, Attachments and Comments are
	// filled only when one doc is read, and the last two only when asked for.
	PanoDoc struct {
		ID                   int64
		CreatedAt            time.Time
		UpdatedAt            time.Time
		Title                string
		Excerpt              string
		Markdown             string
		CreatedByWorkspaceID int64
		UpdatedByWorkspaceID int64
		TTLDays              int
		ExpiresAt            time.Time
		Color                PanoColor
		ChecklistDone        int
		ChecklistTotal       int
		// Version changes on every write; an update must send the one it read.
		Version     int64
		Files       []PanoFile
		Attachments []PanoFile
		Comments    []PanoComment
	}

	// PanoFile is a file of a doc: a linked file, named by a link in its
	// markdown, or an attachment. URL is its public link.
	PanoFile struct {
		ID          int64
		CreatedAt   time.Time
		WorkspaceID int64
		Filename    string
		MimeType    string
		SizeBytes   int
		URL         string
	}

	// PanoComment is a comment on a doc.
	PanoComment struct {
		ID          int64
		CreatedAt   time.Time
		WorkspaceID int64
		Body        string
	}

	// PanoUpload is a file sent to be stored, its content base64.
	PanoUpload struct {
		Filename string
		MimeType string
		Data     string
	}

	ListPanoDocsRequest struct {
		UserID string
	}

	ListPanoDocsResponse struct {
		Docs []PanoDoc
	}

	GetPanoDocRequest struct {
		UserID             string
		WorkspaceID        int64
		ID                 int64
		IncludeComments    bool
		IncludeAttachments bool
	}

	GetPanoDocResponse struct {
		Doc PanoDoc
	}

	// SavePanoDocRequest creates a doc when ID is 0 and replaces its title
	// and markdown otherwise. Files are linked files: each must be named by
	// a link in Markdown, and replaces a linked file of the same name.
	// TTLDays is how long the doc lives from this save; 0 keeps the doc's
	// own, or the default for a new doc. A blank Color, a color's name, keeps
	// the doc's own, or yellow for a new doc. Replacing a doc needs the
	// Version the caller read; creating one needs none.
	SavePanoDocRequest struct {
		UserID      string
		WorkspaceID int64
		ID          int64
		Version     int64
		Title       string
		Markdown    string
		TTLDays     int
		Color       string
		Files       []PanoUpload
	}

	SavePanoDocResponse struct {
		Doc PanoDoc
	}

	// PanoEditOp is one change to a doc's markdown: Op "append" adds Text at
	// the end; "replace" swaps OldString, which must occur exactly once, for
	// NewString.
	PanoEditOp struct {
		Op        string
		Text      string
		OldString string
		NewString string
	}

	// EditPanoDocRequest applies Ops, all or none, to the markdown of the doc
	// at Version.
	EditPanoDocRequest struct {
		UserID      string
		WorkspaceID int64
		ID          int64
		Version     int64
		Ops         []PanoEditOp
	}

	DeletePanoDocRequest struct {
		UserID      string
		WorkspaceID int64
		ID          int64
	}

	AddPanoCommentRequest struct {
		UserID      string
		WorkspaceID int64
		ID          int64
		Body        string
	}

	AddPanoCommentResponse struct {
		Comment PanoComment
	}

	AttachPanoFilesRequest struct {
		UserID      string
		WorkspaceID int64
		ID          int64
		Files       []PanoUpload
	}

	AttachPanoFilesResponse struct {
		Attachments []PanoFile
	}
)

// PanoError is a request the Pano refuses, with the sentence to show its
// caller.
type PanoError struct {
	Message string
	// Conflict is a write made on a stale version of the doc.
	Conflict bool
}

func (e *PanoError) Error() string { return e.Message }

// PanoColor is the color of a doc's post-it, stored as its number. Appended,
// never reordered: the value is stored on every doc.
type PanoColor uint8

const (
	// PanoColorYellow is the zero value, so a doc whose writer named no
	// color is yellow.
	PanoColorYellow PanoColor = iota
	PanoColorPink
	PanoColorGreen
	PanoColorBlue
	PanoColorOrange
	PanoColorPurple
	PanoColorAmber
)

// PanoColors are the colors' names, indexed by PanoColor.
var PanoColors = []string{"yellow", "pink", "green", "blue", "orange", "purple", "amber"}

func (c PanoColor) String() string {
	if int(c) < len(PanoColors) {
		return PanoColors[c]
	}
	return PanoColors[PanoColorYellow]
}

// ParsePanoColor reads a color's name.
func ParsePanoColor(name string) (PanoColor, bool) {
	for i, n := range PanoColors {
		if n == name {
			return PanoColor(i), true
		}
	}
	return 0, false
}
