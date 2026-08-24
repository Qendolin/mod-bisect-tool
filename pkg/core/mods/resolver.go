package mods

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
	"github.com/Qendolin/mod-bisect-tool/pkg/core/sets"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
)

// DependencyResolver is a long-lived service that holds the static universe of all mods
// and their potential providers. It is safe for concurrent use.
type DependencyResolver struct {
	// allMods contains only top-level mods, keyed by top-level mod ID.
	allMods            map[string]*Mod
	potentialProviders PotentialProvidersMap
	loader             RunLoader
}

// resolutionSession holds the state for a single dependency resolution operation.
// It is created for a single call to ResolveEffectiveSet and is not reused.
type resolutionSession struct {
	// Static data from the parent resolver
	// allMods contains only top-level mods, keyed by top-level mod ID.
	allMods            map[string]*Mod
	potentialProviders PotentialProvidersMap
	loader             RunLoader

	// Per-call dynamic data
	modStatuses      map[string]ModStatus
	effectiveSet     map[string]*Mod
	resolutionPath   map[string]ResolutionInfo
	dfsStack         map[string]bool
	cachedProviders  map[string]*ProviderInfo
	unresolvableDeps map[string]UnresolvableDependency
	resolutionFailed bool
	failureReason    string
}

// UnresolvableDependency describes a dependency that could not be satisfied
// during a resolution attempt.
type UnresolvableDependency struct {
	DepID          string
	RequiringModID string
	Predicates     []*version.VersionPredicate
}

// String formats the dependency for logging.
func (d UnresolvableDependency) String() string {
	return fmt.Sprintf("Could not resolve dependency '%s %s' for mod '%s': no valid providers could be activated.",
		d.DepID, formatPredicates(d.Predicates), d.RequiringModID)
}

// ResolutionResult bundles the outcome of a single resolution attempt.
type ResolutionResult struct {
	EffectiveSet sets.Set
	Path         ResolutionPath
	// UnresolvableDeps lists every dependency that could not be satisfied
	// during the attempt, deduplicated by dependency ID and sorted for
	// deterministic output.
	UnresolvableDeps []UnresolvableDependency
}

// NewDependencyResolver creates a new DependencyResolver service. allMods must
// contain only top-level mods keyed by top-level mod ID.
func NewDependencyResolver(allMods map[string]*Mod, potentialProviders PotentialProvidersMap, loader RunLoader) *DependencyResolver {
	return &DependencyResolver{
		allMods:            allMods,
		potentialProviders: potentialProviders,
		loader:             loader,
	}
}

// ResolveEffectiveSet calculates the set of active top-level mods based on
// top-level target IDs, dependencies, and force flags.
func (dr *DependencyResolver) ResolveEffectiveSet(targetSet sets.Set, modStatuses map[string]ModStatus) ResolutionResult {
	startTime := time.Now()
	logging.Infof("Resolver: Resolving effective set for %d mods: %v", len(targetSet), sets.FormatSet(targetSet))

	s := &resolutionSession{
		allMods:            dr.allMods,
		potentialProviders: dr.potentialProviders,
		loader:             dr.loader,
		modStatuses:        modStatuses,
		effectiveSet:       make(map[string]*Mod),
		resolutionPath:     make(map[string]ResolutionInfo),
		dfsStack:           make(map[string]bool),
		cachedProviders:    make(map[string]*ProviderInfo),
		unresolvableDeps:   make(map[string]UnresolvableDependency),
	}

	initialActivationSet := sets.Copy(targetSet)
	for modID, status := range s.modStatuses {
		if status.ForceEnabled {
			initialActivationSet[modID] = struct{}{}
		}
	}

	for modID := range initialActivationSet {
		// If a previous activation attempt resulted in a fatal error, stop processing.
		if s.resolutionFailed {
			break
		}
		status := s.modStatuses[modID]
		reason := "Target"
		if status.ForceEnabled {
			reason = "Forced"
		}
		s.ensureModActive(modID, "System (Initial Set)", reason, modID, "")
	}
	duration := time.Since(startTime)
	if s.resolutionFailed {
		logging.Warnf("Resolver: Resolution failed in %v. Reason: %s", duration, s.failureReason)
		return ResolutionResult{EffectiveSet: make(sets.Set), UnresolvableDeps: s.collectUnresolvableDeps()}
	}

	if err := s.validateBreaks(); err != nil {
		s.resolutionFailed = true
		s.failureReason = err.Error()
		logging.Warnf("Resolver: Resolution failed 'breaks' validation in %v. Reason: %s", duration, s.failureReason)
		return ResolutionResult{EffectiveSet: make(sets.Set), UnresolvableDeps: s.collectUnresolvableDeps()}
	}

	finalSet := make(sets.Set, len(s.effectiveSet))
	effectiveIDs := make([]string, 0, len(s.effectiveSet))
	for id := range s.effectiveSet {
		finalSet[id] = struct{}{}
		effectiveIDs = append(effectiveIDs, id)
	}
	sort.Strings(effectiveIDs)
	logging.Infof("Resolver: Resolution complete in %v. Effective set (%d mods): %v", duration, len(effectiveIDs), effectiveIDs)

	return ResolutionResult{
		EffectiveSet:     finalSet,
		Path:             s.collectResolutionPath(),
		UnresolvableDeps: s.collectUnresolvableDeps(),
	}
}

