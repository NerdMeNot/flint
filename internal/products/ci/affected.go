package ci

import (
	"github.com/bmatcuk/doublestar/v4"
	"github.com/rs/zerolog/log"
)

// AffectedJobs computes which jobs a change touches: every job whose declared
// input files match a changed path, plus every transitive dependent of those
// jobs. This is the "affected mode" core — run only what a diff can actually
// influence, the payoff of declared inputs (D1).
//
// Safety is conservative by construction: a job that declares NO input files
// can't be proven unaffected, so it is always included — you opt INTO being
// skippable by declaring your inputs, never out of running by forgetting to.
// Likewise a dependent of an affected job is always affected (its upstream may
// now produce different outputs), so the result is closed under the needs graph.
//
// Pure: it reads only its arguments (jobs + the changed-file list from a git
// diff), so the graph reasoning is exhaustively testable without a repo.
func AffectedJobs(jobs map[string]Job, changedFiles []string) map[string]bool {
	// 1. Directly affected.
	direct := make(map[string]bool, len(jobs))
	for name, job := range jobs {
		if job.Inputs.Empty() || len(job.Inputs.Files) == 0 {
			direct[name] = true // no declared file inputs → can't prove unaffected
			continue
		}
		for _, f := range changedFiles {
			if matchesAnyGlob(job.Inputs.Files, f) {
				direct[name] = true
				break
			}
		}
	}

	// 2. Reverse-dependency closure: if X is affected and Y needs X, Y is affected.
	dependents := map[string][]string{}
	for name, job := range jobs {
		for _, dep := range job.Needs {
			dependents[dep] = append(dependents[dep], name)
		}
	}
	affected := make(map[string]bool, len(jobs))
	var mark func(name string)
	mark = func(name string) {
		if affected[name] {
			return
		}
		affected[name] = true
		for _, d := range dependents[name] {
			mark(d)
		}
	}
	for name := range direct {
		if direct[name] {
			mark(name)
		}
	}
	return affected
}

// matchesAnyGlob reports whether path matches any of the globs (doublestar, so
// **/*.go spans directories).
func matchesAnyGlob(globs []string, path string) bool {
	for _, g := range globs {
		ok, err := doublestar.Match(g, path)
		if err != nil {
			// A malformed glob never matches, and "no match" here means the job is
			// not affected — so a bad pattern silently drops the job and the run
			// still goes green. Validation rejects these before a run starts; this
			// is the backstop for a pipeline that reached execution anyway.
			log.Warn().Str("glob", g).Err(err).
				Msg("ci: invalid input file glob — treated as no match")
			continue
		}
		if ok {
			return true
		}
	}
	return false
}
