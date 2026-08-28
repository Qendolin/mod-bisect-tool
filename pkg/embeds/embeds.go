package embeds

import _ "embed"

//go:embed dependency_overrides.json
var embeddedOverrides []byte

// GetEmbeddedOverrides returns the content of the built-in dependency override file.
func GetEmbeddedOverrides() []byte {
	return embeddedOverrides
}