// collectUnresolvableDeps returns the recorded unresolvable dependencies,
// sorted by dependency ID for deterministic output.
func (s *resolutionSession) collectUnresolvableDeps() []UnresolvableDependency {
	depIDs := make([]string, 0, len(s.unresolvableDeps))
	for depID := range s.unresolvableDeps {
		depIDs = append(depIDs, depID)
	}
	sort.Strings(depIDs)

	deps := make([]UnresolvableDependency, 0, len(depIDs))
	for _, depID := range depIDs {
		deps = append(deps, s.unresolvableDeps[depID])
	}
	return deps
}

// ensureModActive attempts to activate a top-level mod and its dependencies.
func (s *resolutionSession) ensureModActive(modID, neededBy, reason, satisfiedDep, logPrefix string) bool {
	if s.resolutionFailed {
		return false
	}
	if _, isActive := s.effectiveSet[modID]; isActive {
		s.updateNeededForList(modID, neededBy)
		return true
	}
	// The IsActivatable check correctly uses the global modStatuses map, which is what we want.
	// It allows the resolver to pull in any mod that isn't explicitly force-disabled by the user.
	if status, ok := s.modStatuses[modID]; ok && !status.IsActivatable() {
		return false
	}
	if s.dfsStack[modID] {
		s.resolutionFailed = true
		s.failureReason = fmt.Sprintf("circular dependency detected involving '%s'", modID)
		logging.Error("Resolver: " + s.failureReason)
		return false
	}

	mod, exists := s.allMods[modID]
	if !exists {
		// This can happen if a dependency points to a modID that doesn't exist in any file.
		return false
	}

	s.dfsStack[modID] = true
	s.debugf(logPrefix, "> activate top-level '%s' (for '%s')", modID, neededBy)
	dependencyLogPrefix := logPrefix + "│  "

	// Tentatively add the mod to the set for this recursive path.
	originalState := s.copyState()
	s.effectiveSet[modID] = mod

	allDepsOK := s.resolveDependencies(modID, mod.Metadata.Depends, dependencyLogPrefix)
	if allDepsOK {
		for i, nested := range mod.NestedModules {
			branch := "├─"
			if i == len(mod.NestedModules)-1 {
				branch = "└─"
			}
			if !hasNonImplicitDependencies(nested.Info.Depends) {
				s.debugf(dependencyLogPrefix, "%s nested '%s' bundled in '%s': no dependencies", branch, nested.Info.ID, modID)
				continue
			}
			s.debugf(dependencyLogPrefix, "%s nested '%s' bundled in '%s'", branch, nested.Info.ID, modID)
			nestedDependencyLogPrefix := dependencyLogPrefix + "│  "
			if !s.resolveDependencies(nested.Info.ID, nested.Info.Depends, nestedDependencyLogPrefix) {
				s.debugf(nestedDependencyLogPrefix, "└─ nested '%s' blocked activation of '%s'", nested.Info.ID, modID)
				allDepsOK = false
				break
			}
		}
	}

	s.dfsStack[modID] = false // Pop from the virtual stack.

	if allDepsOK {
		s.debugf(logPrefix, "< activated top-level '%s'", modID)
		s.updateResolutionPath(modID, neededBy, reason, satisfiedDep)
		return true
	} else {
		// Backtrack: The dependencies for this mod could not be resolved.
		// Restore the state to before we tried activating this mod.
		s.restoreState(originalState)
		// Refined log message for clarity during backtracking.
		s.debugf(logPrefix, "< backtrack top-level '%s': dependencies unsatisfied", modID)
		return false
	}
}

