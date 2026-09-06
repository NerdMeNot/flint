package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// ScopeAny is the request-side sentinel meaning "in any scope the subject
// holds" — used by list routes, which act on no single workspace or
// environment and instead filter their results down to what the subject can
// see.
//
// It is deliberately distinct from "*". On the request side "*" means the
// request is genuinely global (an admin object), and only a platform-wide
// policy satisfies it; a workspace-scoped grant must not. That is correct for
// admin routes but wrong for lists, where it made every scope-restricted user
// 403 on GET /projects unless they hand-passed ?workspace= — which is exactly
// the pressure that produced the query-param override this package used to
// honour. ScopeAny asks the right question instead: does this subject hold the
// permission somewhere?
//
// It is not a valid policy value; PermittedWorkspaces panics on nothing and
// simply never matches if one is ever written.
const ScopeAny = "~any"

// flintRBACModel is the Casbin model for Flint's RBAC v2.
//
// 6-field model:
//   - sub: user email, "team:<slug>", or "apikey:<id>"
//   - org: org id the grant belongs to ("*" = all orgs; system grants only)
//   - ws:  workspace slug ("*" = all workspaces)
//   - env: environment name ("*" = all environments)
//   - obj: resource type (admin: workspace,team,... / CI: project,run,gate)
//   - act: action (admin: read,manage / CI: read,write,trigger,cancel,approve,reject)
//
// org is a real dimension rather than a prefix on sub because roles are
// per-org rows while subjects are bare emails: without it a grant made in one
// org authorized the same email in every other one. Single-org installs never
// notice; the day org creation ships, they would.
//
// Grouping is 2-field (no domain): g = user, group.
// Team membership creates g rules; org/workspace/env scope is on policies, not groups.
const flintRBACModel = `
[request_definition]
r = sub, org, ws, env, obj, act

[policy_definition]
p = sub, org, ws, env, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (p.org == "*" || p.org == r.org) && (r.ws == "~any" || p.ws == "*" || p.ws == r.ws) && (r.env == "~any" || p.env == "*" || p.env == r.env) && (p.obj == "*" || p.obj == r.obj) && (p.act == "*" || p.act == r.act)
`

// NewMemoryEnforcer creates a Casbin enforcer with the Flint RBAC model and no
// persistence adapter. Policies live only in memory — used in tests and for
// ephemeral, in-process policy evaluation.
func NewMemoryEnforcer() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(flintRBACModel)
	if err != nil {
		return nil, fmt.Errorf("loading casbin model: %w", err)
	}
	return casbin.NewEnforcer(m)
}

// NewEnforcer creates a Casbin enforcer with the Flint RBAC model and pgx adapter.
//
// Autosave is on, so every AddPolicy/RemoveFilteredPolicy writes through to
// casbin_rules incrementally. Callers must NOT then call SavePolicy(): that
// method is a DELETE-everything-and-reinsert-from-memory full sync, and with
// more than one replica the replica doing the save has no idea what its peers
// have written since boot. A login on replica A would silently delete a grant
// replica B had just made. Incremental writes plus the watcher below keep the
// replicas converged instead.
func NewEnforcer(ctx context.Context, pool db.Pool) (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(flintRBACModel)
	if err != nil {
		return nil, fmt.Errorf("loading casbin model: %w", err)
	}

	a := NewPgxAdapter(pool)

	e, err := casbin.NewEnforcer(m, a)
	if err != nil {
		return nil, fmt.Errorf("creating casbin enforcer: %w", err)
	}

	e.EnableAutoSave(true)

	w, err := NewPgWatcher(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("creating casbin watcher: %w", err)
	}
	if err := e.SetWatcher(w); err != nil {
		return nil, fmt.Errorf("attaching casbin watcher: %w", err)
	}
	// SetWatcher installs a default callback that calls LoadPolicy; be explicit
	// about it so a future casbin change can't quietly turn propagation off.
	if err := w.SetUpdateCallback(func(string) {
		if err := e.LoadPolicy(); err != nil {
			log.Error().Err(err).Msg("casbin: reloading policy after peer update failed")
		}
	}); err != nil {
		return nil, fmt.Errorf("setting casbin watcher callback: %w", err)
	}

	return e, nil
}

// PgxAdapter implements persist.Adapter for pgx v5.
// It stores Casbin policies in the casbin_rules table.
type PgxAdapter struct {
	pool db.Pool
}

// Verify interface compliance.
var _ persist.Adapter = (*PgxAdapter)(nil)

// NewPgxAdapter creates a new adapter backed by a pgx connection pool.
func NewPgxAdapter(pool db.Pool) *PgxAdapter {
	return &PgxAdapter{pool: pool}
}

