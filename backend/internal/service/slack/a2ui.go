// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package slack

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	slackapi "github.com/slack-go/slack"
)

// OpenActionID is the action id of a card's "Open in AgentRQ" button. The
// button is a link, which Slack still reports to the interactions route.
const OpenActionID = "open_in_agentrq"

const (
	// Slack takes 50 blocks a message: one is the sender, one the button.
	maxCardBlocks  = 48
	maxSectionText = 3000
	maxFields      = 10
	maxFieldText   = 2000
	// The validator stores no card deeper than 16; a hand-edited row could
	// loop, and drawing one must never recurse without end.
	maxCardDepth = 32
)

// BuildCardBlocks draws an agent's A2UI card, the message's metadata, as
// Block Kit: Text as mrkdwn sections (a caption as context), a Row of texts
// as fields, Card and Divider as dividers, Image from its public link. What
// Slack cannot show (inputs, buttons, players, a card cut at Slack's limits)
// is left to an "Open in AgentRQ" button linking to the task. It returns nil
// when the metadata holds no card, or the card shows and asks nothing: the
// message's text is posted then.
func BuildCardBlocks(sender, text string, metadata any, taskURL string) []slackapi.Block {
	messages := cardMessages(metadata)
	if messages == nil {
		return nil
	}
	d := &cardDrawer{components: map[string]map[string]any{}}
	for _, msg := range messages {
		if !d.apply(msg) {
			return nil
		}
	}
	d.draw("root", nil, 0)
	content := d.blocks()
	if len(content) == 0 {
		if !d.more {
			return nil
		}
		content = []slackapi.Block{section(escapeMarkdown(text))}
	}
	blocks := append([]slackapi.Block{
		slackapi.NewContextBlock("", slackapi.NewTextBlockObject(slackapi.MarkdownType, senderPrefix(sender), false, false)),
	}, content...)
	if d.more {
		button := slackapi.NewButtonBlockElement(OpenActionID, "",
			slackapi.NewTextBlockObject(slackapi.PlainTextType, "Open in AgentRQ", false, false))
		button.URL = taskURL
		blocks = append(blocks, slackapi.NewActionBlock("", button))
	}
	return blocks
}

// cardMessages is a message's card: a reply's, or a question asked as one.
func cardMessages(metadata any) []any {
	meta, _ := metadata.(map[string]any)
	switch {
	case meta["type"] == "a2ui":
	case meta["type"] == "elicitation_request" && meta["mode"] == "a2ui":
	default:
		return nil
	}
	// A copy: drawing writes the data model, never the caller's metadata.
	var messages []any
	b, _ := json.Marshal(meta["messages"])
	_ = json.Unmarshal(b, &messages)
	return messages
}

type itemKind int

const (
	itemText itemKind = iota
	itemCaption
	itemFields
	itemDivider
	itemImage
)

type cardItem struct {
	kind   itemKind
	text   string
	fields []string
	url    string
}

// cardDrawer is a card's state, built from its messages, and what it draws.
type cardDrawer struct {
	components map[string]map[string]any
	data       any
	items      []cardItem
	// more is set when the card holds something Slack is not shown.
	more bool
}

// apply reads one stored message; false when it deletes the card.
func (d *cardDrawer) apply(msg any) bool {
	obj, _ := msg.(map[string]any)
	if create, ok := obj["createSurface"].(map[string]any); ok {
		d.components, d.data = map[string]map[string]any{}, nil
		d.put(create["components"])
		if dm, ok := create["dataModel"]; ok {
			d.data = dm
		}
	}
	if update, ok := obj["updateComponents"].(map[string]any); ok {
		d.put(update["components"])
	}
	if update, ok := obj["updateDataModel"].(map[string]any); ok {
		path, _ := update["path"].(string)
		value, set := update["value"]
		d.data = writePath(d.data, pointerTokens(path), value, set)
	}
	_, deleted := obj["deleteSurface"]
	return !deleted
}

func (d *cardDrawer) put(list any) {
	items, _ := list.([]any)
	for _, c := range items {
		obj, _ := c.(map[string]any)
		if id, ok := obj["id"].(string); ok {
			d.components[id] = obj
		}
	}
}

