package sourcemap

import (
	"fmt"
	"strings"

	"github.com/sailpoint-oss/cartographer/sourceloc"
)

// SourceMap maps OpenAPI entities back to source code locations.
type SourceMap struct {
	OperationMap map[string]sourceloc.Location `json:"operationMap"`
	SchemaMap    map[string]sourceloc.Location `json:"schemaMap"`
	FieldMap     map[string]sourceloc.Location `json:"fieldMap"`
}

func New() *SourceMap {
	return &SourceMap{
		OperationMap: make(map[string]sourceloc.Location),
		SchemaMap:    make(map[string]sourceloc.Location),
		FieldMap:     make(map[string]sourceloc.Location),
	}
}

func OperationKey(method, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + ":" + strings.TrimSpace(path)
}

func FieldKey(schemaName, fieldName string) string {
	return strings.TrimSpace(schemaName) + "." + strings.TrimSpace(fieldName)
}

func (sm *SourceMap) PutOperation(method, path string, loc sourceloc.Location) {
	if sm == nil || loc.IsZero() {
		return
	}
	if sm.OperationMap == nil {
		sm.OperationMap = make(map[string]sourceloc.Location)
	}
	key := OperationKey(method, path)
	sm.OperationMap[key] = loc
	// Path-only fallback for consumers that only know path.
	if path != "" {
		if _, exists := sm.OperationMap[path]; !exists {
			sm.OperationMap[path] = loc
		}
	}
}

func (sm *SourceMap) FindOperation(method, path string) (sourceloc.Location, bool) {
	if sm == nil {
		return sourceloc.Location{}, false
	}
	if method != "" {
		if loc, ok := sm.OperationMap[OperationKey(method, path)]; ok && !loc.IsZero() {
			return loc, true
		}
	}
	loc, ok := sm.OperationMap[path]
	return loc, ok && !loc.IsZero()
}

func (sm *SourceMap) FindSchema(name string) (sourceloc.Location, bool) {
	if sm == nil {
		return sourceloc.Location{}, false
	}
	loc, ok := sm.SchemaMap[name]
	return loc, ok && !loc.IsZero()
}

func (sm *SourceMap) FindField(schemaName, fieldName string) (sourceloc.Location, bool) {
	if sm == nil {
		return sourceloc.Location{}, false
	}
	loc, ok := sm.FieldMap[FieldKey(schemaName, fieldName)]
	return loc, ok && !loc.IsZero()
}

func readLocation(m map[string]any) sourceloc.Location {
	if len(m) == 0 {
		return sourceloc.Location{}
	}
	if src := asMap(m["x-source"]); len(src) > 0 {
		return sourceloc.Location{
			File:   asString(src["file"]),
			Line:   asInt(src["line"]),
			Column: asInt(src["column"]),
		}
	}
	return sourceloc.Location{
		File:   asString(m["x-source-file"]),
		Line:   asInt(m["x-source-line"]),
		Column: asInt(m["x-source-column"]),
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func asMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}

func asMapFromIface(v any) map[string]interface{} {
	if v == nil {
		return nil
	}
	m, _ := v.(map[string]interface{})
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		out[k] = val
	}
	return out
}

func normalizeSpec(spec map[string]any) map[string]any {
	if spec != nil {
		return spec
	}
	return make(map[string]any)
}

func sourceLookupError(specPath string, err error) error {
	return fmt.Errorf("build source map from %s: %w", specPath, err)
}
