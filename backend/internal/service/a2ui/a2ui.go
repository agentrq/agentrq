// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package a2ui decides whether the UI an agent attached to a message, a list
// of A2UI v1.0 messages (https://a2ui.org) in the basic catalog or our own
// (the basic one, a Chart and a Progress), may be shown,
// and returns a cleaned copy with media references resolved to our storage.
//
// Structure is checked with the official schemas. What they cannot say is
// checked here: which messages and components we accept, that the component
// tree is a tree, the size limits, and that media comes only from files
// attached to the same message.
package a2ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	// BasicCatalogID is the official basic catalog.
	BasicCatalogID = "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"
	// AgentRQCatalogID is ours: the basic catalog and the components in
	// agentrqComponents (catalog/agentrq.json).
	AgentRQCatalogID = "https://agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json"

	MaxBytes      = 64 << 10
	MaxComponents = 200
	MaxDepth      = 16
	// maxProblems caps how many problems one error lists.
	maxProblems = 20

	rootID           = "root"
	attachmentPrefix = "attachment:"
)

var (
	surfaceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

	// The messages an agent may send. callRendererFunction and
	// agentFunctionResponse are refused.
	acceptedKinds = []string{"createSurface", "updateComponents", "updateDataModel", "deleteSurface"}

	displayComponents     = []string{"Text", "Image", "Icon", "Video", "AudioPlayer", "Row", "Column", "List", "Card", "Tabs", "Divider"}
	interactiveComponents = []string{"Button", "TextField", "CheckBox", "ChoicePicker", "Slider", "DateTimeInput", "Modal"}
	// agentrqComponents are display components only our catalog has.
	agentrqComponents = []string{"Chart", "Progress"}

	// CatalogIDs are the catalogs a surface may use.
	CatalogIDs = []string{BasicCatalogID, AgentRQCatalogID}

	// Media properties, which the schema types as DynamicString: a binding or
	// a call would pass it, so the literal attachment rule is ours.
	mediaProperties = map[string][]string{
		"Image":       {"url"},
		"Video":       {"url", "posterUrl"},
		"AudioPlayer": {"url"},
	}
)

// Options says what a message may contain.
type Options struct {
	// Interactive allows the input components (Button, TextField, CheckBox,
	// ChoicePicker, Slider, DateTimeInput, Modal).
	Interactive bool
	// Attachments maps each filename attached to the message to its stored
	// URL, which replaces an `attachment:<filename>` media reference.
	Attachments map[string]string
}

// Surface is an accepted ui: one surface's messages, cleaned and resolved.
type Surface struct {
	SurfaceID string            `json:"surfaceId"`
	Messages  []json.RawMessage `json:"messages"`
	// Deleted is an update that deleted the surface: it has no messages.
	Deleted bool `json:"deleted,omitempty"`
}

// Error lists every problem found, each with where it is and what to do.
type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "ui is not valid A2UI v1.0: " + strings.Join(e.Problems, "; ")
}

// Service validates an agent's ui.
type Service interface {
	Validate(ctx context.Context, raw json.RawMessage, opts Options) (*Surface, error)
	// Merge applies an update, a ui with no createSurface, to a stored
	// surface, and validates the result.
	Merge(ctx context.Context, current *Surface, update json.RawMessage, opts Options) (*Surface, error)
}

type service struct {
	schemas *schemas
}

// New loads the official schemas.
func New() (Service, error) {
	return newService(schemaFiles)
}

func newService(fsys fs.FS) (*service, error) {
	s, err := loadSchemas(fsys)
	if err != nil {
		return nil, fmt.Errorf("loading the A2UI schemas: %w", err)
	}
	return &service{schemas: s}, nil
}

func (s *service) Validate(_ context.Context, raw json.RawMessage, opts Options) (*Surface, error) {
	if len(raw) > MaxBytes {
		return nil, &Error{Problems: []string{fmt.Sprintf("ui is %d bytes; the limit is %d (64 KiB)", len(raw), MaxBytes)}}
	}
	msgs, err := decode(raw)
	if err != nil {
		return nil, &Error{Problems: []string{"ui must be a JSON array of A2UI v1.0 messages: " + err.Error()}}
	}
	// The schema library takes a json.Number for a string, so the schemas
	// see a second copy with plain float64 numbers.
	var plain []any
	_ = json.Unmarshal(raw, &plain) // decode has already accepted it
	if len(msgs) == 0 {
		return nil, &Error{Problems: []string{"ui is empty: send createSurface, then updateComponents with a component whose id is \"root\""}}
	}
	v := &validator{schemas: s.schemas, opts: opts, components: map[string]*component{}}
	for i, m := range msgs {
		v.message(i, m, plain[i])
	}
	if len(v.problems) == 0 {
		v.graph()
	}
	if len(v.problems) > 0 {
		if v.dropped > 0 {
			v.problems = append(v.problems, fmt.Sprintf("and %d more", v.dropped))
		}
		return nil, &Error{Problems: v.problems}
	}
	out := make([]json.RawMessage, len(msgs))
	for i, m := range msgs {
		// Every value was decoded from JSON, so this cannot fail.
		out[i], _ = json.Marshal(m)
	}
	return &Surface{SurfaceID: v.surfaceID, Messages: out}, nil
}

