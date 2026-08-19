// Package main - cafctl auth subcommand
//
// Provides safe, read-only, offline auth inspection: validate a JWT token
// against a provided secret, and print the static RBAC role→permission matrix.
// No network or database is required — auth.NewService works purely in-memory
// for token validation, and auth.HasPermission reads a package-level index.
package main

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/spf13/cobra"
)

// newAuthCmd builds the `auth` command group.
func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication — inspect JWT tokens and the RBAC role matrix (offline)",
		Long: `Authentication inspection commands.

check-token validates a JWT offline against a provided secret and prints its
claims. roles prints the static RBAC role→permission matrix. Both are
read-only and require neither network nor database access.`,
		Example: "  cafctl auth check-token <jwt> --secret <secret>\n  cafctl auth roles",
	}
	cmd.AddCommand(
		newAuthCheckTokenCmd(),
		newAuthRolesCmd(),
	)
	return cmd
}

// newAuthCheckTokenCmd builds `cafctl auth check-token <token>`.
func newAuthCheckTokenCmd() *cobra.Command {
	var secret string
	cmd := &cobra.Command{
		Use:           "check-token <token>",
		Short:         "Validate a JWT token offline and print its claims",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl auth check-token eyJhbGci... --secret my-secret",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := auth.NewService(auth.Config{JWTSecret: secret})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			claims, err := service.ValidateToken(args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sinvalid token: %v\n", ERROR(), err)
				return fmt.Errorf("invalid token: %w", err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%stoken valid\n", OK())
			fmt.Fprintf(out, "  Subject:   %s\n", claims.Subject)
			fmt.Fprintf(out, "  User ID:   %s\n", claims.UserID)
			fmt.Fprintf(out, "  Username:  %s\n", claims.Username)
			fmt.Fprintf(out, "  Email:     %s\n", claims.Email)
			fmt.Fprintf(out, "  Role:      %s\n", claims.Role)
			if claims.IssuedAt != nil {
				fmt.Fprintf(out, "  IssuedAt:  %s\n", claims.IssuedAt.Time.Format("2006-01-02 15:04:05 UTC"))
			}
			if claims.ExpiresAt != nil {
				fmt.Fprintf(out, "  ExpiresAt: %s\n", claims.ExpiresAt.Time.Format("2006-01-02 15:04:05 UTC"))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&secret, "secret", "s", "", "JWT signing secret (required)")
	_ = cmd.MarkFlagRequired("secret")
	return cmd
}

// authRoles lists the RBAC roles in ascending privilege order.
var authRoles = []auth.Role{auth.RoleViewer, auth.RoleDeveloper, auth.RoleOperator, auth.RoleAdmin}

// authPermissions is the representative permission set shown in the matrix,
// kept sorted so the output is deterministic across runs.
var authPermissions = []auth.Permission{
	auth.PermClusterCreate, auth.PermClusterRead,
	auth.PermWorkloadCreate, auth.PermWorkloadRead,
	auth.PermSecurityManage, auth.PermSecurityRead,
	auth.PermUserManage,
	auth.PermCostRead,
	auth.PermAgentManage,
}

// newAuthRolesCmd builds `cafctl auth roles`.
func newAuthRolesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "roles",
		Short:         "Print the RBAC role→permission matrix",
		Args:          cobra.NoArgs,
		Example:       "  cafctl auth roles",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "  cafctl auth roles · RBAC permission matrix")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "")

			perms := append([]auth.Permission(nil), authPermissions...)
			sort.Slice(perms, func(i, j int) bool { return perms[i] < perms[j] })

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			// header row
			fmt.Fprint(w, "PERMISSION")
			for _, r := range authRoles {
				fmt.Fprintf(w, "\t%s", r)
			}
			fmt.Fprintln(w)
			// one row per permission
			for _, p := range perms {
				fmt.Fprintf(w, "%s", p)
				for _, r := range authRoles {
					mark := "-"
					if auth.HasPermission(r, p) {
						mark = "✓"
					}
					fmt.Fprintf(w, "\t%s", mark)
				}
				fmt.Fprintln(w)
			}
			w.Flush()
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}
