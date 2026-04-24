// Package parser provides tree-sitter parsing for Java and TypeScript source files.
package parser

import (
	"fmt"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Pool holds tree-sitter language grammars and creates fresh parsers per Parse call.
type Pool struct {
	mu        sync.Mutex
	languages map[string]*tree_sitter.Language
}

// NewPool creates a new parser pool.
func NewPool() *Pool {
	return &Pool{languages: make(map[string]*tree_sitter.Language)}
}

// Parse parses source bytes using the grammar registered for lang.
// Returns a *tree_sitter.Tree that the caller must close when done.
func (p *Pool) Parse(lang string, source []byte) (*tree_sitter.Tree, error) {
	p.mu.Lock()
	language, ok := p.languages[lang]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("language %q not registered; call Register*() first", lang)
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		return nil, fmt.Errorf("set language %s: %w", lang, err)
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s: returned nil tree", lang)
	}
	return tree, nil
}

// Language returns the *tree_sitter.Language for the given name, or nil.
func (p *Pool) Language(lang string) *tree_sitter.Language {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.languages[lang]
}