// decode keeps numbers as json.Number, so the cleaned copy carries them
// exactly as the agent wrote them.
func decode(raw json.RawMessage) ([]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var msgs []any
	if err := dec.Decode(&msgs); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the array")
	}
	return msgs, nil
}

// component is the latest definition of an id: a later updateComponents
// replaces an earlier one with the same id, as the spec says.
type component struct {
	at  string
	obj map[string]any
}

type validator struct {
	schemas    *schemas
	opts       Options
	surfaceID  string
	catalogID  string
	count      int
	components map[string]*component
	problems   []string
	dropped    int
}

func (v *validator) addf(format string, args ...any) {
	if len(v.problems) == maxProblems {
		v.dropped++
		return
	}
	v.problems = append(v.problems, fmt.Sprintf(format, args...))
}

// message checks the message m; p is the same message with plain numbers.
func (v *validator) message(i int, m, p any) {
	at := fmt.Sprintf("ui[%d]", i)
	obj, ok := m.(map[string]any)
	if !ok {
		v.addf(`%s: must be an object such as {"version": "1.0", "createSurface": {...}}`, at)
		return
	}
	if obj["version"] != "1.0" {
		v.addf(`%s: version must be "1.0"`, at)
		return
	}
	var kinds []string
	for k := range obj {
		if k != "version" {
			kinds = append(kinds, k)
		}
	}
	sort.Strings(kinds)
	if len(kinds) != 1 {
		v.addf("%s: must hold exactly one of %s; it holds %d", at, strings.Join(acceptedKinds, ", "), len(kinds))
		return
	}
	kind := kinds[0]
	if !slices.Contains(acceptedKinds, kind) {
		v.addf("%s: %s is not accepted; send only %s", at, kind, strings.Join(acceptedKinds, ", "))
		return
	}
	at += "." + kind
	body, _ := obj[kind].(map[string]any)
	before := len(v.problems) + v.dropped
	if list, ok := body["components"].([]any); ok {
		plainList := p.(map[string]any)[kind].(map[string]any)["components"].([]any)
		v.componentList(at+".components", list, plainList)
	}
	if len(v.problems)+v.dropped > before {
		// The message's schema would only repeat a component's problem,
		// less precisely.
		return
	}
	if err := v.schemas.messages[kind].Validate(p); err != nil {
		v.addf("%s: %s", at, schemaError(err))
		return
	}
	v.surface(at, body["surfaceId"].(string))
	if kind == "createSurface" {
		id, _ := body["catalogId"].(string)
		if !slices.Contains(CatalogIDs, id) {
			v.addf("%s: catalogId must be %q, the basic catalog, or %q, ours, which adds Chart and Progress", at, BasicCatalogID, AgentRQCatalogID)
			return
		}
		v.catalogID = id
	}
}

func (v *validator) surface(at, id string) {
	switch {
	case !surfaceIDPattern.MatchString(id):
		v.addf("%s: surfaceId %q must be 1 to 64 letters, digits, '_' or '-'", at, id)
	case v.surfaceID == "":
		v.surfaceID = id
	case id != v.surfaceID:
		v.addf("%s: surfaceId %q differs from %q: one message carries one surface", at, id, v.surfaceID)
	}
}

func (v *validator) componentList(at string, list, plain []any) {
	seen := map[string]bool{}
	for j, c := range list {
		cat := fmt.Sprintf("%s[%d]", at, j)
		obj, ok := c.(map[string]any)
		if !ok {
			v.addf(`%s: must be an object such as {"id": "root", "component": "Text", "text": "Hi"}`, cat)
			continue
		}
		id, _ := obj["id"].(string)
		if id != "" {
			cat += fmt.Sprintf(" %q", id)
		}
		if v.count++; v.count == MaxComponents+1 {
			v.addf("%s: more than %d components; the limit is %d", cat, MaxComponents, MaxComponents)
		}
		name, _ := obj["component"].(string)
		if !v.allowed(cat, name) {
			continue
		}
		if unknown := v.unknownProperties(name, obj); len(unknown) > 0 {
			v.addf("%s: %s is not a property of %s, which has %s", cat, strings.Join(unknown, " or "), name, strings.Join(v.schemas.properties[name], ", "))
			continue
		}
		if err := v.schemas.components[name].Validate(plain[j]); err != nil {
			v.addf("%s: %s", cat, schemaError(err))
			continue
		}
		if seen[id] {
			v.addf("%s: id %q is used twice in this list; ids must be unique", cat, id)
			continue
		}
		seen[id] = true
		if c, ok := obj["@catalogId"]; ok && c != BasicCatalogID && (c != AgentRQCatalogID || v.catalogID != AgentRQCatalogID) {
			v.addf("%s: @catalogId must be the surface's catalog, %q, or left out", cat, v.catalogID)
			continue
		}
		v.media(cat, name, obj)
		if name == "Chart" {
			v.chart(cat, obj)
		}
		v.components[id] = &component{at: cat, obj: obj}
	}
}

