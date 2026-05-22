package index

import (
	"path/filepath"
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/parser"
)

func TestSerializationJavaSnakeCaseNaming(t *testing.T) {
	pool := parser.NewPool()
	if err := pool.RegisterJava(); err != nil {
		t.Fatal(err)
	}
	idx := New()
	scanner := NewScanner(pool, idx, "java")
	root := filepath.Join("..", "testdata", "serialization-java")
	if err := scanner.ScanDir(root); err != nil {
		t.Fatalf("scan: %v", err)
	}
	decl, ok := idx.Resolve("com.example.NamingDTO")
	if !ok {
		t.Fatal("NamingDTO not indexed")
	}
	byName := map[string]FieldDecl{}
	for _, f := range decl.Fields {
		byName[f.Name] = f
	}
	if got := byName["userName"].JSONName; got != "user_name" {
		t.Fatalf("userName wire name = %q, want user_name", got)
	}
	if got := byName["customId"].JSONName; got != "custom_id" {
		t.Fatalf("explicit @JsonProperty must win, got %q", got)
	}
}

func TestSerializationPythonSerializationAlias(t *testing.T) {
	pool := parser.NewPool()
	if err := pool.RegisterPython(); err != nil {
		t.Fatal(err)
	}
	idx := New()
	scanner := NewScanner(pool, idx, "python")
	root := filepath.Join("..", "testdata", "serialization-python")
	if err := scanner.ScanDir(root); err != nil {
		t.Fatalf("scan: %v", err)
	}
	decl, ok := idx.ResolveSimple("UserDto")
	if !ok {
		t.Fatal("UserDto not indexed")
	}
	for _, f := range decl.Fields {
		if f.Name == "user_id" && f.JSONName != "userId" {
			t.Fatalf("serialization_alias wire = %q, want userId", f.JSONName)
		}
	}
}

func TestSerializationTSApiPropertyName(t *testing.T) {
	pool := parser.NewPool()
	if err := pool.RegisterTypeScript(); err != nil {
		t.Fatal(err)
	}
	idx := New()
	scanner := NewScanner(pool, idx, "typescript")
	root := filepath.Join("..", "testdata", "serialization-ts", "src")
	if err := scanner.ScanDir(root); err != nil {
		t.Fatalf("scan: %v", err)
	}
	decl, ok := idx.ResolveSimple("UserDto")
	if !ok {
		t.Fatal("UserDto not indexed")
	}
	for _, f := range decl.Fields {
		if f.Name == "userName" && f.JSONName != "user_name" {
			t.Fatalf("ApiProperty name wire = %q, want user_name", f.JSONName)
		}
	}
}