func (s *resolutionSession) debugf(logPrefix, format string, args ...any) {
	logging.Debugf("Resolver: %s%s", logPrefix, fmt.Sprintf(format, args...))
}

// resolveDependencies activates providers for all required dependencies of a
// top-level or nested module. Nested modules are validated without being added
// to the effective top-level set.
func (s *resolutionSession) resolveDependencies(requiringModID string, dependencies VersionRanges, logPrefix string) bool {
	for depID, predicates := range dependencies {
		if IsImplicitMod(depID) {
			continue
		}
		if !s.resolveDependency(depID, predicates, requiringModID, logPrefix) {
			return false
		}
	}
	return true
}

func hasNonImplicitDependencies(dependencies VersionRanges) bool {
	for depID := range dependencies {
		if !IsImplicitMod(depID) {
			return true
		}
	}
	return false
}

// resolveDependency finds a valid provider for a dependency and activates it. This is the heart of the backtracking logic.
func (s *resolutionSession) resolveDependency(depID string, predicates []*version.VersionPredicate, requiringModID, logPrefix string) bool {
	predicateStr := formatPredicates(predicates)
	s.debugf(logPrefix, "└─ require '%s %s' for '%s'", depID, predicateStr, requiringModID)
	childLogPrefix := logPrefix + "│  "

	if _, ok := s.unresolvableDeps[depID]; ok {
		s.debugf(childLogPrefix, "└─ dependency '%s' unresolved: previous attempt failed", depID)
		return false
	}

	if cachedProvider, isCached := s.cachedProviders[depID]; isCached {
		if !checkAnyPredicatesSatisfied(predicates, cachedProvider.VersionOfProvidedItem) {
			s.resolutionFailed = true
			s.failureReason = fmt.Sprintf("dependency conflict for '%s'. Mod '%s' requires '%s', but mod '%s' (v%s) was already chosen.",
				depID, requiringModID, formatPredicates(predicates), cachedProvider.TopLevelModID, cachedProvider.VersionOfProvidedItem)
			logging.Warn("Resolver: " + s.failureReason)
			return false
		}
		// The cached provider is compatible, ensure it's active. This will handle cycles correctly.
		active := s.ensureModActive(cachedProvider.TopLevelModID, requiringModID, "Dependency", depID, childLogPrefix)
		if active {
			s.debugf(childLogPrefix, "└─ dependency '%s' satisfied by cached '%s'", depID, cachedProvider.TopLevelModID)
		} else {
			s.debugf(childLogPrefix, "└─ dependency '%s' unresolved: cached provider '%s' could not activate", depID, cachedProvider.TopLevelModID)
		}
		return active
	}

	candidates := s.findBestProviders(depID, predicates)

	if logging.IsDebugEnabled() {
		var candidateNames []string
		for _, c := range candidates {
			candidateNames = append(candidateNames, fmt.Sprintf("%s v%s", c.TopLevelModID, c.VersionOfProvidedItem))
		}
		s.debugf(childLogPrefix, "├─ candidates (%d): %v", len(candidates), candidateNames)
	}

	originalState := s.copyState()
	for _, provider := range candidates {
		s.debugf(childLogPrefix, "├─ candidate '%s' v%s", provider.TopLevelModID, provider.VersionOfProvidedItem)
		if s.ensureModActive(provider.TopLevelModID, requiringModID, "Dependency", depID, childLogPrefix+"│  ") {
			s.cachedProviders[depID] = provider
			s.debugf(childLogPrefix, "└─ dependency '%s' satisfied", depID)
			return true
		}

		if s.resolutionFailed {
			return false
		}

		s.debugf(childLogPrefix, "└─ candidate '%s' failed; trying next", provider.TopLevelModID)
		s.restoreState(originalState)
	}

	s.unresolvableDeps[depID] = UnresolvableDependency{
		DepID:          depID,
		RequiringModID: requiringModID,
		Predicates:     predicates,
	}
	// Only set the failure reason if one hasn't already been set by a deeper, more specific error.
	if !s.resolutionFailed {
		s.failureReason = fmt.Sprintf("failed to resolve dependency '%s %s' for mod '%s'", depID, predicateStr, requiringModID)
	}
	s.debugf(childLogPrefix, "└─ dependency '%s' unresolved: no candidate could activate", depID)
	return false
}

