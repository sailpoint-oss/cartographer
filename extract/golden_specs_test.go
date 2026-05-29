package extract_test

import (
	"path/filepath"
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/csharpextract"
	"github.com/sailpoint-oss/cartographer/extract/javaextract"
	"github.com/sailpoint-oss/cartographer/extract/pythonextract"
	"github.com/sailpoint-oss/cartographer/extract/testutil"
	"github.com/sailpoint-oss/cartographer/extract/tsextract"
	"github.com/sailpoint-oss/cartographer/extraction"
)

func TestGoldenSpecs(t *testing.T) {
	defaults := testutil.DefaultE2ENormalizers()
	goldenBase := filepath.Join(goldenDir(), "e2e")

	testutil.RunGoldenCases(t, goldenBase, defaults, []testutil.GoldenCase{
		{
			Name:       "inventory-generics",
			GoldenPath: "inventory-generics.yaml",
			Spec:       goldenJavaGenerics,
		},
		{
			Name:       "java-inheritance",
			GoldenPath: "java-inheritance.yaml",
			Spec:       goldenJavaInheritance,
		},
		{
			Name:       "java-enums",
			GoldenPath: "java-enums.yaml",
			Spec:       goldenJavaEnums,
		},
		{
			Name:       "ts-generics",
			GoldenPath: "ts-generics.yaml",
			Spec:       goldenTSGenerics,
		},
		{
			Name:       "java-upstream",
			GoldenPath: "java-upstream.yaml",
			Spec:       goldenJavaUpstream,
		},
		{
			Name:       "java-spring-params",
			GoldenPath: "java-spring-params.yaml",
			Spec:       goldenJavaSpringParams,
		},
		{
			Name:       "java-nullable",
			GoldenPath: "java-nullable.yaml",
			Spec:       goldenJavaNullable,
		},
		{
			Name:       "java-nested",
			GoldenPath: "java-nested.yaml",
			Spec:       goldenJavaNested,
		},
		{
			Name:       "ts-nullable",
			GoldenPath: "ts-nullable.yaml",
			Spec:       goldenTSNullable,
		},
		{
			Name:       "go-http",
			GoldenPath: "go-http.yaml",
			Spec:       goldenGoHTTP,
		},
		{
			Name:       "python-fastapi",
			GoldenPath: "python-fastapi.yaml",
			Spec:       goldenPythonFastAPI,
		},
		{
			Name:       "csharp-minimal",
			GoldenPath: "csharp-minimal.yaml",
			Spec:       goldenCSharpMinimal,
		},
	})
}

func goldenJavaGenerics(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "java-generics", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Inventory",
		Version: "1.0",
	})
}

func goldenJavaInheritance(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "java-inheritance", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:     "Inheritance Test",
		Version:   "1.0",
		TreeShake: true,
	})
}

func goldenJavaEnums(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "java-enums", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:     "Enum Test",
		Version:   "1.0",
		TreeShake: true,
	})
}

func goldenTSGenerics(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "ts-generics", "src")
	result, err := tsextract.Extract(tsextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tsextract.GenerateSpec(result, tsextract.SpecConfig{
		Title:   "TS Generics",
		Version: "1.0",
	})
}

func goldenJavaUpstream(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "java-upstream", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Upstream Fixture API",
		Version: "1.0",
	})
}

func goldenJavaSpringParams(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "java-spring-params", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Spring Params",
		Version: "1.0",
	})
}

func goldenJavaNullable(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "java-nullable", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Java Nullable",
		Version: "1.0",
	})
}

func goldenJavaNested(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "java-nested", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:     "Java Nested",
		Version:   "1.0",
		TreeShake: true,
	})
}

func goldenTSNullable(t *testing.T) map[string]any {
	dir := filepath.Join(testdataDir(), "ts-nullable", "src")
	result, err := tsextract.Extract(tsextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tsextract.GenerateSpec(result, tsextract.SpecConfig{
		Title:   "TS Nullable",
		Version: "1.0",
	})
}

func goldenGoHTTP(t *testing.T) map[string]any {
	root := filepath.Join(testdataDir(), "go-http")
	res, err := extraction.Extract(extraction.Options{
		Lang:    "go",
		RootDir: root,
		Title:   "Example HTTP API",
		Version: "1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	return specMapFromExtraction(t, res.SpecMap)
}

func goldenPythonFastAPI(t *testing.T) map[string]any {
	root := filepath.Join(testdataDir(), "python-fastapi")
	result, err := pythonextract.Extract(pythonextract.Config{
		RootDir:    root,
		SourceDirs: []string{filepath.Join(root, "app")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return pythonextract.GenerateSpec(result, pythonextract.SpecConfig{
		Title:   "Example FastAPI",
		Version: "1.0",
	})
}

func goldenCSharpMinimal(t *testing.T) map[string]any {
	root := filepath.Join(testdataDir(), "csharp-minimal")
	result, err := csharpextract.Extract(csharpextract.Config{
		RootDir:    root,
		SourceDirs: []string{root},
	})
	if err != nil {
		t.Fatal(err)
	}
	return csharpextract.GenerateSpec(result, csharpextract.SpecConfig{
		Title:           "Example Minimal API",
		Version:         "1.0",
		OpenAPIVersion:  "3.2",
		ServiceTemplate: "csharp-web",
	})
}

func specMapFromExtraction(t *testing.T, m map[string]interface{}) map[string]any {
	t.Helper()
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = toAnyValue(v)
	}
	return out
}

func toAnyValue(v interface{}) any {
	switch x := v.(type) {
	case map[string]interface{}:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = toAnyValue(val)
		}
		return m
	case []interface{}:
		s := make([]any, len(x))
		for i, val := range x {
			s[i] = toAnyValue(val)
		}
		return s
	default:
		return v
	}
}
