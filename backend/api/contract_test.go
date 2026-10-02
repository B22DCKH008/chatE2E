package api_test

import (
	"context"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestOpenAPIContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile("openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			status, _ := operation.Extensions["x-status"].(string)
			if status != "planned" && status != "implemented" {
				t.Errorf("%s %s must declare implementation status", method, path)
			}
		}
	}
}
