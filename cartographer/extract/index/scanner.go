package index

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/parser"
)

// Scanner populates the type index by parsing source files with tree-sitter.
type Scanner struct {
	pool  *parser.Pool
	index *Index
	lang  string
}

// NewScanner creates a scanner for the given language.
func NewScanner(pool *parser.Pool, idx *Index, lang string) *Scanner {
	return &Scanner{pool: pool, index: idx, lang: lang}
}

// ScanDir recursively scans root for source files matching the language extension
// and populates the index with type declarations found via tree-sitter queries.
func (s *Scanner) ScanDir(root string) error {
	ext := languageExtension(s.lang)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable dirs
		}
		if d.IsDir() {
			base := d.Name()
			// Skip common non-source directories
			if base == "node_modules" || base == ".git" || base == "build" || base == "target" || base == "dist" || base == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ext) {
			return nil
		}
		return s.scanFile(path)
	})
}

func (s *Scanner) scanFile(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil // skip unreadable files
	}

	tree, err := s.pool.Parse(s.lang, source)
	if err != nil {
		return nil // skip parse failures
	}
	defer tree.Close()

	switch s.lang {
	case "java":
		s.scanJavaFile(path, source, tree)
	case "typescript":
		s.scanTypeScriptFile(path, source, tree)
	}

	return nil
}

func languageExtension(lang string) string {
	switch lang {
	case "java":
		return ".java"
	case "typescript":
		return ".ts"
	default:
		return ""
	}
}
