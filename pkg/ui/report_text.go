package ui

import (
	"fmt"
	"strings"
)

// FormatModRef formats a mod reference for display, uniformly as
// "Name (id ver)", or just "id" when the mod is unknown.
func FormatModRef(mod ModViewModel) string {
	if mod.IsUnknown {
		return mod.ID
	}
	return fmt.Sprintf("%s (%s %s)", mod.Name, mod.ID, mod.Version)
}

// TextStyles controls how rendered mod text is decorated. The nil zero value
// renders plain text (used by the log report); the TUI supplies tview color
// tags.
type TextStyles struct {
	// ModID decorates the head of a mod entry line.
	ModID func(string) string
	// Muted decorates dimmed/auxiliary text.
	Muted func(string) string
	// ShowFile appends " from 'X.jar'" / " from unknown" to each reference.
	ShowFile bool
}

func (st TextStyles) modID(s string) string {
	if st.ModID != nil {
		return st.ModID(s)
	}
	return s
}

func (st TextStyles) muted(s string) string {
	if st.Muted != nil {
		return st.Muted(s)
	}
	return s
}

// writeModRef writes a mod reference, optionally including its source file,
// decorating it with deco (entry lines use ModID, dependency lines Muted).
func (st TextStyles) writeModRef(b *strings.Builder, mod ModViewModel, deco func(string) string) {
	b.WriteString(deco(FormatModRef(mod)))
	if st.ShowFile {
		if mod.IsUnknown {
			b.WriteString(" from unknown")
		} else {
			fmt.Fprintf(b, " from '%s.jar'", mod.BaseFilename)
		}
	}
}

// WriteConflictSetMods writes one entry per mod in the set, each with an
// indented sub-list of mods that would also need to be disabled as a
// side-effect.
func WriteConflictSetMods(b *strings.Builder, mods []CascadingDisables, st TextStyles) {
	for _, entry := range mods {
		b.WriteString("  - ")
		st.writeModRef(b, entry.Mod, st.modID)
		b.WriteByte('\n')

		if len(entry.AlsoRequireDisable) > 0 {
			b.WriteString("    ")
			b.WriteString(st.muted("└ Disabling this mod would also require disabling:"))
			b.WriteString("\n")
			for _, dep := range entry.AlsoRequireDisable {
				b.WriteString("      - ")
				st.writeModRef(b, dep, st.muted)
				b.WriteByte('\n')
			}
		}
		if len(entry.PotentialAlsoRequireDisable) > 0 {
			b.WriteString("    ")
			b.WriteString(st.muted("└ Disabling this mod maybe also requires disabling:"))
			b.WriteByte('\n')
			for _, dep := range entry.PotentialAlsoRequireDisable {
				b.WriteString("      - ")
				st.writeModRef(b, dep, st.muted)
				b.WriteString(" (inferred)")
				b.WriteByte('\n')
			}
		}
	}
}

// WriteConflictSetFooter appends a note about mods that would only need to be
// disabled when the entire conflict set is disabled simultaneously.
func WriteConflictSetFooter(b *strings.Builder, extraIfAll []ModViewModel, st TextStyles) {
	if len(extraIfAll) == 0 {
		return
	}

	b.WriteString("  ")
	b.WriteString(st.muted("If you disable all mods in this conflict, you would also need to disable:"))
	b.WriteString("\n")
	for _, dep := range extraIfAll {
		b.WriteString("    - ")
		st.writeModRef(b, dep, st.muted)
		b.WriteByte('\n')
	}
}

// WriteConflictSet writes the full block for a conflict set: per-mod entries
// (each with their "also require disabling" sub-lists) followed by the footer.
func WriteConflictSet(b *strings.Builder, cs ConflictSetReport, st TextStyles) {
	WriteConflictSetMods(b, cs.Mods, st)
	WriteConflictSetFooter(b, cs.IfAllDisabledAlso, st)
	if len(cs.IfAllDisabledPotentialAlso) > 0 {
		b.WriteString("  ")
		b.WriteString(st.muted("If you disable all mods in this conflict, you maybe also need to disable:"))
		b.WriteByte('\n')
		for _, dep := range cs.IfAllDisabledPotentialAlso {
			b.WriteString("    - ")
			st.writeModRef(b, dep, st.muted)
			b.WriteByte('\n')
		}
	}
}

// WriteGenerallyUnresolvable writes the list of mods with broken dependencies
// unrelated to any identified conflict set. The section header is the caller's
// responsibility.
func WriteGenerallyUnresolvable(b *strings.Builder, reports []UnresolvedDependencyReport, st TextStyles) {
	for _, report := range reports {
		if report.Mod.IsUnknown {
			continue
		}
		b.WriteString("  - ")
		st.writeModRef(b, report.Mod, st.modID)
		b.WriteByte('\n')

		if len(report.UnmetDependencies) > 0 {
			b.WriteString("    ")
			b.WriteString(st.muted("└ Unresolved or unmet dependencies:"))
			b.WriteString("\n")
			for _, dep := range report.UnmetDependencies {
				b.WriteString("      - ")
				st.writeModRef(b, dep, st.muted)
				b.WriteByte('\n')
			}
		}

		if len(report.RequiredByTransitive) > 0 {
			b.WriteString("    ")
			b.WriteString(st.muted("└ Disabling this mod would also require disabling:"))
			b.WriteString("\n")
			for _, depMod := range report.RequiredByTransitive {
				b.WriteString("      - ")
				st.writeModRef(b, depMod, st.muted)
				b.WriteByte('\n')
			}
		}
	}
}
