package httpapi

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type specOp struct {
	OperationID string                `yaml:"operationId"`
	Summary     string                `yaml:"summary"`
	Security    []map[string][]string `yaml:"security"`
	Responses   map[string]yaml.Node  `yaml:"responses"`
}

type specDoc struct {
	OpenAPI string `yaml:"openapi"`
	// every key under a path must be an operation (get, post, ...)
	Paths map[string]map[string]specOp `yaml:"paths"`
}

func loadSpec(t *testing.T) specDoc {
	t.Helper()
	var s specDoc
	require.NoError(t, yaml.Unmarshal(openAPISpec, &s))
	require.True(t, strings.HasPrefix(s.OpenAPI, "3."), "openapi version: %q", s.OpenAPI)
	return s
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var ginParam = regexp.MustCompile(`:([A-Za-z_]+)`)

// every real route is documented, and every documented operation is a real route
func TestOpenAPIMatchesRoutes(t *testing.T) {
	spec := loadSpec(t)

	documented := map[string]bool{}
	for path, ops := range spec.Paths {
		for method := range ops {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}

	registered := map[string]bool{}
	for _, ri := range newRawRouter().(*gin.Engine).Routes() {
		if ri.Path == "/docs" || ri.Path == "/openapi.yaml" {
			continue
		}
		registered[ri.Method+" "+ginParam.ReplaceAllString(ri.Path, "{$1}")] = true
	}

	assert.Equal(t, sortedKeys(registered), sortedKeys(documented),
		"routes and openapi.yaml disagree: add or remove the operation in the spec")
}

func TestOpenAPIOperationsAreComplete(t *testing.T) {
	spec := loadSpec(t)
	public := map[string]bool{"/healthz": true, "/readyz": true, "/auth/register": true, "/auth/login": true}
	unlimited := map[string]bool{"/healthz": true, "/readyz": true}

	seen := map[string]string{}
	for path, ops := range spec.Paths {
		for method, op := range ops {
			name := strings.ToUpper(method) + " " + path

			assert.NotEmpty(t, op.OperationID, name)
			assert.NotEmpty(t, op.Summary, name)
			assert.NotEmpty(t, op.Responses, name)
			if other, dup := seen[op.OperationID]; dup {
				t.Errorf("operationId %q is used by both %s and %s", op.OperationID, other, name)
			}
			seen[op.OperationID] = name

			if public[path] {
				assert.Empty(t, op.Security, "%s is public and must not ask for a token", name)
			} else {
				assert.NotEmpty(t, op.Security, "%s must declare security", name)
				assert.Contains(t, op.Responses, "401", name)
			}
			if !unlimited[path] {
				assert.Contains(t, op.Responses, "429", name)
			}
		}
	}
}

// every $ref points at something that exists
func TestOpenAPIReferencesResolve(t *testing.T) {
	var spec struct {
		Components map[string]map[string]yaml.Node `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(openAPISpec, &spec))

	refs := regexp.MustCompile(`\$ref:\s*'#/components/([A-Za-z]+)/([A-Za-z0-9]+)'`).
		FindAllSubmatch(openAPISpec, -1)
	require.NotEmpty(t, refs)

	for _, m := range refs {
		kind, name := string(m[1]), string(m[2])
		assert.Contains(t, spec.Components[kind], name, "unresolved $ref to %s/%s", kind, name)
	}
}

func TestDocsAreServed(t *testing.T) {
	h := newRawRouter()

	rec := do(h, "GET", "/openapi.yaml", "")
	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "yaml")
	assert.Contains(t, rec.Body.String(), "openapi: 3.0.3")

	rec = do(h, "GET", "/docs", "")
	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), "/openapi.yaml")
}