func (v *validator) allowed(at, name string) bool {
	ours := v.catalogID == AgentRQCatalogID
	if slices.Contains(displayComponents, name) || v.opts.Interactive && slices.Contains(interactiveComponents, name) ||
		ours && slices.Contains(agentrqComponents, name) {
		return true
	}
	use := append([]string{}, displayComponents...)
	if ours {
		use = append(use, agentrqComponents...)
	}
	if v.opts.Interactive {
		use = append(use, interactiveComponents...)
	}
	switch {
	case slices.Contains(agentrqComponents, name):
		v.addf("%s: %s is in our catalog: createSurface's catalogId must be %q to use it", at, name, AgentRQCatalogID)
	case slices.Contains(interactiveComponents, name):
		v.addf("%s: %s is not available here, where the ui only displays; use %s", at, name, strings.Join(use, ", "))
	default:
		v.addf("%s: component %q is not one of %s", at, name, strings.Join(use, ", "))
	}
	return false
}

const (
	// heatmapDays and heatmapHourDays are the longest spans a heatmap's grid
	// draws: a year of days, a month of hours.
	heatmapDays     = 366
	heatmapHourDays = 31
	heatmapDay      = "2006-01-02"
	heatmapHour     = "2006-01-02 15:04"
)

// chart checks what the schema cannot of a Chart's literal points: values
// are zero or more, and a heatmap's labels are all days or all hours, the
// analytics pages' buckets, over a span its grid can draw. Bound points are
// the renderer's to check, which leaves out any it cannot draw.
func (v *validator) chart(at string, obj map[string]any) {
	points, ok := obj["data"].([]any)
	if !ok {
		return
	}
	heatmap := obj["variant"] == "heatmap"
	layout := ""
	var first, last time.Time
	for i, p := range points {
		point := p.(map[string]any)
		if n, _ := point["value"].(json.Number).Float64(); n < 0 {
			v.addf("%s: data[%d].value is %s; values are counts or amounts, zero or more", at, i, point["value"])
			return
		}
		if !heatmap {
			continue
		}
		label := point["label"].(string)
		if layout == "" {
			layout = heatmapDay
			if len(label) > len(heatmapDay) {
				layout = heatmapHour
			}
		}
		t, err := time.Parse(layout, label)
		if err != nil || layout == heatmapHour && t.Minute() != 0 {
			v.addf("%s: data[%d].label %q must be a day, YYYY-MM-DD, or an hour, YYYY-MM-DD HH:00, like the first point's; a heatmap draws one or the other", at, i, label)
			return
		}
		if i == 0 || t.Before(first) {
			first = t
		}
		if i == 0 || t.After(last) {
			last = t
		}
	}
	limit := heatmapDays
	if layout == heatmapHour {
		limit = heatmapHourDays
	}
	if days := int(last.Sub(first).Hours()/24) + 1; heatmap && days > limit {
		v.addf("%s: the heatmap spans %d days; a %s grid draws at most %d", at, days, map[string]string{heatmapDay: "day", heatmapHour: "hour"}[layout], limit)
	}
}

// HasComponent says whether a surface draws a component named name.
func HasComponent(s *Surface, name string) bool {
	for _, raw := range s.Messages {
		var m struct {
			UpdateComponents struct {
				Components []struct {
					Component string `json:"component"`
				} `json:"components"`
			} `json:"updateComponents"`
		}
		_ = json.Unmarshal(raw, &m) // every message was marshalled by Validate
		for _, c := range m.UpdateComponents.Components {
			if c.Component == name {
				return true
			}
		}
	}
	return false
}

func (v *validator) unknownProperties(name string, obj map[string]any) []string {
	var unknown []string
	for prop := range obj {
		if !slices.Contains(v.schemas.properties[name], prop) {
			unknown = append(unknown, fmt.Sprintf("%q", prop))
		}
	}
	sort.Strings(unknown)
	return unknown
}