// findBestProviders finds all activatable mods that provide a given dependency and satisfy the version predicates.
func (s *resolutionSession) findBestProviders(depID string, predicates []*version.VersionPredicate) []*ProviderInfo {
	providerCandidates, ok := getProvidersForDep(s.potentialProviders, depID, s.loader)
	if !ok || len(providerCandidates) == 0 {
		return nil
	}

	// First, filter the list to only providers that are activatable and meet the version requirements.
	var validProviders []*ProviderInfo
	for i := range providerCandidates {
		candidate := &providerCandidates[i]

		if status, ok := s.modStatuses[candidate.TopLevelModID]; ok && !status.IsActivatable() {
			continue
		}

		if !checkAnyPredicatesSatisfied(predicates, candidate.VersionOfProvidedItem) {
			continue
		}

		validProviders = append(validProviders, candidate)
	}

	// Second, sort the list of valid providers by priority. This is crucial for making
	// the best choices first during the backtracking search.
	sort.Slice(validProviders, func(i, j int) bool {
		a := validProviders[i]
		b := validProviders[j]

		// Priority 1: Higher version of the provided item is better.
		versionCmp := a.VersionOfProvidedItem.Compare(b.VersionOfProvidedItem)
		if versionCmp != 0 {
			return versionCmp > 0 // descending order
		}

		// Priority 2: A direct provide is better than a nested provide.
		if a.IsDirectProvide != b.IsDirectProvide {
			return a.IsDirectProvide // true comes before false
		}

		// Priority 3 (Tie-breaker): Alphabetical ID for determinism.
		return a.TopLevelModID < b.TopLevelModID
	})

	return validProviders
}

// validateBreaks performs a final check on the successful resolution set.
func (s *resolutionSession) validateBreaks() error {
	for modID, mod := range s.effectiveSet {
		if mod.Metadata.Breaks == nil {
			continue
		}
		for brokenDepID, predicates := range mod.Metadata.Breaks {
			provider, isProvided := s.cachedProviders[brokenDepID]
			if !isProvided {
				continue
			}
			for _, p := range predicates {
				if p.Test(provider.VersionOfProvidedItem) {
					predicateStr := formatPredicates(predicates)
					return fmt.Errorf("mod '%s' (v%s) breaks '%s' (provided by '%s' v%s) due to rule '%s %s'",
						modID, mod.Metadata.Version.Version, brokenDepID, provider.TopLevelModID, provider.VersionOfProvidedItem, brokenDepID, predicateStr)
				}
			}
		}
	}
	return nil
}

// --- State Management and Logging Helpers ---

// formatPredicates creates a readable string representation of version predicates.
func formatPredicates(predicates []*version.VersionPredicate) string {
	if len(predicates) == 0 {
		return "*"
	}
	var parts []string
	for _, p := range predicates {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ", ")
}

