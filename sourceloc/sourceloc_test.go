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
				"x-source-file":   "Foo.java",
				"x-source-line":   42,
				"x-source-column": 5,
			},
		},
		{
			name: "file only",
			loc:  Location{File: "Bar.ts"},
			want: map[string]interface{}{
				"x-source-file": "Bar.ts",
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
				"x-source-line":   10,
				"x-source-column": 3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.loc.EmitExtensions()
			if len(got) != len(tt.want) {
				t.Errorf("EmitExtensions() returned %d keys, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("EmitExtensions()[%q] = %v, want %v", k, got[k], v)
				}
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

	if m["x-source-file"] != "foo.go" {
		t.Errorf("expected x-source-file=foo.go, got %v", m["x-source-file"])
	}
	if m["x-source-line"] != 10 {
		t.Errorf("expected x-source-line=10, got %v", m["x-source-line"])
	}
	if m["operationId"] != "getFoo" {
		t.Error("ApplyTo should not overwrite existing keys")
	}
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
