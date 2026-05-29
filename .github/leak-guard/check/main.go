// Command check is the leak-guard scanner shipped to each public repo. It
// runs as part of PR CI and from the pre-push git hook. It loads the
// salted bloom filter + shape-only patterns living next to it on disk,
// tokenises the file paths it was given, and exits non-zero on any hit.
//
// This source is intentionally standalone: it has zero dependency on any
// upstream Go module and ships its own go.mod. The master denylist that
// feeds the filter does NOT live here; only the irreversible bloom artefact
// produced from it is shipped.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

func main() {
	defGuardDir := defaultGuardDir()
	var (
		bloomPath    = flag.String("bloom", filepath.Join(defGuardDir, "bloom.bin"), "path to bloom filter")
		saltPath     = flag.String("salt", filepath.Join(defGuardDir, "salt.bin"), "path to salt")
		patternsPath = flag.String("patterns", filepath.Join(defGuardDir, "patterns.yaml"), "path to patterns")
		allowPath    = flag.String("allow", filepath.Join(defGuardDir, "allow.txt"), "path to allow list (optional)")
		skipPath      = flag.String("skip", filepath.Join(defGuardDir, "skip-globs.txt"), "path to managed skip globs (optional)")
		skipLocalPath = flag.String("skip-local", filepath.Join(defGuardDir, "skip-globs.local.txt"), "path to per-repo skip globs (optional, not overwritten by regen)")
		root         = flag.String("root", ".", "scan root")
		verbose      = flag.Bool("verbose", false, "show offending tokens (use only locally; CI should run without)")
		failOnWarn   = flag.Bool("fail-on-warning", false, "exit non-zero on warnings too (default: only errors fail)")
	)
	flag.Parse()

	c, err := loadChecker(*bloomPath, *saltPath, *patternsPath, *allowPath, *skipPath, *skipLocalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "leak-guard: %v\n", err)
		os.Exit(2)
	}
	hits, err := c.scanDir(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "leak-guard: %v\n", err)
		os.Exit(2)
	}
	renderHits(os.Stdout, hits, *verbose)
	errs := 0
	for _, h := range hits {
		if h.severity == "error" {
			errs++
		}
	}
	if errs > 0 {
		os.Exit(1)
	}
	if *failOnWarn && len(hits) > 0 {
		os.Exit(1)
	}
}

// defaultGuardDir attempts to locate the leak-guard directory by walking up
// from the source file's package (works for `go run .` invocations from
// .github/leak-guard/check/) and finally falls back to the standard relative
// path so explicit flags always remain available.
func defaultGuardDir() string {
	wd, err := os.Getwd()
	if err == nil {
		if filepath.Base(wd) == "check" && filepath.Base(filepath.Dir(wd)) == "leak-guard" {
			return filepath.Dir(wd)
		}
	}
	return filepath.Join(".github", "leak-guard")
}

// --- bloom filter --------------------------------------------------------

type bloom struct {
	bits      []byte
	bitLen    uint32
	hashCount uint8
	salt      []byte
}

const (
	bloomMagic   = "LGBF"
	bloomVersion = 1
)

func readBloom(r io.Reader, salt []byte) (*bloom, error) {
	if len(salt) == 0 {
		return nil, errors.New("salt empty")
	}
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, err
	}
	if string(magic) != bloomMagic {
		return nil, fmt.Errorf("bad magic %q", magic)
	}
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	if hdr[0] != bloomVersion {
		return nil, fmt.Errorf("bloom version %d unsupported", hdr[0])
	}
	hashCount := hdr[1]
	if hashCount == 0 {
		return nil, fmt.Errorf("bloom hashCount must be > 0")
	}
	var bitLen uint32
	if err := binary.Read(r, binary.BigEndian, &bitLen); err != nil {
		return nil, err
	}
	if bitLen == 0 {
		return nil, fmt.Errorf("bloom bitLen must be > 0")
	}
	body := make([]byte, (bitLen+7)/8)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return &bloom{bits: body, bitLen: bitLen, hashCount: hashCount, salt: append([]byte(nil), salt...)}, nil
}

