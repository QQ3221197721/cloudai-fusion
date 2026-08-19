// Package main - cafctl gitops subcommand (drift detection)
package main

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/gitops"
	"github.com/spf13/cobra"
)

func newGitopsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gitops",
		Short: "GitOps — configuration drift analysis (offline)",
	}
	cmd.AddCommand(newGitopsDriftCmd())
	return cmd
}

// staticProvider implements gitops.StateProvider with fixed demo data
type staticProvider struct {
	desired []gitops.ResourceState
	live    []gitops.ResourceState
}

func (p *staticProvider) DesiredState(ctx context.Context, app *gitops.Application) ([]gitops.ResourceState, error) {
	return p.desired, nil
}

func (p *staticProvider) LiveState(ctx context.Context, app *gitops.Application) ([]gitops.ResourceState, error) {
	return p.live, nil
}

func (p *staticProvider) Real() bool { return false }

// newGitopsDriftCmd runs drift detection with demo data via StaticStateProvider
func newGitopsDriftCmd() *cobra.Command {
	var appName string
	cmd := &cobra.Command{
		Use:           "drift [--app <name>]",
		Short:         "Check GitOps configuration drift against demo desired state",
		Example:       "  cafctl gitops drift --app my-app",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Create demo data
			desired := []gitops.ResourceState{
				{Kind: "Deployment", Name: "web-server", Namespace: "default", Fields: map[string]string{"replicas": "3", "image": "nginx:1.25"}},
				{Kind: "Service", Name: "web-svc", Namespace: "default", Fields: map[string]string{"type": "LoadBalancer", "port": "80"}},
			}
			live := []gitops.ResourceState{
				{Kind: "Deployment", Name: "web-server", Namespace: "default", Fields: map[string]string{"replicas": "4", "image": "nginx:1.25"}},
				{Kind: "Service", Name: "web-svc", Namespace: "default", Fields: map[string]string{"type": "ClusterIP", "port": "80"}},
			}

			provider := &staticProvider{desired: desired, live: live}
			scanner := gitops.NewClusterDriftScanner(gitops.DriftDetectorConfig{Provider: provider})

			appObj := &gitops.Application{Name: orDash(appName), Engine: gitops.EngineFlux}
			drifts, err := scanner.Scan(ctx, appObj)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return fmt.Errorf("scan failed: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl gitops drift · configuration drift analysis")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Application: %s (engine=%s)\n", appObj.Name, appObj.Engine)
			fmt.Fprintf(out, "Total drifted fields detected: %d\n", len(drifts))

			for i, d := range drifts {
				if i >= 5 && len(drifts) > 5 {
					break
				}
				fmt.Fprintf(out, "  ✗ %-10s/%s.%s [%s]: expected=%q actual=%q\n",
					d.ResourceKind, d.Namespace, d.ResourceName, d.Severity, d.Expected, d.Actual)
			}
			if len(drifts) > 5 {
				fmt.Fprintf(out, "  ... and %d more\n", len(drifts)-5)
			}

			fmt.Fprintln(out, "")
			if len(drifts) > 0 {
				fmt.Fprintf(out, "%s Configuration drift detected!\n", warningSymbol)
			} else {
				fmt.Fprintf(out, "%s No drift detected (demo mode).\n", OK())
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&appName, "app", "", "Application name/ID")
	return cmd
}
