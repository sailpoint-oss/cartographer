package sourcemap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sailpoint-oss/cartographer/sourceloc"
)

func (sm *SourceMap) WriteJSON(outPath string) error {
	if sm == nil {
		sm = New()
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create sourcemap dir: %w", err)
	}
	data, err := json.MarshalIndent(sm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sourcemap: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf("write sourcemap: %w", err)
	}
	return nil
}

func ReadJSON(path string) (*SourceMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sourcemap: %w", err)
	}
	var sm SourceMap
	if err := json.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("parse sourcemap: %w", err)
	}
	if sm.OperationMap == nil {
		sm.OperationMap = make(map[string]sourceloc.Location)
	}
	if sm.SchemaMap == nil {
		sm.SchemaMap = make(map[string]sourceloc.Location)
	}
	if sm.FieldMap == nil {
		sm.FieldMap = make(map[string]sourceloc.Location)
	}
	return &sm, nil
}
