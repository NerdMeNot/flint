package pipeline

// export.go — validation building blocks shared with product dialects
// (internal/products/ci). The jobs→steps CI dialect reuses this package's
// error machinery (ValidationIssue, codes, did-you-mean) rather than growing
// its own; these thin wrappers expose the internals it needs.

// CheckExpr type-checks a ${{ }} expression against the given context without
// evaluating it. Returns nil when the expression compiles. This is the
// validation-time counterpart of EvalCondition — checking against the SAME
// context shape the engine provides at runtime is what keeps "validates clean,
// fails at runtime" from happening.
func CheckExpr(expression string, ctx ExprContext) error {
	return compileExpr(expression, ctx)
}

// FindClosest returns the candidate with the smallest Levenshtein distance to
// target (max distance 3), or "" when nothing is close — the "did you mean"
// engine.
func FindClosest(target string, candidates map[string]bool) string {
	return findClosest(target, candidates)
}

// EnumSuggestion builds a suggestion for an invalid enum value: a did-you-mean
// when the input is a near-miss, plus the list of valid values.
func EnumSuggestion(got string, valid []string) string {
	return enumSuggestion(got, valid)
}

// IsValidApprover reports whether a gate approver has a valid format:
// role:slug, team:slug, or an email address.
func IsValidApprover(approver string) bool {
	return isValidApprover(approver)
}

// IsValidEnvName reports whether a string is a valid environment variable name.
func IsValidEnvName(name string) bool {
	return isValidEnvName(name)
}

// ValidShells lists the shells a step may select.
func ValidShells() []string {
	return validShells
}
