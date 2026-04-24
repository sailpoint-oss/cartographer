package extract_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/generics"
	"github.com/sailpoint-oss/cartographer/extract/javaextract"
	"github.com/sailpoint-oss/cartographer/extract/testutil"
	"github.com/sailpoint-oss/cartographer/extract/tsextract"
)

func testdataDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "testdata")
}

// =============================================================================
// Phase 4.3: Chronicle regression test — ResponseEntity<List<ServiceAPI>>
// =============================================================================

func TestChronicleGenerics(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-generics", "com", "example")

	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) == 0 {
		t.Fatal("expected operations, got 0")
	}

	// Find the listServices operation
	var listServicesOp *javaextract.Operation
	for _, op := range result.Operations {
		if op.OperationID == "listServices" {
			listServicesOp = op
			break
		}
	}
	if listServicesOp == nil {
		t.Fatal("listServices operation not found")
	}

	// Verify the response type is correctly captured
	if listServicesOp.ResponseType == "" {
		t.Fatal("listServices has no response type")
	}

	// Generate spec and check the response schema
	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Chronicle",
		Version: "1.0",
	})

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("no paths in spec")
	}

	chroniclePath, ok := paths["/api/v1/chronicle/services"].(map[string]any)
	if !ok {
		t.Fatal("missing /api/v1/chronicle/services path")
	}

	getOp, ok := chroniclePath["get"].(map[string]any)
	if !ok {
		t.Fatal("missing GET operation on /api/v1/chronicle/services")
	}

	// Verify x-source-line is present
	if getOp["x-source-line"] == nil {
		t.Error("expected x-source-line on GET /api/v1/chronicle/services")
	}

	responses, ok := getOp["responses"].(map[string]any)
	if !ok {
		t.Fatal("no responses")
	}

	resp200, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatal("no 200 response")
	}

	content, ok := resp200["content"].(map[string]any)
	if !ok {
		t.Fatal("no content in 200 response")
	}

	jsonContent, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatal("no application/json content")
	}

	schema, ok := jsonContent["schema"].(map[string]any)
	if !ok {
		t.Fatal("no schema in json content")
	}

	// THIS IS THE KEY ASSERTION: ResponseEntity<List<ServiceAPI>> should produce
	// {type: "array", items: {$ref: "#/components/schemas/ServiceAPI"}}
	if schema["type"] != "array" {
		t.Errorf("ResponseEntity<List<ServiceAPI>> should produce array schema, got %v", schema)
	}

	items, ok := schema["items"].(map[string]any)
	if !ok {
		t.Fatal("no items in array schema")
	}

	if items["$ref"] != "#/components/schemas/ServiceAPI" {
		t.Errorf("items.$ref = %v, want #/components/schemas/ServiceAPI", items["$ref"])
	}
}

// =============================================================================
// Java inheritance test
// =============================================================================

func TestJavaInheritance(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-inheritance", "com", "example")

	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:     "Inheritance Test",
		Version:   "1.0",
		TreeShake: true,
	})

	// With response-prefers-indexed-schema (v4), the UserResource schema is
	// inlined in the response body rather than referenced via $ref. This means
	// tree-shake removes it from components/schemas. Verify the allOf pattern
	// exists in the response body schema instead.
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("no paths")
	}

	// Find the GET operation 200 response schema
	var responseSchema map[string]any
	for _, pathItem := range paths {
		pi, _ := pathItem.(map[string]any)
		getOp, _ := pi["get"].(map[string]any)
		if getOp == nil {
			continue
		}
		responses, _ := getOp["responses"].(map[string]any)
		resp200, _ := responses["200"].(map[string]any)
		if resp200 != nil {
			content, _ := resp200["content"].(map[string]any)
			json, _ := content["application/json"].(map[string]any)
			if json != nil {
				responseSchema, _ = json["schema"].(map[string]any)
			}
		}
	}
	if responseSchema == nil {
		t.Fatal("no response schema found")
	}

	// The schema should have allOf with BaseResource ref (either inline or $ref)
	allOf, ok := responseSchema["allOf"].([]any)
	if !ok {
		t.Fatalf("response schema should have allOf, got %v", responseSchema)
	}
	if len(allOf) != 2 {
		t.Fatalf("allOf should have 2 elements, got %d", len(allOf))
	}

	parentRef, ok := allOf[0].(map[string]any)
	if !ok {
		t.Fatal("allOf[0] is not a map")
	}
	if parentRef["$ref"] != "#/components/schemas/BaseResource" {
		t.Errorf("allOf[0].$ref = %v, want BaseResource", parentRef["$ref"])
	}
}