// media replaces each attachment reference with the stored URL.
func (v *validator) media(at, name string, obj map[string]any) {
	for _, prop := range mediaProperties[name] {
		val, ok := obj[prop]
		if !ok {
			continue
		}
		s, ok := val.(string)
		if !ok {
			v.addf("%s: %s must be attachment:<filename> of a file attached to this reply, as a literal string: media cannot come from a data binding or a function call", at, prop)
			continue
		}
		filename, ok := strings.CutPrefix(s, attachmentPrefix)
		if !ok || filename == "" {
			v.addf("%s: %s must be attachment:<filename> of a file attached to this reply: media cannot come from a link", at, prop)
			continue
		}
		u, ok := v.opts.Attachments[filename]
		if !ok {
			v.addf("%s: %s names %q, but no file by that name is attached to this reply (attached: %s)", at, prop, filename, attachedNames(v.opts.Attachments))
			continue
		}
		obj[prop] = u
	}
}

func attachedNames(m map[string]string) string {
	if len(m) == 0 {
		return "none"
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, fmt.Sprintf("%q", n))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// graph checks the final component tree: one root, every reference names a
// component, no cycles, and no deeper than MaxDepth.
func (v *validator) graph() {
	if len(v.components) == 0 {
		return
	}
	if v.components[rootID] == nil {
		v.addf(`no component has id "root": the surface is drawn from the component with id "root"`)
		return
	}
	ids := make([]string, 0, len(v.components))
	for id := range v.components {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	broken := false
	for _, id := range ids {
		c := v.components[id]
		for _, r := range refs(c.obj) {
			if v.components[r.id] == nil {
				v.addf("%s: %s names %q, which is not a component in this ui", c.at, r.prop, r.id)
				broken = true
			}
		}
	}
	if broken {
		return
	}
	depth := map[string]int{}
	for _, id := range ids {
		if v.height(id, depth, nil) < 0 {
			return
		}
	}
	if d := depth[rootID]; d > MaxDepth {
		v.addf("the component tree is %d levels deep from root; the limit is %d", d, MaxDepth)
	}
}

// height is the number of levels from id down to its deepest leaf, or -1
// after reporting a cycle. path is the chain of ids being walked.
func (v *validator) height(id string, depth map[string]int, path []string) int {
	if h, ok := depth[id]; ok {
		return h
	}
	if i := slices.Index(path, id); i >= 0 {
		v.addf("components form a cycle: %s", strings.Join(append(path[i:], id), " -> "))
		return -1
	}
	path = append(path, id)
	h := 0
	for _, r := range refs(v.components[id].obj) {
		ch := v.height(r.id, depth, path)
		if ch < 0 {
			return -1
		}
		h = max(h, ch)
	}
	depth[id] = h + 1
	return h + 1
}

type ref struct {
	prop string
	id   string
}

// refs lists the component ids a component names. The schema has already
// checked each property's shape.
func refs(obj map[string]any) []ref {
	var out []ref
	for _, prop := range []string{"child", "trigger", "content"} {
		if id, ok := obj[prop].(string); ok {
			out = append(out, ref{prop, id})
		}
	}
	switch children := obj["children"].(type) {
	case []any:
		for i, c := range children {
			out = append(out, ref{fmt.Sprintf("children[%d]", i), c.(string)})
		}
	case map[string]any:
		out = append(out, ref{"children.componentId", children["componentId"].(string)})
	}
	tabs, _ := obj["tabs"].([]any)
	for i, t := range tabs {
		out = append(out, ref{fmt.Sprintf("tabs[%d].child", i), t.(map[string]any)["child"].(string)})
	}
	return out
}

// schemaError condenses the library's error, a chain of "validating
// <schema location>: " prefixes that name our schema files, to the properties
// it went through and the failure at the end.
func schemaError(err error) string {
	parts := strings.Split(err.Error(), ": ")
	var props []string
	n, prev := 0, ""
	for ; n < len(parts) && strings.HasPrefix(parts[n], "validating "); n++ {
		loc := strings.TrimPrefix(parts[n], "validating ")
		// A location that extends the one before it is the same walk, one
		// step deeper: only its new part names new properties.
		rest := strings.TrimPrefix(loc, prev)
		prev = loc
		for _, piece := range strings.Split(rest, "/properties/")[1:] {
			prop, _, _ := strings.Cut(piece, "/")
			props = append(props, prop)
		}
	}
	msg := strings.Join(parts[n:], ": ")
	if i := strings.Index(msg, "oneOf: did not validate against any of"); i >= 0 {
		msg = msg[:i] + "matches none of the forms the schema allows"
	}
	if len(props) > 0 {
		msg = strings.Join(props, ".") + ": " + msg
	}
	return msg
}
