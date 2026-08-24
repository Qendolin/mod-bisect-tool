package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods"
	"github.com/Qendolin/mod-bisect-tool/pkg/ui"
)

// buildUnresolvableModInfos converts the reconcile report's directly-unresolvable
// mods (mod id -> failing dependencies) into a deterministic list for the UI.
func (a *App) buildUnresolvableModInfos(mods map[string][]string) []ui.UnresolvableModInfo {
	allMods := a.bisectSvc.StateManager().GetAllMods()
	ids := make([]string, 0, len(mods))
	for id := range mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	infos := make([]ui.UnresolvableModInfo, 0, len(ids))
	for _, id := range ids {
		infos = append(infos, ui.UnresolvableModInfo{
			Mod:         makeModVM(id, allMods),
			DepsDisplay: formatDependencyRefs(allMods[id], mods[id]),
		})
	}
	return infos
}

// formatDependencyRefs renders each failing dependency id together with its
// version predicates, e.g. "nonexistent (>=1.0)", one entry per dependency.
func formatDependencyRefs(mod *mods.Mod, depIDs []string) []string {
	refs := make([]string, 0, len(depIDs))
	for _, depID := range depIDs {
		ref := depID
		if mod != nil {
			if predicates := mod.Metadata.Depends[depID]; len(predicates) > 0 {
				parts := make([]string, 0, len(predicates))
				for _, p := range predicates {
					parts = append(parts, p.String())
				}
				ref = fmt.Sprintf("%s (%s)", depID, strings.Join(parts, ", "))
			}
		}
		refs = append(refs, ref)
	}
	return refs
}
