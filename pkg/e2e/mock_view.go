// Package e2e provides end-to-end tests that drive pkg/app.App's controller
// methods through a recording, no-op ui.View implementation and assert on the
// recorded view calls and view models.
package e2e

import (
	"sync"
	"testing"
	"time"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// DialogKind enumerates the blocking dialogs the app can request.
type DialogKind int

const (
	DialogErrorModLoadingGeneric DialogKind = iota
	DialogErrorModLoadingNoMods
	DialogErrorBisectionInitialization
	DialogErrorBisectionCannotContinue
	DialogErrorBisectionPrepare
	DialogInfoBisectionModsMissingExpected
	DialogInfoBisectionUnresolvableModsDisabled
	DialogQuestionBisectionContinueWithMissingMods
)

func (k DialogKind) String() string {
	switch k {
	case DialogErrorModLoadingGeneric:
		return "ShowDialogErrorModLoadingGeneric"
	case DialogErrorModLoadingNoMods:
		return "ShowDialogErrorModLoadingNoMods"
	case DialogErrorBisectionInitialization:
		return "ShowDialogErrorBisectionInitialization"
	case DialogErrorBisectionCannotContinue:
		return "ShowDialogErrorBisectionCannotContinue"
	case DialogErrorBisectionPrepare:
		return "ShowDialogErrorBisectionPrepare"
	case DialogInfoBisectionModsMissingExpected:
		return "ShowDialogInfoBisectionModsMissingExpected"
	case DialogInfoBisectionUnresolvableModsDisabled:
		return "ShowDialogInfoBisectionUnresolvableModsDisabled"
	case DialogQuestionBisectionContinueWithMissingMods:
		return "ShowDialogQuestionBisectionContinueWithMissingMods"
	default:
		return "UnknownDialog"
	}
}

// DialogInvocation describes a single blocking dialog requested by the app.
// The app is blocked inside the dialog method until Respond is called,
// mirroring the blocking-dialog contract of the real UI implementations.
type DialogInvocation struct {
	Kind         DialogKind
	Path         string
	Err          error
	MissingMods  sets.Set
	DisabledMods sets.Set

	respond chan bool
}

// Respond unblocks the dialog. The value is used for question dialogs
// (returned to the app) and ignored for info/error dialogs.
func (d *DialogInvocation) Respond(value bool) {
	d.respond <- value
}

// MockView is a recording, no-op implementation of ui.View for e2e tests.
// Dialog methods block until the test responds via the returned
// DialogInvocation, honoring the blocking-dialog design.
type MockView struct {
	mu          sync.Mutex
	calls       []string
	updateCount int

	dialogCh  chan DialogInvocation
	readyCh   chan struct{}
	readyOnce sync.Once

	// unresolvableCh receives the mods passed to OnUnresolvableMods (once).
	unresolvableCh chan []ui.UnresolvableModInfo
	// haltedCh receives the groups passed to OnBisectionHalted (once each).
	haltedCh                   chan HaltInvocation
	initialModStateSelectionCh chan []string
}

// HaltInvocation describes a single OnBisectionHalted call.
type HaltInvocation struct {
	GroupA sets.Set
	GroupB sets.Set
}

// NewMockView creates an empty recording view.
func NewMockView() *MockView {
	return &MockView{
		dialogCh:                   make(chan DialogInvocation),
		readyCh:                    make(chan struct{}),
		unresolvableCh:             make(chan []ui.UnresolvableModInfo, 1),
		haltedCh:                   make(chan HaltInvocation, 1),
		initialModStateSelectionCh: make(chan []string, 1),
	}
}

func (m *MockView) record(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, name)
}

// Calls returns a copy of the recorded method names in invocation order.
func (m *MockView) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// HasCall reports whether a method was invoked at least once.
func (m *MockView) HasCall(name string) bool {
	for _, c := range m.Calls() {
		if c == name {
			return true
		}
	}
	return false
}

// UpdateCount returns how many times Update was called.
func (m *MockView) UpdateCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updateCount
}

// WaitReady blocks until OnBisectionReady fires (i.e. loading succeeded and the
// initial reconciliation is done), or fails the test on timeout.
func (m *MockView) WaitReady(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-m.readyCh:
		return
	case <-time.After(timeout):
		t.Fatalf("MockView: timed out waiting for OnBisectionReady; calls: %v", m.Calls())
	}
}

// WaitUnresolvable blocks until OnUnresolvableMods fires and returns the mods
// that were reported, or fails the test on timeout.
func (m *MockView) WaitUnresolvable(t *testing.T, timeout time.Duration) []ui.UnresolvableModInfo {
	t.Helper()
	select {
	case mods := <-m.unresolvableCh:
		return mods
	case <-time.After(timeout):
		t.Fatalf("MockView: timed out waiting for OnUnresolvableMods; calls: %v", m.Calls())
	}
	return nil
}

// WaitDialog blocks until the next blocking dialog is requested and returns it.
// It only watches the dialog channel; a closed ready channel does not satisfy it.
func (m *MockView) WaitDialog(t *testing.T, timeout time.Duration) DialogInvocation {
	t.Helper()
	select {
	case inv := <-m.dialogCh:
		return inv
	case <-time.After(timeout):
		t.Fatalf("MockView: timed out waiting for a dialog; calls: %v", m.Calls())
	}
	return DialogInvocation{}
}