func (b *bloom) contains(token string) bool {
	for _, idx := range b.indexes(token) {
		if b.bits[idx/8]&(1<<(idx%8)) == 0 {
			return false
		}
	}
	return true
}

func (b *bloom) indexes(token string) []uint32 {
	h := sha256.New()
	h.Write(b.salt)
	h.Write([]byte(token))
	d := h.Sum(nil)
	h1 := binary.BigEndian.Uint64(d[0:8])
	h2 := binary.BigEndian.Uint64(d[8:16])
	out := make([]uint32, b.hashCount)
	for i := uint8(0); i < b.hashCount; i++ {
		combined := h1 + uint64(i)*h2
		out[i] = uint32(combined % uint64(b.bitLen))
	}
	return out
}

// --- patterns ------------------------------------------------------------

type patternCfg struct {
	Patterns []pattern `yaml:"patterns"`
}

type pattern struct {
	ID              string   `yaml:"id"`
	Description     string   `yaml:"description"`
	Regex           string   `yaml:"regex"`
	ExcludePrefixes []string `yaml:"exclude_prefixes,omitempty"`
	Severity        string   `yaml:"severity"`

	compiled *regexp.Regexp
}

func loadPatterns(path string) (*patternCfg, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg patternCfg
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	for i := range cfg.Patterns {
		re, err := regexp.Compile(cfg.Patterns[i].Regex)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", cfg.Patterns[i].ID, err)
		}
		cfg.Patterns[i].compiled = re
	}
	return &cfg, nil
}

// --- tokenizer -----------------------------------------------------------

type tok struct {
	value  string
	line   int
	column int
}

func tokenize(content string) []tok {
	var out []tok
	seen := make(map[string]struct{})
	emit := func(v string, l, c int) {
		v = strings.ToLower(strings.TrimSpace(v))
		if len(v) < 3 {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, tok{value: v, line: l, column: c})
	}
	line := 1
	col := 0
	var run strings.Builder
	var runLine, runCol int
	flush := func() {
		if run.Len() == 0 {
			return
		}
		raw := run.String()
		emit(raw, runLine, runCol)
		if strings.ContainsAny(raw, "-_.") {
			emit(strings.Map(func(r rune) rune {
				switch r {
				case '-', '_', '.':
					return -1
				}
				return r
			}, raw), runLine, runCol)
		}
		hasLower, hasUpper := false, false
		for _, r := range raw {
			if unicode.IsLower(r) {
				hasLower = true
			}
			if unicode.IsUpper(r) {
				hasUpper = true
			}
		}
		if hasLower && hasUpper {
			emit(camelToKebab(raw), runLine, runCol)
		}
		run.Reset()
	}
	for _, r := range content {
		if r == '\n' {
			flush()
			line++
			col = 0
			continue
		}
		col++
		if isTokenRune(r) {
			if run.Len() == 0 {
				runLine = line
				runCol = col
			}
			run.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func isTokenRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '-', '_', '.', ':', '/':
		return true
	}
	return false
}

func camelToKebab(s string) string {
	var b strings.Builder
	runes := []rune(s)
	var prev rune
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				b.WriteByte('-')
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				b.WriteByte('-')
			}
		}
		b.WriteRune(unicode.ToLower(r))
		prev = r
	}
	return b.String()
}

// --- checker -------------------------------------------------------------

type checker struct {
	bloom    *bloom
	patterns *patternCfg
	allow    map[string]struct{}
	skipGlobs []string
}

type hit struct {
	kind      string
	file      string
	line      int
	column    int
	token     string
	patternID string
	severity  string
}