func (s *resolutionSession) copyState() map[string]*Mod {
	stateCopy := make(map[string]*Mod, len(s.effectiveSet))
	for k, v := range s.effectiveSet {
		stateCopy[k] = v
	}
	return stateCopy
}

func (s *resolutionSession) restoreState(state map[string]*Mod) {
	s.effectiveSet = state
}

func (s *resolutionSession) updateNeededForList(modID, neededByModID string) {
	if neededByModID == "System (Initial Set)" {
		return
	}
	info, ok := s.resolutionPath[modID]
	if !ok {
		return
	}
	for _, existingNeeder := range info.NeededFor {
		if existingNeeder == neededByModID {
			return
		}
	}
	info.NeededFor = append(info.NeededFor, neededByModID)
	sort.Strings(info.NeededFor)
	s.resolutionPath[modID] = info
}

func (s *resolutionSession) updateResolutionPath(modID, neededBy, reason, satisfiedDep string) {
	existingInfo, entryExists := s.resolutionPath[modID]
	finalReason := reason
	if entryExists && (existingInfo.Reason == "Target" || existingInfo.Reason == "Forced") {
		finalReason = existingInfo.Reason
	}
	neededForSet := make(sets.Set)
	if entryExists {
		for _, n := range existingInfo.NeededFor {
			neededForSet[n] = struct{}{}
		}
	}
	if neededBy != "System (Initial Set)" {
		neededForSet[neededBy] = struct{}{}
	}
	neededForList := sets.MakeSlice(neededForSet)

	s.resolutionPath[modID] = ResolutionInfo{
		ModID:            modID,
		Reason:           finalReason,
		NeededFor:        neededForList,
		SatisfiedDep:     satisfiedDep,
		SelectedProvider: s.cachedProviders[satisfiedDep],
	}
}

func (s *resolutionSession) collectResolutionPath() ResolutionPath {
	pathSlice := make([]ResolutionInfo, 0, len(s.effectiveSet))
	for _, mod := range s.effectiveSet {
		if info, ok := s.resolutionPath[mod.Metadata.ID]; ok {
			pathSlice = append(pathSlice, info)
		} else {
			logging.Errorf("Resolver: Mod '%s' in effective set but missing resolution path.", mod.Metadata.ID)
			pathSlice = append(pathSlice, ResolutionInfo{ModID: mod.Metadata.ID, Reason: "Error: Path Undefined"})
		}
	}
	sort.Slice(pathSlice, func(i, j int) bool {
		return pathSlice[i].ModID < pathSlice[j].ModID
	})

	return ResolutionPath(pathSlice)
}

// UnresolvableModDetails contains categorized information about unresolvable mods,
// separating direct failures from transitive failures and mapping their root causes.
type UnresolvableModDetails struct {
	DirectlyUnresolvable     map[string][]string // ModID -> missing dependencies
	TransitivelyUnresolvable map[string]sets.Set // ModID -> set of root cause ModIDs
}

