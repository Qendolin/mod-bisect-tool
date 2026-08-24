package app

import (
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// GenerateLogReport creates a detailed plain-text summary of the entire
// bisection process, including the execution history and all conflict sets,
// suitable for logging.
func GenerateLogReport(bvm ui.BisectionViewModel, hvm ui.ExecutionLogViewModel, rvm ui.ResultViewModel) string {
	var builder strings.Builder

	// --- Section 1: Overview ---
	builder.WriteString("===== Bisection Overview =====\n")
	writeOverview(&builder, &bvm, &rvm)
	builder.WriteString("\n")

	// --- Section 2: Detailed Execution History ---
	builder.WriteString("===== Bisection History (Execution Log) =====\n")
	writeExecutionHistory(&builder, &hvm, &bvm)
	builder.WriteString("\n")

	// --- Section 3: Final Conflict Sets ---
	builder.WriteString("===== Final Conflict Sets =====\n")
	writeFinalConflictSets(&builder, &rvm)
	builder.WriteString("\n")

	// --- Section 4: Remaining Search State ---
	builder.WriteString("===== Remaining Search State =====\n")
	writeRemainingState(&builder, &bvm)

	return builder.String()
}

// writeOverview summarizes the overall bisection state.
func writeOverview(b *strings.Builder, bvm *ui.BisectionViewModel, rvm *ui.ResultViewModel) {
	if !bvm.IsReady {
		b.WriteString("Bisection has not been started.\n")
		return
	}

	status := "Complete"
	switch {
	case bvm.Progress.IsComplete:
		status = "Complete"
	case bvm.Progress.StepCount == 0:
		status = "Not started"
	default:
		status = "In Progress"
	}
	fmt.Fprintf(b, "Status: %s\n", status)
	fmt.Fprintf(b, "Round: %d | Iteration: %d\n", bvm.Progress.Round, bvm.Progress.Iteration)
	fmt.Fprintf(b, "Tests executed: %d of ~%d estimated\n", bvm.Progress.StepCount, bvm.Progress.EstimatedMaxTests)
	if bvm.Progress.LastTestResult != "" {
		fmt.Fprintf(b, "Last test result: %s\n", bvm.Progress.LastTestResult)
	}
	if bvm.Progress.LastFoundElement != "" {
		fmt.Fprintf(b, "Last found conflict element: %s\n", bvm.Progress.LastFoundElement)
	}
	if rvm.IsVerificationStep {
		b.WriteString("Awaiting a verification step to confirm the current conflict set.\n")
	}
	fmt.Fprintf(b, "Total mods in folder: %d\n", len(bvm.Mods.All))
	fmt.Fprintf(b, "Preferred loader (cli): %s\n", bvm.Loader.Preferred.String())
	if bvm.Loader.Chosen != "" {
		fmt.Fprintf(b, "Loader used: %s\n", bvm.Loader.Chosen.String())
	}
	if rvm.CanContinueSearch {
		fmt.Fprintf(b, "More independent conflict sets may exist: %d candidate(s) remain.\n", len(bvm.Sets.Candidate))
	}
}

// writeExecutionHistory renders every completed test with full context.
func writeExecutionHistory(b *strings.Builder, hvm *ui.ExecutionLogViewModel, bvm *ui.BisectionViewModel) {
	if len(hvm.Entries) == 0 {
		b.WriteString("No tests were executed.\n")
		return
	}

	for i, entry := range hvm.Entries {
		isVerification := entry.Kind == imcs.TestPlanVerification
		testSet := entry.Plan.ModIDsToTest

		verificationTag := ""
		if isVerification {
			verificationTag = " [VERIFICATION]"
		}

		fmt.Fprintf(b, "#%-3d: Step %-3d | Round %d, Iter %d | Test(%d mods)%s -> %s\n",
			i+1,
			entry.Step,
			entry.Round,
			entry.Iteration,
			len(testSet),
			verificationTag,
			entry.Result,
		)

		fmt.Fprintf(b, "    Tested mods: %s\n", formatModList(sets.MakeSlice(testSet), bvm.Mods.Infos))

		// Context captured before the test was executed.
		fmt.Fprintf(b, "    Conflict set so far (%d): %s\n",
			len(entry.ConflictSet), formatModList(sets.MakeSlice(entry.ConflictSet), bvm.Mods.Infos))
		fmt.Fprintf(b, "    Candidate set before test (%d): %s\n",
			len(entry.Candidates), formatModList(sets.MakeSlice(entry.Candidates), bvm.Mods.Infos))
		fmt.Fprintf(b, "    Cleared set before test (%d): %s\n",
			len(entry.ClearedSet), formatModList(sets.MakeSlice(entry.ClearedSet), bvm.Mods.Infos))
	}
}

// writeFinalConflictSets renders all conflict sets, including the current
// (most recent) one, which is only archived into ArchivedConflictSets once
// ContinueSearch runs. It also includes the generally unresolvable mods.
func writeFinalConflictSets(b *strings.Builder, rvm *ui.ResultViewModel) {
	conflictSets := rvm.ArchivedConflictSets
	if len(rvm.CurrentConflict.Mods) > 0 {
		conflictSets = append(conflictSets, rvm.CurrentConflict)
	}

	if len(conflictSets) == 0 {
		b.WriteString("Bisection completed. No problematic mods were found.\n")
		return
	}

	fmt.Fprintf(b, "Found %d conflict set(s).\n", len(conflictSets))

	plain := ui.TextStyles{ShowFile: true}

	for i, conflictSet := range conflictSets {
		fmt.Fprintf(b, "\n--- Conflict Set #%d ---\n", i+1)
		if i < len(rvm.ArchivedConflictSets) {
			b.WriteString("  (archived from a previous round)\n")
		} else {
			b.WriteString("  (current round)\n")
		}
		ui.WriteConflictSet(b, conflictSet, plain)
	}

	// Generally unresolvable mods (dependency issues unrelated to conflicts).
	if len(rvm.GenerallyUnresolvable) > 0 {
		b.WriteString("\n--- Mods with unresolved dependencies (may need manual review) ---\n")
		ui.WriteGenerallyUnresolvable(b, rvm.GenerallyUnresolvable, plain)
	}
}

// writeRemainingState reports the sets that would be relevant for continuing
// the search.
func writeRemainingState(b *strings.Builder, bvm *ui.BisectionViewModel) {
	if !bvm.IsReady {
		b.WriteString("Bisection has not been started.\n")
		return
	}

	fmt.Fprintf(b, "Candidate set remaining (%d): %s\n",
		len(bvm.Sets.Candidate), formatModList(sets.MakeSlice(bvm.Sets.Candidate), bvm.Mods.Infos))
	fmt.Fprintf(b, "Cleared set (%d): %s\n",
		len(bvm.Sets.Cleared), formatModList(sets.MakeSlice(bvm.Sets.Cleared), bvm.Mods.Infos))
	if len(bvm.Sets.PendingAddition) > 0 {
		fmt.Fprintf(b, "Pending additions (will re-enter the search pool next iteration) (%d): %s\n",
			len(bvm.Sets.PendingAddition), formatModList(sets.MakeSlice(bvm.Sets.PendingAddition), bvm.Mods.Infos))
	}
}

// formatModList renders a set of mod IDs as a comma-separated list, enriching
// each entry with the mod's uniform reference format.
func formatModList(modIDs []string, modsInfo map[string]ui.ModViewModel) string {
	if len(modIDs) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(modIDs))
	for _, id := range modIDs {
		if mod, ok := modsInfo[id]; ok {
			parts = append(parts, ui.FormatModRef(mod))
		} else {
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, ", ")
}