// draw adds a component's items; scope is a List item's data path.
func (d *cardDrawer) draw(id string, scope []string, depth int) {
	c := d.components[id]
	if c == nil || depth > maxCardDepth {
		return
	}
	switch c["component"] {
	case "Text":
		text := strings.TrimSpace(d.str(c["text"], scope))
		if text == "" {
			return
		}
		kind := itemText
		if c["variant"] == "caption" {
			kind = itemCaption
		}
		d.items = append(d.items, cardItem{kind: kind, text: mrkdwn(text)})
	case "Image":
		url := d.str(c["url"], scope)
		if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
			// Only a public link: Slack cannot sign in to fetch the rest.
			d.more = true
			return
		}
		d.items = append(d.items, cardItem{kind: itemImage, url: url, text: d.str(c["description"], scope)})
	case "Divider":
		d.items = append(d.items, cardItem{kind: itemDivider})
	case "Card":
		child, _ := c["child"].(string)
		d.draw(child, scope, depth+1)
		d.items = append(d.items, cardItem{kind: itemDivider})
	case "Column", "List":
		d.children(c["children"], scope, depth)
	case "Row":
		start := len(d.items)
		d.children(c["children"], scope, depth)
		d.fold(start)
	case "Tabs":
		tabs, _ := c["tabs"].([]any)
		for _, t := range tabs {
			tab, _ := t.(map[string]any)
			if title := strings.TrimSpace(d.str(tab["title"], scope)); title != "" {
				d.items = append(d.items, cardItem{kind: itemText, text: "*" + escapeMarkdown(title) + "*"})
			}
			child, _ := tab["child"].(string)
			d.draw(child, scope, depth+1)
		}
	case "Icon":
		// Decoration only.
	default:
		// Inputs, buttons, a modal, players: the page has them.
		d.more = true
	}
}

// children draws a fixed list of ids, or a template once per data item.
func (d *cardDrawer) children(children any, scope []string, depth int) {
	switch list := children.(type) {
	case []any:
		for _, id := range list {
			s, _ := id.(string)
			d.draw(s, scope, depth+1)
		}
	case map[string]any:
		id, _ := list["componentId"].(string)
		path, _ := list["path"].(string)
		at := d.tokens(path, scope)
		switch items := readPath(d.data, at).(type) {
		case []any:
			for i := range items {
				d.draw(id, append(slices.Clone(at), strconv.Itoa(i)), depth+1)
			}
		case map[string]any:
			for _, k := range slices.Sorted(maps.Keys(items)) {
				d.draw(id, append(slices.Clone(at), k), depth+1)
			}
		}
	}
}

// fold makes a Row's texts, from start on, one section of fields side by
// side; a Row holding anything else stays one item under another.
func (d *cardDrawer) fold(start int) {
	row := d.items[start:]
	if len(row) < 2 || len(row) > maxFields {
		return
	}
	fields := make([]string, len(row))
	for i, it := range row {
		if it.kind != itemText && it.kind != itemCaption {
			return
		}
		fields[i] = it.text
	}
	d.items = append(d.items[:start], cardItem{kind: itemFields, fields: fields})
}

// blocks turns the items into Block Kit, with no divider at either end or
// twice in a row, within Slack's limits.
func (d *cardDrawer) blocks() []slackapi.Block {
	var out []slackapi.Block
	divider := false
	for _, it := range d.items {
		if it.kind == itemDivider {
			divider = len(out) > 0
			continue
		}
		if len(out)+btoi(divider) >= maxCardBlocks {
			d.more = true
			break
		}
		if divider {
			out = append(out, slackapi.NewDividerBlock())
			divider = false
		}
		out = append(out, d.block(it))
	}
	return out
}