// WaitHalted blocks until OnBisectionHalted fires and returns the reported
// groups, or fails the test on timeout.
func (m *MockView) WaitHalted(t *testing.T, timeout time.Duration) HaltInvocation {
	t.Helper()
	select {
	case inv := <-m.haltedCh:
		return inv
	case <-time.After(timeout):
		t.Fatalf("MockView: timed out waiting for OnBisectionHalted; calls: %v", m.Calls())
	}
	return HaltInvocation{}
}

// WaitInitialModStateSelection blocks until OnInitialModStateSelection fires and returns the reported mods, or fails the test on timeout.
func (m *MockView) WaitInitialModStateSelection(t *testing.T, timeout time.Duration) []string {
	t.Helper()
	select {
	case mods := <-m.initialModStateSelectionCh:
		return mods
	case <-time.After(timeout):
		t.Fatalf("MockView: timed out waiting for OnInitialModStateSelection; calls: %v", m.Calls())
	}
	return nil
}

// block sends an invocation to the dialog channel and blocks until Respond.
func (m *MockView) block(inv DialogInvocation) bool {
	if inv.respond == nil {
		inv.respond = make(chan bool)
	}
	m.dialogCh <- inv
	return <-inv.respond
}

// --- ui.View implementation ---

func (m *MockView) Start() error {
	m.record("Start")
	return nil
}

func (m *MockView) Stop() {
	m.record("Stop")
}

func (m *MockView) Update() {
	m.mu.Lock()
	m.updateCount++
	m.mu.Unlock()
	m.record("Update")
}

func (m *MockView) ShowDialogErrorModLoadingGeneric(path string, err error) {
	m.record("ShowDialogErrorModLoadingGeneric")
	m.block(DialogInvocation{Kind: DialogErrorModLoadingGeneric, Path: path, Err: err})
}

func (m *MockView) ShowDialogErrorModLoadingNoMods(path string) {
	m.record("ShowDialogErrorModLoadingNoMods")
	m.block(DialogInvocation{Kind: DialogErrorModLoadingNoMods, Path: path})
}

func (m *MockView) ShowDialogErrorBisectionInitialization(err error) {
	m.record("ShowDialogErrorBisectionInitialization")
	m.block(DialogInvocation{Kind: DialogErrorBisectionInitialization, Err: err})
}

func (m *MockView) ShowDialogErrorBisectionCannotContinue(err error) {
	m.record("ShowDialogErrorBisectionCannotContinue")
	m.block(DialogInvocation{Kind: DialogErrorBisectionCannotContinue, Err: err})
}

func (m *MockView) ShowDialogErrorBisectionPrepare(err error) {
	m.record("ShowDialogErrorBisectionPrepare")
	m.block(DialogInvocation{Kind: DialogErrorBisectionPrepare, Err: err})
}

func (m *MockView) ShowDialogInfoBisectionModsMissingExpected(missingMods sets.Set) {
	m.record("ShowDialogInfoBisectionModsMissingExpected")
	m.block(DialogInvocation{Kind: DialogInfoBisectionModsMissingExpected, MissingMods: missingMods})
}

func (m *MockView) ShowDialogInfoBisectionUnresolvableModsDisabled(disabledMods sets.Set) {
	m.record("ShowDialogInfoBisectionUnresolvableModsDisabled")
	m.block(DialogInvocation{Kind: DialogInfoBisectionUnresolvableModsDisabled, DisabledMods: disabledMods})
}

func (m *MockView) ShowDialogQuestionBisectionContinueWithMissingMods(missingMods sets.Set) bool {
	m.record("ShowDialogQuestionBisectionContinueWithMissingMods")
	return m.block(DialogInvocation{Kind: DialogQuestionBisectionContinueWithMissingMods, MissingMods: missingMods})
}

func (m *MockView) OnLoadingStarted() {
	m.record("OnLoadingStarted")
}

func (m *MockView) OnLoadingProgress(fileName string, i, count int) {
	m.record("OnLoadingProgress")
}

func (m *MockView) OnBisectionReady() {
	m.record("OnBisectionReady")
	m.readyOnce.Do(func() { close(m.readyCh) })
}

func (m *MockView) OnUnresolvableMods(mods []ui.UnresolvableModInfo) {
	m.record("OnUnresolvableMods")
	select {
	case m.unresolvableCh <- mods:
	default:
	}
}

func (m *MockView) OnInitialModStateSelection(initiallyDisabled []string) {
	m.record("OnInitialModStateSelection")
	select {
	case m.initialModStateSelectionCh <- initiallyDisabled:
	default:
	}
}

func (m *MockView) OnTestReady() {
	m.record("OnTestReady")
}

func (m *MockView) OnBisectionHalted(groupA, groupB sets.Set) {
	m.record("OnBisectionHalted")
	select {
	case m.haltedCh <- HaltInvocation{GroupA: groupA, GroupB: groupB}:
	default:
	}
}

func (m *MockView) OnIterationComplete() {
	m.record("OnIterationComplete")
}

var _ ui.View = (*MockView)(nil)