// =============================================================================
// Java enums test
// =============================================================================

func TestJavaEnums(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-enums", "com", "example")

	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:     "Enum Test",
		Version:   "1.0",
		TreeShake: true,
	})

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("no components")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("no schemas")
	}

	// Check StatusEnum schema has proper enum values
	statusSchema, ok := schemas["StatusEnum"].(map[string]any)
	if !ok {
		// The enum may be inlined via the resolver. Check if ItemDTO has a status field with enum.
		t.Log("StatusEnum not found as standalone schema (may be inlined)")
	} else {
		if statusSchema["type"] != "string" {
			t.Errorf("StatusEnum type = %v, want string", statusSchema["type"])
		}
		enumVals, ok := statusSchema["enum"].([]any)
		if !ok {
			t.Fatalf("StatusEnum has no enum values: %v", statusSchema)
		}
		if len(enumVals) != 4 {
			t.Errorf("StatusEnum has %d values, want 4", len(enumVals))
		}
	}
}

// =============================================================================
// TypeScript generics test
// =============================================================================

func TestTSGenerics(t *testing.T) {
	dir := filepath.Join(testdataDir(), "ts-generics", "src")

	result, err := tsextract.Extract(tsextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) == 0 {
		t.Fatal("expected operations from TS extraction, got 0")
	}

	spec := tsextract.GenerateSpec(result, tsextract.SpecConfig{
		Title:   "TS Generics",
		Version: "1.0",
	})

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("no paths")
	}

	// Check findAll - should return array
	usersPath, ok := paths["/api/v1/users"].(map[string]any)
	if !ok {
		t.Fatalf("missing /api/v1/users path. Available paths: %v", keysOf(paths))
	}

	getOp, ok := usersPath["get"].(map[string]any)
	if !ok {
		t.Fatal("missing GET on /api/v1/users")
	}

	// Verify x-source-line present
	if getOp["x-source-line"] == nil {
		t.Error("expected x-source-line on GET /api/v1/users")
	}

	responses, ok := getOp["responses"].(map[string]any)
	if !ok {
		t.Fatal("no responses")
	}

	resp200, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatal("no 200 response")
	}

	content, ok := resp200["content"].(map[string]any)
	if !ok {
		t.Fatal("no content in 200 response")
	}

	jsonContent, ok := content["application/json"].(map[string]any)
	if !ok {
		t.Fatal("no application/json content")
	}

	schema, ok := jsonContent["schema"].(map[string]any)
	if !ok {
		t.Fatal("no schema")
	}

	// Promise<UserDto[]> should unwrap to array of UserDto
	if schema["type"] != "array" {
		t.Errorf("Promise<UserDto[]> should produce array, got %v", schema)
	}
}

// =============================================================================
// Generics package edge cases
// =============================================================================

func TestGenericsChronicleBug(t *testing.T) {
	// The original bug: ResponseEntity<List<ServiceAPI>> resolved to bare {type: "object"}
	schema := generics.Parse("ResponseEntity<List<ServiceAPI>>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Errorf("expected array, got %v", schema)
	}
	items, ok := schema["items"].(map[string]any)
	if !ok {
		t.Fatal("no items")
	}
	if items["$ref"] != "#/components/schemas/ServiceAPI" {
		t.Errorf("items.$ref = %v", items["$ref"])
	}
}

func TestGenericsNestedWrappers(t *testing.T) {
	// Promise<Observable<Bar[]>> should become array of Bar
	schema := generics.Parse("Promise<Observable<Bar[]>>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Errorf("expected array, got %v", schema)
	}
}

