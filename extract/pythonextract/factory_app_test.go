package pythonextract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFactoryAppRouteDiscovery(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app", "main.py")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `from fastapi import FastAPI

def create_app() -> FastAPI:
    app = FastAPI()

    @app.get("/health")
    def health():
        return {"ok": True}

    return app
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "app")}})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result.Operations) == 0 {
		t.Fatal("expected routes from create_app factory")
	}
	found := false
	for _, op := range result.Operations {
		if op.Path == "/health" && op.Method == "GET" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing GET /health, got %#v", result.Operations)
	}
}
