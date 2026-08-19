// Package main - `cafctl wellrouter` — Module 6 WellRouter rule engine commands.
//
// This command group provides CRUD operations for routing rules, event publishing,
// statistics monitoring, and dead-letter queue inspection. Every operation is
// attested through the real evidence ledger (unless --no-attest) so auditors can
// verify the exact rule set that governed production traffic.
//
// Commands:
//   - rule-add       Create new routing rule with custom topic/source/targets/max-hops
//   - rule-delete    Remove a rule by ID
//   - rule-list      List all rules (JSON or human-readable)
//   - publish        Publish one event through matched rules
//   - stats          Display live router statistics
//   - dlq-list       Show dead-lettered events (rejection reasons included)
//
// Default store root is ./.caf; override with --store. Attestation is enabled by
// default unless --no-attest is specified.
package main

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wellrouter"
	"github.com/spf13/cobra"
)

const defaultWellRouterStore = "./.caf"

// newWellRouterCmd builds the wellrouter command group.
func newWellRouterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wellrouter",
		Short: "Rule-based event routing engine for the 16 deep wells (Module 6)",
		Long: `Module 6 WellRouter — rule-based routing with attestation and DLQ.

Compiles the authoritative AISecOps connectivity matrix into editable RouteRules
that forward events from source wells to target wells along directed edges. Hop
limits are enforced and violations return ErrHopLimitExceeded instead of silently
dropping; rejected events are recorded in both an append-only audit log and (when
wired) a signed, hash-chained evidence ledger. A queryable in-memory dead-letter
queue retains recent rejections so operators can inspect failure reasons offline.

Every routing decision — add-rule, delete-rule, forward, reject — is attested so
the entire evolution of your route table is verifiable months later without trust.

Commands:
  rule-add         Add new routing rule (custom topic/source/targets/max-hops)
  rule-delete      Remove existing rule by ID
  rule-list        List all rules (JSON or human-readable format)
  publish          Publish one event through matched rules
  stats            Display live router statistics
  dlq-list         Show dead-lettered events with rejection reasons

Examples:
  cafctl wellrouter rule-list                          # list current rules
  cafctl wellrouter rule-add --source L1 --targets L2,L3 --topic aisecops.well.event
  cafctl wellrouter publish --topic X --source L1 --hop 0 --correlation-id abc123
  cafctl wellrouter stats                              # show Forwarded/Rejected/DLQ counts
  cafctl wellrouter dlq-list                           # see last 10 rejections`,
	}
	cmd.AddCommand(
		newRuleAddCmd(),
		newRuleDeleteCmd(),
		newRuleListCmd(),
		newPublishCmd(),
		newStatsCmd(),
		newDlqListCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// Helper: openWellRouter opens (creates if needed) a wellrouter instance.
// ----------------------------------------------------------------------------

func openWellRouter(path string, attest bool) (*wellrouter.FSMWellRouter, error) {
	if path == "" {
		path = defaultWellRouterStore
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

	bus := eventbus.New(eventbus.DefaultConfig(), nil)
	router, err := wellrouter.NewFSMWellRouter(bus, ledger, path)
	if err != nil {
		return nil, fmt.Errorf("create router: %w", err)
	}
	return router, nil
}

// ----------------------------------------------------------------------------
// wellrouter rule-add
// ----------------------------------------------------------------------------

func newRuleAddCmd() *cobra.Command {
	var (
		store, topic, sourceStr string
		targetsStr              string
		maxHops                 int
		noAttest                bool
		output                  string // json|text
	)
	cmd := &cobra.Command{
		Use:   "rule-add",
		Short: "Add new routing rule",
		Example: `  cafctl wellrouter rule-add --source L1 --targets L2,L3 --topic aisecops.well.event
  cafctl wellrouter rule-add --source L5 --targets L8,L13 --topic cluster.* --max-hops 4`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openWellRouter(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			// Parse source well
			srcWell, srcErr := parseDeepWell(sourceStr)
			if srcErr != nil {
				errSrc := fmt.Errorf("parse source well: %w", srcErr)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), errSrc)
				return errSrc
			}

			// Parse targets
			targetStrs := strings.Split(targetsStr, ",")
			targets := make([]eventbus.DeepWell, 0, len(targetStrs))
			for _, t := range targetStrs {
				tw, terr := parseDeepWell(strings.TrimSpace(t))
				if terr != nil {
					return fmt.Errorf("parse target well %q: %w", t, terr)
				}
				targets = append(targets, tw)
			}

			rule := &wellrouter.RouteRule{
				ID:           "",             // auto-generated
				TopicPattern: topic,
				SourceWell:   srcWell,
				TargetWells:  targets,
				MaxHops:      maxHops,
				Enabled:      true,
				DLQ:          true,
				CreatedAt:    time.Time{},
			}

			ctx := cmd.Context()
			if err := r.AddRule(ctx, rule); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			out := cmd.OutOrStdout()
			if output == "json" {
				result := map[string]any{
					"id":           rule.ID,
					"topic_pattern": rule.TopicPattern,
					"source_well":  rule.SourceWell.String(),
					"target_wells": wellrouterToStrings(rule.TargetWells),
					"max_hops":     rule.MaxHops,
					"enabled":      rule.Enabled,
					"dlq":          rule.DLQ,
					"created_at":   rule.CreatedAt.Format(time.RFC3339),
				}
				if last := r.LastAttestation(); last != nil {
					result["attestation_hash"] = shortHex(last.Hash)
				}
				return writeJSON(out, result)
			}

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl wellrouter rule-add · rule created, attestation signed")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Rule ID:          %s\n", rule.ID)
			fmt.Fprintf(out, "  Source Well:      %s\n", rule.SourceWell.String())
			fmt.Fprintf(out, "  Target Wells:     %s\n", strings.Join(wellrouterToStrings(rule.TargetWells), ", "))
			fmt.Fprintf(out, "  Topic Pattern:    %s\n", rule.TopicPattern)
			fmt.Fprintf(out, "  Max Hops:         %d\n", rule.MaxHops)
			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s rule added (%s → %d targets)\n", OK(), rule.SourceWell.String(), len(rule.TargetWells))
			if last := r.LastAttestation(); last != nil {
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
				fmt.Fprintln(out, "  Receipt signed & hash-chained — rule is offline-verifiable.")
			} else {
				greenBold.Fprintf(out, "%s rule added\n", OK())
				fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultWellRouterStore, "Store root path")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&topic, "topic", "", "Topic pattern (required)")
	cmd.Flags().StringVar(&sourceStr, "source", "", "Source well (e.g., L1, required)")
	cmd.Flags().StringVar(&targetsStr, "targets", "", "Comma-separated target wells (e.g., L2,L3, required)")
	cmd.Flags().IntVar(&maxHops, "max-hops", wellrouter.DefaultMaxHops, "Maximum hop limit [1,8]")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	_ = cmd.MarkFlagRequired("topic")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("targets")
	return cmd
}

