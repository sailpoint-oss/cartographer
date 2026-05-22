package authscope

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// BuildFromAMSRepo reads scopeRepo.json and rightSetRepo.json under repo and returns a Mapping.
func BuildFromAMSRepo(repoRoot string) (*Mapping, error) {
	jsonDir := filepath.Join(repoRoot, "ams-repo", "src", "main", "resources", "com", "sailpoint", "ams", "repository", "json")
	rsPath := filepath.Join(jsonDir, "rightSetRepo.json")
	scPath := filepath.Join(jsonDir, "scopeRepo.json")

	rsData, err := os.ReadFile(rsPath)
	if err != nil {
		return nil, fmt.Errorf("read rightSetRepo: %w", err)
	}
	scData, err := os.ReadFile(scPath)
	if err != nil {
		return nil, fmt.Errorf("read scopeRepo: %w", err)
	}

	var rightSets []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Rights      []string `json:"rights"`
		RightSetIDs []string `json:"rightSetIds"`
	}
	if err := json.Unmarshal(rsData, &rightSets); err != nil {
		return nil, fmt.Errorf("parse rightSetRepo: %w", err)
	}
	var scopes []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		RightSetIDs []string `json:"rightSetIds"`
	}
	if err := json.Unmarshal(scData, &scopes); err != nil {
		return nil, fmt.Errorf("parse scopeRepo: %w", err)
	}

	rsToRights := make(map[string][]string)
	for _, rs := range rightSets {
		rsToRights[rs.ID] = expandRightSet(rs.ID, rightSets, make(map[string]bool))
	}

	scopeToRights := make(map[string][]string)
	scopeNames := make(map[string]string)
	for _, sc := range scopes {
		scopeNames[sc.ID] = sc.Name
		rights := make(map[string]bool)
		for _, rsID := range sc.RightSetIDs {
			for _, r := range rsToRights[rsID] {
				rights[r] = true
			}
		}
		var list []string
		for r := range rights {
			list = append(list, r)
		}
		sort.Strings(list)
		scopeToRights[sc.ID] = list
	}

	rightToScopes := make(map[string][]string)
	for scopeID, rights := range scopeToRights {
		for _, r := range rights {
			rightToScopes[r] = append(rightToScopes[r], scopeID)
		}
	}
	for r, scs := range rightToScopes {
		sort.Strings(scs)
		rightToScopes[r] = dedupeStrings(scs)
	}

	return &Mapping{
		RightToScopes: rightToScopes,
		ScopeNames:    scopeNames,
		ScopeToRights: scopeToRights,
	}, nil
}

func expandRightSet(id string, all []struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Rights      []string `json:"rights"`
	RightSetIDs []string `json:"rightSetIds"`
}, visiting map[string]bool) []string {
	if visiting[id] {
		return nil
	}
	visiting[id] = true
	defer delete(visiting, id)

	var rs *struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Rights      []string `json:"rights"`
		RightSetIDs []string `json:"rightSetIds"`
	}
	for i := range all {
		if all[i].ID == id {
			rs = &all[i]
			break
		}
	}
	if rs == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, r := range rs.Rights {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, child := range rs.RightSetIDs {
		for _, r := range expandRightSet(child, all, visiting) {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	sort.Strings(out)
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// WriteJSON writes mapping to path.
func WriteJSON(path string, m *Mapping) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
