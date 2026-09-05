package api

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"
)

// The specification drifted once already: it described an attachment route
// that had gained a path segment, and schemas in snake_case against an API
// that had moved to camelCase. Nothing caught either, because nothing compared
// the document to the server.
//
// These tests are that comparison. They read the routes out of a Fiber app the
// registration functions have populated — the same functions the real server
// calls — so a route added without a matching entry in openapi.yaml fails here
// rather than being discovered by whoever generated a client from it.

// specPath is openapi.yaml, relative to this package.
const specPath = "../../../openapi.yaml"

// muxRoutes are the two paths the standard-library mux in app.go serves.
//
// They cannot come from the Fiber app because they never reach it: the mux
// takes exact path matches first, which is how an endless SSE stream avoids
// Fiber's response buffering. Listed here so the spec still has to document
// them.
var muxRoutes = []string{
	"GET /events/stream",
	"GET /workspaces/{id}/events",
}

// registeredRoutes builds the route table the way the server does.
//
// The handler is bare on purpose. Every register function only closes over
// `h`; nothing it touches is dereferenced until a request arrives, and no
// request does. That keeps this test free of the dependency graph the real
// constructor needs, which is what lets it run as a plain unit test.
func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()

	app := fiber.New()
	h := &handler{router: app.Group("/api/v1")}

	h.registerPublicAuthRoutes()
	h.registerProtectedAuthRoutes()
	if err := h.registerWorkspaceRoutes(); err != nil {
		t.Fatalf("registerWorkspaceRoutes: %v", err)
	}
	if err := h.registerTaskRoutes(); err != nil {
		t.Fatalf("registerTaskRoutes: %v", err)
	}
	h.registerEventRoutes()
	h.registerWorkflowRoutes()
	if err := h.registerTelemetryRoutes(); err != nil {
		t.Fatalf("registerTelemetryRoutes: %v", err)
	}
	h.registerPushRoutes(nil)

	routes := map[string]bool{}
	for _, r := range app.GetRoutes(true) {
		// Fiber records HEAD alongside every GET, and registers a catch-all
		// USE entry the spec has no business describing.
		if r.Method != fiber.MethodGet && r.Method != fiber.MethodPost &&
			r.Method != fiber.MethodPut && r.Method != fiber.MethodPatch &&
			r.Method != fiber.MethodDelete {
			continue
		}
		path := strings.TrimPrefix(r.Path, "/api/v1")
		if path == "" || path == "/" {
			continue
		}
		routes[r.Method+" "+toOpenAPIPath(path)] = true
	}
	for _, r := range muxRoutes {
		routes[r] = true
	}
	return routes
}

// toOpenAPIPath rewrites Fiber's `:name` parameters as OpenAPI's `{name}`.
var paramPattern = regexp.MustCompile(`:([A-Za-z0-9_]+)`)

func toOpenAPIPath(path string) string {
	return paramPattern.ReplaceAllString(path, "{$1}")
}

// specDocument is only the part of openapi.yaml these tests read.
type specDocument struct {
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

func loadSpec(t *testing.T) specDocument {
	t.Helper()

	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Clean(specPath), err)
	}
	var doc specDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("openapi.yaml declares no paths")
	}
	return doc
}

func specOperations(t *testing.T) map[string]bool {
	t.Helper()

	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	ops := map[string]bool{}
	for path, item := range loadSpec(t).Paths {
		for key := range item {
			if methods[key] {
				ops[strings.ToUpper(key)+" "+path] = true
			}
		}
	}
	return ops
}

func TestOpenAPIDocumentsEveryRoute(t *testing.T) {
	routes := registeredRoutes(t)
	ops := specOperations(t)

	var undocumented []string
	for route := range routes {
		if !ops[route] {
			undocumented = append(undocumented, route)
		}
	}
	sort.Strings(undocumented)

	if len(undocumented) > 0 {
		t.Errorf("routes the server serves but openapi.yaml does not describe:\n  %s\n\n"+
			"Add them to backend/openapi.yaml.", strings.Join(undocumented, "\n  "))
	}
}

func TestOpenAPIDescribesNoRouteThatIsGone(t *testing.T) {
	routes := registeredRoutes(t)
	ops := specOperations(t)

	var phantom []string
	for op := range ops {
		if !routes[op] {
			phantom = append(phantom, op)
		}
	}
	sort.Strings(phantom)

	if len(phantom) > 0 {
		t.Errorf("openapi.yaml describes routes the server does not serve:\n  %s\n\n"+
			"Remove them from backend/openapi.yaml, or fix the path.", strings.Join(phantom, "\n  "))
	}
}

// The convention is camelCase everywhere in the API surface, and the previous
// specification broke it — `created_at` where the server sends `createdAt`.
// A generated client would have carried that straight into someone's codebase.
func TestOpenAPISchemaPropertiesAreCamelCase(t *testing.T) {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]yaml.Node `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	if len(doc.Components.Schemas) == 0 {
		t.Fatal("openapi.yaml declares no component schemas")
	}

	for name, schema := range doc.Components.Schemas {
		for property := range schema.Properties {
			if strings.Contains(property, "_") {
				t.Errorf("%s.%s is snake_case; the API surface is camelCase", name, property)
			}
		}
	}
}
