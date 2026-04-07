# Pipeline Framework — Remaining Work for Production Grade

## Priority 1: Correctness (Must Fix)

These are bugs or missing validations that would cause incorrect behavior or confusing failures.

### P1.1 — Template input substitution in all step fields
**Files:** `pkg/pipeline/resolve.go`
**Current:** Input substitution (`${{ inputs.NAME }}`) only happens in `step.Run` and `step.Env` for file templates.
**Missing:** `step.Image`, `step.WorkingDir`, `step.Cache.Key`, `step.Services[].Image`, `step.Services[].Env`
**Example:** `image: node${{ inputs.node_version }}` in a file template won't resolve.
**Fix:** Add substitution calls for all string fields in `resolveFileTemplate()`.

### P1.2 — Template input type validation
**Files:** `pkg/pipeline/resolve.go`
**Current:** `resolveInputs()` checks required/optional but ignores the declared `Type` field.
**Missing:** Boolean inputs should only accept `"true"`/`"false"`. Choice inputs should only accept values from `Options`.
**Fix:** Add type-specific validation in `resolveInputs()`.

### P1.3 — StepTemplate CRD missing Options field
**Files:** `internal/crd/v1/steptemplate_types.go`
**Current:** `StepTemplateInput` has Name, Type, Required, Default, Description — but no `Options`.
**Missing:** Choice-type inputs need an `Options []string` field.
**Fix:** Add field to CRD type + update deepcopy.

### P1.4 — Empty file template validation
**Files:** `pkg/pipeline/resolve.go`
**Current:** If a file template resolves to zero steps, it silently becomes a no-op.
**Fix:** Return error in `resolveFileTemplate()` when `tmpl.Steps` is empty.

### P1.5 — Cross-repo ref format validation
**Files:** `pkg/pipeline/resolve.go`
**Current:** `ParseUseRef()` doesn't validate that org/repo/file/ref are all non-empty.
**Missing:** `"//path@"` or `"org/@v1"` would parse as cross-repo without error.
**Fix:** Add non-empty checks in `ParseUseRef()` for cross-repo refs.

### P1.6 — Duplicate promotion `from` detection
**Files:** `pkg/pipeline/validate.go`
**Current:** Multiple promotions with the same `from` environment are allowed.
**Spec says:** "Multiple promotion triggers are valid only if they have different from environments."
**Fix:** Add uniqueness check for `promotion[].from` in `validateTriggers()`.

### P1.7 — Gate approver format tightening
**Files:** `pkg/pipeline/validate.go`
**Current:** `isValidApprover()` allows `"role:"` (empty slug) and `"@"` (empty email).
**Fix:** Require non-empty slug after `role:`/`team:` prefix. Require text before and after `@` for email.

---

## Priority 2: Validation Robustness

Additional validations that catch user mistakes early with helpful messages.

### P2.1 — Trigger compatibility validation
**Files:** `pkg/pipeline/validate.go`
**Current:** No validation of trigger combinations.
**Missing:**
- Duplicate triggers of same type (except promotion) should error
- Multiple promotions with same `from` should error
- Warning for unusual combinations (PR + promotion)
**Fix:** Add `validateTriggerCompatibility()` function.

### P2.2 — Environment variable name validation
**Files:** `pkg/pipeline/validate.go`
**Current:** Env keys can be anything — hyphens, spaces, lowercase.
**Fix:** Warn (not error) if env key doesn't match `[A-Za-z_][A-Za-z0-9_]*` pattern.

### P2.3 — Reserved name conflicts
**Files:** `pkg/pipeline/validate.go`
**Current:** Manual input names and matrix dimension keys can shadow built-in context variables (e.g., `inputs.branch` would shadow `branch`).
**Fix:** Warn if manual input or matrix key name matches a reserved context variable name.

### P2.4 — Cache key expression validation
**Files:** `pkg/pipeline/validate.go`
**Current:** `cache.key` is not validated as a valid expression.
**Fix:** Attempt to compile `cache.key` as an expression (same approach as `if:` validation).

### P2.5 — Artifact dependency validation
**Files:** `pkg/pipeline/validate.go`
**Current:** `inputs[].from` checks that the step exists, but doesn't verify that step has `outputs`.
**Fix:** Warning when `inputs[].from` references a step that declares no outputs.

### P2.6 — Artifact path format validation
**Files:** `pkg/pipeline/validate.go`
**Current:** Artifact paths (`inputs[].path`, `outputs[].path`) are not validated.
**Fix:** Validate paths are absolute (start with `/`) and don't contain `..`.

### P2.7 — Step name length limit
**Files:** `pkg/pipeline/parser.go`
**Current:** No length limit on step names.
**Fix:** Add max length (e.g., 128 chars) for step names.

### P2.8 — Environment simulation for matrix steps
**Files:** `pkg/pipeline/env.go`
**Current:** `SimulateEnv()` shows matrix steps as a single entry.
**Fix:** Add `MatrixCombinations int` to `StepStatus` so the preview shows "test (6 combinations)".

---

## Priority 3: API Quality

Clean up the public API surface, add documentation, improve error messages.