// CalculateUnresolvableModsDetails performs a full, iterative dependency check to find all
// broken mods and meticulously tracks the causal chain of failure.
//
// It guarantees that transitively broken mods are accurately mapped to the directly unresolvable
// mods (root causes) that initiated the cascading failure.
//
// Use Case: Ideal for generating detailed, user-facing error reports or logs where explaining
// *why* a mod is broken is just as important as knowing that it is broken.
// Note: This is slightly more resource-intensive than CalculateTransitivelyUnresolvableMods.
// initialCandidates must contain top-level mod IDs.
func (dr *DependencyResolver) CalculateUnresolvableModsDetails(initialCandidates sets.Set) UnresolvableModDetails {
	available := sets.Copy(initialCandidates)

	// 1. Initial pass: Find all directly unresolvable mods (the root causes)
	directlyUnresolvable := dr.CalculateDirectlyUnresolvableModsWithDetails(available)

	transitivelyUnresolvable := make(map[string]sets.Set)
	rootCauses := make(map[string]sets.Set)

	for modID := range directlyUnresolvable {
		delete(available, modID)
		rootCauses[modID] = sets.Set{modID: struct{}{}}
	}

	// 2. Iterative pass: Find transitively unresolvable mods and track causality
	for {
		newlyUnresolvable := dr.CalculateDirectlyUnresolvableModsWithDetails(available)
		if len(newlyUnresolvable) == 0 {
			break
		}

		for modID, failedDeps := range newlyUnresolvable {
			mod := dr.allMods[modID]
			modRootCauses := sets.Set{}

			// To find the root cause, we look at the potential providers for the dependencies that just failed.
			for _, depID := range failedDeps {
				predicates := mod.Metadata.Depends[depID]
				if providers, ok := getProvidersForDep(dr.potentialProviders, depID, dr.loader); ok {
					for _, p := range providers {
						// Check if this provider actually satisfied the version requirements
						if checkAnyPredicatesSatisfied(predicates, p.VersionOfProvidedItem) {
							// If this provider was removed because it is unresolvable, inherit its root causes
							if causes, isUnresolvable := rootCauses[p.TopLevelModID]; isUnresolvable {
								for cause := range causes {
									modRootCauses[cause] = struct{}{}
								}
							}
						}
					}
				}
			}

			// Fallback: If we somehow lost the causal chain, this mod acts as its own root cause
			if len(modRootCauses) == 0 {
				modRootCauses[modID] = struct{}{}
			}

			rootCauses[modID] = modRootCauses
			transitivelyUnresolvable[modID] = modRootCauses
			delete(available, modID)
		}
	}

	return UnresolvableModDetails{
		DirectlyUnresolvable:     directlyUnresolvable,
		TransitivelyUnresolvable: transitivelyUnresolvable,
	}
}

// CalculateTransitivelyUnresolvableMods iteratively calculates the complete boolean set of
// all mods that cannot be resolved, whether directly missing a dependency or failing transitively
// due to cascading dependency failures.
//
// Performance: This is highly optimized for speed. It uses fast-path boolean checks and
// avoids tracking causality or allocating slices for failed dependencies.
//
// Use Case: Ideal for background processing, bisection loops, or filtering where you only
// need to know *if* a mod is broken to exclude it from further testing.
// initialCandidates must contain top-level mod IDs.
func (dr *DependencyResolver) CalculateTransitivelyUnresolvableMods(initialCandidates sets.Set) sets.Set {
	currentlyAvailable := sets.Copy(initialCandidates)
	totalUnresolvable := sets.Set{}

	for {
		newlyFoundUnresolvable := dr.CalculateDirectlyUnresolvableMods(currentlyAvailable)

		if len(newlyFoundUnresolvable) == 0 {
			break
		}

		for modID := range newlyFoundUnresolvable {
			totalUnresolvable[modID] = struct{}{}
		}

		currentlyAvailable = sets.SubtractInPlace(currentlyAvailable, newlyFoundUnresolvable)
	}

	return totalUnresolvable
}

// CalculateDirectlyUnresolvableMods performs a fast, first-degree check to determine which
// mods are immediately unresolvable because they lack providers for their direct dependencies.
//
// It does NOT check if the providers themselves are resolvable (no transitive checks).
// It halts checking a mod the moment its first failed dependency is encountered (early exit).
//
// Use Case: Used internally as the fast-path engine for CalculateTransitivelyUnresolvableMods,
// or for quick surface-level validation of a mod set.
// availableMods must contain top-level mod IDs.
func (dr *DependencyResolver) CalculateDirectlyUnresolvableMods(availableMods sets.Set) sets.Set {
	resMap := dr.calculateDirectlyUnresolvable(availableMods, true)
	resSet := make(sets.Set, len(resMap))
	for modID := range resMap {
		resSet[modID] = struct{}{}
	}
	return resSet
}

// CalculateDirectlyUnresolvableModsWithDetails performs a first-degree check like
// CalculateDirectlyUnresolvableMods, but maps each unresolvable mod to a complete slice
// of every direct dependency that failed to resolve.
//
// Performance: It must evaluate every dependency for every mod without early exiting,
// resulting in memory allocations for the string slices.
//
// Use Case: Useful when you need a shallow, immediate error report for a specific subset
// of mods, without traversing the entire dependency tree.
// availableMods must contain top-level mod IDs.
func (dr *DependencyResolver) CalculateDirectlyUnresolvableModsWithDetails(availableMods sets.Set) map[string][]string {
	return dr.calculateDirectlyUnresolvable(availableMods, false)
}

