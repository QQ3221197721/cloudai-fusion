// Package main - `cafctl tenant` — Module 11 Multi-tenant GPU Sharing (Phase 2).
//
// Wraps pkg/tenants, which in turn reuses pkg/scheduler GPUSharingManager
// (MPS/MIG operations). This Phase 2 upgrade implements:
//   - FSM state machine: pending → active ⇄ suspended → deleted (terminal)
//   - Ed25519 attestation on ALL write ops via pkg/evidence.Ledger (actor "cafctl-tenant")
//   - New CLI subcommands: activate, suspend, resume, delete-pool (pool-level lifecycle)
//   - State guards on AddTenant (pending|active), AllocateToTenant (active only)
//
// Storage layout (--store, default ./.caf):
//   <store>/tenants/v0.1/pools.json    all pools + tenant memberships
package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/spf13/cobra"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/tenants"
)

const defaultTenantStore = "./.caf"

// newTenantCmd builds the `tenant` command group.
func newTenantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Multi-tenant GPU sharing (Module 11) — pools, MIG slices, MPS shares",
		Long: `Multi-tenant GPU Sharing (Module 11, Phase 2) — assign MIG slices or MPS shares to tenants.

Builds on pkg/scheduler GPUSharingManager: MIG mode partitions GPUs into
isolated instances (A100/H100), MPS mode shares compute across processes.
Phase 2 adds an FSM lifecycle (pending -> active <-> suspended -> deleted,
terminal) plus Ed25519-signed attestations for every write operation via
pkg/evidence (pass --no-attest to run without receipts).`,
		Example: `  cafctl tenant create --pool gpu-mig-01 --name team-a --mode mig --gpu-type a100 --gpus 0
  cafctl tenant activate --pool gpu-mig-01
  cafctl tenant list --pool gpu-mig-01
  cafctl tenant add-tenant --pool gpu-mig-01 --name alice --slices 2
  cafctl tenant allocate --pool gpu-mig-01 --tenant alice --slices 2
  cafctl tenant suspend --pool gpu-mig-01
  cafctl tenant resume --pool gpu-mig-01
  cafctl tenant delete --pool gpu-mig-01 --tenant alice
  cafctl tenant delete-pool --pool gpu-mig-01`,
	}
	cmd.AddCommand(
		newTenantCreateCmd(),
		newTenantListCmd(),
		newTenantAddTenantCmd(),
		newTenantAllocateCmd(),
		newTenantDeleteCmd(),
		newTenantActivateCmd(),
		newTenantSuspendCmd(),
		newTenantResumeCmd(),
		newTenantDeletePoolCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// tenant activate / suspend / resume / delete-pool (Phase 2 FSM lifecycle)
// ----------------------------------------------------------------------------

// tenantPoolTransitionCmd builds a pool-level lifecycle subcommand sharing
// flags, JSON/text output, and error handling.
func tenantPoolTransitionCmd(use, short, example string, apply func(ctx context.Context, mgr *tenants.Manager, poolID string) (*tenants.TenantPool, error)) *cobra.Command {
	var (
		store, output, poolID string
		noAttest              bool
	)
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		Example:       example,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openTenantManager(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pool, err := resolvePool(mgr, poolID)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			updated, err := apply(cmd.Context(), mgr, pool.ID)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"pool_id":            updated.ID,
					"name":               updated.Name,
					"status":             updated.Status,
					"members":            len(updated.Members),
					"updated_at":         updated.UpdatedAt.Format(time.RFC3339),
					"attestation_hash":   mgr.LastAttestationHash,
					"attestation_signed": mgr.LastAttestation() != nil,
				})
			}
			renderTenantPoolTransition(cmd.OutOrStdout(), use, updated, mgr, noAttest)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTenantStore, "Store root directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&poolID, "pool", "", "Pool ID or name (required)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	_ = cmd.MarkFlagRequired("pool")
	return cmd
}

