package extract

import (
	"fmt"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/parser"
)

// Config configures extraction.
type Config struct {
	RootDir    string   // project root
	SourceDirs []string // source directories to scan
	Language   string   // java, typescript
	OutputPath string   // where to write OpenAPI spec
}

// Engine runs tree-sitter based extraction for Java and TypeScript.
// Go extraction uses the goextract package directly (go/ast based).
type Engine struct {
	parserPool *parser.Pool
	idx        *index.Index
}

// NewEngine creates an extraction engine with tree-sitter parsing.
func NewEngine() *Engine {
	return &Engine{
		parserPool: parser.NewPool(),
		idx:        index.New(),
	}
}

// Extract runs extraction on the configured project using tree-sitter.
func (e *Engine) Extract(cfg Config) (*Metadata, error) {
	// Register language grammar
	switch cfg.Language {
	case "java":
		if err := e.parserPool.RegisterJava(); err != nil {
			return nil, fmt.Errorf("register java: %w", err)
		}
	case "typescript":
		if err := e.parserPool.RegisterTypeScript(); err != nil {
			return nil, fmt.Errorf("register typescript: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported language for tree-sitter extraction: %s", cfg.Language)
	}

	// Scan source directories to populate type index
	scanner := index.NewScanner(e.parserPool, e.idx, cfg.Language)
	dirs := cfg.SourceDirs
	if len(dirs) == 0 {
		dirs = []string{cfg.RootDir}
	}
	for _, dir := range dirs {
		if err := scanner.ScanDir(dir); err != nil {
			return nil, fmt.Errorf("scan %s: %w", dir, err)
		}
	}

	// Build metadata from indexed types
	meta := &Metadata{
		Operations: nil,
		Schemas:    make(map[string]interface{}),
		Tags:       make(map[string]TagInfo),
		Webhooks:   make(map[string]interface{}),
	}

	// Convert all indexed types to OpenAPI schemas
	for _, decl := range e.idx.All() {
		schema := e.idx.ToOpenAPISchema(decl, nil)
		meta.Schemas[decl.Name] = schema
	}

	return meta, nil
}

// ParserPool returns the underlying parser pool for direct query usage.
func (e *Engine) ParserPool() *parser.Pool {
	return e.parserPool
}

// TypeIndex returns the underlying type index.
func (e *Engine) TypeIndex() *index.Index {
	return e.idx
}