// calculateDirectlyUnresolvable is the underlying internal engine for all first-degree
// unresolvability checks.
//
// Parameters:
//   - availableMods: The pool of mods currently considered active/available.
//   - earlyExit: If true, optimizes performance by instantly marking a mod as unresolvable
//     upon its first missing dependency and skipping slice allocations. If false, it
//     exhaustively maps all missing dependencies for each broken mod.
//
// availableMods must contain top-level mod IDs.
func (dr *DependencyResolver) calculateDirectlyUnresolvable(availableMods sets.Set, earlyExit bool) map[string][]string {
	unresolvable := make(map[string][]string)
	sortedCandidates := sets.MakeSlice(availableMods)

	for _, modID := range sortedCandidates {
		mod, ok := dr.allMods[modID]
		if !ok {
			continue // Should not happen with a correctly constructed set
		}

		var failedDeps []string
		isUnresolvable := false

		for depID, predicates := range mod.Metadata.Depends {
			if IsImplicitMod(depID) {
				continue
			}

			if !dr.findValidProviderInSet(depID, predicates, availableMods) {
				isUnresolvable = true
				if earlyExit {
					break // Fast path: Stop checking other dependencies
				}
				failedDeps = append(failedDeps, depID)
			}
		}

		if isUnresolvable {
			unresolvable[modID] = failedDeps
		}
	}

	return unresolvable
}

// findValidProviderInSet searches for at least one provider that satisfies the dependency,
// is available in the given set, and matches at least one version predicate.
func (dr *DependencyResolver) findValidProviderInSet(depID string, predicates []*version.VersionPredicate, availableMods sets.Set) bool {
	providerCandidates, found := getProvidersForDep(dr.potentialProviders, depID, dr.loader)
	if !found {
		return false // No potential providers exist for this dependency at all.
	}

	for _, provider := range providerCandidates {
		// Condition 1: The provider's top-level mod must be in our available set.
		if _, providerIsAvailable := availableMods[provider.TopLevelModID]; !providerIsAvailable {
			continue
		}

		// Condition 2: The provider's version must satisfy at least one predicate for this dependency.
		if checkAnyPredicatesSatisfied(predicates, provider.VersionOfProvidedItem) {
			return true // Found a valid provider.
		}
	}

	// Scanned all potential providers and none were suitable.
	return false
}

// checkAnyPredicatesSatisfied returns true if the version satisfies at least
// one predicate in the slice. The predicates in a VersionRanges slice are ORed
// together (see VersionRanges).
func checkAnyPredicatesSatisfied(predicates []*version.VersionPredicate, v version.Version) bool {
	for _, p := range predicates {
		if p.Test(v) {
			return true // At least one predicate was satisfied.
		}
	}
	return false
}

// getProvidersForDep retrieves providers for a dependency, supporting cross-format ID matching for multi loaders.
func getProvidersForDep(potentialProviders PotentialProvidersMap, depID string, loader RunLoader) ([]ProviderInfo, bool) {
	if providers, ok := potentialProviders[depID]; ok {
		return providers, true
	}
	if loader == RunLoaderFabricWithNeoForge || loader == RunLoaderNeoForgeWithFabric {
		if providers, ok := potentialProviders[strings.ReplaceAll(depID, "-", "_")]; ok {
			return providers, true
		}
		if providers, ok := potentialProviders[strings.ReplaceAll(depID, "-", "")]; ok {
			return providers, true
		}
		if providers, ok := potentialProviders[strings.ReplaceAll(depID, "_", "-")]; ok {
			return providers, true
		}
		if providers, ok := potentialProviders[strings.ReplaceAll(depID, "_", "")]; ok {
			return providers, true
		}
	}
	return nil, false
}
