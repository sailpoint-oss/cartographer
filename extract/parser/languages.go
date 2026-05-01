package parser

import (
	"unsafe"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// RegisterJava registers the Java grammar.
func (p *Pool) RegisterJava() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.languages["java"] = tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_java.Language()))
	return nil
}

// RegisterTypeScript registers the TypeScript grammar.
func (p *Pool) RegisterTypeScript() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.languages["typescript"] = tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_typescript.LanguageTypescript()))
	return nil
}

// RegisterPython registers the Python grammar.
func (p *Pool) RegisterPython() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.languages["python"] = tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_python.Language()))
	return nil
}

// RegisterCSharp registers the C# grammar.
func (p *Pool) RegisterCSharp() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.languages["csharp"] = tree_sitter.NewLanguage(unsafe.Pointer(tree_sitter_csharp.Language()))
	return nil
}
