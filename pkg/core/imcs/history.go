package imcs

import (
	"fmt"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// UndoFrame captures a complete undoable action, containing both the state
// before the action and the plan that was executed.
type UndoFrame struct {
	State SearchState
	Plan  TestPlan
}

// UndoStack provides an undo/redo-style history for search states.
type UndoStack struct {
	frames []UndoFrame
}

func NewUndoStack() *UndoStack {
	return &UndoStack{frames: make([]UndoFrame, 0)}
}

func (s *UndoStack) Push(frame UndoFrame) {
	s.frames = append(s.frames, frame)
}

func (s *UndoStack) Pop() (UndoFrame, error) {
	if len(s.frames) == 0 {
		return UndoFrame{}, fmt.Errorf("undo stack is empty")
	}
	frame := s.frames[len(s.frames)-1]
	s.frames = s.frames[:len(s.frames)-1]
	return frame, nil
}

func (s *UndoStack) Size() int {
	return len(s.frames)
}

// Peek returns the most recent state from the history.
func (s *UndoStack) Peek() (UndoFrame, bool) {
	if len(s.frames) == 0 {
		return UndoFrame{}, false
	}
	lastIndex := len(s.frames) - 1
	snapshot := s.frames[lastIndex]
	return snapshot, true
}

// Clear removes all states from the history.
func (s *UndoStack) Clear() {
	s.frames = make([]UndoFrame, 0)
}

// copySteps deep-copies a slice of SearchStep.
func copySteps(steps []SearchStep) []SearchStep {
	copied := make([]SearchStep, len(steps))
	for i, step := range steps {
		copiedStepStableSet := make(sets.Set, len(step.StableSet))
		for k := range step.StableSet {
			copiedStepStableSet[k] = struct{}{}
		}
		copiedStepCandidates := make([]string, len(step.Candidates))
		copy(copiedStepCandidates, step.Candidates)

		copied[i] = SearchStep{
			StableSet:  copiedStepStableSet,
			Candidates: copiedStepCandidates,
		}
	}
	return copied
}

// deepCopyState creates a new SearchState with all maps and slices copied.
func deepCopyState(state SearchState) SearchState {
	// This function contains the logic you had in your original Push method.
	copiedConflictSet := make(sets.Set, len(state.ConflictSet))
	for k := range state.ConflictSet {
		copiedConflictSet[k] = struct{}{}
	}

	copiedCandidates := make([]string, len(state.Candidates))
	copy(copiedCandidates, state.Candidates)

	copiedStableSet := make(sets.Set, len(state.StableSet))
	for k := range state.StableSet {
		copiedStableSet[k] = struct{}{}
	}

	return SearchState{
		ConflictSet:             copiedConflictSet,
		Candidates:              copiedCandidates,
		StableSet:               copiedStableSet,
		SearchStack:             copySteps(state.SearchStack),
		IsHandlingIndeterminate: state.IsHandlingIndeterminate,
		IsVerifyingConflictSet:  state.IsVerifyingConflictSet,
		IsHalted:                state.IsHalted,
		AllModIDs:               state.AllModIDs, // This can be a shallow copy as it's immutable reference data
		IsComplete:              state.IsComplete,
		LastFoundElement:        state.LastFoundElement,
		LastTestResult:          state.LastTestResult,
		Round:                   state.Round,
		Iteration:               state.Iteration,
		Step:                    state.Step,
		IndeterminateCount:      state.IndeterminateCount,
	}
}
