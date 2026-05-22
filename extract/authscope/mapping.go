// Package authscope translates AMS rights to PAT OAuth scopes for OpenAPI emission.
package authscope

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Mapping holds the right→scope translation table and scope metadata.
type Mapping struct {
	// RightToScopes maps each AMS right id to scope ids that grant it.
	RightToScopes map[string][]string `json:"rightToScopes"`
	// ScopeNames maps scope id to display name (from AMS scopeRepo).
	ScopeNames map[string]string `json:"scopeNames"`
	// ScopeToRights maps scope id to rights it grants (expanded).
	ScopeToRights map[string][]string `json:"scopeToRights"`
}

// Load reads a mapping JSON file produced by ams-mapping-gen.
func Load(path string) (*Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := parseMappingJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse mapping %s: %w", path, err)
	}
	return m, nil
}

func parseMappingJSON(data []byte) (*Mapping, error) {
	var m Mapping
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m.RightToScopes == nil {
		m.RightToScopes = make(map[string][]string)
	}
	if m.ScopeNames == nil {
		m.ScopeNames = make(map[string]string)
	}
	if m.ScopeToRights == nil {
		m.ScopeToRights = make(map[string][]string)
	}
	return &m, nil
}

// DefaultTestMappingPath returns the fictional fixture used by unit tests.
func DefaultTestMappingPath() string {
	return filepath.Join("extract", "authscope", "testdata", "mapping.json")
}

// MinimalScopes returns the smallest set of scope ids whose union covers all rights.
// Unmapped rights are returned separately.
func (m *Mapping) MinimalScopes(rights []string) (scopes []string, unmapped []string) {
	if m == nil || len(rights) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool)
	unique := make([]string, 0, len(rights))
	for _, r := range rights {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		unique = append(unique, r)
	}
	sort.Strings(unique)

	remaining := make(map[string]bool, len(unique))
	for _, r := range unique {
		if _, ok := m.RightToScopes[r]; !ok || len(m.RightToScopes[r]) == 0 {
			unmapped = append(unmapped, r)
			continue
		}
		remaining[r] = true
	}

	chosen := make(map[string]bool)
	for len(remaining) > 0 {
		bestScope := ""
		bestCover := 0
		for scopeID, scopeRights := range m.ScopeToRights {
			if chosen[scopeID] {
				continue
			}
			cover := 0
			for _, r := range scopeRights {
				if remaining[r] {
					cover++
				}
			}
			if cover > bestCover {
				bestCover = cover
				bestScope = scopeID
			}
		}
		if bestScope == "" || bestCover == 0 {
			for r := range remaining {
				unmapped = append(unmapped, r)
			}
			break
		}
		chosen[bestScope] = true
		for _, r := range m.ScopeToRights[bestScope] {
			delete(remaining, r)
		}
	}

	out := make([]string, 0, len(chosen))
	for s := range chosen {
		out = append(out, s)
	}
	sort.Strings(out)
	sort.Strings(unmapped)
	return out, unmapped
}

// RightsForScopes returns all rights granted by the given scope ids.
func (m *Mapping) RightsForScopes(scopes []string) []string {
	if m == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, s := range scopes {
		for _, r := range m.ScopeToRights[s] {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	sort.Strings(out)
	return out
}

// IsKnownRight reports whether id appears in the mapping as a right.
func (m *Mapping) IsKnownRight(id string) bool {
	if m == nil {
		return false
	}
	_, ok := m.RightToScopes[id]
	return ok
}

// IsKnownScope reports whether id appears in the mapping as a scope.
func (m *Mapping) IsKnownScope(id string) bool {
	if m == nil {
		return false
	}
	_, ok := m.ScopeToRights[id]
	return ok
}

// Classify splits tokens into rights and oauth scope ids using the mapping.
// Tokens unknown to both tables are treated as rights (typical @RequireRight output).
func (m *Mapping) Classify(tokens []string) (rights, oauthScopes []string) {
	if len(tokens) == 0 {
		return nil, nil
	}
	seenR := make(map[string]bool)
	seenS := make(map[string]bool)
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if m != nil && m.IsKnownScope(t) {
			if !seenS[t] {
				seenS[t] = true
				oauthScopes = append(oauthScopes, t)
			}
			continue
		}
		if !seenR[t] {
			seenR[t] = true
			rights = append(rights, t)
		}
	}
	sort.Strings(rights)
	sort.Strings(oauthScopes)
	return rights, oauthScopes
}
