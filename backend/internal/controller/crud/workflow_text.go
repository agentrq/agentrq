package crud

import (
	"fmt"
	"sort"
	"strings"
)

// The text representation of a workflow graph, e.g.
//
//	workflow: new_feature
//	event: code_changed
//	- agent:doc
//	  - event:doc_updated
//	- agent:blog
//	  - event:new_feature_added
//	    - agent:twitter
//	    - agent:hackernews
//
// The grammar alternates strictly: an event is consumed by agents, and each
// agent may emit one event, which is in turn consumed by agents. That
// alternation is the whole point of the format — there are no conditionals to
// express, so nesting depth alone carries the structure.
//
// Indentation is two spaces per level. Every list line is `- <kind>:<name>`,
// so a line's meaning never depends on its depth; a missing prefix is a
// reported error rather than a silent reinterpretation.

const textIndentWidth = 2

// TextNode is one parsed line of a workflow text document: an agent
// (workspace) or the event it emits, plus whatever it consumes in turn.
type TextNode struct {
	// Kind is "agent" or "event".
	Kind string
	// Name is the workspace name or event name.
	Name string
	// Line is the 1-based source line, so errors and editor markers can point
	// at the exact input the user typed.
	Line int
	// Children are the nodes nested directly under this one: agents under an
	// event, or the single emitted event under an agent.
	Children []*TextNode
}

// ParsedWorkflowText is a whole workflow document.
type ParsedWorkflowText struct {
	Name       string
	StartEvent string
	// Roots are the agents consuming StartEvent.
	Roots []*TextNode
}

// WorkflowTextError is a parse or validation failure carrying the source line,
// so the editor can mark the offending row instead of showing a bare message.
type WorkflowTextError struct {
	Line int
	Msg  string
}

func (e *WorkflowTextError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

func textErr(line int, format string, args ...any) *WorkflowTextError {
	return &WorkflowTextError{Line: line, Msg: fmt.Sprintf(format, args...)}
}

// ParseWorkflowText reads the text representation into a tree.
//
// It reports the first error it finds rather than accumulating: indentation
// mistakes cascade, so a list of downstream errors caused by one bad line is
// noise, not help.
func ParseWorkflowText(input string) (*ParsedWorkflowText, error) {
	lines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")

	parsed := &ParsedWorkflowText{}
	// stack[i] is the node currently open at depth i; a child at depth d
	// attaches to stack[d-1].
	var stack []*TextNode
	seenHeader := false

	for i, raw := range lines {
		lineNo := i + 1
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		if strings.Contains(raw, "\t") {
			return nil, textErr(lineNo, "use spaces, not tabs, for indentation")
		}

		trimmed := strings.TrimLeft(raw, " ")
		indent := len(raw) - len(trimmed)

		// Header lines: `workflow: <name>` and `event: <name>`.
		if !strings.HasPrefix(trimmed, "- ") {
			if indent != 0 {
				return nil, textErr(lineNo, "%q must not be indented", firstWord(trimmed))
			}
			key, value, ok := splitField(trimmed)
			if !ok {
				return nil, textErr(lineNo, "expected \"workflow: <name>\", \"event: <name>\", or a \"- agent:<name>\" list item")
			}
			switch key {
			case "workflow":
				if parsed.Name != "" {
					return nil, textErr(lineNo, "workflow name already set to %q", parsed.Name)
				}
				if value == "" {
					return nil, textErr(lineNo, "workflow name cannot be empty")
				}
				parsed.Name = value
			case "event":
				if parsed.StartEvent != "" {
					return nil, textErr(lineNo, "start event already set to %q; nest later events under an agent", parsed.StartEvent)
				}
				if value == "" {
					return nil, textErr(lineNo, "start event name cannot be empty")
				}
				parsed.StartEvent = value
				seenHeader = true
			default:
				return nil, textErr(lineNo, "unknown field %q (expected \"workflow\" or \"event\")", key)
			}
			continue
		}

		if !seenHeader {
			return nil, textErr(lineNo, "list items must come after a \"event: <name>\" line")
		}

		if indent%textIndentWidth != 0 {
			return nil, textErr(lineNo, "indent by %d spaces per level (got %d)", textIndentWidth, indent)
		}
		depth := indent / textIndentWidth

		if depth > len(stack) {
			return nil, textErr(lineNo, "unexpected indentation: jumped %d levels", depth-len(stack))
		}

		node, err := parseListItem(trimmed, lineNo)
		if err != nil {
			return nil, err
		}

		// Enforce the alternation. Depth 0 sits under the start event, so it
		// must be an agent; below that, a node's kind is fixed by its parent's.
		if depth == 0 {
			if node.Kind != "agent" {
				return nil, textErr(lineNo, "expected \"- agent:<name>\" under event %q, got %q", parsed.StartEvent, node.Kind)
			}
		} else {
			parent := stack[depth-1]
			switch parent.Kind {
			case "agent":
				if node.Kind != "event" {
					return nil, textErr(lineNo, "an agent may only emit an event; expected \"- event:<name>\" under agent %q", parent.Name)
				}
				if len(parent.Children) > 0 {
					return nil, textErr(lineNo, "agent %q already emits event %q; an agent emits at most one event", parent.Name, parent.Children[0].Name)
				}
			case "event":
				if node.Kind != "agent" {
					return nil, textErr(lineNo, "expected \"- agent:<name>\" under event %q, got %q", parent.Name, node.Kind)
				}
			}
		}

		// Truncate to this depth, then attach.
		stack = stack[:depth]
		if depth == 0 {
			parsed.Roots = append(parsed.Roots, node)
		} else {
			parent := stack[depth-1]
			parent.Children = append(parent.Children, node)
		}
		stack = append(stack, node)
	}

	if parsed.Name == "" {
		return nil, textErr(0, "missing \"workflow: <name>\" line")
	}
	if parsed.StartEvent == "" {
		return nil, textErr(0, "missing \"event: <name>\" line naming the start event")
	}
	return parsed, nil
}

// parseListItem reads `- agent:name` / `- event:name`.
func parseListItem(trimmed string, lineNo int) (*TextNode, error) {
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	if body == "" {
		return nil, textErr(lineNo, "list item is empty")
	}

	kind, name, ok := strings.Cut(body, ":")
	if !ok {
		return nil, textErr(lineNo, "expected \"agent:<name>\" or \"event:<name>\", got %q", body)
	}
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)

	if kind != "agent" && kind != "event" {
		return nil, textErr(lineNo, "unknown kind %q (expected \"agent\" or \"event\")", kind)
	}
	if name == "" {
		return nil, textErr(lineNo, "%s name cannot be empty", kind)
	}
	return &TextNode{Kind: kind, Name: name, Line: lineNo}, nil
}