func loadChecker(bloomPath, saltPath, patternsPath, allowPath, skipPath, skipLocalPath string) (*checker, error) {
	salt, err := os.ReadFile(saltPath)
	if err != nil {
		return nil, fmt.Errorf("salt: %w", err)
	}
	bf, err := os.Open(bloomPath)
	if err != nil {
		return nil, fmt.Errorf("bloom: %w", err)
	}
	defer bf.Close()
	b, err := readBloom(bf, salt)
	if err != nil {
		return nil, err
	}
	p, err := loadPatterns(patternsPath)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]struct{})
	if allowPath != "" {
		raw, err := os.ReadFile(allowPath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, ln := range strings.Split(string(raw), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || strings.HasPrefix(ln, "#") {
				continue
			}
			allow[strings.ToLower(ln)] = struct{}{}
		}
	}
	var skipGlobs []string
	for _, p := range []string{skipPath, skipLocalPath} {
		if p == "" {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, ln := range strings.Split(string(raw), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || strings.HasPrefix(ln, "#") {
				continue
			}
			skipGlobs = append(skipGlobs, ln)
		}
	}
	return &checker{bloom: b, patterns: p, allow: allow, skipGlobs: skipGlobs}, nil
}

func (c *checker) shouldSkipFile(absPath, root string) bool {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		rel = absPath
	}
	rel = filepath.ToSlash(rel)
	for _, glob := range c.skipGlobs {
		if matched, _ := filepath.Match(glob, rel); matched {
			return true
		}
		if matched, _ := filepath.Match(glob, filepath.Base(rel)); matched {
			return true
		}
		if strings.HasPrefix(rel, strings.TrimSuffix(glob, "/**")+"/") {
			return true
		}
	}
	return false
}

func (c *checker) scanDir(root string) ([]hit, error) {
	var all []hit
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name(), path, root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldInspect(path) {
			return nil
		}
		if c.shouldSkipFile(path, root) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(raw, 0) >= 0 {
			return nil
		}
		all = append(all, c.scanContent(path, string(raw))...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].file != all[j].file {
			return all[i].file < all[j].file
		}
		if all[i].line != all[j].line {
			return all[i].line < all[j].line
		}
		return all[i].column < all[j].column
	})
	return all, nil
}

func (c *checker) scanContent(file, content string) []hit {
	var out []hit
	for _, t := range tokenize(content) {
		if _, ok := c.allow[t.value]; ok {
			continue
		}
		if c.bloom.contains(t.value) {
			out = append(out, hit{kind: "bloom", file: file, line: t.line, column: t.column, token: t.value, severity: "error"})
		}
	}
	for i := range c.patterns.Patterns {
		p := &c.patterns.Patterns[i]
		for _, loc := range p.compiled.FindAllStringIndex(content, -1) {
			match := content[loc[0]:loc[1]]
			skip := false
			for _, prefix := range p.ExcludePrefixes {
				if strings.HasPrefix(match, prefix) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			line, col := positionOf(content, loc[0])
			sev := p.Severity
			if sev == "" {
				sev = "warning"
			}
			out = append(out, hit{kind: "pattern", file: file, line: line, column: col, token: match, patternID: p.ID, severity: sev})
		}
	}
	return out
}

func positionOf(content string, byteIdx int) (int, int) {
	line, col := 1, 1
	for i, r := range content {
		if i >= byteIdx {
			break
		}
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func renderHits(w io.Writer, hits []hit, verbose bool) {
	if len(hits) == 0 {
		fmt.Fprintln(w, "leak-guard: no findings")
		return
	}
	for _, h := range hits {
		if h.kind == "bloom" && !verbose {
			fmt.Fprintf(w, "%s:%d:%d [bloom] internal-sensitivity check matched a token at this location; ask a maintainer for triage\n", h.file, h.line, h.column)
			continue
		}
		if h.kind == "bloom" {
			fmt.Fprintf(w, "%s:%d:%d [bloom] token %q matched the private denylist\n", h.file, h.line, h.column, h.token)
			continue
		}
		fmt.Fprintf(w, "%s:%d:%d [%s] %q (%s)\n", h.file, h.line, h.column, h.patternID, h.token, h.severity)
	}
}

func shouldSkipDir(name, fullPath, root string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", "out",
		".idea", ".vscode", "target", "bin", "obj":
		return true
	}
	rel, _ := filepath.Rel(root, fullPath)
	rel = filepath.ToSlash(rel)
	switch rel {
	case ".github/leak-guard", ".github/leak-guard/check":
		return true
	}
	return false
}

func shouldInspect(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".java", ".py", ".cs",
		".rs", ".rb", ".kt", ".swift",
		".yaml", ".yml", ".json", ".jsonc", ".toml",
		".md", ".mdx", ".txt", ".sh", ".html", ".xml":
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "makefile", "dockerfile", "license", "readme":
		return true
	}
	return false
}
