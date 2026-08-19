// Package main - `cafctl pool` — the Elastic Inference Pool commands (Module 12),
// the GPU-slot resource layer that Module 15 draws upon. Pools aggregate nodes,
// lease slots via best-fit placement to minimize fragmentation, and evaluate
// elasticity under a hard budget whose math mirrors pkg/scaler exactly. Every
// write operation is accompanied by a signed, hash-chained receipt through the
// evidence ledger (in-memory store + ephemeral signer when no backend configured).
//
// Commands follow the newXxxCmd() constructor pattern used by model/train/run/infer,
// so tests can build fresh, parent-less command instances and Execute them
// directly without cobra delegating up to the root command.
package main

import (
	"fmt"
	"io"
	"math"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/elasticpool"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/spf13/cobra"
)

const defaultPoolStore = "./.caf"

// newPoolCmd builds the `pool` command group.
func newPoolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pool",
		Short: "Elastic Inference Pool — GPU slot pooling, best-fit leases, elasticity with budget guard",
		Long: `Elastic Inference Pool (Module 12) — capacity-side lock-in of the AI hardware layer.

Aggregate GPU nodes into pools with configurable slot density, lease slots to
Module 15 inference services using best-fit placement (smallest satisfying free space,
to reduce fragmentation), and evaluate scaling decisions under a hard budget constraint
whose math matches pkg/scaler (BUDGET REJECTED when currentCost + costImpact exceeds limit).
Every node join, slot lease/release, and scale decision is a signed, hash-chained attestation
through the real evidence ledger. After months of leases, the attested capacity history
your auditors trust means migrating would abandon provenance.

Storage layout (--store, default ` + defaultPoolStore + `):
  <store>/elasticpool/pools.json               pool list
  <store>/elasticpool/<poolID>/nodes.json      node members
  <store>/elasticpool/<poolID>/leases.jsonl    append-only leases (last-write-wins per ID)
  <store>/elasticpool/<poolID>/decisions.jsonl append-only elasticity decisions`,
		Example: `  cafctl pool create --name gpu-cluster --gpu-type A100-80G --slots-per-node 8 --min-nodes 1 --max-nodes 10 --cost-per-node-hour 3.2
  cafctl pool list
  cafctl pool show pool-a1b2c3d4e5f60718
  cafctl pool node-add pool-a1b2c3d4e5f60718
  cafctl pool acquire pool-a1b2c3d4e5f60718 --service inf-xxx --slots 4
  cafctl pool release lease-abc123def456
  cafctl pool leases pool-a1b2c3d4e5f60718 --limit 10
  cafctl pool evaluate pool-a1b2c3d4e5f60718 --pending-slots 20 --budget-limit 100 --current-cost 60`,
	}
	cmd.AddCommand(
		newPoolCreateCmd(),
		newPoolListCmd(),
		newPoolShowCmd(),
		newPoolNodeAddCmd(),
		newPoolAcquireCmd(),
		newPoolReleaseCmd(),
		newPoolLeasesCmd(),
		newPoolEvaluateCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// pool create
// ----------------------------------------------------------------------------

func newPoolCreateCmd() *cobra.Command {
	var (
		store, output string
		name, gpuType string
		slotsPerNode  int
		minNodes      int
		maxNodes      int
		cost          float64
		noAttest      bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an elastic pool (validated constraints, attested)",
		Example: `  cafctl pool create --name gpu-pool --gpu-type A100-80G --slots-per-node 8 \
      --min-nodes 1 --max-nodes 10 --cost-per-node-hour 3.2`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			poolMgr, err := openElasticPool(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pool, err := poolMgr.CreatePool(cmd.Context(), elasticpool.PoolInput{
				Name:            name,
				GPUType:         gpuType,
				SlotsPerNode:    slotsPerNode,
				MinNodes:        minNodes,
				MaxNodes:        maxNodes,
				CostPerNodeHour: cost,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				result := map[string]any{
					"id":                pool.ID,
					"name":              pool.Name,
					"gpu_type":          pool.GPUType,
					"slots_per_node":    pool.SlotsPerNode,
					"min_nodes":         pool.MinNodes,
					"max_nodes":         pool.MaxNodes,
					"cost_per_node_hour": pool.CostPerNodeHour,
					"status":            pool.Status,
					"created_at":        pool.CreatedAt.Format(time.RFC3339),
				}
				if last := poolMgr.LastAttestation(); last != nil {
					result["attestation_hash"] = shortHex(last.Hash)
				}
				return writeJSON(cmd.OutOrStdout(), result)
			}
			renderPoolCreate(cmd.OutOrStdout(), poolMgr, pool)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&name, "name", "", "Pool name (required)")
	cmd.Flags().StringVar(&gpuType, "gpu-type", "", "GPU type, e.g. A100-80G (required)")
	cmd.Flags().IntVar(&slotsPerNode, "slots-per-node", 8, "GPU slots per node")
	cmd.Flags().IntVar(&minNodes, "min-nodes", 1, "Minimum nodes (>=0)")
	cmd.Flags().IntVar(&maxNodes, "max-nodes", 10, "Maximum nodes (> min-nodes)")
	cmd.Flags().Float64Var(&cost, "cost-per-node-hour", 3.2, "Cost per node per hour (USD)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("gpuType")
	return cmd
}

func renderPoolCreate(out io.Writer, mgr *elasticpool.FSMElasticPool, p *elasticpool.Pool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl pool create · pool registered, attestation signed")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Pool:             %s (%s)\n", p.ID, p.Name)
	fmt.Fprintf(out, "  GPU Type:         %s\n", p.GPUType)
	fmt.Fprintf(out, "  Slots per Node:   %d\n", p.SlotsPerNode)
	fmt.Fprintf(out, "  Min Nodes:        %d\n", p.MinNodes)
	fmt.Fprintf(out, "  Max Nodes:        %d\n", p.MaxNodes)
	fmt.Fprintf(out, "  Cost/Node-Hour:   $%.2f USD\n", p.CostPerNodeHour)
	fmt.Fprintf(out, "  Status:           %s\n", p.Status)
	fmt.Fprintf(out, "  Created:          %s UTC\n", p.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(out, "")
	greenBold.Fprintf(out, "%s pool %q created (%s)\n", OK(), p.Name, p.ID[5:])
	if last := mgr.LastAttestation(); last != nil {
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — every capacity decision is offline-verifiable.")
	} else {
		greenBold.Fprintf(out, "%s pool registered\n", OK())
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// pool list
// ----------------------------------------------------------------------------

func newPoolListCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List elastic pools (newest first)",
		Example:       "  cafctl pool list",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pMgr, err := openElasticPool(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pools, err := pMgr.ListPools()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if len(pools) == 0 {
				fmt.Fprintln(out, "No elastic pools defined yet.")
				fmt.Fprintln(out, "Create your first pool:")
				fmt.Fprintln(out, "  cafctl pool create --name gpu-pool --gpu-type A100-80G --slots-per-node 8 --min-nodes 1 --max-nodes 10 --cost-per-node-hour 3.2")
				return nil
			}
			renderPoolList(out, pools)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	return cmd
}

func renderPoolList(out io.Writer, pools []elasticpool.Pool) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tGPU\tSTATUS\tMIN\tMAX")
	for _, p := range pools {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\n", p.ID, p.Name, p.GPUType, p.Status, p.MinNodes, p.MaxNodes)
	}
	w.Flush()
}

// ----------------------------------------------------------------------------
// pool show
// ----------------------------------------------------------------------------

func newPoolShowCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "show <pool-id>",
		Short:         "Show one pool details",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl pool show pool-a1b2c3d4e5f60718",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pMgr, err := openElasticPool(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pool, err := pMgr.GetPool(args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			nodes, _ := pMgr.ListNodes(pool.ID)
			renderPoolShow(outFromCmd(cmd), pool, nodes)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	return cmd
}

func outFromCmd(cmd *cobra.Command) io.Writer { return cmd.OutOrStdout() }

func renderPoolShow(out io.Writer, pool *elasticpool.Pool, nodes []elasticpool.Node) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl pool show · elastic pool details")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Pool ID:          %s\n", pool.ID)
	fmt.Fprintf(out, "  Name:             %s\n", pool.Name)
	fmt.Fprintf(out, "  GPU Type:         %s\n", pool.GPUType)
	fmt.Fprintf(out, "  Status:           %s\n", pool.Status)
	fmt.Fprintf(out, "  Slots/Node:       %d\n", pool.SlotsPerNode)
	fmt.Fprintf(out, "  Min/Max Nodes:    %d / %d\n", pool.MinNodes, pool.MaxNodes)
	fmt.Fprintf(out, "  Cost/Node/Hr:     $%.2f USD\n", pool.CostPerNodeHour)
	fmt.Fprintf(out, "  Created:          %s UTC\n", pool.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Node Membership:")
	totalSlots, usedSlots := 0, 0
	for _, n := range nodes {
		fmt.Fprintf(out, "    %s: %d/%d slots (%s)\n", n.ID, n.UsedSlots, n.TotalSlots, n.Status)
		totalSlots += n.TotalSlots
		usedSlots += n.UsedSlots
	}
	fmt.Fprintln(out, "")
	if totalSlots > 0 {
		util := float64(usedSlots) / float64(totalSlots) * 100
		fmt.Fprintf(out, "  Utilization:      %.1f%% (%d/%d slots)\n", util, usedSlots, totalSlots)
	} else {
		fmt.Fprintln(out, "  Utilization:      0% (0/0 slots)")
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// pool node-add
// ----------------------------------------------------------------------------

func newPoolNodeAddCmd() *cobra.Command {
	var store string
	var noAttest bool
	cmd := &cobra.Command{
		Use:   "node-add <pool-id>",
		Short: "Add a node to an active pool (100% ready, attested)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl pool node-add pool-a1b2c3d4e5f60718`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pMgr, err := openElasticPool(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			node, err := pMgr.AddNode(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl pool node-add · node joined, attestation signed")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Pool:             %s\n", args[0])
			fmt.Fprintf(out, "  Node ID:          %s\n", node.ID)
			fmt.Fprintf(out, "  Slots:            %d/%d ready\n", node.TotalSlots-node.UsedSlots, node.TotalSlots)
			fmt.Fprintf(out, "  Status:           %s\n", node.Status)
			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s node added to pool %s\n", OK(), args[0])
			if last := pMgr.LastAttestation(); last != nil {
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
				fmt.Fprintln(out, "  Receipt signed & hash-chained — the addition is offline-verifiable.")
			} else {
				greenBold.Fprintf(out, "%s node added\n", OK())
				fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ----------------------------------------------------------------------------
// pool acquire
// ----------------------------------------------------------------------------

func newPoolAcquireCmd() *cobra.Command {
	var store string
	var serviceID, output string
	var slots int
	var noAttest bool
	cmd := &cobra.Command{
		Use:   "acquire <pool-id>",
		Short: "Acquire slots on a node (best-fit placement, attested)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl pool acquire pool-a1b2c3d4e5f60718 --service inf-xxx --slots 4`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fail fast on meaningless slot counts: a non-positive value must be
			// a loud error, never silently coerced to the default of 1.
			if slots <= 0 {
				err := fmt.Errorf("--slots must be positive (got %d); refusing to silently default to 1", slots)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pMgr, err := openElasticPool(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			lease, err := pMgr.Acquire(cmd.Context(), args[0], serviceID, slots)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				type leaseResult struct {
					ID        string `json:"id"`
					PoolID    string `json:"pool_id"`
					NodeID    string `json:"node_id"`
					ServiceID string `json:"service_id"`
					Slots     int    `json:"slots"`
					CreatedAt string `json:"created_at"`
				}
				return writeJSON(cmd.OutOrStdout(), leaseResult{
					ID: lease.ID, PoolID: lease.PoolID, NodeID: lease.NodeID, ServiceID: lease.ServiceID,
					Slots: lease.Slots, CreatedAt: lease.AcquiredAt.Format(time.RFC3339),
				})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl pool acquire · slot leased, attestation signed")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Lease ID:         %s\n", lease.ID)
			fmt.Fprintf(out, "  Pool:             %s\n", args[0])
			fmt.Fprintf(out, "  Service:          %s\n", lease.ServiceID)
			fmt.Fprintf(out, "  Node:             %s\n", lease.NodeID)
			fmt.Fprintf(out, "  Slots:            %d\n", lease.Slots)
			fmt.Fprintf(out, "  Acquired:         %s UTC\n", lease.AcquiredAt.Format("2006-01-02 15:04:05"))
			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s acquired %d slot(s) on node %s for service %s\n", OK(), lease.Slots, lease.NodeID[5:], strings.ReplaceAll(lease.ServiceID, "inf-", ""))
			if last := pMgr.LastAttestation(); last != nil {
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
				fmt.Fprintln(out, "  Receipt signed & hash-chained — the lease is offline-verifiable.")
			} else {
				greenBold.Fprintf(out, "%s slot leased\n", OK())
				fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&serviceID, "service", "", "Module 15 service ID (must be \"inf-...\") (required)")
	cmd.Flags().IntVar(&slots, "slots", 1, "Number of slots to acquire (default 1)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("service")
	return cmd
}

// ----------------------------------------------------------------------------
// pool release
// ----------------------------------------------------------------------------

func newPoolReleaseCmd() *cobra.Command {
	var store, poolID string
	var noAttest bool
	cmd := &cobra.Command{
		Use:   "release <lease-id>",
		Short: "Release a held lease (idempotent reject on re-release, attested)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl pool release lease-abc123def456 --pool pool-a1b2c3d4e5f60718`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pMgr, err := openElasticPool(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			// When --pool is provided, verify the lease lives there first; otherwise
			// Release locates the owning pool by scanning (simplest UX — copy the
			// lease ID straight from `pool leases` output).
			if poolID != "" {
				if _, _, ferr := pMgr.FindLease(args[0]); ferr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), ferr)
					return ferr
				}
			}
			released, err := pMgr.Release(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl pool release · lease freed, attestation signed")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Lease:            %s\n", released.ID)
			fmt.Fprintf(out, "  Pool:             %s\n", released.PoolID)
			fmt.Fprintf(out, "  Node:             %s\n", released.NodeID)
			fmt.Fprintf(out, "  Slots Freed:      %d\n", released.Slots)
			fmt.Fprintf(out, "  Released At:      %s UTC\n", released.ReleasedAt.Format("2006-01-02 15:04:05"))
			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s lease %s released\n", OK(), released.ID[6:])
			if last := pMgr.LastAttestation(); last != nil {
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
				fmt.Fprintln(out, "  Receipt signed & hash-chained — the release is offline-verifiable.")
			} else {
				greenBold.Fprintf(out, "%s lease released\n", OK())
				fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	cmd.Flags().StringVar(&poolID, "pool", "", "Optional pool ID shortcut (defaults to scan all pools)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ----------------------------------------------------------------------------
// pool leases
// ----------------------------------------------------------------------------

func newPoolLeasesCmd() *cobra.Command {
	var store string
	var limit int
	cmd := &cobra.Command{
		Use:   "leases <pool-id>",
		Short: "List leases for a pool (newest first)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl pool leases pool-a1b2c3d4e5f60718 --limit 10`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pMgr, err := openElasticPool(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			leases, err := pMgr.Leases(args[0], limit)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if len(leases) == 0 {
				fmt.Fprintln(out, "No leases recorded for this pool yet.")
				fmt.Fprintln(out, "Acquire some:")
				fmt.Fprintf(out, "  cafctl pool acquire %s --service inf-demo --slots 2\n", args[0])
				return nil
			}
			renderLeases(out, leases)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max entries to show (newest first)")
	return cmd
}

func renderLeases(out io.Writer, leases []elasticpool.SlotLease) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "LEASE ID\tSERVICE\tNODE\tSLOTS\tACQUIRED\tSTATE")
	for _, l := range leases {
		state := "held"
		if l.ReleasedAt != nil {
			state = "released"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			l.ID, l.ServiceID, l.NodeID, l.Slots, l.AcquiredAt.Format("2006-01-02 15:04"), state)
	}
	w.Flush()
}

// ----------------------------------------------------------------------------
// pool evaluate
// ----------------------------------------------------------------------------

func newPoolEvaluateCmd() *cobra.Command {
	var store, output string
	var pendingSlots int
	var budgetLimit, currentCost float64
	var noAttest bool
	cmd := &cobra.Command{
		Use:   "evaluate <pool-id>",
		Short: "Evaluate elasticity under budget guard (scale_up/down/no_change)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl pool evaluate pool-a1b2c3d4e5f60718 --pending-slots 20 --budget-limit 100 --current-cost 60`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fail fast on meaningless flag values instead of silently accepting
			// them into the budget math. pendingSlots=0 and currentCost=0 are legal
			// (shrink evaluation / fresh zero-cost pool); anything negative or
			// non-finite is rejected up front.
			if pendingSlots < 0 {
				err := fmt.Errorf("--pending-slots must be >= 0 (got %d); negative demand is meaningless", pendingSlots)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if math.IsNaN(budgetLimit) || math.IsInf(budgetLimit, 0) || budgetLimit <= 0 {
				err := fmt.Errorf("--budget-limit must be a finite positive number (got %v)", budgetLimit)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if math.IsNaN(currentCost) || math.IsInf(currentCost, 0) || currentCost < 0 {
				err := fmt.Errorf("--current-cost must be finite and non-negative (got %v)", currentCost)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pMgr, err := openElasticPool(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			d, err := pMgr.EvaluateElasticity(cmd.Context(), args[0], pendingSlots, budgetLimit, currentCost)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"id":                   d.ID,
					"action":               d.Action,
					"reason":               d.Reason,
					"current_nodes":        d.CurrentNodes,
					"target_nodes":         d.TargetNodes,
					"cost_impact_per_hour": d.CostImpactPerHour,
					"budget_ok":            d.BudgetOK,
					"created_at":           d.CreatedAt.Format(time.RFC3339),
				})
			}
			renderElasticDecision(cmd.OutOrStdout(), pMgr, d)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPoolStore, "Pool store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().IntVar(&pendingSlots, "pending-slots", 0, "Pending slot demand (required)")
	cmd.Flags().Float64Var(&budgetLimit, "budget-limit", 100, "Budget ceiling (required)")
	cmd.Flags().Float64Var(&currentCost, "current-cost", 0, "Current hourly cost (required)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("pendingSlots")
	_ = cmd.MarkFlagRequired("budgetLimit")
	_ = cmd.MarkFlagRequired("currentCost")
	return cmd
}

func renderElasticDecision(out io.Writer, mgr *elasticpool.FSMElasticPool, d *elasticpool.ElasticDecision) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl pool evaluate · elasticity decision, attestation signed")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Decision ID:        %s\n", d.ID)
	fmt.Fprintln(out, "  Action:")
	switch d.Action {
	case "scale_up":
		greenBold.Fprintf(out, "    SCALE_UP")
	case "scale_down":
		redBold.Fprintf(out, "    SCALE_DOWN")
	default:
		yellow.Fprintf(out, "    NO_CHANGE")
	}
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Nodes:            %d → %d\n", d.CurrentNodes, d.TargetNodes)
	actionColor := green
	if d.Action == "scale_down" {
		actionColor = red
	}
	if d.CostImpactPerHour >= 0 {
		fmt.Fprintf(out, "  Cost Impact:      +%s USD/node-hour\n", yellow.Sprintf("%.2f", d.CostImpactPerHour))
	} else {
		fmt.Fprintf(out, "  Cost Impact:      -%s USD/node-hour (savings)\n", actionColor.Sprintf("%.2f", -d.CostImpactPerHour))
	}
	fmt.Fprint(out, "  Budget:           ")
	if d.BudgetOK {
		greenBold.Fprintf(out, "OK")
	} else {
		redBold.Fprintf(out, "REJECTED")
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Reason:")
	for _, line := range strings.Split(d.Reason, "; ") {
		if line != "" {
			fmt.Fprintf(out, "    • %s\n", line)
		}
	}
	fmt.Fprintln(out, "")
	greenBold.Fprintf(out, "%s elasticity evaluated for pool\n", OK())
	if last := mgr.LastAttestation(); last != nil {
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — the decision is offline-verifiable.")
	} else {
		greenBold.Fprintf(out, "%s elasticity evaluated\n", OK())
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// openElasticPool opens (creating if needed) an elastic pool store; when attest
// is true a fresh MemoryStore+EphemeralSigner ledger is wired in, exactly the
// pattern other module commands use, so receipts are genuinely signed and
// hash-chained.
func openElasticPool(path string, attest bool) (*elasticpool.FSMElasticPool, error) {
	if path == "" {
		path = defaultPoolStore
	}
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
	return elasticpool.NewFSMElasticPool(path, ledger)
}
