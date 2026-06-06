package pipeline

import (
	"fmt"
	"sort"
	"strings"
)

// ExpandMatrix returns a new Pipeline with matrix steps expanded into
// individual steps — one per combination. Non-matrix steps are unchanged.
//
// For a step named "test" with matrix: {node: ["16","18"], os: ["ubuntu","alpine"]},
// this produces 4 steps: test[node=16,os=alpine], test[node=16,os=ubuntu],
// test[node=18,os=alpine], test[node=18,os=ubuntu].
//
// DependsOn references to expanded steps are rewritten so downstream steps
// depend on ALL variants (full barrier).
//
// Matrix expressions (${{ matrix.node }}) are interpolated in run, image,
// workingDir, if, env values, and cache key.
func ExpandMatrix(p *Pipeline) *Pipeline {
	if p == nil {
		return p
	}

	// Identify which original step names were expanded.
	expandedNames := make(map[string][]string) // original name → list of expanded names

	// First pass: expand matrix steps.
	var expanded []Step
	for _, s := range p.Steps {
		if len(s.Matrix) == 0 {
			expanded = append(expanded, s)
			continue
		}

		combos := CartesianProduct(s.Matrix)
		var variantNames []string

		for _, combo := range combos {
			variant := expandStep(s, combo)
			variantNames = append(variantNames, variant.Name)
			expanded = append(expanded, variant)
		}

		expandedNames[s.Name] = variantNames
	}

	// Second pass: rewrite DependsOn references.
	for i := range expanded {
		expanded[i].DependsOn = rewriteDependsOn(expanded[i].DependsOn, expandedNames)
	}

	result := *p
	result.Steps = expanded
	return &result
}

// expandStep creates a single expanded variant from a matrix step and one combination.
func expandStep(s Step, combo map[string]string) Step {
	variant := s
	variant.Name = s.Name + "[" + MatrixKey(combo) + "]"
	variant.Matrix = nil

	// Add matrix dimension values as env vars (MATRIX_NODE=16, etc.).
	if variant.Env == nil {
		variant.Env = make(map[string]string)
	}
	for k, v := range combo {
		variant.Env["MATRIX_"+strings.ToUpper(k)] = v
	}

	// Interpolate ${{ matrix.* }} expressions in string fields.
	// Copy the Commands slice to avoid mutating the original step.
	if len(s.Run.Commands) > 0 {
		cmds := make([]string, len(s.Run.Commands))
		copy(cmds, s.Run.Commands)
		variant.Run.Commands = cmds
		for i, cmd := range cmds {
			variant.Run.Commands[i] = interpolateMatrix(cmd, combo)
		}
	}
	variant.Image = interpolateMatrix(variant.Image, combo)
	variant.WorkingDir = interpolateMatrix(variant.WorkingDir, combo)
	variant.If = interpolateMatrix(variant.If, combo)

	// Interpolate env values.
	if len(variant.Env) > 0 {
		newEnv := make(map[string]string, len(variant.Env))
		for k, v := range variant.Env {
			newEnv[k] = interpolateMatrix(v, combo)
		}
		variant.Env = newEnv
	}

	// Interpolate cache key.
	if variant.Cache != nil {
		cacheCopy := *variant.Cache
		cacheCopy.Key = interpolateMatrix(cacheCopy.Key, combo)
		variant.Cache = &cacheCopy
	}

	return variant
}

// rewriteDependsOn replaces references to expanded step names with all their variants.
func rewriteDependsOn(deps []string, expandedNames map[string][]string) []string {
	if len(deps) == 0 || len(expandedNames) == 0 {
		return deps
	}

	var rewritten []string
	for _, dep := range deps {
		if variants, ok := expandedNames[dep]; ok {
			rewritten = append(rewritten, variants...)
		} else {
			rewritten = append(rewritten, dep)
		}
	}
	return rewritten
}

// interpolateMatrix replaces ${{ matrix.KEY }} with the value from the combination.
func interpolateMatrix(s string, combo map[string]string) string {
	if s == "" || !strings.Contains(s, "matrix.") {
		return s
	}
	for k, v := range combo {
		s = strings.ReplaceAll(s, "${{ matrix."+k+" }}", v)
		s = strings.ReplaceAll(s, "${{matrix."+k+"}}", v)
	}
	return s
}

// CartesianProduct computes all combinations of the matrix dimensions.
// Keys are sorted alphabetically for deterministic ordering.
func CartesianProduct(matrix map[string][]string) []map[string]string {
	if len(matrix) == 0 {
		return nil
	}

	// Sort keys for deterministic order.
	keys := make([]string, 0, len(matrix))
	for k := range matrix {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Start with one empty combination and multiply through each dimension.
	combos := []map[string]string{{}}
	for _, key := range keys {
		vals := matrix[key]
		var next []map[string]string
		for _, combo := range combos {
			for _, val := range vals {
				newCombo := make(map[string]string, len(combo)+1)
				for k, v := range combo {
					newCombo[k] = v
				}
				newCombo[key] = val
				next = append(next, newCombo)
			}
		}
		combos = next
	}

	return combos
}

// MatrixKey formats a combination as a deterministic key string.
// Keys are sorted: "node=16,os=ubuntu".
func MatrixKey(combo map[string]string) string {
	keys := make([]string, 0, len(combo))
	for k := range combo {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%s", k, combo[k])
	}
	return strings.Join(parts, ",")
}