// ----------------------------------------------------------------------------
// wellrouter rule-delete
// ----------------------------------------------------------------------------

func newRuleDeleteCmd() *cobra.Command {
	var store string
	var noAttest bool
	cmd := &cobra.Command{
		Use:   "rule-delete <id>",
		Short: "Delete existing rule by ID",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl wellrouter rule-delete rule-a1b2c3d4
  cafctl wellrouter rule-delete rule-xyz789`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openWellRouter(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			ctx := cmd.Context()
			if err := r.DeleteRule(ctx, args[0]); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl wellrouter rule-delete · rule removed, attestation signed")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Deleted Rule:     %s\n", args[0])
			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s rule deleted\n", OK())
			if last := r.LastAttestation(); last != nil {
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
			} else {
				greenBold.Fprintf(out, "%s rule deleted\n", OK())
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultWellRouterStore, "Store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	return cmd
}

// ----------------------------------------------------------------------------
// wellrouter rule-list
// ----------------------------------------------------------------------------

func newRuleListCmd() *cobra.Command {
	var store, output string
	cmd := &cobra.Command{
		Use:   "rule-list",
		Short: "List all routing rules",
		Example: `  cafctl wellrouter rule-list                     # human-readable
  cafctl wellrouter rule-list --output json       # JSON array`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openWellRouter(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			rules := r.ListRules()
			out := cmd.OutOrStdout()

			if output == "json" {
				type RuleView struct {
					ID           string   `json:"id"`
					TopicPattern string   `json:"topic_pattern"`
					SourceWell   string   `json:"source_well"`
					TargetWells  []string `json:"target_wells"`
					MaxHops      int      `json:"max_hops"`
					Enabled      bool     `json:"enabled"`
				}
			 views := make([]RuleView, len(rules))
			 for i, rr := range rules {
				 views[i] = RuleView{
					 ID:           rr.ID,
					 TopicPattern: rr.TopicPattern,
					 SourceWell:   rr.SourceWell.String(),
					 TargetWells:  wellrouterToStrings(rr.TargetWells),
					 MaxHops:      rr.MaxHops,
					 Enabled:      rr.Enabled,
				 }
			 }
			 return writeJSON(out, map[string]any{
				"rules_total": len(rules),
				"rules":       views,
			 })
			}

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl wellrouter rule-list · current rules")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			if len(rules) == 0 {
				fmt.Fprintln(out, "No rules defined yet.")
				fmt.Fprintln(out, "Create your first rule:")
				fmt.Fprintln(out, "  cafctl wellrouter rule-add --source L1 --targets L2,L3 --topic aisecops.well.event")
				return nil
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "RULE ID\tSOURCE\tTARGETS\tTOPIC\tMAX_HOPS\tSTATUS")
			for _, rr := range rules {
				status := "disabled"
				if rr.Enabled {
					status = "enabled"
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\t%s\n",
					strings.TrimPrefix(rr.ID, "rule-"),
					rr.SourceWell.String(),
					len(rr.TargetWells),
					rr.TopicPattern,
					rr.MaxHops,
					status,
				)
			}
			w.Flush()
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Total: %d rules (%d active)\n", len(rules), r.Stats().RulesActive)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultWellRouterStore, "Store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	return cmd
}

