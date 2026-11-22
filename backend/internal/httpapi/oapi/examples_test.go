package oapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// Contract test (IMPLEMENTATION-PLAN §3.6): the examples in openapi/examples
// fit the document, and every operation has one of a successful response.
const (
	examplesDir = "../../../openapi/examples"
	// The source document, not GetSwagger: codegen renames the operation ids
	// of the embedded copy to Go names.
	specFile = "../../../openapi/tenantry.v1.yaml"
)

type example struct {
	Request   any            `json:"request"`
	Responses map[string]any `json:"responses"`
}

func TestExamplesFitTheSpec(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromFile(specFile)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]*openapi3.Operation{}
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			ops[op.OperationID] = op
		}
	}

	files, err := filepath.Glob(filepath.Join(examplesDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(name, func(t *testing.T) {
			op, ok := ops[name]
			if !ok {
				t.Fatalf("no operation %q in the document", name)
			}
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var ex example
			if err := json.Unmarshal(raw, &ex); err != nil {
				t.Fatal(err)
			}

			if ex.Request != nil {
				if op.RequestBody == nil {
					t.Fatal("an example request for an operation without a body")
				}
				check(t, "request", op.RequestBody.Value.Content.Get("application/json"), ex.Request)
			} else if op.RequestBody != nil && op.RequestBody.Value.Required {
				t.Error("no example request for an operation that requires a body")
			}

			for status, body := range ex.Responses {
				resp := op.Responses.Status(atoi(t, status))
				if resp == nil {
					t.Errorf("response %s is not documented", status)
					continue
				}
				if len(resp.Value.Content) == 0 {
					// A response without a body (204): its example is null.
					if body != nil {
						t.Errorf("response %s has no body, its example must be null", status)
					}
				} else {
					check(t, "response "+status, resp.Value.Content.Get("application/json"), body)
				}
				if status[0] == '2' {
					covered[name] = true
				}
			}
		})
	}

	var missing []string
	for name := range ops {
		if !covered[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("operations without an example of a successful response in openapi/examples: %v", missing)
	}
}

// check validates value against the schema of media, as the request
// validator does (unknown fields, patterns, enums, required fields).
func check(t *testing.T, what string, media *openapi3.MediaType, value any) {
	t.Helper()
	if media == nil || media.Schema == nil {
		t.Errorf("%s: no application/json schema in the document", what)
		return
	}
	// Round-trip so numbers are float64, as JSON decoding gives the validator.
	b, _ := json.Marshal(value)
	var v any
	_ = json.Unmarshal(b, &v)
	if err := media.Schema.Value.VisitJSON(v, openapi3.MultiErrors()); err != nil {
		t.Errorf("%s does not fit the schema: %v", what, err)
	}
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			t.Fatalf("status %q is not a number", s)
		}
		n = n*10 + int(r-'0')
	}
	return n
}
