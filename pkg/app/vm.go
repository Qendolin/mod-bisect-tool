package app

import (
	"sort"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

func makeModVM(id string, mods map[string]*mods.Mod) ui.ModViewModel {
	if mod, ok := mods[id]; ok {
		return ui.ModViewModel{
			BaseFilename: mod.BaseFilename,
			ID:           mod.Metadata.ID,
			Name:         mod.FriendlyName(),
			Version:      mod.Metadata.Version.String(),
		}
	} else {
		return ui.ModViewModel{
			ID:        id,
			IsUnknown: true,
		}
	}
}

func (a *App) GetViewModel() ui.BisectionViewModel {
	vm := ui.BisectionViewModel{
		IsReady: false,
		Loader: ui.LoaderViewModel{
			Chosen:    a.loader,
			Preferred: a.cliArgs.Loader,
		},
	}
	if !a.IsBisectionReady() {
		return vm
	}

	engine := a.bisectSvc.Engine()
	enumState := a.bisectSvc.EnumerationState()
	state := engine.GetCurrentState()
	currentPlan, _ := a.bisectSvc.GetCurrentTestPlan()
	allMods := a.bisectSvc.StateManager().GetAllMods()

	isVerification := currentPlan != nil && currentPlan.IsVerificationStep

	vm.IsReady = true
	vm.Progress = ui.BisectionProgressViewModel{
		IsComplete:         state.IsComplete,
		IsVerificationStep: isVerification,
		IsHalted:           state.IsHalted,
		StepCount:          engine.GetStepCount(),
		Iteration:          state.Iteration,
		Round:              state.Round,
		EstimatedMaxTests:  engine.GetEstimatedMaxTests(),
		CanUndo:            a.bisectSvc.Engine().UndoCount() > 0,
		LastTestResult:     state.LastTestResult,
		LastFoundElement:   state.LastFoundElement,
	}
	vm.Sets = ui.SearchSetsViewModel{
		AllConflicts:    enumState.FoundConflictSets,
		CurrentConflict: state.ConflictSet,
		Candidate:       state.GetCandidateSet(),
		Cleared:         state.GetClearedSet(),
		PendingAddition: engine.GetPendingAdditions(),
	}
	if currentPlan != nil {
		vm.CurrentTestPlan = ui.TestPlanViewModel{ModIDsToTest: currentPlan.ModIDsToTest}
	}
	vm.Mods = ui.ModsViewModel{
		All:   state.AllModIDs,
		Infos: make(map[string]ui.ModViewModel, len(allMods)),
	}
	for id := range allMods {
		vm.Mods.Infos[id] = makeModVM(id, allMods)
	}

	return vm
}

// GetExecutionLogViewModel materializes history only when a history consumer
// requests it, rather than copying it during every regular redraw.
func (a *App) GetExecutionLogViewModel() ui.ExecutionLogViewModel {
	return makeExecutionLogVM(a.bisectSvc.GetCombinedExecutionLog())
}

func makeExecutionLogVM(entries []imcs.CompletedTest) ui.ExecutionLogViewModel {
	vm := ui.ExecutionLogViewModel{Entries: make([]ui.ExecutionLogEntryViewModel, 0, len(entries))}
	for _, entry := range entries {
		state := entry.StateBeforeTest
		vm.Entries = append(vm.Entries, ui.ExecutionLogEntryViewModel{
			Step:        state.Step,
			Round:       state.Round,
			Iteration:   state.Iteration,
			Result:      entry.Result,
			Kind:        entry.Plan.Kind,
			Plan:        ui.TestPlanViewModel{ModIDsToTest: sets.Copy(entry.Plan.ModIDsToTest())},
			ConflictSet: sets.Copy(state.ConflictSet),
			Candidates:  historyCandidates(state, entry.Plan.IsVerificationStep()),
			StableSet:   sets.Copy(state.GetStableSet()),
			ClearedSet:  sets.Copy(state.GetClearedSet()),
		})
	}
	return vm
}

func historyCandidates(state imcs.SearchState, verification bool) sets.Set {
	if verification {
		return sets.Set{}
	}
	return sets.Copy(state.GetCandidateSet())
}

// GetResultViewModel processes raw bisection data into a clean structured view model.
func (a *App) GetResultViewModel() (result ui.ResultViewModel) {
	if !a.IsBisectionReady() {
		result.State = ui.StateNotReady
		return result
	}

	state := a.bisectSvc.Engine().GetCurrentState()

	// Can only continue into a new round if the current round is complete
	result.CanContinueSearch = state.IsComplete && len(state.GetCandidateSet()) > 0

	if !state.IsComplete && state.LastFoundElement == "" {
		// First iteration in progress
		result.State = ui.StateNoResultsYet
		return result
	}

	modState := a.bisectSvc.StateManager()
	currentPlan := a.bisectSvc.GetActiveTestPlan()

	modMap := modState.GetAllMods()
	allModsSet := sets.MakeSet(modState.GetAllModIDs())
	generallyUnresolvable := modState.Resolver().CalculateTransitivelyUnresolvableMods(allModsSet)

	for _, cs := range a.bisectSvc.EnumerationState().FoundConflictSets {
		result.ArchivedConflictSets = append(result.ArchivedConflictSets, buildConflictSetReport(cs, allModsSet, modMap, generallyUnresolvable, modState))
	}

	// Always map the currently active/latest conflict group to CurrentConflict
	if len(state.ConflictSet) > 0 {
		result.CurrentConflict = buildConflictSetReport(state.ConflictSet, allModsSet, modMap, generallyUnresolvable, modState)
	}

	result.State = ui.StateInProgress
	if state.IsComplete {
		result.State = ui.StateComplete
	}

	// Calculate global dependency health
	details := modState.Resolver().CalculateUnresolvableModsDetails(allModsSet)
	if len(details.DirectlyUnresolvable) > 0 {
		result.GenerallyUnresolvable = buildGenerallyUnresolvableReport(details, modMap)
	}

	result.IsVerificationStep = currentPlan != nil && currentPlan.IsVerificationStep

	return result
}

// Helpers to build sub-components cleanly

func buildCascadingDisablesSlice(conflictSet, allModsSet sets.Set, modMap map[string]*mods.Mod, generallyUnresolvable sets.Set, modState *mods.StateManager) ([]ui.CascadingDisables, sets.Set) {
	union := sets.Set{}
	var list []ui.CascadingDisables

	for _, id := range sets.MakeSlice(conflictSet) {
		item := ui.CascadingDisables{Mod: makeModVM(id, modMap)}

		perModUnresolvable := modState.Resolver().CalculateTransitivelyUnresolvableMods(sets.Subtract(allModsSet, sets.MakeSet([]string{id})))
		perModSpecific := sets.Subtract(perModUnresolvable, generallyUnresolvable)

		for extraID := range perModSpecific {
			union[extraID] = struct{}{}
		}

		for _, depID := range sets.MakeSlice(perModSpecific) {
			item.AlsoRequireDisable = append(item.AlsoRequireDisable, makeModVM(depID, modMap))
		}
		list = append(list, item)
	}
	return list, union
}

func buildConflictSetReport(conflictSet, allModsSet sets.Set, modMap map[string]*mods.Mod, generallyUnresolvable sets.Set, modState *mods.StateManager) ui.ConflictSetReport {
	modsSlice, union := buildCascadingDisablesSlice(conflictSet, allModsSet, modMap, generallyUnresolvable, modState)

	fullSetUnresolvable := modState.Resolver().CalculateTransitivelyUnresolvableMods(sets.Subtract(allModsSet, conflictSet))
	fullSetSpecific := sets.Subtract(fullSetUnresolvable, generallyUnresolvable)
	extraIfAll := sets.Subtract(fullSetSpecific, union)

	var footerRefs []ui.ModViewModel
	for _, depID := range sets.MakeSlice(extraIfAll) {
		footerRefs = append(footerRefs, makeModVM(depID, modMap))
	}

	return ui.ConflictSetReport{
		Mods:              modsSlice,
		IfAllDisabledAlso: footerRefs,
	}
}

func buildGenerallyUnresolvableReport(details mods.UnresolvableModDetails, modMap map[string]*mods.Mod) []ui.UnresolvedDependencyReport {
	causedByRoot := make(map[string]sets.Set)
	for transitiveID, roots := range details.TransitivelyUnresolvable {
		for rootID := range roots {
			if _, ok := causedByRoot[rootID]; !ok {
				causedByRoot[rootID] = sets.Set{}
			}
			causedByRoot[rootID][transitiveID] = struct{}{}
		}
	}

	var topLevelSlice []string
	for modID := range details.DirectlyUnresolvable {
		topLevelSlice = append(topLevelSlice, modID)
	}
	sort.Strings(topLevelSlice)

	var reports []ui.UnresolvedDependencyReport
	for _, modID := range topLevelSlice {
		report := ui.UnresolvedDependencyReport{Mod: makeModVM(modID, modMap)}

		if failedDeps := details.DirectlyUnresolvable[modID]; len(failedDeps) > 0 {
			sort.Strings(failedDeps)
			for _, depID := range failedDeps {
				report.UnmetDependencies = append(report.UnmetDependencies, makeModVM(depID, modMap))
			}
		}

		if caused, ok := causedByRoot[modID]; ok && len(caused) > 0 {
			causedSlice := sets.MakeSlice(caused)
			sort.Strings(causedSlice)
			for _, depID := range causedSlice {
				report.RequiredByTransitive = append(report.RequiredByTransitive, makeModVM(depID, modMap))
			}
		}
		reports = append(reports, report)
	}
	return reports
}
