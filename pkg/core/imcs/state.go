package imcs

import (
	"sort"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// TestResult indicates the outcome of a test run.
type TestResult string

const (
	// TestResultFail indicates the test exhibited the undesirable outcome.
	TestResultFail TestResult = "FAIL"
	// TestResultGood indicates the test ran successfully without the undesirable outcome.
	TestResultGood TestResult = "GOOD"
	// TestResultIndeterminate indicates that a secondary issue prevented the
	// outcome from being observed, e.g. a crash before the primary issue could manifest.
	TestResultIndeterminate TestResult = "INDETERMINATE"
	// This is the initial value
	TestResultUndefined TestResult = ""
)

// SearchStep represents a single logical step (or frame) in the
// iterative implementation of the `FindNextConflictElement` bisection algorithm.
// Each step contains the local context (StableSet and Candidates) relevant
// to that specific recursive call, pushed onto the SearchState's SearchStack.
type SearchStep struct {
	// A set of components known to be stable (not cause an issue by themselves) in the current search context.
	StableSet sets.Set
	// Mods currently being searched within for this specific bisection step.
	// It is the union of C_1 and C_2.
	Candidates []string
}

// SearchState (renamed from SearchSnapshot) represents a complete, immutable state of the conflict search.
type SearchState struct {
	// --- Fields representing the state of the extended "IMCS_Enumerator" procedure ---

	// Round is the top-level enumeration count, tracking how many independent conflict sets have been found.
	Round int

	// --- Fields representing the state of the main "FindConflictSet" procedure ---

	// ConflictSet contains mods already identified as part of the minimal conflict set.
	ConflictSet sets.Set
	// Candidates is the global pool of mods remaining to be searched for conflicts.
	// This set only shrinks when a new element is added to ConflictSet.
	Candidates []string
	// StableSet is the set of mods globally proven to be "good" and not part of any
	// conflict. It grows as bisections on candidate sets result in GOOD.
	StableSet sets.Set
	// Iteration is the count of conflict elements being searched for within the current round (e.g., finding the 2nd element in a 3-mod conflict).
	Iteration int

	// --- Fields representing the state of the "FindNextConflictElement" bisection ---

	// Step counts tests within the current FindNextConflictElement (bisection) run.
	// It resets at the start of each new FindNextConflictElement iteration.
	Step int

	// IndeterminateCount is the cumulative number of INDETERMINATE results submitted
	// in the current search. Each one triggers a complement test, so it is used to
	// refine the estimated maximum number of tests.
	IndeterminateCount int

	// SearchStack is an iterative implementation of the recursive "FindNextConflictElement"
	// procedure. Each SearchStep on the stack is a snapshot of a recursive call's arguments
	// (the local stable set and local candidates for that bisection).
	SearchStack []SearchStep

	// IsHandlingIndeterminate is true while a complement test is being planned
	// and executed in response to an INDETERMINATE result on the first half of a split.
	// It keeps the current step context so the complement test (on the second half)
	// can be planned next and its result can branch the search correctly.
	IsHandlingIndeterminate bool

	// --- Global Metadata and Flags ---

	// IsVerifyingConflictSet is true if the next test planned should be the final `test(ConflictSet)` optimization step.
	IsVerifyingConflictSet bool
	// IsHalted is true if the search was stopped because both halves of a split
	// returned INDETERMINATE. No further tests can be planned. The search stack
	// is left intact so the UI can reconstruct the two conflicting groups from
	// the current candidate set.
	IsHalted bool
	// AllModIDs is the universe of all mods, used for context and resetting candidates.
	AllModIDs []string
	// IsComplete is true if the search has concluded and no more tests are needed.
	IsComplete bool
	// LastFoundElement stores the ID of the most recently discovered conflict element.
	LastFoundElement string
	// LastTestResult stores the outcome of the test that produced this state.
	LastTestResult TestResult
}

// newSearchStep creates a search step with sorted candidates for determinism.
func newSearchStep(stableSet sets.Set, candidates []string) SearchStep {
	// Sorting ensures that the binary search partitioning is deterministic across runs.
	sort.Strings(candidates)
	return SearchStep{
		StableSet:  stableSet,
		Candidates: candidates,
	}
}

// NewInitialState creates the starting state for a new search.
func NewInitialState() SearchState {
	return SearchState{
		ConflictSet:             make(sets.Set),
		Candidates:              make([]string, 0),
		StableSet:               make(sets.Set),
		SearchStack:             make([]SearchStep, 0),
		IsHandlingIndeterminate: false,
		IsVerifyingConflictSet:  false,
		IsHalted:                false,
		AllModIDs:               make([]string, 0),
		IsComplete:              false,
		LastFoundElement:        "",
		LastTestResult:          "",
		Round:                   1,
		Iteration:               1,
		Step:                    1,
		IndeterminateCount:      0,
	}
}

// GetCurrentStep returns the top of the search stack, which represents the
// state of the current "FindNextConflictElement" bisection.
func (s SearchState) GetCurrentStep() (SearchStep, bool) {
	if len(s.SearchStack) == 0 {
		return SearchStep{}, false
	}
	return s.SearchStack[len(s.SearchStack)-1], true
}

// GetStableSet determines the current StableSet based on the state.
func (s SearchState) GetStableSet() (stable sets.Set) {
	if step, ok := s.GetCurrentStep(); ok {
		// Mid-bisection: use the context from the top of the stack.
		return step.StableSet
	} else {
		// Start of a new bisection: use the top-level candidates.
		// The StableSet for a new bisection is the conflict set.
		return s.ConflictSet
	}
}

// GetCandidateSet determines the current StableSet based on the state.
func (s SearchState) GetCandidateSet() (candidates sets.Set) {
	if step, ok := s.GetCurrentStep(); ok {
		return sets.MakeSet(step.Candidates)
	} else {
		return sets.MakeSet(s.Candidates)
	}
}

// GetClearedSet calculates the implicit ClearedSet
func (s SearchState) GetClearedSet() (cleared sets.Set) {
	if step, ok := s.GetCurrentStep(); ok {
		return sets.Subtract(step.StableSet, s.ConflictSet)
	} else {
		return sets.Subtract(s.StableSet, s.ConflictSet)
	}
}
