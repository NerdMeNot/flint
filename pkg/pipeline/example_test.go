package pipeline_test

import (
	"fmt"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// ExampleParse decodes a pipeline YAML document into the typed Pipeline.
func ExampleParse() {
	data := []byte(`
triggers:
  push:
    branches: [main]
steps:
  - name: test
    image: golang:1.24
    run: go test ./...
`)
	p, err := pipeline.Parse(data)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("%d step(s); first=%s\n", len(p.Steps), p.Steps[0].Name)
	// Output: 1 step(s); first=test
}

// ExampleValidate reports semantic problems (here, an invalid when: value) as
// structured issues rather than failing to parse.
func ExampleValidate() {
	data := []byte(`
triggers:
  push:
    branches: [main]
steps:
  - name: test
    run: go test ./...
    when: whenever
`)
	result := pipeline.Validate(data, pipeline.ValidateOptions{})
	fmt.Println("valid:", result.Valid())
	for _, e := range result.Errors() {
		fmt.Printf("%s: %s\n", e.Field, e.Message)
	}
	// Output:
	// valid: false
	// steps[0].when: invalid when value "whenever"
}
