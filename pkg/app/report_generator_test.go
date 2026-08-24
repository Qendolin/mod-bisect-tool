package app

import (
	"strings"
	"testing"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// TestGenerateLogReportIncludesCurrentConflictSet ensures the report does not
// drop the just-completed conflict set, which lives only in CurrentConflict
// until ContinueSearch archives it.
func TestGenerateLogReportIncludesCurrentConflictSet(t *testing.T) {
	bvm := ui.BisectionViewModel{
		IsReady: true,
		Progress: ui.BisectionProgressViewModel{
			IsComplete: true,
		},
		Mods: ui.ModsViewModel{
			All: []string{"a", "b"},
			Infos: map[string]ui.ModViewModel{
				"a": {ID: "a", Name: "Mod A", Version: "1.0", BaseFilename: "moda"},
				"b": {ID: "b", Name: "Mod B", Version: "2.0", BaseFilename: "modb"},
			},
		},
	}
	rvm := ui.ResultViewModel{
		State: ui.StateComplete,
		CurrentConflict: ui.ConflictSetReport{
			Mods: []ui.CascadingDisables{
				{Mod: ui.ModViewModel{ID: "a", Name: "Mod A", Version: "1.0", BaseFilename: "moda"}},
				{Mod: ui.ModViewModel{ID: "b", Name: "Mod B", Version: "2.0", BaseFilename: "modb"}},
			},
		},
	}

	report := GenerateLogReport(bvm, ui.ExecutionLogViewModel{}, rvm)

	for _, want := range []string{"moda", "modb", "Conflict Set #1", "(current round)"} {
		if !strings.Contains(report, want) {
			t.Errorf("expected report to contain %q, got:\n%s", want, report)
		}
	}
	if strings.Contains(report, "No problematic mods were found") {
		t.Errorf("report wrongly claims no problematic mods were found:\n%s", report)
	}
}

// TestGenerateLogReportIncludesArchivedAndCurrentConflictSets verifies that
// both archived and current conflict sets are rendered and labeled distinctly.
func TestGenerateLogReportIncludesArchivedAndCurrentConflictSets(t *testing.T) {
	bvm := ui.BisectionViewModel{
		IsReady: true,
		Progress: ui.BisectionProgressViewModel{
			IsComplete: true,
		},
		Mods: ui.ModsViewModel{All: []string{"a", "b"}},
	}
	rvm := ui.ResultViewModel{
		State: ui.StateComplete,
		ArchivedConflictSets: []ui.ConflictSetReport{
			{Mods: []ui.CascadingDisables{{Mod: ui.ModViewModel{ID: "a", Name: "Mod A"}}}},
		},
		CurrentConflict: ui.ConflictSetReport{
			Mods: []ui.CascadingDisables{{Mod: ui.ModViewModel{ID: "b", Name: "Mod B"}}},
		},
	}

	report := GenerateLogReport(bvm, ui.ExecutionLogViewModel{}, rvm)

	for _, want := range []string{
		"Found 2 conflict set(s)",
		"Conflict Set #1",
		"Conflict Set #2",
		"(archived from a previous round)",
		"(current round)",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("expected report to contain %q, got:\n%s", want, report)
		}
	}
}

// TestGenerateLogReportNoConflicts verifies the report's empty-result wording.
func TestGenerateLogReportNoConflicts(t *testing.T) {
	bvm := ui.BisectionViewModel{IsReady: true, Progress: ui.BisectionProgressViewModel{IsComplete: true}}
	rvm := ui.ResultViewModel{State: ui.StateComplete}

	report := GenerateLogReport(bvm, ui.ExecutionLogViewModel{}, rvm)
	if !strings.Contains(report, "No problematic mods were found") {
		t.Errorf("expected no-problem message, got:\n%s", report)
	}
}

// TestGenerateLogReportExecutionHistoryDetail verifies that each history entry
// lists the mods that were actually tested, with friendly names.
func TestGenerateLogReportExecutionHistoryDetail(t *testing.T) {
	bvm := ui.BisectionViewModel{
		IsReady: true,
		Mods: ui.ModsViewModel{
			Infos: map[string]ui.ModViewModel{
				"a": {ID: "a", Name: "Mod A", Version: "1.0", BaseFilename: "moda"},
			},
		},
	}
	rvm := ui.ResultViewModel{State: ui.StateNoResultsYet}

	hvm := ui.ExecutionLogViewModel{Entries: []ui.ExecutionLogEntryViewModel{{
		Step: 1, Round: 1, Iteration: 1,
		Result: imcs.TestResultGood,
		Kind:   imcs.TestPlanVerification,
		Plan:   ui.TestPlanViewModel{ModIDsToTest: sets.MakeSet([]string{"a"})},
	}}}
	report := GenerateLogReport(bvm, hvm, rvm)

	for _, want := range []string{"-> GOOD", "Tested mods: Mod A (a 1.0)"} {
		if !strings.Contains(report, want) {
			t.Errorf("expected report to contain %q, got:\n%s", want, report)
		}
	}
}
