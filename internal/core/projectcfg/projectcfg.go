// Package projectcfg is the single mapper from a declared project configuration
// to the database upsert params. Both creation paths — the HTTP API
// (internal/platform/server) and the Project CRD reconciler
// — convert their own input into a projectcfg.Spec and
// call ToUpsertParams, so a project is byte-identical however it was created and
// the two paths can't drift.
package projectcfg

import (
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/core/db"
)

const (
	defaultColour = "#6366f1"
	defaultBranch = "main"
	defaultPath   = ".flint/"
)

// PipelineSource declares where a project's pipeline YAML lives. Type is "self"
// (in the project's own repo, the default) or "external" (a separate repo/ref).
type PipelineSource struct {
	Type string
	Path string
	Repo string // external only
	Ref  string // external only
}

// Spec is the backend-neutral project configuration. The CRD spec and the API
// request body both reduce to this.
type Spec struct {
	Repo           string // "owner/repo" (required)
	ForgeRef       string // forge connection display name (required)
	DisplayName    string
	Description    string
	Colour         string // hex; defaults to defaultColour
	Icon           string
	Workspace      string // workspace slug; empty → org default ("Unsorted")
	DefaultBranch  string // defaults to "main"
	PipelineSource *PipelineSource
}

// PipelineSourceJSON renders the pipeline_source column value, applying defaults.
// nil / "self" with no path → the default {"type":"self","path":".flint/"}.
func PipelineSourceJSON(ps *PipelineSource) []byte {
	out := struct {
		Type string `json:"type"`
		Path string `json:"path,omitempty"`
		Repo string `json:"repo,omitempty"`
		Ref  string `json:"ref,omitempty"`
	}{Type: "self", Path: defaultPath}

	if ps != nil {
		if ps.Type == "external" {
			out.Type = "external"
			out.Repo = ps.Repo
			out.Ref = orDefault(ps.Ref, defaultBranch)
			out.Path = orDefault(ps.Path, defaultPath)
		} else if ps.Path != "" {
			out.Path = ps.Path
		}
	}
	b, _ := json.Marshal(out)
	return b
}

// ToUpsertParams maps a Spec to db.UpsertProjectParams, applying defaults. Tags
// and the forge webhook id are intentionally NOT set here — tags are UI-managed,
// and the webhook id is recorded separately after provisioning.
func ToUpsertParams(s Spec) db.UpsertProjectParams {
	return db.UpsertProjectParams{
		RepoPath:       s.Repo,
		RepoUrl:        "https://github.com/" + s.Repo,
		DisplayName:    nilIfEmpty(s.DisplayName),
		Description:    nilIfEmpty(s.Description),
		Colour:         orDefault(s.Colour, defaultColour),
		Icon:           nilIfEmpty(s.Icon),
		DefaultBranch:  orDefault(s.DefaultBranch, defaultBranch),
		PipelineSource: PipelineSourceJSON(s.PipelineSource),
		ForgeRef:       s.ForgeRef,
		Workspace:      s.Workspace,
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