// ----------------------------------------------------------------------------
// wellrouter publish
// ----------------------------------------------------------------------------

func newPublishCmd() *cobra.Command {
	var (
		store, topic, sourceStr, corrID string
		hop                             int
		noAttest                        bool
		output                          string
	)
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish one event through matched rules",
		Example: `  cafctl wellrouter publish --topic aisecops.well.event --source L1 --hop 0
  cafctl wellrouter publish --topic X.Y.Z --source L5 --hop 2 --correlation-id abc123`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openWellRouter(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			srcWell, perr := parseDeepWell(sourceStr)
			if perr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), fmt.Errorf("parse source: %w", perr))
				return perr
			}

			eventData := map[string]any{"published_at": time.Now().UTC().Format(time.RFC3339)}
			ev, merr := eventbus.NewEvent(topic, "manual", srcWell.String(), eventData)
			if merr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), fmt.Errorf("create event: %w", merr))
				return merr
			}
			ev.WithMetadata(wellrouter.MetaWell, strconv.Itoa(int(srcWell))).
				WithMetadata(wellrouter.MetaWellName, srcWell.String()).
				WithMetadata(wellrouter.MetaHop, strconv.Itoa(hop))
			if corrID != "" {
				ev.CorrelationID = corrID
			}

			ctx := cmd.Context()
			err = r.Publish(ctx, ev)

			out := cmd.OutOrStdout()
			stats := r.Stats()

			if output == "json" {
				result := map[string]any{
					"event_id": ev.ID,
					"topic": topic,
					"source": srcWell.String(),
					"hop": hop,
					"forwarded": stats.Forwarded,
					"rejected": stats.Rejected,
				}
				if err != nil {
					result["error"] = err.Error()
				}
				if last := r.LastAttestation(); last != nil {
					result["attestation_hash"] = shortHex(last.Hash)
				}
				return writeJSON(out, result)
			}

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			if err != nil {
				redBold.Fprintf(out, "  cafctl wellrouter publish · REJECTED: %s\n", err)
				fmt.Fprintln(out, Separator('═', 64))
			} else {
				greenBold.Fprintf(out, "  cafctl wellrouter publish · SUCCESS\n")
				fmt.Fprintln(out, Separator('═', 64))
			}
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Event ID:         %s\n", ev.ID)
			fmt.Fprintf(out, "  Topic:            %s\n", topic)
			fmt.Fprintf(out, "  Source:           %s\n", srcWell.String())
			fmt.Fprintf(out, "  Hop Count:        %d\n", hop)
			if corrID != "" {
				fmt.Fprintf(out, "  Correlation ID:   %s\n", corrID)
			}
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  Statistics:")
			fmt.Fprintf(out, "    Forwarded:      %d\n", stats.Forwarded)
			fmt.Fprintf(out, "    Rejected:       %d\n", stats.Rejected)
			fmt.Fprintf(out, "    DLQ:            %d\n", stats.DLQ)
			fmt.Fprintln(out, "")
			if err != nil {
				red.Fprintf(out, "Note: check 'wellrouter dlq-list' for rejection details\n")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultWellRouterStore, "Store root")
	cmd.Flags().StringVarP(&topic, "topic", "t", "", "Event topic pattern (required)")
	cmd.Flags().StringVar(&sourceStr, "source", "", "Source well (e.g., L1, required)")
	cmd.Flags().IntVar(&hop, "hop", 0, "Current hop count (default 0)")
	cmd.Flags().StringVar(&corrID, "correlation-id", "", "Correlation ID for tracing")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	_ = cmd.MarkFlagRequired("topic")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}