### P3.1 — Comprehensive godoc
**Files:** All `pkg/pipeline/*.go`
**Current:** Minimal documentation on types and functions.
**Fix:** Add detailed godoc for every exported type, function, and method. Include usage examples.

### P3.2 — Error codes
**Files:** `pkg/pipeline/errors.go`
**Current:** Errors are free-form strings. Clients can't distinguish error types programmatically.
**Fix:** Add error code constants (e.g., `ErrCodeMissingTrigger`, `ErrCodeDuplicateStep`, `ErrCodeInvalidExpression`). Add `Code string` field to `ValidationIssue`.

### P3.3 — Suggestions everywhere
**Files:** `pkg/pipeline/validate.go`, `pkg/pipeline/parser.go`
**Current:** Suggestions only on `dependsOn` typos and environment typos.
**Missing:** Suggestions for invalid `when` values, invalid `shell` values, invalid `type` values.
**Fix:** Add suggestion strings to all enum-like validation errors (already done for some, extend to all).

### P3.4 — Webhook context variables
**Files:** `pkg/pipeline/expr.go`
**Current:** `RuntimeContextOpts` and `BuildRuntimeContext()` don't include webhook body/headers.
**Fix:** Add `WebhookBody map[string]any` and `WebhookHeaders map[string]string` to `RuntimeContextOpts`. Populate `webhook` in context.

### P3.5 — Helper functions unexported
**Files:** `pkg/pipeline/validate.go`, `pkg/pipeline/env.go`
**Current:** Some internal helpers could accidentally be used by external consumers.
**Fix:** Verify all helpers that should be internal are unexported. `findClosest`, `levenshtein`, `isValidApprover`, `toSet`, `parseDuration`, `formatList`, `joinStrings` are already unexported — good.

---

## Priority 4: Test Coverage

Comprehensive tests for edge cases and all validation paths.

### P4.1 — Parser edge case tests
**Files:** `pkg/pipeline/parser_test.go`
**Missing tests:**
- Pipeline with all 7 trigger types simultaneously
- Step with every field set
- Maximum matrix combinations (256)
- Very long step names
- Unicode in step names
- Empty strings in various fields

### P4.2 — Validator edge case tests
**Files:** `pkg/pipeline/validate_test.go`
**Missing tests:**
- All trigger compatibility rules
- Duplicate promotion `from`
- Reserved name conflicts
- Nested step validation in file templates
- Expression validation with various invalid syntaxes
- Cache key expression validation

### P4.3 — Template resolution edge case tests
**Files:** `pkg/pipeline/resolve_test.go`
**Missing tests:**
- Template with all input types (string, boolean, choice)
- Template input type mismatch
- Empty file template
- Deeply nested use: resolution
- Cross-repo ref with special characters
- Template where run command has multiple input expressions

### P4.4 — Environment simulation edge case tests
**Files:** `pkg/pipeline/env_test.go`
**Missing tests:**
- Pipeline with no environments at all
- Pipeline with only PR trigger (non-env-aware)
- Matrix step in simulation
- All trigger types in simulation
- Step with `when: always` in filtered environment

### P4.5 — DAG edge case tests
**Files:** `pkg/pipeline/dag_test.go`
**Missing tests:**
- Large DAG (50+ steps)
- DAG with all steps filtered out by environment
- DAG with matrix steps
- Single-step pipeline

---

## Priority 5: Production Hardening

Items that matter at scale or under adversarial input.

### P5.1 — Field size limits
**Files:** `pkg/pipeline/parser.go`
**Current:** Only overall YAML size is limited (1 MB).
**Fix:** Add limits for individual fields: step.run (64 KB), env values (32 KB), number of steps (100), number of env vars per step (50), number of services per step (10).

### P5.2 — YAML anchors and aliases handling
**Files:** `pkg/pipeline/parser.go`
**Current:** YAML anchors (`&name`) and aliases (`*name`) are handled by the YAML parser transparently.
**Risk:** Billion laughs attack via YAML anchors causing exponential expansion.
**Fix:** Limit YAML node depth or total expanded size.

### P5.3 — Expression evaluation timeout
**Files:** `pkg/pipeline/expr.go`
**Current:** No timeout on expression evaluation.
**Risk:** Pathological expressions could hang the parser.
**Fix:** Add context with timeout to expression evaluation.

### P5.4 — Template resolution depth limit
**Files:** `pkg/pipeline/resolve.go`
**Current:** No limit on template resolution recursion.
**Risk:** A template that references another template that references the first creates infinite recursion.
**Fix:** Add max resolution depth (e.g., 10 levels).

---

## Summary

| Priority | Items | Effort | Timeline |
|----------|-------|--------|----------|
| P1 — Correctness | 7 items | 2-3 hours | Before any release |
| P2 — Validation | 8 items | 3-4 hours | Before v0.1 |
| P3 — API Quality | 5 items | 2-3 hours | Before v0.1 |
| P4 — Test Coverage | 5 item groups | 3-4 hours | Before v0.1 |
| P5 — Hardening | 4 items | 2-3 hours | Before production use |
| **Total** | **29 items** | **~15 hours** | |