func (d *cardDrawer) block(it cardItem) slackapi.Block {
	switch it.kind {
	case itemCaption:
		return slackapi.NewContextBlock("", slackapi.NewTextBlockObject(slackapi.MarkdownType, d.cut(it.text, maxSectionText), false, false))
	case itemFields:
		fields := make([]*slackapi.TextBlockObject, len(it.fields))
		for i, f := range it.fields {
			fields[i] = slackapi.NewTextBlockObject(slackapi.MarkdownType, d.cut(f, maxFieldText), false, false)
		}
		return slackapi.NewSectionBlock(nil, fields, nil)
	case itemImage:
		alt := strings.TrimSpace(it.text)
		if alt == "" {
			alt = "Image"
		}
		return slackapi.NewImageBlock(it.url, d.cut(alt, maxFieldText), "", nil)
	default:
		return section(d.cut(it.text, maxSectionText))
	}
}

// cut shortens a text to Slack's limit for its place, marking the card as
// having more than Slack shows.
func (d *cardDrawer) cut(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	d.more = true
	return string([]rune(s)[:limit-1]) + "…"
}

func section(text string) slackapi.Block {
	return slackapi.NewSectionBlock(slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false), nil, nil)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

var placeholder = regexp.MustCompile(`\\?\$\{([^}]*)\}`)

// str resolves a DynamicString: a literal, a data binding, or a function
// call. Of the functions only formatString is read; the others show the
// value they were given, unformatted.
func (d *cardDrawer) str(v any, scope []string) string {
	obj, ok := v.(map[string]any)
	if !ok {
		return scalar(v)
	}
	if path, ok := obj["@path"].(string); ok {
		return scalar(readPath(d.data, d.tokens(path, scope)))
	}
	args, _ := obj["args"].(map[string]any)
	if obj["@call"] != "formatString" {
		return d.str(args["value"], scope)
	}
	format, _ := args["value"].(string)
	return placeholder.ReplaceAllStringFunc(format, func(m string) string {
		inner := placeholder.FindStringSubmatch(m)[1]
		if strings.HasPrefix(m, `\`) || strings.Contains(inner, "(") {
			// Escaped, or a function call the page evaluates.
			return strings.TrimPrefix(m, `\`)
		}
		return scalar(readPath(d.data, d.tokens(inner, scope)))
	})
}

// tokens is a data path's place in the model: an absolute path, or one
// relative to a List item's scope.
func (d *cardDrawer) tokens(path string, scope []string) []string {
	if strings.HasPrefix(path, "/") {
		return pointerTokens(path)
	}
	out := slices.Clone(scope)
	if path != "" {
		out = append(out, strings.Split(path, "/")...)
	}
	return out
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func pointerTokens(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	tokens := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, t := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}
	return tokens
}

func readPath(node any, tokens []string) any {
	for _, t := range tokens {
		switch x := node.(type) {
		case map[string]any:
			node = x[t]
		case []any:
			i, err := strconv.Atoi(t)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			node = x[i]
		default:
			return nil
		}
	}
	return node
}

// writePath sets the value at tokens, or removes it when set is false,
// making the objects on the way as needed.
func writePath(node any, tokens []string, value any, set bool) any {
	if len(tokens) == 0 {
		if !set {
			return nil
		}
		return value
	}
	t, rest := tokens[0], tokens[1:]
	if list, ok := node.([]any); ok {
		if i, err := strconv.Atoi(t); err == nil && i >= 0 && i < len(list) {
			list[i] = writePath(list[i], rest, value, set)
		}
		return list
	}
	obj, ok := node.(map[string]any)
	if !ok {
		obj = map[string]any{}
	}
	if !set && len(rest) == 0 {
		delete(obj, t)
		return obj
	}
	obj[t] = writePath(obj[t], rest, value, set)
	return obj
}

// mrkdwn writes a card's markdown text for Slack: escaped, so no text
// becomes a mention or a link, and with markdown's bold in Slack's form.
func mrkdwn(s string) string {
	return strings.ReplaceAll(escapeMarkdown(s), "**", "*")
}

// senderPrefix names who wrote a message at the start of its blocks.
func senderPrefix(sender string) string {
	switch sender {
	case "agent":
		return "🤖 *Agent:* "
	case "human":
		return "👤 *Human:* "
	}
	return fmt.Sprintf("👤 *%s:* ", sender)
}
