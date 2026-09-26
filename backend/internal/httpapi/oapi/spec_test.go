package oapi

import (
	"context"
	"testing"
)

// Codegen only rejects what it cannot turn into Go types. The request
// validator needs a fully valid document (e.g. every pattern must compile).
func TestEmbeddedSpecIsValid(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("openapi/tenantry.v1.yaml is not a valid OpenAPI document: %v", err)
	}
}
