package testutil

import (
	"path/filepath"
	"testing"
)

// GoldenCase describes one end-to-end spec snapshot test.
type GoldenCase struct {
	Name        string
	GoldenPath  string // relative to goldenBase when goldenBase is set
	Spec        func(t *testing.T) map[string]any
	Normalizers []NormalizeFunc
}

// RunGoldenCases runs each case as a subtest, comparing generated specs to golden YAML.
// goldenBase is joined with each case's GoldenPath when that path is not absolute.
// defaults are prepended before per-case Normalizers.
func RunGoldenCases(t *testing.T, goldenBase string, defaults []NormalizeFunc, cases []GoldenCase) {
	t.Helper()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Helper()
			spec := tc.Spec(t)
			norm := append(append([]NormalizeFunc{}, defaults...), tc.Normalizers...)
			goldenPath := tc.GoldenPath
			if goldenBase != "" && !filepath.IsAbs(goldenPath) {
				goldenPath = filepath.Join(goldenBase, goldenPath)
			}
			AssertGolden(t, goldenPath, spec, WithNormalize(norm...))
		})
	}
}
