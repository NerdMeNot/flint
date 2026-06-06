package main

import (
	"context"
	"fmt"

	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/spf13/cobra"
)

func adminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands for managing Flint",
	}

	cmd.AddCommand(createUserCmd())
	cmd.AddCommand(resetPasswordCmd())

	return cmd
}

func createUserCmd() *cobra.Command {
	var (
		email      string
		name       string
		role       string
		password   string
		configPath string
	)

	cmd := &cobra.Command{
		Use:   "create-user",
		Short: "Create a local user with email + password authentication",
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" {
				return fmt.Errorf("--email is required")
			}

			ctx := context.Background()

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			pool, err := dbkit.NewPool(ctx, dbkit.Config{
				Host:     cfg.Database.Host,
				Port:     cfg.Database.Port,
				Database: cfg.Database.Database,
				User:     cfg.Database.User,
				Password: cfg.Database.Password,
				SSLMode:  cfg.Database.SSLMode,
			})
			if err != nil {
				return fmt.Errorf("database: %w", err)
			}
			defer pool.Close()

			q := db.New(pool)

			// Get org.
			org, err := q.GetOrg(ctx)
			if err != nil {
				return fmt.Errorf("org not found — run the server first to initialize: %w", err)
			}

			// Generate password if not provided.
			if password == "" {
				password = auth.GenerateRandomPassword()
			}

			hash, err := auth.HashPassword(password)
			if err != nil {
				return fmt.Errorf("hashing password: %w", err)
			}

			// Create user.
			var namePtr *string
			if name != "" {
				namePtr = &name
			}
			userID, err := q.CreateLocalUser(ctx, db.CreateLocalUserParams{
				OrgID:        org.ID,
				Email:        email,
				Name:         namePtr,
				PasswordHash: &hash,
			})
			if err != nil {
				return fmt.Errorf("creating user (may already exist): %w", err)
			}

			// Assign role.
			if role == "" {
				role = cfg.Auth.DefaultRoleOrFallback()
			}
			roleRow, err := q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{
				OrgID: org.ID,
				Slug:  role,
			})
			if err != nil {
				return fmt.Errorf("finding role %s: %w", role, err)
			}
			if err := q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{
				Subject: email,
				RoleID:  roleRow.ID,
			}); err != nil {
				return fmt.Errorf("assigning role: %w", err)
			}

			// Regenerate Casbin policies.
			enforcer, err := auth.NewEnforcer(pool)
			if err == nil {
				_ = auth.RegenerateForSubject(ctx, q, pool, enforcer, email)
			}

			fmt.Printf("User created successfully:\n")
			fmt.Printf("  ID:       %s\n", userID)
			fmt.Printf("  Email:    %s\n", email)
			fmt.Printf("  Role:     %s\n", role)
			fmt.Printf("  Password: %s\n", password)
			fmt.Printf("\nSave this password — it will not be shown again.\n")

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "user email address (required)")
	cmd.Flags().StringVar(&name, "name", "", "display name")
	cmd.Flags().StringVar(&role, "role", "", "role slug (default: from config)")
	cmd.Flags().StringVar(&password, "password", "", "password (auto-generated if not set)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")

	return cmd
}

func resetPasswordCmd() *cobra.Command {
	var (
		email      string
		configPath string
	)

	cmd := &cobra.Command{
		Use:   "reset-password",
		Short: "Reset a local user's password",
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" {
				return fmt.Errorf("--email is required")
			}

			ctx := context.Background()

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			pool, err := dbkit.NewPool(ctx, dbkit.Config{
				Host:     cfg.Database.Host,
				Port:     cfg.Database.Port,
				Database: cfg.Database.Database,
				User:     cfg.Database.User,
				Password: cfg.Database.Password,
				SSLMode:  cfg.Database.SSLMode,
			})
			if err != nil {
				return fmt.Errorf("database: %w", err)
			}
			defer pool.Close()

			q := db.New(pool)

			org, err := q.GetOrg(ctx)
			if err != nil {
				return fmt.Errorf("org not found: %w", err)
			}

			user, err := q.GetUserByEmail(ctx, db.GetUserByEmailParams{
				OrgID: org.ID, Email: email,
			})
			if err != nil {
				return fmt.Errorf("user not found: %w", err)
			}

			newPassword := auth.GenerateRandomPassword()
			hash, err := auth.HashPassword(newPassword)
			if err != nil {
				return fmt.Errorf("hashing password: %w", err)
			}

			if err := q.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
				ID:           user.ID,
				PasswordHash: &hash,
			}); err != nil {
				return fmt.Errorf("updating password: %w", err)
			}

			fmt.Printf("Password reset for %s:\n", email)
			fmt.Printf("  New password: %s\n", newPassword)
			fmt.Printf("\nSave this password — it will not be shown again.\n")

			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "user email address (required)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")

	return cmd
}
