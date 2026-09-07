// Package main - cafctl capability check subcommand (M51 Capability Gate).
//
// This command surfaces real, offline hardware capability detection:
//
//   - capability check <resource> --action=<action> (M51) — checks whether the
//     current system has the required hardware capabilities to perform an action
//     on a resource. It performs real-time SGX/GPU/eBPF detection and reports
//     graceful degradation options.
//
// All operations are local and deterministic; no network calls are performed.
package main

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/spf13/cobra"
)

func newCapabilityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capability",
		Short: "Hardware capability detection and gate checks",
	}
	cmd.AddCommand(newCapCheckCmd())
	return cmd
}

// newCapCheckCmd implements `cafctl capability check <resource>`
func newCapCheckCmd() *cobra.Command {
	var resource string
	var action string

	cmd := &cobra.Command{
		Use:   "check <resource>",
		Short: "Check capability gate for resource access",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl capability check sgx --action=enclave-create
  cafctl capability check gpu --action=inference
  cafctl capability check ebpf --action=monitoring`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			resource = args[0]
			ctx := context.Background()

			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl capability check · hardware capability gate (M51)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Detect all hardware capabilities
			detector := capability.NewDetector()
			flags := detector.DetectAll(ctx)

			fmt.Fprintln(out, "Hardware Capability Scan:")
			fmt.Fprintf(out, "  Hypervisor: %s\n", detectHypervisorString(flags.Hypervisor))
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Capability Status:")
			
			// Check SGX
			sgxStatus := getStatusSymbol(flags.SGX.Available)
			fmt.Fprintf(out, "  %-12s %s SGX %s (%s)\n", "TEE", sgxStatus, 
				formatBool(flags.SGX.Available), flags.SGX.Version)
			if !flags.SGX.Available {
				fmt.Fprintf(out, "                    └─ Fallback: software attestation\n")
			}

			// Check GPU
			gpuStatus := getStatusSymbol(flags.GPU.Available)
			fmt.Fprintf(out, "  %-12s %s GPU %s (%s)\n", "AI/Compute", gpuStatus,
				formatBool(flags.GPU.Available), formatGPUName(flags.GPU.Model))
			if flags.GPU.Available {
				fmt.Fprintf(out, "                    ├─ VRAM: %d MB\n", flags.GPU.VRAMMB)
				fmt.Fprintf(out, "                    ├─ MIG: %s\n", formatBool(flags.GPU.MIGSupported))
			} else {
				fmt.Fprintf(out, "                    └─ Fallback: CPU-only mode\n")
			}

			// Check eBPF
			ebpfStatus := getStatusSymbol(flags.EBPF.Available)
			levels := map[int]string{1: "minimal", 2: "full", 3: "advanced"}
			fmt.Fprintf(out, "  %-12s %s eBPF %s (%s support)\n", "Observability", ebpfStatus,
				formatBool(flags.EBPF.Available), levels[flags.EBPF.SupportLevel])
			if flags.EBPF.KernelVersion != "" {
				fmt.Fprintf(out, "                    └─ Kernel: %s\n", flags.EBPF.KernelVersion)
			}
			if !flags.EBPF.Available {
				fmt.Fprintf(out, "                    └─ Fallback: userspace metrics\n")
			}
			fmt.Fprintln(out, "")

			// Check specific resource/action combination
			fmt.Fprintln(out, "Resource Access Check:")
			fmt.Fprintf(out, "  Resource: %s\n", resource)
			fmt.Fprintf(out, "  Action: %s\n", action)
			fmt.Fprintln(out, "")

			checkResult := checkCapabilityForAction(resource, action, flags)
			
			if checkResult.Allowed {
				fmt.Fprintf(out, "%s Access permitted.\n", OK())
			} else {
				fmt.Fprintf(out, "%s Access denied or degraded.\n", ERROR())
			}
			fmt.Fprintln(out, "")

			if len(checkResult.Degradations) > 0 {
				fmt.Fprintln(out, "Graceful Degradation Plan:")
				for _, deg := range checkResult.Degradations {
					fmt.Fprintf(out, "  • %s → %s\n", deg.Feature, deg.Fallback)
				}
				fmt.Fprintln(out, "")
			}

			fmt.Fprintln(out, "Full Feature Flags:")
			fmt.Fprintf(out, "  Committed: %v\n", flags.Committed)
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Capability check complete.\n", OK())
			fmt.Fprintln(out, "")

			return nil
		},
	}
	cmd.Flags().StringVar(&action, "action", "", "Action to check (required)")
	_ = cmd.MarkFlagRequired("action")
	return cmd
}

// CapCheckResult represents the result of a capability check
type CapCheckResult struct {
	Allowed       bool
	Degradations  []DegradationInfo
	Message       string
}

// DegradationInfo describes a fallback option
type DegradationInfo struct {
	Feature string
	Fallback string
}

// checkCapabilityForAction determines if an action is allowed on a resource
func checkCapabilityForAction(resource, action string, flags capability.FeatureFlags) CapCheckResult {
	result := CapCheckResult{}

	// Define resource-action requirements
	switch resource {
	case "sgx", "tee", "attestation":
		switch action {
		case "enclave-create", "secure-compute", "attest":
			if flags.SGX.Available {
				result.Allowed = true
				result.Message = "SGX enclave creation permitted with TEE protection"
			} else {
				result.Allowed = false
				result.Degradations = append(result.Degradations, DegradationInfo{
					Feature: "TEE security",
					Fallback: "software attestation fallback",
				})
				result.Message = "Software attestation will be used instead of SGX"
			}
		default:
			result.Allowed = true
			result.Message = "SGX not required for this action"
		}

	case "gpu", "accelerator", "compute":
		switch action {
		case "inference", "training", "matrix-op":
			if flags.GPU.Available {
				result.Allowed = true
				result.Message = fmt.Sprintf("GPU acceleration available: %s", formatGPUName(flags.GPU.Model))
				if flags.GPU.MIGSupported {
					result.Message += " with MIG partitioning"
				}
			} else {
				result.Allowed = false
				result.Degradations = append(result.Degradations, DegradationInfo{
					Feature: "GPU acceleration",
					Fallback: "CPU-only mode",
				})
				result.Message = "Will run on CPU only (slower)"
			}
		default:
			result.Allowed = true
			result.Message = "GPU not required for this action"
		}

	case "ebpf", "observability", "monitoring":
		switch action {
		case "monitoring", "tracing", "metrics":
			if flags.EBPF.Available {
				result.Allowed = true
				levelDesc := []string{"", "minimal", "full", "advanced"}[flags.EBPF.SupportLevel]
				result.Message = fmt.Sprintf("eBPF %s observability enabled", levelDesc)
			} else {
				result.Allowed = false
				result.Degradations = append(result.Degradations, DegradationInfo{
					Feature: "eBPF observability",
					Fallback: "userspace metrics",
				})
				result.Message = "Using userspace metrics collection"
			}
		default:
			result.Allowed = true
			result.Message = "eBPF not required for this action"
		}

	default:
		result.Allowed = true
		result.Message = fmt.Sprintf("No specific capability requirements for resource %q", resource)
	}

	return result
}

// Helper functions
func getStatusSymbol(ok bool) string {
	if ok {
		return greenBold.Sprint("✓ AVAILABLE")
	}
	return redBold.Sprint("✗ NOT FOUND")
}

func formatBool(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func formatGPUName(model string) string {
	if model == "" {
		return "none"
	}
	// Truncate long names
	if len(model) > 50 {
		return model[:47] + "..."
	}
	return model
}

func detectHypervisorString(hyp string) string {
	if hyp == "" {
		return "bare-metal"
	}
	return hyp
}