func newTenantActivateCmd() *cobra.Command {
	return tenantPoolTransitionCmd("activate",
		"Activate a pending tenant pool (pending → active)",
		"  cafctl tenant activate --pool <pool-id>",
		func(ctx context.Context, mgr *tenants.Manager, poolID string) (*tenants.TenantPool, error) {
			return mgr.ActivatePool(ctx, poolID)
		})
}

func newTenantSuspendCmd() *cobra.Command {
	return tenantPoolTransitionCmd("suspend",
		"Suspend an active tenant pool (active → suspended)",
		"  cafctl tenant suspend --pool <pool-id>",
		func(ctx context.Context, mgr *tenants.Manager, poolID string) (*tenants.TenantPool, error) {
			return mgr.SuspendPool(ctx, poolID)
		})
}

func newTenantResumeCmd() *cobra.Command {
	return tenantPoolTransitionCmd("resume",
		"Resume a suspended tenant pool (suspended → active)",
		"  cafctl tenant resume --pool <pool-id>",
		func(ctx context.Context, mgr *tenants.Manager, poolID string) (*tenants.TenantPool, error) {
			return mgr.ResumePool(ctx, poolID)
		})
}

func newTenantDeletePoolCmd() *cobra.Command {
	return tenantPoolTransitionCmd("delete-pool",
		"Delete a tenant pool (active|suspended → deleted, terminal; MIG instances destroyed)",
		"  cafctl tenant delete-pool --pool <pool-id>",
		func(ctx context.Context, mgr *tenants.Manager, poolID string) (*tenants.TenantPool, error) {
			return mgr.DeletePool(ctx, poolID)
		})
}

