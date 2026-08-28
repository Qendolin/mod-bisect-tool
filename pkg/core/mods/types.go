package mods

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Qendolin/mod-bisect-tool/pkg/core/mods/version"
	"github.com/Qendolin/mod-bisect-tool/pkg/logging"
	"github.com/titanous/json5"
)

// ManifestLoader identifies which mod loader a mod manifest targets.
type ManifestLoader string

const (
	ManifestLoaderNone     = ManifestLoader("")
	ManifestLoaderFabric   = ManifestLoader("Fabric")
	ManifestLoaderQuilt    = ManifestLoader("Quilt")
	ManifestLoaderNeoForge = ManifestLoader("NeoForge")
)

// VersionField is a wrapper for version.Version that handles JSON unmarshaling
// from a string, ensuring the version is parsed and valid at load time.
type VersionField struct {
	version.Version
}

func (vf VersionField) String() string {
	if vf.Version == nil {
		return "<invalid>"
	}
	return vf.Version.String()
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (vf *VersionField) UnmarshalJSON(data []byte) error {
	var versionStr string
	if err := json.Unmarshal(data, &versionStr); err != nil {
		logging.Debugf("VersionField: Unmarshal to string failed: %v", err)
		return fmt.Errorf("version field is not a string: %w", err)
	}

	parsed, err := version.Parse(versionStr, false)
	if err != nil {
		logging.Debugf("VersionField: version.Parse failed for '%s': %v", versionStr, err)
		return fmt.Errorf("parsing version string '%s': %w", versionStr, err)
	}

	vf.Version = parsed
	return nil
}

// VersionRanges is a custom type for dependency maps that handles parsing
// of version predicate strings into a slice of VersionPredicate objects.
// A dependency can be satisfied if ANY of the predicates in the slice are met (OR relationship).
type VersionRanges map[string][]*version.VersionPredicate

// UnmarshalJSON implements the json.Unmarshaler interface, allowing us to parse
// the complex "string or array of strings" format for version ranges.
func (vr *VersionRanges) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json5.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing dependency block: %w", err)
	}

	parsed := make(VersionRanges)

	for depID, value := range raw {
		var predicateStrings []string

		switch v := value.(type) {
		case string:
			predicateStrings = append(predicateStrings, v)
		case []interface{}:
			for i, item := range v {
				str, ok := item.(string)
				if !ok {
					return fmt.Errorf("dependency '%s' has a non-string element at index %d in its version range array", depID, i)
				}
				predicateStrings = append(predicateStrings, str)
			}
		default:
			return fmt.Errorf("dependency '%s' has an invalid version range format (must be string or array of strings)", depID)
		}

		predicates := make([]*version.VersionPredicate, len(predicateStrings))
		for i, pStr := range predicateStrings {
			p, err := version.ParseVersionPredicate(pStr)
			if err != nil {
				return fmt.Errorf("parsing version predicate '%s' for dependency '%s': %w", pStr, depID, err)
			}
			predicates[i] = p
		}
		parsed[depID] = predicates
	}

	*vr = parsed
	return nil
}

// ProviderInfo describes a mod that can satisfy a dependency.
type ProviderInfo struct {
	TopLevelModID         string
	VersionOfProvidedItem version.Version
	IsDirectProvide       bool
	TopLevelModVersion    version.Version
}

// PotentialProvidersMap maps a dependency ID to a list of ProviderInfo structs.
type PotentialProvidersMap map[string][]ProviderInfo

// ModMetadata contains the metadata extracted from a mod's manifest file.
// It represents the core information about a mod as defined in its manifest (fabric.mod.json, quilt.mod.json, or neoforge.mods.toml).
type ModMetadata struct {
	// ID is the unique identifier for the mod (e.g., "example-mod").
	ID string
	// Name is the human-readable name of the mod.
	Name string
	// Version is the mod's version number.
	Version VersionField
	// Loader indicates which mod loader the mod is designed for.
	Loader ManifestLoader
	// Provides is a list of mod IDs this mod provides. This does not include the mod's own ID.
	Provides []string
	// Depends maps mod IDs to version ranges, representing required dependencies.
	Depends VersionRanges
	// Breaks maps mod IDs to version ranges, representing mods that are broken by this mod.
	Breaks VersionRanges
	// Recommends maps mod IDs to version ranges, representing recommended (but not required) dependencies.
	Recommends VersionRanges
	// Suggests maps mod IDs to version ranges, representing suggested (optional) dependencies.
	Suggests VersionRanges
	// Conflicts maps mod IDs to version ranges, representing mods that are incompatible with this mod.
	Conflicts VersionRanges
	// Jars is a list of nested JAR file paths referenced by this mod (e.g., for container mods).
	Jars []string
	// Indicates that this mod metadata doesn't represent a real mod
	IsJavaLibrary bool
}

// NestedModule holds metadata for a mod found inside another JAR file,
// including its full path within the parent archive.
type NestedModule struct {
	Info      ModMetadata
	PathInJar string
}

// Mod represents a single discovered mod and its metadata.
type Mod struct {
	Path              string
	BaseFilename      string
	Metadata          ModMetadata
	NestedModules     []NestedModule
	EffectiveProvides map[string]version.Version // Maps all unique IDs this mod provides to their version.
}

