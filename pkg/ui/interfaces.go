package ui

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// AppController is the top-level controller handed to the UI. It exposes the
// lifecycle and read models, plus accessors for the narrower role controllers.
type AppController interface {
	StartLoadingProcess(modsPath string, loader mods.RunLoader)

	// CompleteLoading finishes the loading phase. After the unresolvable mods
	// screen's decisions have been applied, it merges any re-added mods so they
	// participate in the search immediately, and signals the UI that loading is
	// done.
	CompleteLoading()
	CompleteInitialModState(keepDisabled, omitted sets.Set)

	GetViewModel() BisectionViewModel
	GetExecutionLogViewModel() ExecutionLogViewModel
	GetResultViewModel() ResultViewModel

	GetBisectionController() BisectionController
	GetModStatusController() ModStatusController
}

// BisectionController defines the operations that drive the bisection search.
type BisectionController interface {
	Step()
	Undo() error
	ResetSearch()
	ContinueSearch()
	Reconcile()
	IsBisectionReady() bool

	CancelTest()
	SubmitTestResult(result imcs.TestResult)
}

// ModStatusController defines the operations to inspect and change the
// per-mod status. Changes are staged via SetOverride and applied atomically
// by Commit, which also triggers a reconciliation.
type ModStatusController interface {
	// GetModStatuses returns the current status of every mod, merged with any
	// staged (not yet committed) overrides.
	GetModStatuses() map[string]ModStatusViewModel

	// SetOverride stages a new override for a single mod. It is not applied
	// until Commit is called.
	SetOverride(id string, override ModStatusOverride)

	// Commit applies all staged overrides to the underlying state manager and
	// triggers a reconciliation.
	Commit()

	// Discard drops all staged overrides without applying them.
	Discard()

	// ResolveEffectiveSet calculates the full set of mods that would be active
	// for a test of the given target mods, including any dependencies pulled in transitively.
	ResolveEffectiveSet(targetSet sets.Set) (effectiveSet sets.Set)

	// ResolveUnresolvableMods applies the decisions made on the unresolvable
	// mods screen. Mods marked UnresolvableModActionIgnore have their failing
	// dependencies dropped and stay active; everything else stays disabled. The
	// state is reconciled afterwards. It has no other side effects and can be
	// called at any time.
	ResolveUnresolvableMods(decisions map[string]UnresolvableModAction)
}

// View defines the operations that the business logic can request from the UI.
type View interface {
	Start() error
	Stop()
	Update()

	// Dialogs (Blocking)
	ShowDialogErrorModLoadingGeneric(path string, err error)
	ShowDialogErrorModLoadingNoMods(path string)
	ShowDialogErrorBisectionInitialization(err error)
	ShowDialogErrorBisectionCannotContinue(err error)
	ShowDialogErrorBisectionPrepare(err error)

	ShowDialogInfoBisectionModsMissingExpected(missingMods sets.Set)
	ShowDialogInfoBisectionUnresolvableModsDisabled(disabledMods sets.Set)

	ShowDialogQuestionBisectionContinueWithMissingMods(missingMods sets.Set) bool

	OnLoadingStarted()
	OnLoadingProgress(fileName string, i, count int)
	// OnUnresolvableMods is called after loading when one or more mods could not
	// be activated due to unresolvable dependencies. The UI presents each mod
	// with an ignore/disable choice; once the user is done, it must call
	// AppController.ResolveUnresolvableMods to continue.
	OnUnresolvableMods(mods []UnresolvableModInfo)
	OnInitialModStateSelection(initiallyDisabled []string)
	OnBisectionReady()
	OnTestReady()
	OnIterationComplete()
	// OnBisectionHalted is called when the search halts because two groups of
	// mods block each other through undeclared dependencies. The UI presents the
	// two groups as a full page; this is non-blocking.
	OnBisectionHalted(groupA, groupB sets.Set)
}
