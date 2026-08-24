package bisect

import (
	"github.com/Qendolin/mod-bisect-tool/pkg/core/imcs"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
)

// Enumeration holds the state for the entire process of finding multiple independent conflict sets.
type Enumeration struct {
	FoundConflictSets    []sets.Set
	ArchivedExecutionLog *imcs.ExecutionLog
}

// NewEnumeration creates and initializes the state for a new enumeration process.
func NewEnumeration() *Enumeration {
	return &Enumeration{
		FoundConflictSets:    make([]sets.Set, 0),
		ArchivedExecutionLog: imcs.NewExecutionLog(),
	}
}

// AddFoundConflictSet records a newly found conflict set and removes its
// members from the master candidate pool for future searches.
func (e *Enumeration) AddFoundConflictSet(conflictSet sets.Set) {
	if len(conflictSet) == 0 {
		return
	}
	e.FoundConflictSets = append(e.FoundConflictSets, conflictSet)
}

// AppendLog archives the history from a completed bisection run.
func (e *Enumeration) AppendLog(log *imcs.ExecutionLog) {
	e.ArchivedExecutionLog.Append(log)
}