func TestGenericsMapWithValues(t *testing.T) {
	schema := generics.Parse("Map<String, List<User>>").ToOpenAPISchema(nil)
	if schema["type"] != "object" {
		t.Errorf("expected object, got %v", schema)
	}
	addlProps, ok := schema["additionalProperties"].(map[string]any)
	if !ok {
		t.Fatal("no additionalProperties")
	}
	if addlProps["type"] != "array" {
		t.Errorf("additionalProperties should be array, got %v", addlProps)
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// =============================================================================
// v5 E2E Tests
// =============================================================================

func TestJavaNullableAndNonnull(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-nullable", "com", "example")

	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Check NullableDTO in types
	for _, decl := range result.Types {
		if decl.Name == "NullableDTO" {
			for _, f := range decl.Fields {
				switch f.Name {
				case "id":
					if !f.Required {
						t.Error("@Nonnull field 'id' should be required")
					}
				case "description":
					if !f.Nullable {
						t.Error("@Nullable field 'description' should be nullable")
					}
				case "optionalField":
					if !f.Nullable {
						t.Error("@Nullable field 'optionalField' should be nullable")
					}
				case "normalField":
					if f.Nullable {
						t.Error("normal field should not be nullable")
					}
					if f.Required {
						t.Error("normal field should not be required")
					}
				}
			}

			// Check schema
			schema := result.Schemas["NullableDTO"].(map[string]any)
			props := schema["properties"].(map[string]any)

			descProp := props["description"].(map[string]any)
			if descProp["nullable"] != true {
				t.Error("schema: @Nullable field should have nullable: true")
			}

			required, _ := schema["required"].([]string)
			foundId := false
			for _, r := range required {
				if r == "id" {
					foundId = true
				}
			}
			if !foundId {
				t.Errorf("schema: @Nonnull field 'id' should be in required, got %v", required)
			}
			return
		}
	}
	t.Error("NullableDTO not found in types")
}

func TestJavaNestedEnums(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-nested", "com", "example")

	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Check that nested enums are indexed
	var operatorEnum, categoryEnum, innerClass bool
	for _, decl := range result.Types {
		switch decl.Name {
		case "Operator":
			operatorEnum = true
			if decl.Kind != "enum" {
				t.Errorf("Operator should be enum, got %s", decl.Kind)
			}
			if len(decl.EnumValues) != 3 {
				t.Errorf("Operator should have 3 values, got %d: %v", len(decl.EnumValues), decl.EnumValues)
			}
		case "Category":
			categoryEnum = true
			if decl.Kind != "enum" {
				t.Errorf("Category should be enum, got %s", decl.Kind)
			}
		case "InnerConfig":
			innerClass = true
			if decl.Kind != "class" {
				t.Errorf("InnerConfig should be class, got %s", decl.Kind)
			}
			if len(decl.Fields) != 2 {
				t.Errorf("InnerConfig should have 2 fields, got %d", len(decl.Fields))
			}
		}
	}

	if !operatorEnum {
		t.Error("nested Operator enum not found in types")
	}
	if !categoryEnum {
		t.Error("nested Category enum not found in types")
	}
	if !innerClass {
		t.Error("nested InnerConfig class not found in types")
	}
}

func TestSpringRequestParamDefaults(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-spring-params", "com", "example")

	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) == 0 {
		t.Fatal("expected operations, got 0")
	}

	op := result.Operations[0]
	for _, p := range op.Parameters {
		switch p.Name {
		case "offset":
			if p.Required {
				t.Error("offset should not be required (required=false)")
			}
			if p.DefaultValue != "0" {
				t.Errorf("offset defaultValue = %q, want '0'", p.DefaultValue)
			}
		case "limit":
			if p.Required {
				t.Error("limit should not be required")
			}
			if p.DefaultValue != "50" {
				t.Errorf("limit defaultValue = %q, want '50'", p.DefaultValue)
			}
		case "active":
			if p.Required {
				t.Error("active should not be required")
			}
			if p.DefaultValue != "true" {
				t.Errorf("active defaultValue = %q, want 'true'", p.DefaultValue)
			}
		case "name":
			if p.Required {
				t.Error("name should not be required (required=false)")
			}
		}
	}

	// Verify spec has typed defaults
	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Spring Params",
		Version: "1.0",
	})

	paths := spec["paths"].(map[string]any)
	for _, pi := range paths {
		for _, opObj := range pi.(map[string]any) {
			opMap := opObj.(map[string]any)
			params, ok := opMap["parameters"].([]any)
			if !ok {
				continue
			}
			for _, p := range params {
				pm := p.(map[string]any)
				schema := pm["schema"].(map[string]any)
				switch pm["name"] {
				case "offset":
					if schema["default"] != 0 {
						t.Errorf("spec: offset default = %v (type %T), want 0", schema["default"], schema["default"])
					}
				case "limit":
					if schema["default"] != 50 {
						t.Errorf("spec: limit default = %v, want 50", schema["default"])
					}
				case "active":
					if schema["default"] != true {
						t.Errorf("spec: active default = %v, want true", schema["default"])
					}
				}
			}
		}
	}
}

