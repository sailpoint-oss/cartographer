package sourceloc

import (
	"testing"
)

func TestEmitExtensions(t *testing.T) {
	tests := []struct {
		name string
		loc  Location
		want map[string]interface{}
	}{
		{
			name: "all fields",
			loc:  Location{File: "Foo.java", Line: 42, Column: 5},
			want: map[string]interface{}{
				"x-source": map[string]interface{}{
					"file":   "Foo.java",
					"line":   42,
					"column": 5,
				},
			},
		},
		{
			name: "file only",
			loc:  Location{File: "Bar.ts"},
			want: map[string]interface{}{
				"x-source": map[string]interface{}{"file": "Bar.ts"},
			},
		},
		{
			name: "empty",
			loc:  Location{},
			want: map[string]interface{}{},
		},
		{
			name: "line and column only",
			loc:  Location{Line: 10, Column: 3},
			want: map[string]interface{}{
				"x-source": map[string]interface{}{"line": 10, "column": 3},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.loc.EmitExtensions()
			if len(got) != len(tt.want) {
				t.Errorf("EmitExtensions() returned %d keys, want %d", len(got), len(tt.want))
			}
			if !equalMaps(got, tt.want) {
				t.Errorf("EmitExtensions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestApplyTo(t *testing.T) {
	m := map[string]interface{}{
		"operationId": "getFoo",
	}
	loc := Location{File: "foo.go", Line: 10, Column: 1}
	loc.ApplyTo(m)

	source, ok := m["x-source"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected x-source object, got %v", m["x-source"])
	}
	if source["file"] != "foo.go" {
		t.Errorf("expected x-source.file=foo.go, got %v", source["file"])
	}
	if source["line"] != 10 {
		t.Errorf("expected x-source.line=10, got %v", source["line"])
	}
	if m["operationId"] != "getFoo" {
		t.Error("ApplyTo should not overwrite existing keys")
	}
}

func equalMaps(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		am, aok := av.(map[string]interface{})
		bm, bok := bv.(map[string]interface{})
		if aok || bok {
			if !aok || !bok || !equalMaps(am, bm) {
				return false
			}
			continue
		}
		if av != bv {
			return false
		}
	}
	return true
}

func TestIsZero(t *testing.T) {
	if !(Location{}).IsZero() {
		t.Error("empty Location should be zero")
	}
	if (Location{File: "x"}).IsZero() {
		t.Error("Location with File should not be zero")
	}
	if (Location{Line: 1}).IsZero() {
		t.Error("Location with Line should not be zero")
	}
}
