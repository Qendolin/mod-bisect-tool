package imcs

import "github.com/Qendolin/mod-bisect-tool/pkg/core/sets"

// TestPlanKind identifies the algorithm branch that produced a test plan.
type TestPlanKind string

const (
	TestPlanComplement   TestPlanKind = "complement"
	TestPlanContinuation TestPlanKind = "continuation"
	TestPlanVerification TestPlanKind = "verification"
	TestPlanNewBisection TestPlanKind = "new_bisection"
)

// TestPlan is an immutable object representing a single, well-defined test.
// Its fields retain the algorithm's split data so the engine can explain how
// the plan was produced without recalculating the decision.
type TestPlan struct {
	Kind        TestPlanKind
	StableSet   sets.Set
	C1          sets.OrderedSet
	C2          sets.OrderedSet
	ConflictSet sets.Set
}

// ModIDsToTest returns the effective mod set represented by the plan.
func (p TestPlan) ModIDsToTest() sets.Set {
	switch p.Kind {
	case TestPlanComplement:
		return sets.Union(p.StableSet, sets.MakeSet(p.C2))
	case TestPlanContinuation, TestPlanNewBisection:
		return sets.Union(p.StableSet, sets.MakeSet(p.C1))
	case TestPlanVerification:
		return p.ConflictSet
	default:
		return sets.Set{}
	}
}

// IsVerificationStep reports whether this plan tests the current conflict set.
func (p TestPlan) IsVerificationStep() bool {
	return p.Kind == TestPlanVerification
}

// CompletedTest is a record of a test that was planned and executed.
type CompletedTest struct {
	Plan            TestPlan
	Result          TestResult
	StateBeforeTest SearchState
}

// ExecutionLog records a linear history of all completed tests for display.
type ExecutionLog struct {
	entries []CompletedTest
}

// NewExecutionLog creates a new, empty log.
func NewExecutionLog() *ExecutionLog {
	return &ExecutionLog{
		entries: make([]CompletedTest, 0),
	}
}

// Append appends another execution log to the current one.
func (el *ExecutionLog) Append(other *ExecutionLog) {
	if other == nil {
		return
	}
	el.entries = append(el.entries, other.entries...)
}

// Log adds a new completed test to the log.
func (el *ExecutionLog) Log(test CompletedTest) {
	el.entries = append(el.entries, test)
}

// GetEntries returns a copy of all recorded entries.
func (el *ExecutionLog) GetEntries() []CompletedTest {
	entriesCopy := make([]CompletedTest, len(el.entries))
	copy(entriesCopy, el.entries)
	return entriesCopy
}

// Clear resets the log.
func (el *ExecutionLog) Clear() {
	el.entries = make([]CompletedTest, 0)
}

// Size returns the number of entires
func (el *ExecutionLog) Size() int {
	return len(el.entries)
}

// GetLastTest returns the most recently completed test, if one exists.
func (el *ExecutionLog) GetLastTest() (CompletedTest, bool) {
	if len(el.entries) == 0 {
		return CompletedTest{}, false
	}
	return el.entries[len(el.entries)-1], true
}