// FriendlyName returns a human-readable name for the mod.
func (m *Mod) FriendlyName() string {
	if m == nil {
		return "Unknown Mod"
	}
	if m.Metadata.Name != "" {
		return m.Metadata.Name
	}
	return m.Metadata.ID
}

// ModStatus represents the current runtime state of a single mod.
type ModStatus struct {
	ID             string
	Mod            *Mod
	ForceEnabled   bool // Is mutually exclusive with ForceDisabled and Omitted
	ForceDisabled  bool
	Omitted        bool // Previously called ManuallyGood. Mod is not a search candidate, but can be activated
	IsMissing      bool
	IsProblematic  bool
	IsUnresolvable bool
}

func (s ModStatus) IsSearchCandidate() bool {
	return !s.ForceEnabled && !s.ForceDisabled && !s.Omitted && !s.IsMissing && !s.IsUnresolvable && !s.IsProblematic
}

func (s ModStatus) IsActivatable() bool {
	return !s.ForceDisabled && !s.IsMissing && !s.IsUnresolvable && !s.IsProblematic
}

func (s ModStatus) IsUserEditable() bool {
	return !s.IsMissing
}

// ResolutionInfo stores details about why a mod is included in an effective set.
type ResolutionInfo struct {
	ModID            string
	Reason           string
	NeededFor        []string
	SatisfiedDep     string
	SelectedProvider *ProviderInfo
}

// ResolutionPath is a slice of ResolutionInfo that provides a custom string
// representation for logging the dependency activation paths.
type ResolutionPath []ResolutionInfo

// String implements the fmt.Stringer interface for ResolutionPath.
func (rp ResolutionPath) String() string {
	var depLogMessages []string
	for _, info := range rp {
		// We only want to log mods that were pulled in as dependencies.
		if info.Reason != "Dependency" {
			continue
		}

		// Add a header only if we find at least one dependency to log.
		if len(depLogMessages) == 0 {
			depLogMessages = append(depLogMessages, "Dependency activation paths:")
		}

		neededForStr := strings.Join(info.NeededFor, ", ")
		providerStr := ""
		if info.SelectedProvider != nil {
			providerStr = fmt.Sprintf(" (via %s v%s)", info.SelectedProvider.TopLevelModID, info.SelectedProvider.VersionOfProvidedItem)
		}
		depLogMessages = append(depLogMessages, fmt.Sprintf("  - Mod '%s': Satisfies: '%s'%s, Required by: [%s]",
			info.ModID, info.SatisfiedDep, providerStr, neededForStr))
	}

	if len(depLogMessages) == 0 {
		return "No cross-mod dependencies were activated."
	}
	return strings.Join(depLogMessages, "\n")
}

// IsImplicitMod checks if a dependency ID is for an implicit (non-mod) dependency.
func IsImplicitMod(depID string) bool {
	switch depID {
	case "java", "minecraft", "fabricloader", "quilt_loader", "neoforge", "forge":
		return true
	}
	return false
}

// OverrideAction defines the type of modification for a rule.
type OverrideAction int

const (
	ActionReplace OverrideAction = iota
	ActionAdd
	ActionRemove
)

func (a OverrideAction) String() string {
	switch a {
	case ActionReplace:
		return "Replace"
	case ActionAdd:
		return "Add"
	case ActionRemove:
		return "Remove"
	default:
		return "Unknown"
	}
}

// OverrideSource indicates where an override rule originates from.
type OverrideSource int

const (
	// OverrideSourceBuiltin indicates an override from the embedded dependency_overrides.json
	OverrideSourceBuiltin OverrideSource = iota
	// OverrideSourceUserProvided indicates an override from a user-provided config file
	OverrideSourceUserProvided
)

// String returns the string representation of the OverrideSource.
func (s OverrideSource) String() string {
	switch s {
	case OverrideSourceBuiltin:
		return "builtin"
	case OverrideSourceUserProvided:
		return "user-provided"
	}
	return "unknown"
}

// OverrideRule is the interface for any dependency or provides override rule.
type OverrideRule interface {
	Apply(mm *ModMetadata)
	Target() string
	Field() string
	Key() string
	Action() OverrideAction
	Value() string
	Source() OverrideSource
}

// DependencyOverrides holds a pre-parsed list of polymorphic rules.
type DependencyOverrides struct {
	Rules []OverrideRule
}

// FileMissingError represents an error for a single missing mod file.
type FileMissingError struct {
	ModID        string
	FileBasePath string
}

func (e *FileMissingError) Error() string {
	return fmt.Sprintf("file not found for mod '%s': %s", e.ModID, e.FileBasePath)
}

// MissingFilesError is a wrapper error that contains one or more FileMissingError instances.
type MissingFilesError struct {
	Errors []*FileMissingError
}

func (e *MissingFilesError) Error() string {
	missing := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		missing[i] = fmt.Sprintf("%s at %s", err.ModID, err.FileBasePath)
	}
	return fmt.Sprintf("found %d missing mod files: %s", len(e.Errors), missing)
}