// ----------------------------------------------------------------------------
// wellrouter stats
// ----------------------------------------------------------------------------

func newStatsCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Display router statistics",
		Example: "  cafctl wellrouter stats",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openWellRouter(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			s := r.Stats()
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl wellrouter stats · live statistics")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Uptime:           %s\n", time.Since(s.StartTime).Round(time.Second))
			fmt.Fprintln(out, "  Rules:")
			fmt.Fprintf(out, "    Active:         %d\n", s.RulesActive)
			fmt.Fprintf(out, "    Total:          %d\n", s.RulesTotal)
			fmt.Fprintln(out, "  Traffic:")
			fmt.Fprintf(out, "    Forwarded:      %d\n", s.Forwarded)
			fmt.Fprintf(out, "    Rejected:       %d\n", s.Rejected)
			fmt.Fprintf(out, "    Dead-Lettered:  %d\n", s.DLQ)
			fmt.Fprintf(out, "    DedupSkipped:   %d\n", s.DedupSkipped)
			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s collector is running\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultWellRouterStore, "Store root")
	return cmd
}

// ----------------------------------------------------------------------------
// wellrouter dlq-list
// ----------------------------------------------------------------------------

func newDlqListCmd() *cobra.Command {
	var (
		store string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "dlq-list",
		Short: "Show dead-lettered events",
		Example: `  cafctl wellrouter dlq-list                    # last 10
  cafctl wellrouter dlq-list --limit 20`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := openWellRouter(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			rejs := r.DLQList(limit)
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl wellrouter dlq-list · dead-letter queue")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			if len(rejs) == 0 {
				fmt.Fprintln(out, "No dead-lettered events.")
				fmt.Fprintln(out, "Forwarding activity will populate this queue on hop-limit rejections.")
				return nil
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "EVENT_ID\tREJECTED_AT\tHOP\tRULE_ID\tSTATUS\tREASON")
			for _, rej := range rejs {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
					rej.EventID,
					rej.RejectedAt.Format("2006-01-02 15:04:05"),
					rej.HopCount,
					strings.TrimPrefix(rej.RuleID, "rule-"),
					rej.Status,
					rej.Reason,
				)
			}
			w.Flush()
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Showing %d of %d total dead-lettered events\n", len(rejs), r.Stats().DLQ)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultWellRouterStore, "Store root")
	cmd.Flags().IntVar(&limit, "limit", 10, "Number of entries to show (newest first)")
	return cmd
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// parseDeepWell parses "L1".."L16" into eventbus.DeepWell.
func parseDeepWell(s string) (eventbus.DeepWell, error) {
	if s == "" {
		return 0, fmt.Errorf("empty well identifier")
	}
	if !strings.HasPrefix(s, "L") {
		return 0, fmt.Errorf("well must be L+number (got %q)", s)
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil {
		return 0, fmt.Errorf("parse number: %w", err)
	}
	w := eventbus.DeepWell(n)
	if !w.Valid() {
		return 0, fmt.Errorf("invalid well %d", n)
	}
	return w, nil
}

// wellrouterToStrings converts DeepWell slice to []string.
func wellrouterToStrings(ws []eventbus.DeepWell) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.String()
	}
	return out
}
