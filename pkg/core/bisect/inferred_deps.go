package bisect

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// CanInferDependencies reports whether the search is halted and inferred dependencies
// have not yet been evaluated in this session.
func (s *Service) CanInferDependencies() bool {
	return s.engine.GetCurrentState().IsHalted && !s.inferredDepsAttempted
}

// InferDependencies identifies undeclared dependencies by analyzing mod bytecode,
// filtering out targets that are not currently activatable. When the search is
// halted on a split, inference is restricted to undeclared dependencies crossing
// between the two conflicting candidate halves.
func (s *Service) InferDependencies() []mods.InferredDependency {
	deps := mods.InferDependencies(s.state.GetAllMods(), s.state.Resolver())
	statuses := s.state.GetModStatusesSnapshot()

	state := s.engine.GetCurrentState()
	var c1, c2 sets.Set
	if state.IsHalted {
		candidateSlice := sets.MakeSlice(state.GetCandidateSet())
		gA, gB := sets.Split(candidateSlice)
		c1 = sets.MakeSet(gA)
		c2 = sets.MakeSet(gB)
	}

	filtered := make([]mods.InferredDependency, 0, len(deps))
	for _, dep := range deps {
		status, ok := statuses[dep.TargetID]
		if !ok || !status.IsActivatable() {
			logging.Infof("BisectService: Skipping inferred dependency '%s' -> '%s': target not activatable.", dep.SourceID, dep.TargetID)
			continue
		}

		// When halted, limit inference strictly to dependencies crossing the split
		if state.IsHalted {
			_, srcInC1 := c1[dep.SourceID]
			_, tgtInC2 := c2[dep.TargetID]
			_, srcInC2 := c2[dep.SourceID]
			_, tgtInC1 := c1[dep.TargetID]

			crossesSplit := (srcInC1 && tgtInC2) || (srcInC2 && tgtInC1)
			if !crossesSplit {
				continue
			}
		}

		filtered = append(filtered, dep)
	}
	return filtered
}

// ApplyInferredDependencies applies inferred dependencies, retries the halted split,
// and marks inference as attempted for this session. It returns the dependencies actually injected.
func (s *Service) ApplyInferredDependencies(deps []mods.InferredDependency) []mods.InferredDependency {
	if len(deps) == 0 {
		return nil
	}
	s.inferredDepsAttempted = true
	injected := s.state.Resolver().ApplyInferredDependencies(deps)
	s.engine.RetryHaltedSplit()
	return injected
}

// DismissInferredDependencies marks inference as attempted without applying dependencies,
// keeping the search halted.
func (s *Service) DismissInferredDependencies() {
	s.inferredDepsAttempted = true
}