// LoadPolicy loads all policies from the database into the model.
func (a *PgxAdapter) LoadPolicy(m model.Model) error {
	rows, err := a.pool.Query(context.Background(),
		"SELECT ptype, v0, v1, v2, v3, v4, v5 FROM casbin_rules")
	if err != nil {
		return fmt.Errorf("querying casbin_rules: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ptype, v0, v1, v2, v3, v4, v5 string
		if err := rows.Scan(&ptype, &v0, &v1, &v2, &v3, &v4, &v5); err != nil {
			return fmt.Errorf("scanning casbin rule: %w", err)
		}

		rule := filterEmpty([]string{v0, v1, v2, v3, v4, v5})
		line := ptype + ", " + strings.Join(rule, ", ")
		// A rule that fails to parse would otherwise vanish from the policy set
		// with no trace — an access-control rule silently not enforced.
		if err := persist.LoadPolicyLine(line, m); err != nil {
			return fmt.Errorf("loading casbin rule %q: %w", line, err)
		}
	}

	return rows.Err()
}

// SavePolicy saves all policies from the model to the database.
// It replaces all existing rules (full sync).
func (a *PgxAdapter) SavePolicy(m model.Model) error {
	ctx := context.Background()

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, "DELETE FROM casbin_rules"); err != nil {
		return fmt.Errorf("clearing casbin_rules: %w", err)
	}

	for ptype, ast := range m["p"] {
		for _, rule := range ast.Policy {
			vals := padRule(rule)
			if _, err := tx.Exec(ctx,
				"INSERT INTO casbin_rules (ptype, v0, v1, v2, v3, v4, v5) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING",
				ptype, vals[0], vals[1], vals[2], vals[3], vals[4], vals[5]); err != nil {
				return fmt.Errorf("inserting casbin rule: %w", err)
			}
		}
	}

	for ptype, ast := range m["g"] {
		for _, rule := range ast.Policy {
			vals := padRule(rule)
			if _, err := tx.Exec(ctx,
				"INSERT INTO casbin_rules (ptype, v0, v1, v2, v3, v4, v5) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING",
				ptype, vals[0], vals[1], vals[2], vals[3], vals[4], vals[5]); err != nil {
				return fmt.Errorf("inserting casbin rule: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}

// AddPolicy adds a policy rule to the database.
func (a *PgxAdapter) AddPolicy(sec, ptype string, rule []string) error {
	return a.insertRule(context.Background(), ptype, rule)
}

// RemovePolicy removes a policy rule from the database.
func (a *PgxAdapter) RemovePolicy(sec, ptype string, rule []string) error {
	vals := padRule(rule)
	_, err := a.pool.Exec(context.Background(),
		"DELETE FROM casbin_rules WHERE ptype = $1 AND v0 = $2 AND v1 = $3 AND v2 = $4 AND v3 = $5 AND v4 = $6 AND v5 = $7",
		ptype, vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
	if err != nil {
		return fmt.Errorf("removing casbin rule: %w", err)
	}
	return nil
}

// RemoveFilteredPolicy removes policy rules matching the filter.
func (a *PgxAdapter) RemoveFilteredPolicy(sec, ptype string, fieldIndex int, fieldValues ...string) error {
	var conditions []string
	var args []any

	conditions = append(conditions, "ptype = $1")
	args = append(args, ptype)

	fields := []string{"v0", "v1", "v2", "v3", "v4", "v5"}
	for i, val := range fieldValues {
		if val == "" {
			continue
		}
		idx := fieldIndex + i
		if idx >= len(fields) {
			break
		}
		argNum := len(args) + 1
		conditions = append(conditions, fmt.Sprintf("%s = $%d", fields[idx], argNum))
		args = append(args, val)
	}

	query := "DELETE FROM casbin_rules WHERE " + strings.Join(conditions, " AND ")
	_, err := a.pool.Exec(context.Background(), query, args...)
	if err != nil {
		return fmt.Errorf("removing filtered casbin rules: %w", err)
	}
	return nil
}

func (a *PgxAdapter) insertRule(ctx context.Context, ptype string, rule []string) error {
	vals := padRule(rule)
	_, err := a.pool.Exec(ctx,
		"INSERT INTO casbin_rules (ptype, v0, v1, v2, v3, v4, v5) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING",
		ptype, vals[0], vals[1], vals[2], vals[3], vals[4], vals[5])
	if err != nil {
		return fmt.Errorf("inserting casbin rule: %w", err)
	}
	return nil
}

// padRule ensures a rule has exactly 6 values, padding with empty strings.
func padRule(rule []string) [6]string {
	var out [6]string
	for i, v := range rule {
		if i >= 6 {
			break
		}
		out[i] = v
	}
	return out
}

// filterEmpty removes trailing empty strings from a rule.
func filterEmpty(rule []string) []string {
	for i := len(rule) - 1; i >= 0; i-- {
		if rule[i] != "" {
			return rule[:i+1]
		}
	}
	return nil
}
