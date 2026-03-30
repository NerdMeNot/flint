package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/db"
)

// DeployWindow defines when deployments are allowed.
type DeployWindow struct {
	Days     []string `json:"days"`     // e.g., ["mon", "tue", "wed", "thu", "fri"]
	Hours    string   `json:"hours"`    // e.g., "06:00-18:00"
	Timezone string   `json:"timezone"` // e.g., "UTC", "America/New_York"
}

// CheckEnvironmentAccess validates that the current conditions allow deployment
// to a protected environment. Returns nil if access is allowed.
func CheckEnvironmentAccess(ctx context.Context, q *db.Queries, orgID, envName, triggerBranch string) error {
	env, err := q.GetProtectedEnvironment(ctx, db.GetProtectedEnvironmentParams{
		OrgID: orgID,
		Name:  envName,
	})
	if err != nil {
		// No protected environment config → unprotected, allow freely.
		return nil
	}

	// Check branch restrictions.
	if len(env.DeployBranches) > 0 {
		if !matchesBranch(triggerBranch, env.DeployBranches) {
			return fmt.Errorf("branch %q is not allowed for environment %q (allowed: %s)",
				triggerBranch, envName, strings.Join(env.DeployBranches, ", "))
		}
	}

	// Check deploy window.
	if len(env.DeployWindow) > 0 {
		var window DeployWindow
		if err := json.Unmarshal(env.DeployWindow, &window); err == nil {
			if err := checkDeployWindow(window); err != nil {
				return fmt.Errorf("environment %q: %w", envName, err)
			}
		}
	}

	return nil
}

// GetEnvironmentMinRole returns the minimum role required for a protected environment.
// Returns empty string if the environment is not protected.
func GetEnvironmentMinRole(ctx context.Context, q *db.Queries, orgID, envName string) string {
	env, err := q.GetProtectedEnvironment(ctx, db.GetProtectedEnvironmentParams{
		OrgID: orgID,
		Name:  envName,
	})
	if err != nil {
		return ""
	}
	return env.MinRole
}

// GetEnvironmentApprovers returns the list of authorized approvers for an environment.
func GetEnvironmentApprovers(ctx context.Context, q *db.Queries, orgID, envName string) []string {
	env, err := q.GetProtectedEnvironment(ctx, db.GetProtectedEnvironmentParams{
		OrgID: orgID,
		Name:  envName,
	})
	if err != nil {
		return nil
	}
	return env.Approvers
}

// matchesBranch checks if a branch matches any of the allowed patterns.
// Supports glob patterns like "release/*".
func matchesBranch(branch string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, branch); matched {
			return true
		}
		// Direct string match as fallback.
		if pattern == branch {
			return true
		}
	}
	return false
}

// checkDeployWindow validates that the current time is within the deploy window.
func checkDeployWindow(w DeployWindow) error {
	if len(w.Days) == 0 && w.Hours == "" {
		return nil // no restrictions
	}

	loc := time.UTC
	if w.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(w.Timezone)
		if err != nil {
			return fmt.Errorf("invalid timezone %q: %w", w.Timezone, err)
		}
	}

	now := time.Now().In(loc)

	// Check day restriction.
	if len(w.Days) > 0 {
		currentDay := strings.ToLower(now.Weekday().String()[:3])
		dayAllowed := false
		for _, d := range w.Days {
			if strings.EqualFold(strings.TrimSpace(d), currentDay) {
				dayAllowed = true
				break
			}
		}
		if !dayAllowed {
			return fmt.Errorf("deployments not allowed on %s (allowed: %s)",
				now.Weekday().String(), strings.Join(w.Days, ", "))
		}
	}

	// Check hours restriction (format: "HH:MM-HH:MM").
	if w.Hours != "" {
		parts := strings.SplitN(w.Hours, "-", 2)
		if len(parts) == 2 {
			startTime, err1 := time.Parse("15:04", strings.TrimSpace(parts[0]))
			endTime, err2 := time.Parse("15:04", strings.TrimSpace(parts[1]))
			if err1 == nil && err2 == nil {
				currentMinutes := now.Hour()*60 + now.Minute()
				startMinutes := startTime.Hour()*60 + startTime.Minute()
				endMinutes := endTime.Hour()*60 + endTime.Minute()

				if currentMinutes < startMinutes || currentMinutes > endMinutes {
					return fmt.Errorf("deployments not allowed at %s (allowed: %s)",
						now.Format("15:04"), w.Hours)
				}
			}
		}
	}

	return nil
}

// IsAuthorizedApprover checks if a user is in the list of authorized approvers
// for a gate. Supports "team:<slug>" and "user:<email>" formats.
func IsAuthorizedApprover(ctx context.Context, q *db.Queries, orgID, userID, userEmail string, approvers []string) (bool, error) {
	if len(approvers) == 0 {
		return true, nil // no approver restriction
	}

	for _, approver := range approvers {
		parts := strings.SplitN(approver, ":", 2)
		if len(parts) != 2 {
			// Treat as direct email match.
			if strings.EqualFold(approver, userEmail) {
				return true, nil
			}
			continue
		}

		switch parts[0] {
		case "user":
			if strings.EqualFold(parts[1], userEmail) {
				return true, nil
			}
		case "team":
			isMember, err := q.IsUserInTeamBySlug(ctx, db.IsUserInTeamBySlugParams{
				OrgID:  orgID,
				Slug:   parts[1],
				UserID: userID,
			})
			if err != nil {
				return false, fmt.Errorf("checking team membership: %w", err)
			}
			if isMember {
				return true, nil
			}
		}
	}

	return false, nil
}