// renderTenantPoolTransition prints a lifecycle receipt (text mode).
func renderTenantPoolTransition(out io.Writer, verb string, p *tenants.TenantPool, mgr *tenants.Manager, noAttest bool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl tenant %s · pool status: %s\n", verb, p.Status)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Pool:          %s (%s)\n", shortID(p.ID), p.Name)
	fmt.Fprintf(out, "  Status:        %s\n", p.Status)
	fmt.Fprintf(out, "  Members:       %d\n", len(p.Members))
	fmt.Fprintf(out, "  Updated:       %s\n", p.UpdatedAt.Format(time.RFC3339))
	if !noAttest {
		if ev := mgr.LastAttestation(); ev != nil {
			fmt.Fprintf(out, "  Attestation:   seq #%d · %s · hash %s... (Ed25519-signed)\n", ev.Seq, ev.Action, ev.Hash[:16])
		} else {
			fmt.Fprintln(out, "  Attestation:   disabled (run without --no-attest for signed receipts)")
		}
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// tenant add-tenant
// ----------------------------------------------------------------------------

func newTenantAddTenantCmd() *cobra.Command {
	var (
		store, output, poolID, name, uid, resourceMode string
		slices                                         int
		noAttest                                       bool
	)
	cmd := &cobra.Command{
		Use:   "add-tenant",
		Short: "Add a tenant to an existing pool",
		Example: `  cafctl tenant add-tenant --pool <pool-id> --name alice-project --mode mig-slice --slices 4
  cafctl tenant add-tenant --pool <pool-id> --name bob-inference --uid bob@example.com --mode mps-share`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openTenantManager(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pool, err := resolvePool(mgr, poolID)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			input := tenants.MemberInput{
				Name:         name,
				UID:          uid,
				ResourceMode: resourceMode,
				Slices:       slices,
				MaxClients:   16, // default
			}

			member, err := mgr.AddTenant(cmd.Context(), pool.ID, input)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			if output == "json" {
				result := map[string]any{
					"id":                 member.ID,
					"pool_id":            member.PoolID,
					"name":               member.Name,
					"uid":                member.UID,
					"status":             member.Status,
					"resource_mode":      member.ResourceMode,
					"created_at":         member.CreatedAt.Format(time.RFC3339),
					"attestation_hash":   mgr.LastAttestationHash,
					"attestation_signed": mgr.LastAttestation() != nil,
				}
				if len(member.MIGSlices) > 0 {
					result["mig_slices"] = len(member.MIGSlices)
				}
				return writeJSON(cmd.OutOrStdout(), result)
			}

			renderTenantAdded(cmd.OutOrStdout(), member, mgr, noAttest)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTenantStore, "Store root directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&poolID, "pool", "", "Pool ID or name (required)")
	cmd.Flags().StringVar(&name, "name", "", "Tenant display name (required)")
	cmd.Flags().StringVar(&uid, "uid", "", "User ID (optional)")
	cmd.Flags().StringVar(&resourceMode, "mode", "mps-share", "Resource mode: 'mig-slice' or 'mps-share'")
	cmd.Flags().IntVar(&slices, "slices", 1, "Number of MIG slices (for mig-slice mode)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation placeholder (dev only)")

	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}

// openTenantManager loads the store and returns a Manager.
// Hardware note: NVIDIA GPU operations (nvidia-smi / nvidia-cuda-mps-control)
// are only exercised on MIG slice creation; without such binaries on the host,
// create/list/delete still work in bookkeeping-only mode (see pkg/tenants).
func openTenantManager(storePath string, attest bool) (*tenants.Manager, error) {
	if storePath == "" {
		storePath = defaultTenantStore
	}
	gpuMgr := scheduler.NewGPUSharingManager(scheduler.GPUSharingConfig{})
	var ledger *evidence.Ledger
	if attest {
		signer, serr := evidence.GenerateEphemeralSigner()
		if serr != nil {
			return nil, fmt.Errorf("generate signer: %w", serr)
		}
		l, lerr := evidence.NewLedger(evidence.LedgerConfig{
			Store:    evidence.NewMemoryStore(),
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		if lerr != nil {
			return nil, fmt.Errorf("build ledger: %w", lerr)
		}
		ledger = l
	}
	return tenants.NewManagerWithLedger(storePath, gpuMgr, ledger)
}

// parseGPUIndices parses "0,1,2" into []int{0,1,2}.
func parseGPUIndices(s string) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid GPU index %q", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid GPU indices in %q", s)
	}
	return out, nil
}

// ----------------------------------------------------------------------------
// tenant create
// ----------------------------------------------------------------------------

func newTenantCreateCmd() *cobra.Command {
	var (
		store, output      string
		poolID, name, mode string
		gpuType, gpus      string
		migProfile         string
		slices             int
		node               int
		noAttest           bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a tenant pool (assign GPUs to MIG or MPS mode)",
		Example: `  cafctl tenant create --pool gpu-mig-01 --name team-a --mode mig --gpu-type a100 --gpus 0
  cafctl tenant create --pool gpu-mps-01 --name team-b --mode mps --gpu-type h100 --gpus 0,1`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openTenantManager(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			gpuIdx, err := parseGPUIndices(gpus)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pool, err := mgr.CreatePool(cmd.Context(), tenants.PoolInput{
				Name:        name,
				GPUType:     gpuType,
				MigProfile:  migProfile,
				Mode:        tenants.PoolMode(mode),
				NodeIndex:   node,
				GPUIndices:  gpuIdx,
				TotalSlices: slices,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				result := map[string]any{
					"id":                  pool.ID,
					"pool_id":             poolID,
					"name":                pool.Name,
					"gpu_type":            pool.GPUType,
					"mode":                pool.Mode,
					"mig_profile":         pool.MigProfile,
					"node_index":          pool.NodeIndex,
					"gpu_indices":         pool.GPUIndices,
					"total_slices":        pool.TotalSlices,
					"member_count":        len(pool.Members),
					"created_at":          pool.CreatedAt.Format(time.RFC3339),
					"status":              pool.Status,
					"attestation_enabled": mgr.AttestationEnabled && !noAttest,
					"attestation_hash":    mgr.LastAttestationHash,
					"attestation_signed":  mgr.LastAttestation() != nil,
				}
				return writeJSON(cmd.OutOrStdout(), result)
			}
			renderTenantPoolCreate(cmd.OutOrStdout(), mgr, pool, noAttest)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTenantStore, "Store root directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&poolID, "pool", "", "User-facing pool ID (stored as pool name key)")
	cmd.Flags().StringVar(&name, "name", "", "Tenant pool display name (required)")
	cmd.Flags().StringVar(&mode, "mode", "mps", "Sharing mode: mig | mps")
	cmd.Flags().StringVar(&gpuType, "gpu-type", "a100", "GPU type for MIG profile selection")
	cmd.Flags().StringVar(&gpus, "gpus", "0", "Comma-separated GPU indices")
	cmd.Flags().StringVar(&migProfile, "mig-profile", "1g.5gb", "MIG profile (mig mode only)")
	cmd.Flags().IntVar(&slices, "slices", 0, "Total MIG slices (0 = auto from profile)")
	cmd.Flags().IntVar(&node, "node", 0, "Node index")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation placeholder (dev only)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// ----------------------------------------------------------------------------
// tenant list
// ----------------------------------------------------------------------------

func newTenantListCmd() *cobra.Command {
	var (
		store, output, poolID string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pools, or members of one pool via --pool",
		Example: `  cafctl tenant list
  cafctl tenant list --pool <pool-id>`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openTenantManager(store, false) // read-only: no ledger needed
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			if poolID != "" {
				pool, err := resolvePool(mgr, poolID)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
					return err
				}
				if output == "json" {
					return writeJSON(cmd.OutOrStdout(), pool)
				}
				renderTenantMembers(cmd.OutOrStdout(), pool)
				return nil
			}

			pools := mgr.ListPools()
			if output == "json" {
				if pools == nil {
					pools = []*tenants.TenantPool{}
				}
				return writeJSON(cmd.OutOrStdout(), pools)
			}
			renderTenantPools(cmd.OutOrStdout(), pools)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTenantStore, "Store root directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&poolID, "pool", "", "Pool ID (UUID or name) whose members to list")
	return cmd
}

// resolvePool finds a pool by UUID or (fallback) by exact name — lets demos
// use human-friendly names instead of copying UUIDs.
func resolvePool(mgr *tenants.Manager, poolID string) (*tenants.TenantPool, error) {
	if pool, err := mgr.GetPool(poolID); err == nil {
		return pool, nil
	}
	for _, p := range mgr.ListPools() {
		if p.Name == poolID || p.ID == poolID {
			return p, nil
		}
	}
	return nil, fmt.Errorf("pool %q not found (tried UUID and name)", poolID)
}

// resolveTenant finds a tenant ID within a pool by UUID or name prefix.
func resolveTenant(pool *tenants.TenantPool, tenant string) (string, error) {
	for i := range pool.Members {
		m := &pool.Members[i]
		if m.ID == tenant || m.Name == tenant {
			return m.ID, nil
		}
	}
	return "", fmt.Errorf("tenant %q not found in pool %q", tenant, pool.ID)
}

// ----------------------------------------------------------------------------
// tenant allocate
// ----------------------------------------------------------------------------

func newTenantAllocateCmd() *cobra.Command {
	var (
		store, output, poolID, tenant string
		slices                        int
		noAttest                      bool
	)
	cmd := &cobra.Command{
		Use:           "allocate",
		Short:         "Allocate additional MIG slices (or MPS client capacity) to a tenant",
		Example:       `  cafctl tenant allocate --pool <pool-id> --tenant <tenant-id> --slices 2`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openTenantManager(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pool, err := resolvePool(mgr, poolID)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			tenantID, err := resolveTenant(pool, tenant)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			member, err := mgr.AllocateToTenant(cmd.Context(), pool.ID, tenantID, slices)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"tenant_id":          member.ID,
					"pool_id":            member.PoolID,
					"name":               member.Name,
					"mig_slices":         len(member.MIGSlices),
					"max_clients":        member.MaxClients,
					"updated_at":         member.UpdatedAt.Format(time.RFC3339),
					"status":             member.Status,
					"attestation_hash":   mgr.LastAttestationHash,
					"attestation_signed": mgr.LastAttestation() != nil,
				})
			}
			renderTenantAllocated(cmd.OutOrStdout(), member, mgr, noAttest)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTenantStore, "Store root directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&poolID, "pool", "", "Pool ID or name (required)")
	cmd.Flags().StringVar(&tenant, "tenant", "", "Tenant ID or name (required)")
	cmd.Flags().IntVar(&slices, "slices", 1, "Additional slices to allocate")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation placeholder (dev only)")
	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("tenant")
	_ = cmd.MarkFlagRequired("slices")
	return cmd
}

// ----------------------------------------------------------------------------
// tenant delete
// ----------------------------------------------------------------------------

func newTenantDeleteCmd() *cobra.Command {
	var (
		store, output, poolID, tenant string
		noAttest                      bool
	)
	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Delete a tenant from a pool (MIG instances are destroyed)",
		Example:       `  cafctl tenant delete --pool <pool-id> --tenant <tenant-id>`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openTenantManager(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pool, err := resolvePool(mgr, poolID)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			tenantID, err := resolveTenant(pool, tenant)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if err := mgr.RemoveTenant(cmd.Context(), pool.ID, tenantID); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"pool_id":            pool.ID,
					"tenant_id":          tenantID,
					"status":             "deleted",
					"deleted_at":         time.Now().UTC().Format(time.RFC3339),
					"attestation_hash":   mgr.LastAttestationHash,
					"attestation_signed": mgr.LastAttestation() != nil,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%stenant %s deleted from pool %s\n",
				OK(), shortID(tenantID), shortID(pool.ID))
			if !noAttest && mgr.LastAttestationHash != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  attestation: %s... (Ed25519-signed)\n", mgr.LastAttestationHash[:16])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTenantStore, "Store root directory")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&poolID, "pool", "", "Pool ID or name (required)")
	cmd.Flags().StringVar(&tenant, "tenant", "", "Tenant ID or name (required)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation placeholder (dev only)")
	_ = cmd.MarkFlagRequired("pool")
	_ = cmd.MarkFlagRequired("tenant")
	return cmd
}

// ----------------------------------------------------------------------------
// Renderers
// ----------------------------------------------------------------------------

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// writeAttestLine prints the latest ledger receipt (text mode), or an
// explanatory line when attestation is disabled. Shared by tenant renderers.
func writeAttestLine(out io.Writer, mgr *tenants.Manager, noAttest bool) {
	if noAttest {
		return
	}
	if mgr != nil {
		if ev := mgr.LastAttestation(); ev != nil {
			fmt.Fprintf(out, "  Attestation:   seq #%d · %s · hash %s... (Ed25519-signed)\n", ev.Seq, ev.Action, ev.Hash[:16])
			return
		}
	}
	fmt.Fprintln(out, "  Attestation:   disabled (run without --no-attest for signed receipts)")
}

func renderTenantPoolCreate(out io.Writer, mgr *tenants.Manager, p *tenants.TenantPool, noAttest bool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl tenant create · pool registered (pending activation)")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Pool:          %s (%s)\n", shortID(p.ID), p.Name)
	fmt.Fprintf(out, "  Status:        %s\n", p.Status)
	fmt.Fprintf(out, "  Mode:          %s\n", p.Mode)
	fmt.Fprintf(out, "  GPU Type:      %s\n", p.GPUType)
	if p.Mode == tenants.PoolModeMIG {
		fmt.Fprintf(out, "  MIG Profile:   %s\n", p.MigProfile)
	}
	fmt.Fprintf(out, "  Node / GPUs:   node=%d gpus=%v\n", p.NodeIndex, p.GPUIndices)
	fmt.Fprintf(out, "  Total Slices:  %d\n", p.TotalSlices)
	fmt.Fprintf(out, "  Members:       %d\n", len(p.Members))
	fmt.Fprintf(out, "  Created:       %s\n", p.CreatedAt.Format(time.RFC3339))
	writeAttestLine(out, mgr, noAttest)
	fmt.Fprintln(out, "")
}

func renderTenantPools(out io.Writer, pools []*tenants.TenantPool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 100))
	fmt.Fprintln(out, "  TENANT POOLS (Module 11 Phase 2)")
	fmt.Fprintln(out, Separator('─', 100))
	fmt.Fprintf(out, "  %-18s %-20s %-6s %-10s %-14s %-8s %s\n",
		"POOL", "NAME", "MODE", "GPUS", "SLICES", "MEMBERS", "STATUS")
	fmt.Fprintln(out, Separator('─', 100))
	if len(pools) == 0 {
		fmt.Fprintln(out, "  (no pools — create one with `cafctl tenant create`)")
	}
	for _, p := range pools {
		fmt.Fprintf(out, "  %-18s %-20s %-6s %-10s %-14d %-8d %s\n",
			shortID(p.ID), truncate(p.Name, 20), p.Mode,
			fmt.Sprintf("%v", p.GPUIndices), p.TotalSlices, len(p.Members), p.Status)
	}
	fmt.Fprintln(out, Separator('═', 100))
	fmt.Fprintln(out, "")
}

func renderTenantMembers(out io.Writer, pool *tenants.TenantPool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 88))
	fmt.Fprintf(out, "  POOL %s (%s) · mode=%s · slices=%d\n",
		shortID(pool.ID), pool.Name, pool.Mode, pool.TotalSlices)
	fmt.Fprintln(out, Separator('─', 88))
	fmt.Fprintf(out, "  %-18s %-20s %-9s %-8s %s\n",
		"TENANT", "NAME", "STATUS", "SLICES", "UPDATED")
	fmt.Fprintln(out, Separator('─', 88))
	if len(pool.Members) == 0 {
		fmt.Fprintln(out, "  (no members)")
	}
	for i := range pool.Members {
		m := &pool.Members[i]
		fmt.Fprintf(out, "  %-18s %-20s %-9s %-8d %s\n",
			shortID(m.ID), truncate(m.Name, 20), m.Status,
			len(m.MIGSlices), m.UpdatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Fprintln(out, Separator('═', 88))
	fmt.Fprintln(out, "")
}

func renderTenantAdded(out io.Writer, m *tenants.TenantMember, mgr *tenants.Manager, noAttest bool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl tenant add-tenant · tenant registered")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Tenant:        %s (%s)\n", shortID(m.ID), m.Name)
	fmt.Fprintf(out, "  Pool:          %s\n", shortID(m.PoolID))
	if m.UID != "" {
		fmt.Fprintf(out, "  UID:           %s\n", m.UID)
	}
	fmt.Fprintf(out, "  Status:        %s\n", m.Status)
	fmt.Fprintf(out, "  Resource Mode: %s\n", m.ResourceMode)
	fmt.Fprintf(out, "  MIG Slices:    %d\n", len(m.MIGSlices))
	if m.MaxClients > 0 {
		fmt.Fprintf(out, "  Max Clients:   %d\n", m.MaxClients)
	}
	fmt.Fprintf(out, "  Created:       %s\n", m.CreatedAt.Format(time.RFC3339))
	writeAttestLine(out, mgr, noAttest)
	fmt.Fprintln(out, "")
}

func renderTenantAllocated(out io.Writer, m *tenants.TenantMember, mgr *tenants.Manager, noAttest bool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl tenant allocate · capacity granted")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Tenant:        %s (%s)\n", shortID(m.ID), m.Name)
	fmt.Fprintf(out, "  Pool:          %s\n", shortID(m.PoolID))
	fmt.Fprintf(out, "  MIG Slices:    %d\n", len(m.MIGSlices))
	if m.MaxClients > 0 {
		fmt.Fprintf(out, "  Max Clients:   %d\n", m.MaxClients)
	}
	fmt.Fprintf(out, "  Updated:       %s\n", m.UpdatedAt.Format(time.RFC3339))
	writeAttestLine(out, mgr, noAttest)
	fmt.Fprintln(out, "")
}
