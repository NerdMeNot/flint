package ci

import (
	"fmt"
	"sort"
	"strings"
)

// maxJobs and maxMatrixCombos bound compile fan-out for untrusted webhook input.
const (
	maxJobs         = 200
	maxMatrixCombos = 256
)

// Validate checks the structural invariants the compiler and engine assume.
// It does not resolve modules (use:/extends:) — that happens before Validate on
// the merged pipeline.
//
// POLICY: parse it → run it, or refuse it loudly. A feature the schema accepts
// but the engine does not enforce yet is a validation ERROR, never a silent
// no-op — an author must never discover in production that a field they set
// was decorative.
//
// This is a thin wrapper over ValidateDetailed (validate_rich.go), which
// collects every issue with error codes and did-you-mean suggestions; here the
// error-severity issues are folded into a single error for the webhook/API
// paths. The CLI calls ValidateDetailed directly for full reporting.
func (p *Pipeline) Validate() error {
	r := p.ValidateDetailed()
	errs := r.Errors()
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return fmt.Errorf("ci: %s", errs[0].String())
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.String()
	}
	return fmt.Errorf("ci: %d validation errors: %s", len(errs), strings.Join(msgs, "; "))
}

func sortedJobNames(jobs map[string]Job) []string {
	names := make([]string, 0, len(jobs))
	for n := range jobs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}