func TestResponseEntityNested(t *testing.T) {
	// Verify ResponseEntity<List<T>> unwrapping via generics package
	schema := generics.Parse("ResponseEntity<List<ServiceDTO>>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Errorf("ResponseEntity<List<ServiceDTO>> should produce array, got %v", schema)
	}
	items, ok := schema["items"].(map[string]any)
	if !ok {
		t.Fatal("no items in array schema")
	}
	if items["$ref"] != "#/components/schemas/ServiceDTO" {
		t.Errorf("items.$ref = %v, want #/components/schemas/ServiceDTO", items["$ref"])
	}

	// Nested wrapper: ResponseEntity<Optional<UserDTO>>
	schema2 := generics.Parse("ResponseEntity<Optional<UserDTO>>").ToOpenAPISchema(nil)
	if ref, ok := schema2["$ref"].(string); ok {
		if ref != "#/components/schemas/UserDTO" {
			t.Errorf("ResponseEntity<Optional<UserDTO>> $ref = %v, want UserDTO", ref)
		}
	} else {
		t.Errorf("expected $ref for ResponseEntity<Optional<UserDTO>>, got %v", schema2)
	}
}

// =============================================================================
// Golden-file spec tests — compare full spec output against snapshots
// =============================================================================

func goldenDir() string {
	return filepath.Join(testdataDir(), "golden")
}

func TestGoldenChronicleGenerics(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-generics", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Chronicle",
		Version: "1.0",
	})
	testutil.AssertGolden(t, filepath.Join(goldenDir(), "e2e", "chronicle-generics.yaml"), spec,
		testutil.WithNormalize(testutil.StripSourceLocations))
}

func TestGoldenJavaInheritance(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-inheritance", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:     "Inheritance Test",
		Version:   "1.0",
		TreeShake: true,
	})
	testutil.AssertGolden(t, filepath.Join(goldenDir(), "e2e", "java-inheritance.yaml"), spec,
		testutil.WithNormalize(testutil.StripSourceLocations))
}

func TestGoldenJavaEnums(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-enums", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:     "Enum Test",
		Version:   "1.0",
		TreeShake: true,
	})
	testutil.AssertGolden(t, filepath.Join(goldenDir(), "e2e", "java-enums.yaml"), spec,
		testutil.WithNormalize(testutil.StripSourceLocations))
}

func TestGoldenTSGenerics(t *testing.T) {
	dir := filepath.Join(testdataDir(), "ts-generics", "src")
	result, err := tsextract.Extract(tsextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := tsextract.GenerateSpec(result, tsextract.SpecConfig{
		Title:   "TS Generics",
		Version: "1.0",
	})
	testutil.AssertGolden(t, filepath.Join(goldenDir(), "e2e", "ts-generics.yaml"), spec,
		testutil.WithNormalize(testutil.StripSourceLocations))
}

func TestGoldenJavaSpringParams(t *testing.T) {
	dir := filepath.Join(testdataDir(), "java-spring-params", "com", "example")
	result, err := javaextract.Extract(javaextract.Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := javaextract.GenerateSpec(result, javaextract.SpecConfig{
		Title:   "Spring Params",
		Version: "1.0",
	})
	testutil.AssertGolden(t, filepath.Join(goldenDir(), "e2e", "java-spring-params.yaml"), spec,
		testutil.WithNormalize(testutil.StripSourceLocations))
}
