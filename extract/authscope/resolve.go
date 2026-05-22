package authscope

import (
	_ "embed"
)

//go:embed testdata/mapping.json
var embeddedTestMapping []byte

// ResolveMapping loads the mapping from path, or the embedded fictional test fixture when path is empty.
func ResolveMapping(path string) (*Mapping, error) {
	if path != "" {
		return Load(path)
	}
	return LoadBytes(embeddedTestMapping)
}

// LoadBytes parses mapping JSON from data.
func LoadBytes(data []byte) (*Mapping, error) {
	return parseMappingJSON(data)
}

// ApplyOptionsFromPath builds ApplyOptions from extraction settings.
func ApplyOptionsFromPath(enabled bool, mappingPath string) (ApplyOptions, error) {
	if !enabled {
		return ApplyOptions{Enabled: false}, nil
	}
	m, err := ResolveMapping(mappingPath)
	if err != nil {
		return ApplyOptions{}, err
	}
	return ApplyOptions{Enabled: true, Mapping: m}, nil
}