// splitField reads a `key: value` header line.
func splitField(s string) (key string, value string, ok bool) {
	key, value, ok = strings.Cut(s, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), strings.TrimSpace(value), true
}

func firstWord(s string) string {
	if i := strings.IndexAny(s, " :"); i >= 0 {
		return s[:i]
	}
	return s
}

// ── Serialization ─────────────────────────────────────────────────────────────

// TextStep is one edge, named rather than keyed by ID, so the serializer stays
// independent of storage and can be unit-tested without a database.
type TextStep struct {
	EventName     string
	WorkspaceName string
	EmitEventName string
}

// RenderWorkflowText renders a workflow's steps back into the text format.
//
// It walks outward from the start event so the output mirrors how the graph
// actually runs. A graph containing a cycle would walk forever, so each branch
// stops if it revisits an event; setup-time validation should prevent that, but
// the renderer must not hang on data that slipped past it.
func RenderWorkflowText(name string, startEvent string, steps []TextStep) string {
	byEvent := make(map[string][]TextStep, len(steps))
	for _, s := range steps {
		byEvent[s.EventName] = append(byEvent[s.EventName], s)
	}
	// Stable output regardless of row order, so the text is diffable.
	for event := range byEvent {
		group := byEvent[event]
		sort.Slice(group, func(i, j int) bool {
			if group[i].WorkspaceName != group[j].WorkspaceName {
				return group[i].WorkspaceName < group[j].WorkspaceName
			}
			return group[i].EmitEventName < group[j].EmitEventName
		})
		byEvent[event] = group
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "workflow: %s\n", name)
	if startEvent == "" {
		return sb.String()
	}
	fmt.Fprintf(&sb, "event: %s\n", startEvent)

	var walk func(event string, depth int, seen map[string]bool)
	walk = func(event string, depth int, seen map[string]bool) {
		if seen[event] {
			return
		}
		seen[event] = true
		defer delete(seen, event)

		for _, step := range byEvent[event] {
			indent := strings.Repeat(" ", depth*textIndentWidth)
			fmt.Fprintf(&sb, "%s- agent:%s\n", indent, step.WorkspaceName)
			if step.EmitEventName == "" {
				continue
			}
			emitIndent := strings.Repeat(" ", (depth+1)*textIndentWidth)
			fmt.Fprintf(&sb, "%s- event:%s\n", emitIndent, step.EmitEventName)
			walk(step.EmitEventName, depth+2, seen)
		}
	}
	walk(startEvent, 0, map[string]bool{})

	return sb.String()
}

// FlattenWorkflowText turns a parsed tree back into a flat edge list.
//
// The tree can name the same event in more than one branch (a diamond re-join),
// which would otherwise yield duplicate edges, so identical
// event→workspace→emit triples are emitted once.
func FlattenWorkflowText(parsed *ParsedWorkflowText) []TextStep {
	var steps []TextStep
	seen := make(map[TextStep]bool)

	var walk func(eventName string, agents []*TextNode)
	walk = func(eventName string, agents []*TextNode) {
		for _, agent := range agents {
			step := TextStep{EventName: eventName, WorkspaceName: agent.Name}
			var emitted *TextNode
			if len(agent.Children) > 0 {
				emitted = agent.Children[0]
				step.EmitEventName = emitted.Name
			}
			if !seen[step] {
				seen[step] = true
				steps = append(steps, step)
			}
			if emitted != nil {
				walk(emitted.Name, emitted.Children)
			}
		}
	}
	walk(parsed.StartEvent, parsed.Roots)

	return steps
}
