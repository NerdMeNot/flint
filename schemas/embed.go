// Package schemas embeds the published JSON Schemas (pipeline language, …) so
// the server can serve them for editor tooling without a docs deployment.
package schemas

import _ "embed"

// PipelineSchema is the JSON Schema for .flint/*.yaml pipeline files.
//
//go:embed flint-pipeline.schema.json
var PipelineSchema []byte
