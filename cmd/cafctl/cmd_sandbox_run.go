// Package main - cafctl sandbox subcommands (M42 Sandbox/Security Scanner).
//
// These commands surface real, offline, in-memory sandbox capabilities:
//
//   - sandbox run (M42) — runs a static analysis security scan on an artifact list using the
//     real pkg/sandbox plugin scanner. It does NOT execute code; it only inspects artifacts,
//     checks permission boundaries, and reports deterministic findings.
//
// All operations are read-only, deterministic, and do not require network access or actual execution.
package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/sandbox"
	"github.com/spf13/cobra"
)

// ----------------------------------------------------------------------------
// sandbox (parent)
// ----------------------------------------------------------------------------

func newSandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Sandbox security scanning and execution isolation",
	}
	cmd.AddCommand(newSandboxRunCmd())
	return cmd
}

// ----------------------------------------------------------------------------
// sandbox run (M42) — Security Scanning Engine
// ----------------------------------------------------------------------------

// newSandboxRunCmd implements `cafctl sandbox run` by running the real
// pkg/sandbox PluginScanner against a set of demo artifacts. It validates that
// all requested permissions are within the boundary and prints a deterministic report.
func newSandboxRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "run [--name <artifact-name>] [--profile default|restricted|full]",
		Short:         "Run security scan on an artifact (offline)",
		Args:          cobra.NoArgs,
		Example:       "  cafctl sandbox run\n  cafctl sandbox run --name myplugin --profile restricted",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, _ := cmd.Flags().GetString("name")
			profileStr, _ := cmd.Flags().GetString("profile")

			if name == "" {
				name = "demo-plugin"
			}
			if profileStr == "" {
				profileStr = "default"
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl sandbox run · sandbox security scanning engine (M42)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Create sample artifacts for the demo
			artifacts := sandbox.ArtifactList{
				Files: []sandbox.Artifact{
					{
						Path:       "/app/plugins/demo.so",
						Checksum:   "a1b2c3d4e5f6",
						ImportPath: "github.com/demo/plugin",
						Platform:   "linux/amd64",
						SizeBytes:  1024 * 512, // 512KB
					},
					{
						Path:       "/app/plugins/metrics.so",
						Checksum:   "deadbeef1234",
						ImportPath: "github.com/demo/metrics",
						Platform:   "linux/amd64",
						SizeBytes:  1024 * 256, // 256KB
					},
				},
				Err: nil,
			}

			// Create sandbox profile based on selection
			var profile *sandbox.SandboxProfile
			switch profileStr {
			case "restricted":
				profile = &sandbox.SandboxProfile{
					Name:    "restricted",
					MemoryLimit: 256,
					CPULimit:  0.5,
					Network: sandbox.NetworkPolicy{
						AllowOutbound: false,
						AllowInbound:  false,
						BlockPrivateIPs: true,
					},
					Permissions: []sandbox.Permission{
						sandbox.PermRead,
					},
					BannedImports: []string{"unsafe", "syscall"},
				}
			case "full":
				profile = &sandbox.SandboxProfile{
					Name:    "full",
					MemoryLimit: 2048,
					CPULimit:  2.0,
					Network: sandbox.NetworkPolicy{
						AllowOutbound: true,
						AllowInbound:  false,
						BlockPrivateIPs: true,
					},
					Permissions: []sandbox.Permission{
						sandbox.PermRead,
						sandbox.PermWrite,
						sandbox.PermNetworkOutbound,
					},
				}
			default:
				profile = &sandbox.SandboxProfile{
					Name:    "default",
					MemoryLimit: 512,
					CPULimit:  1.0,
					Network: sandbox.NetworkPolicy{
						AllowOutbound: true,
						AllowInbound:  false,
					},
					Permissions: []sandbox.Permission{
						sandbox.PermRead,
						sandbox.PermNetworkOutbound,
					},
					BannedImports: []string{"unsafe"},
				}
			}

			// Validate profile
			if err := profile.Validate(); err != nil {
				return fmt.Errorf("validate profile %q: %w", profile.Name, err)
			}

			// Initialize scanners
			scanner := &sandbox.StaticAnalysisScanner{
				BannedPatterns: []string{"/etc/passwd", "/root/", "/var/log/"},
				UnsafeImports:  profile.BannedImports,
			}

			isolator := &sandbox.ExecutionIsolator{}
			if err := isolator.EnforceConfig(profile.MemoryLimit, profile.CPULimit); err != nil {
				return fmt.Errorf("enforce config: %w", err)
			}

			// Run scan
			report := scanner.ScanPlugin(name, artifacts)

			// Check permission boundaries
			boundary := &sandbox.PermissionBoundary{
				Role:    profile.Name + "-role",
				Allowed: profile.Permissions,
			}

			requestedPerms := []sandbox.Permission{sandbox.PermRead, sandbox.PermNetworkOutbound, sandbox.PermExec}
			deniedPerms := boundary.Check(requestedPerms)
			capabilities := boundary.Capabilities()

			fmt.Fprintln(out, "Scan Configuration:")
			fmt.Fprintf(out, "  Artifact Name: %s\n", name)
			fmt.Fprintf(out, "  Profile:       %s\n", profile.Name)
			fmt.Fprintf(out, "  Memory Limit:  %d MB\n", profile.MemoryLimit)
			fmt.Fprintf(out, "  CPU Limit:     %.1f cores\n", profile.CPULimit)
			fmt.Fprintf(out, "  Permissions:   %v\n", capabilities)
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Artifact List:")
			for _, art := range artifacts.Files {
				fmt.Fprintf(out, "  %s (%d bytes)\n", art.Path, art.SizeBytes)
				fmt.Fprintf(out, "      SHA256: %s\n", art.Checksum)
				fmt.Fprintf(out, "      Platform: %s\n", art.Platform)
				fmt.Fprintf(out, "      Import Path: %s\n", art.ImportPath)
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Static Analysis Results:")
			if len(report.DangerousImports) > 0 {
				fmt.Fprintf(out, "  ⚠ Dangerous Imports Detected:\n")
				for _, di := range report.DangerousImports {
					fmt.Fprintf(out, "    - %s\n", di)
				}
				fmt.Fprintln(out, "")
			} else {
				fmt.Fprintf(out, "  ✓ No dangerous imports detected.\n")
				fmt.Fprintln(out, "")
			}

			if len(report.DangerousCalls) > 0 {
				fmt.Fprintf(out, "  ⚠ Banned Patterns Found:\n")
				for _, dc := range report.DangerousCalls {
					fmt.Fprintf(out, "    - %s\n", dc)
				}
				fmt.Fprintln(out, "")
			} else {
				fmt.Fprintf(out, "  ✓ No banned patterns found.\n")
				fmt.Fprintln(out, "")
			}

			fmt.Fprintln(out, "Permission Boundary Check:")
			fmt.Fprintf(out, "  Role: %s\n", boundary.Role)
			fmt.Fprintf(out, "  Requested: [read, network-outbound, exec]\n")
			fmt.Fprintf(out, "  Denied:    ")
			if len(deniedPerms) == 0 {
				fmt.Fprintln(out, "(none)")
				fmt.Fprintf(out, "  Status: %s All requests permitted.\n", OK())
			} else {
				fmt.Fprintln(out, formatPerms(deniedPerms))
				fmt.Fprintf(out, "  Status: %s Some permissions denied.\n", ERROR())
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Execution Isolation Status:")
			if report.Pass && report.TotalFindings == 0 {
				fmt.Fprintf(out, "  ✓ All security checks passed.\n")
				fmt.Fprintf(out, "  ✓ Isolator configured: memory=%dMB, cpu=%.1f cores\n", profile.MemoryLimit, profile.CPULimit)
			} else if !report.Pass {
				fmt.Fprintf(out, "  ⚠ Scan found %d issues. Execute with caution.\n", report.TotalFindings)
			} else {
				fmt.Fprintf(out, "  ✓ Executable ready for safe deployment.\n")
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Report Summary:")
			fmt.Fprintf(out, "  Total Findings:   %d\n", report.TotalFindings)
			fmt.Fprintf(out, "  Dangerous Imports:%d\n", len(report.DangerousImports))
			fmt.Fprintf(out, "  Dangerous Calls:  %d\n", len(report.DangerousCalls))
			fmt.Fprintf(out, "  Banned Features:  %d\n", len(report.BannedFeatures))
			fmt.Fprintf(out, "  Secure:           %v\n", report.Secure)
			fmt.Fprintf(out, "  Pass:             %v\n", report.Pass)
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Security scan complete for artifact %q.\n", OK(), name)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().String("name", "", "Artifact name (optional)")
	cmd.Flags().String("profile", "default", "Sandbox profile: default, restricted, full")
	return cmd
}

func formatPerms(perms []sandbox.Permission) string {
	if len(perms) == 0 {
		return ""
	}
	names := make([]string, len(perms))
	for i, p := range perms {
		names[i] = p.String()
	}
	sort.Strings(names)
	return "{" + strings.Join(names, ", ") + "}"
}
