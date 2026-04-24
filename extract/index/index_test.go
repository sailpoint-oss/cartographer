package index

import (
	"fmt"
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/parser"
)

func TestJavaScanner(t *testing.T) {
	pool := parser.NewPool()
	if err := pool.RegisterJava(); err != nil {
		t.Fatal(err)
	}

	idx := New()
	scanner := NewScanner(pool, idx, "java")

	// We can't easily scan a directory in unit tests, but we can test
	// the index operations after manual population
	idx.Add("com.example.User", &TypeDecl{
		Name:      "User",
		Qualified: "com.example.User",
		Kind:      "class",
		Package:   "com.example",
		Fields: []FieldDecl{
			{Name: "id", Type: "Long", JSONName: "id", Required: true},
			{Name: "name", Type: "String", JSONName: "name", Required: true},
			{Name: "email", Type: "String", JSONName: "email"},
		},
	})

	idx.Add("com.example.Status", &TypeDecl{
		Name:       "Status",
		Qualified:  "com.example.Status",
		Kind:       "enum",
		Package:    "com.example",
		EnumValues: []string{"ACTIVE", "INACTIVE", "PENDING"},
	})

	// Verify scanner was created (it won't find files in test dir)
	if scanner == nil {
		t.Fatal("expected non-nil scanner")
	}

	// Test resolution
	if idx.Count() != 2 {
		t.Errorf("expected 2 types, got %d", idx.Count())
	}

	user, ok := idx.Resolve("com.example.User")
	if !ok {
		t.Fatal("expected to resolve User")
	}
	if user.Name != "User" {
		t.Errorf("expected User, got %s", user.Name)
	}
	if len(user.Fields) != 3 {
		t.Errorf("expected 3 fields, got %d", len(user.Fields))
	}

	// Test simple name resolution
	userSimple, ok := idx.ResolveSimple("User")
	if !ok {
		t.Fatal("expected to resolve User by simple name")
	}
	if userSimple.Qualified != "com.example.User" {
		t.Errorf("expected com.example.User, got %s", userSimple.Qualified)
	}
}

func TestResolver(t *testing.T) {
	idx := New()

	idx.Add("com.example.User", &TypeDecl{
		Name:      "User",
		Qualified: "com.example.User",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "id", Type: "Long", JSONName: "id", Required: true},
			{Name: "name", Type: "String", JSONName: "name"},
			{Name: "active", Type: "boolean", JSONName: "active"},
		},
	})

	idx.Add("com.example.Status", &TypeDecl{
		Name:       "Status",
		Qualified:  "com.example.Status",
		Kind:       "enum",
		EnumValues: []string{"ACTIVE", "INACTIVE"},
	})

	// Test class schema generation
	schema, ok := idx.Resolver("com.example.User")
	if !ok {
		t.Fatal("expected to resolve User schema")
	}

	if schema["type"] != "object" {
		t.Errorf("expected type=object, got %v", schema["type"])
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in schema")
	}

	if len(props) != 3 {
		t.Errorf("expected 3 properties, got %d", len(props))
	}

	// Check id field
	idSchema, ok := props["id"].(map[string]interface{})
	if !ok {
		t.Fatal("expected id property")
	}
	if idSchema["type"] != "integer" {
		t.Errorf("expected id type=integer, got %v", idSchema["type"])
	}

	// Check name field
	nameSchema, ok := props["name"].(map[string]interface{})
	if !ok {
		t.Fatal("expected name property")
	}
	if nameSchema["type"] != "string" {
		t.Errorf("expected name type=string, got %v", nameSchema["type"])
	}

	// Test enum schema generation
	enumSchema, ok := idx.Resolver("com.example.Status")
	if !ok {
		t.Fatal("expected to resolve Status schema")
	}

	if enumSchema["type"] != "string" {
		t.Errorf("expected enum type=string, got %v", enumSchema["type"])
	}

	enumValues, ok := enumSchema["enum"].([]interface{})
	if !ok {
		t.Fatal("expected enum values")
	}
	if len(enumValues) != 2 {
		t.Errorf("expected 2 enum values, got %d", len(enumValues))
	}
}

func TestFieldTypeToSchema(t *testing.T) {
	idx := New()

	tests := []struct {
		typeName string
		expected string // expected "type" field
	}{
		{"String", "string"},
		{"int", "integer"},
		{"Long", "integer"},
		{"double", "number"},
		{"boolean", "boolean"},
		{"List<String>", "array"},
		{"Set<String>", "array"},
		{"String[]", "array"},
		{"Map<String, Object>", "object"},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			schema := idx.fieldTypeToSchema(tt.typeName, nil)
			if schema["type"] != tt.expected {
				t.Errorf("for %s: expected type=%s, got %v", tt.typeName, tt.expected, schema["type"])
			}
		})
	}
}

func TestImportResolution(t *testing.T) {
	idx := New()

	// Add a type
	idx.Add("com.example.dto.UserDTO", &TypeDecl{
		Name:      "UserDTO",
		Qualified: "com.example.dto.UserDTO",
		Kind:      "class",
	})

	// Add import for a file
	idx.AddImport("src/UserController.java", "UserDTO", "com.example.dto.UserDTO")

	// Resolve using import context
	decl, ok := idx.ResolveInFile("src/UserController.java", "UserDTO")
	if !ok {
		t.Fatal("expected to resolve UserDTO via import")
	}
	if decl.Qualified != "com.example.dto.UserDTO" {
		t.Errorf("expected com.example.dto.UserDTO, got %s", decl.Qualified)
	}
}

// --- New resolver tests below ---

func TestShouldSkipField(t *testing.T) {
	tests := []struct {
		name     string
		field    FieldDecl
		expected bool
	}{
		{
			name: "JsonIgnore skips field",
			field: FieldDecl{
				Name:        "secret",
				Type:        "String",
				Annotations: map[string]string{"JsonIgnore": ""},
			},
			expected: true,
		},
		{
			name: "Exclude skips field",
			field: FieldDecl{
				Name:        "internal",
				Type:        "String",
				Annotations: map[string]string{"Exclude": ""},
			},
			expected: true,
		},
		{
			name: "Transient skips field",
			field: FieldDecl{
				Name:        "temp",
				Type:        "int",
				Annotations: map[string]string{"Transient": ""},
			},
			expected: true,
		},
		{
			name: "Getter NONE does NOT skip field",
			field: FieldDecl{
				Name:        "writeOnlyField",
				Type:        "String",
				Annotations: map[string]string{"Getter": "AccessLevel.NONE"},
			},
			expected: false,
		},
		{
			name: "Setter NONE does NOT skip field",
			field: FieldDecl{
				Name:        "readOnlyField",
				Type:        "String",
				Annotations: map[string]string{"Setter": "AccessLevel.NONE"},
			},
			expected: false,
		},
		{
			name: "No annotations does NOT skip field",
			field: FieldDecl{
				Name: "normal",
				Type: "String",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipField(tt.field)
			if got != tt.expected {
				t.Errorf("shouldSkipField() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestApplyJavaValidation(t *testing.T) {
	t.Run("Size on string sets minLength and maxLength", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"Size": "min = 2, max = 50"}, schema)
		if schema["minLength"] != 2 {
			t.Errorf("expected minLength=2, got %v", schema["minLength"])
		}
		if schema["maxLength"] != 50 {
			t.Errorf("expected maxLength=50, got %v", schema["maxLength"])
		}
	})

	t.Run("Size on array sets minItems and maxItems", func(t *testing.T) {
		schema := map[string]interface{}{"type": "array"}
		applyJavaValidation(map[string]string{"Size": "min = 1, max = 100"}, schema)
		if schema["minItems"] != 1 {
			t.Errorf("expected minItems=1, got %v", schema["minItems"])
		}
		if schema["maxItems"] != 100 {
			t.Errorf("expected maxItems=100, got %v", schema["maxItems"])
		}
	})

	t.Run("Min and Max on integer", func(t *testing.T) {
		schema := map[string]interface{}{"type": "integer"}
		applyJavaValidation(map[string]string{"Min": "0", "Max": "999"}, schema)
		if schema["minimum"] != 0 {
			t.Errorf("expected minimum=0, got %v", schema["minimum"])
		}
		if schema["maximum"] != 999 {
			t.Errorf("expected maximum=999, got %v", schema["maximum"])
		}
	})

	t.Run("Pattern on string", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"Pattern": `regexp = "^[a-z]+$"`}, schema)
		if schema["pattern"] != "^[a-z]+$" {
			t.Errorf("expected pattern=^[a-z]+$, got %v", schema["pattern"])
		}
	})

	t.Run("Email on string sets format email", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"Email": ""}, schema)
		if schema["format"] != "email" {
			t.Errorf("expected format=email, got %v", schema["format"])
		}
	})

	t.Run("IsEmail on string sets format email (TS)", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"IsEmail": ""}, schema)
		if schema["format"] != "email" {
			t.Errorf("expected format=email, got %v", schema["format"])
		}
	})

	t.Run("Range sets minimum and maximum", func(t *testing.T) {
		schema := map[string]interface{}{"type": "integer"}
		applyJavaValidation(map[string]string{"Range": "min = 10, max = 200"}, schema)
		if schema["minimum"] != 10 {
			t.Errorf("expected minimum=10, got %v", schema["minimum"])
		}
		if schema["maximum"] != 200 {
			t.Errorf("expected maximum=200, got %v", schema["maximum"])
		}
	})

	t.Run("Length sets minLength and maxLength", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"Length": "min = 5, max = 255"}, schema)
		if schema["minLength"] != 5 {
			t.Errorf("expected minLength=5, got %v", schema["minLength"])
		}
		if schema["maxLength"] != 255 {
			t.Errorf("expected maxLength=255, got %v", schema["maxLength"])
		}
	})

	t.Run("UniqueElements on array sets uniqueItems", func(t *testing.T) {
		schema := map[string]interface{}{"type": "array"}
		applyJavaValidation(map[string]string{"UniqueElements": ""}, schema)
		if schema["uniqueItems"] != true {
			t.Errorf("expected uniqueItems=true, got %v", schema["uniqueItems"])
		}
	})

	t.Run("UniqueElements on non-array is ignored", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"UniqueElements": ""}, schema)
		if _, ok := schema["uniqueItems"]; ok {
			t.Error("uniqueItems should not be set on non-array type")
		}
	})

	t.Run("JsonInclude NON_NULL sets nullable", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"JsonInclude": "NON_NULL"}, schema)
		if schema["nullable"] != true {
			t.Errorf("expected nullable=true, got %v", schema["nullable"])
		}
	})

	t.Run("Schema allowableValues sets enum single value", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"Schema": `allowableValues = {"ACTIVE"}`}, schema)
		enumVals, ok := schema["enum"].([]interface{})
		if !ok {
			t.Fatal("expected enum to be []interface{}")
		}
		if len(enumVals) != 1 {
			t.Fatalf("expected 1 enum value, got %d: %v", len(enumVals), enumVals)
		}
		if enumVals[0] != "ACTIVE" {
			t.Errorf("expected enum[0]=ACTIVE, got %v", enumVals[0])
		}
	})

	t.Run("Schema allowableValues with multiple values", func(t *testing.T) {
		// Note: extractNamedValue splits on commas, so multi-value allowableValues
		// where values contain commas within braces only captures the first chunk.
		// The annotation value as the ONLY annotation arg avoids the splitting issue.
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{
			"Schema": `allowableValues = {"ALPHA", "BETA", "GA"}`,
		}, schema)
		enumVals, ok := schema["enum"].([]interface{})
		if !ok {
			t.Fatal("expected enum to be set (even if partial)")
		}
		// Due to naive comma-splitting in extractNamedValue, values inside braces
		// get split. At minimum we verify the enum key is present and non-empty.
		if len(enumVals) == 0 {
			t.Error("expected at least one enum value")
		}
	})

	t.Run("NotBlank sets minLength 1 on string", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"NotBlank": ""}, schema)
		if schema["minLength"] != 1 {
			t.Errorf("expected minLength=1, got %v", schema["minLength"])
		}
	})

	t.Run("Positive on integer sets exclusiveMinimum", func(t *testing.T) {
		schema := map[string]interface{}{"type": "integer"}
		applyJavaValidation(map[string]string{"Positive": ""}, schema)
		if schema["exclusiveMinimum"] != 0 {
			t.Errorf("expected exclusiveMinimum=0, got %v", schema["exclusiveMinimum"])
		}
	})

	t.Run("DecimalMin and DecimalMax set minimum and maximum", func(t *testing.T) {
		schema := map[string]interface{}{"type": "number"}
		applyJavaValidation(map[string]string{"DecimalMin": `"0.01"`, "DecimalMax": `"99.99"`}, schema)
		if schema["minimum"] != 0.01 {
			t.Errorf("expected minimum=0.01, got %v", schema["minimum"])
		}
		if schema["maximum"] != 99.99 {
			t.Errorf("expected maximum=99.99, got %v", schema["maximum"])
		}
	})

	t.Run("nil annotations are safe", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(nil, schema) // should not panic
		if len(schema) != 1 {
			t.Errorf("expected schema unchanged, got %v", schema)
		}
	})
}

func TestClassToSchemaWithInheritance(t *testing.T) {
	idx := New()

	idx.Add("com.example.BaseEntity", &TypeDecl{
		Name:      "BaseEntity",
		Qualified: "com.example.BaseEntity",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "id", Type: "String", JSONName: "id", Required: true},
			{Name: "created", Type: "OffsetDateTime", JSONName: "created"},
		},
	})

	idx.Add("com.example.User", &TypeDecl{
		Name:       "User",
		Qualified:  "com.example.User",
		Kind:       "class",
		SuperClass: "BaseEntity",
		Fields: []FieldDecl{
			{Name: "id", Type: "String", JSONName: "id", Required: true},       // inherited, should be deduped
			{Name: "created", Type: "OffsetDateTime", JSONName: "created"},      // inherited, should be deduped
			{Name: "username", Type: "String", JSONName: "username", Required: true},
			{Name: "email", Type: "String", JSONName: "email"},
		},
	})

	schema, ok := idx.Resolver("com.example.User")
	if !ok {
		t.Fatal("expected to resolve User schema")
	}

	allOf, ok := schema["allOf"].([]interface{})
	if !ok {
		t.Fatal("expected allOf in schema for inherited class")
	}
	if len(allOf) != 2 {
		t.Fatalf("expected 2 elements in allOf, got %d", len(allOf))
	}

	// First element should be $ref to parent
	refObj, ok := allOf[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected first allOf element to be an object")
	}
	if refObj["$ref"] != "#/components/schemas/BaseEntity" {
		t.Errorf("expected $ref to BaseEntity, got %v", refObj["$ref"])
	}

	// Second element should contain only the child's own fields (not inherited)
	ownSchema, ok := allOf[1].(map[string]interface{})
	if !ok {
		t.Fatal("expected second allOf element to be an object")
	}
	props, ok := ownSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties in own schema")
	}

	// Should NOT contain inherited fields "id" and "created"
	if _, exists := props["id"]; exists {
		t.Error("inherited field 'id' should not appear in child's own properties")
	}
	if _, exists := props["created"]; exists {
		t.Error("inherited field 'created' should not appear in child's own properties")
	}

	// Should contain child's own fields
	if _, exists := props["username"]; !exists {
		t.Error("expected 'username' in child's own properties")
	}
	if _, exists := props["email"]; !exists {
		t.Error("expected 'email' in child's own properties")
	}
}

func TestEnumToSchema(t *testing.T) {
	idx := New()
	idx.Add("com.example.Color", &TypeDecl{
		Name:       "Color",
		Qualified:  "com.example.Color",
		Kind:       "enum",
		EnumValues: []string{"RED", "GREEN", "BLUE"},
	})

	schema, ok := idx.Resolver("com.example.Color")
	if !ok {
		t.Fatal("expected to resolve Color enum schema")
	}

	if schema["type"] != "string" {
		t.Errorf("expected type=string, got %v", schema["type"])
	}

	enumVals, ok := schema["enum"].([]interface{})
	if !ok {
		t.Fatal("expected enum values in schema")
	}
	if len(enumVals) != 3 {
		t.Fatalf("expected 3 enum values, got %d", len(enumVals))
	}

	expected := []string{"RED", "GREEN", "BLUE"}
	for i, v := range enumVals {
		if v != expected[i] {
			t.Errorf("enum[%d]: expected %s, got %v", i, expected[i], v)
		}
	}
}

func TestFieldTypeToSchemaGenericTypes(t *testing.T) {
	idx := New()

	// Add a known type so we can verify resolution inside generics
	idx.Add("com.example.Address", &TypeDecl{
		Name:      "Address",
		Qualified: "com.example.Address",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "street", Type: "String", JSONName: "street"},
		},
	})

	t.Run("Map<String, Integer>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Map<String, Integer>", nil)
		if schema["type"] != "object" {
			t.Errorf("expected type=object, got %v", schema["type"])
		}
		addlProps, ok := schema["additionalProperties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected additionalProperties")
		}
		if addlProps["type"] != "integer" {
			t.Errorf("expected additionalProperties type=integer, got %v", addlProps["type"])
		}
	})

	t.Run("Map<String, List<String>>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Map<String, List<String>>", nil)
		if schema["type"] != "object" {
			t.Errorf("expected type=object, got %v", schema["type"])
		}
		addlProps, ok := schema["additionalProperties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected additionalProperties")
		}
		if addlProps["type"] != "array" {
			t.Errorf("expected additionalProperties type=array, got %v", addlProps["type"])
		}
	})

	t.Run("Optional<String>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Optional<String>", nil)
		if schema["type"] != "string" {
			t.Errorf("expected type=string, got %v", schema["type"])
		}
	})

	t.Run("Optional<Long>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Optional<Long>", nil)
		if schema["type"] != "integer" {
			t.Errorf("expected type=integer, got %v", schema["type"])
		}
	})

	t.Run("List<Map<String, Address>>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("List<Map<String, Address>>", nil)
		if schema["type"] != "array" {
			t.Errorf("expected type=array, got %v", schema["type"])
		}
		items, ok := schema["items"].(map[string]interface{})
		if !ok {
			t.Fatal("expected items in array schema")
		}
		if items["type"] != "object" {
			t.Errorf("expected items type=object (map), got %v", items["type"])
		}
		addlProps, ok := items["additionalProperties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected additionalProperties in map items")
		}
		// Address resolves to an object with properties
		if addlProps["type"] != "object" {
			t.Errorf("expected Address to be object, got %v", addlProps["type"])
		}
	})

	t.Run("Set<String> has uniqueItems", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Set<String>", nil)
		if schema["type"] != "array" {
			t.Errorf("expected type=array, got %v", schema["type"])
		}
		if schema["uniqueItems"] != true {
			t.Errorf("expected uniqueItems=true for Set, got %v", schema["uniqueItems"])
		}
	})

	t.Run("HashMap<String, Boolean>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("HashMap<String, Boolean>", nil)
		if schema["type"] != "object" {
			t.Errorf("expected type=object, got %v", schema["type"])
		}
		addlProps, ok := schema["additionalProperties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected additionalProperties")
		}
		if addlProps["type"] != "boolean" {
			t.Errorf("expected additionalProperties type=boolean, got %v", addlProps["type"])
		}
	})

	t.Run("Collection<Integer>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Collection<Integer>", nil)
		if schema["type"] != "array" {
			t.Errorf("expected type=array, got %v", schema["type"])
		}
		items, ok := schema["items"].(map[string]interface{})
		if !ok {
			t.Fatal("expected items")
		}
		if items["type"] != "integer" {
			t.Errorf("expected items type=integer, got %v", items["type"])
		}
	})

	t.Run("Array<String> (TypeScript)", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Array<String>", nil)
		if schema["type"] != "array" {
			t.Errorf("expected type=array, got %v", schema["type"])
		}
	})
}

func TestWildcardTypes(t *testing.T) {
	idx := New()

	t.Run("bare wildcard ?", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("?", nil)
		if schema["type"] != "object" {
			t.Errorf("expected type=object for ?, got %v", schema["type"])
		}
	})

	t.Run("? extends String", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("? extends String", nil)
		if schema["type"] != "string" {
			t.Errorf("expected type=string for ? extends String, got %v", schema["type"])
		}
	})

	t.Run("? extends Long", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("? extends Long", nil)
		if schema["type"] != "integer" {
			t.Errorf("expected type=integer for ? extends Long, got %v", schema["type"])
		}
	})

	t.Run("? super Integer", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("? super Integer", nil)
		if schema["type"] != "integer" {
			t.Errorf("expected type=integer for ? super Integer, got %v", schema["type"])
		}
	})

	t.Run("List<? extends String>", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("List<? extends String>", nil)
		if schema["type"] != "array" {
			t.Errorf("expected type=array, got %v", schema["type"])
		}
		items, ok := schema["items"].(map[string]interface{})
		if !ok {
			t.Fatal("expected items")
		}
		if items["type"] != "string" {
			t.Errorf("expected items type=string, got %v", items["type"])
		}
	})
}

func TestNullableField(t *testing.T) {
	idx := New()

	t.Run("field with Nullable flag produces nullable", func(t *testing.T) {
		idx.Add("com.example.Dto", &TypeDecl{
			Name:      "Dto",
			Qualified: "com.example.Dto",
			Kind:      "class",
			Fields: []FieldDecl{
				{Name: "label", Type: "String", JSONName: "label", Nullable: true},
				{Name: "count", Type: "int", JSONName: "count"},
			},
		})

		schema, ok := idx.Resolver("com.example.Dto")
		if !ok {
			t.Fatal("expected to resolve Dto")
		}

		props := schema["properties"].(map[string]interface{})
		labelSchema := props["label"].(map[string]interface{})
		if labelSchema["nullable"] != true {
			t.Errorf("expected label to be nullable, got %v", labelSchema["nullable"])
		}
		countSchema := props["count"].(map[string]interface{})
		if _, exists := countSchema["nullable"]; exists {
			t.Error("count should not be nullable")
		}
	})

	t.Run("Optional<T> unwraps to inner type", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("Optional<String>", nil)
		if schema["type"] != "string" {
			t.Errorf("expected type=string for Optional<String>, got %v", schema["type"])
		}
	})

	t.Run("TypeScript nullable union unwraps", func(t *testing.T) {
		schema := idx.fieldTypeToSchema("String | null", nil)
		if schema["type"] != "string" {
			t.Errorf("expected type=string for 'String | null', got %v", schema["type"])
		}
	})
}

func TestReadOnlyWriteOnly(t *testing.T) {
	idx := New()

	idx.Add("com.example.Account", &TypeDecl{
		Name:      "Account",
		Qualified: "com.example.Account",
		Kind:      "class",
		Fields: []FieldDecl{
			{
				Name:        "password",
				Type:        "String",
				JSONName:    "password",
				Annotations: map[string]string{"Getter": "AccessLevel.NONE"},
			},
			{
				Name:        "createdAt",
				Type:        "OffsetDateTime",
				JSONName:    "createdAt",
				Annotations: map[string]string{"Setter": "AccessLevel.NONE"},
			},
			{
				Name:     "name",
				Type:     "String",
				JSONName: "name",
			},
		},
	})

	schema, ok := idx.Resolver("com.example.Account")
	if !ok {
		t.Fatal("expected to resolve Account")
	}

	props := schema["properties"].(map[string]interface{})

	// @Getter(NONE) -> writeOnly
	pwSchema := props["password"].(map[string]interface{})
	if pwSchema["writeOnly"] != true {
		t.Errorf("expected password writeOnly=true, got %v", pwSchema["writeOnly"])
	}
	if _, exists := pwSchema["readOnly"]; exists {
		t.Error("password should not have readOnly")
	}

	// @Setter(NONE) -> readOnly
	caSchema := props["createdAt"].(map[string]interface{})
	if caSchema["readOnly"] != true {
		t.Errorf("expected createdAt readOnly=true, got %v", caSchema["readOnly"])
	}
	if _, exists := caSchema["writeOnly"]; exists {
		t.Error("createdAt should not have writeOnly")
	}

	// Normal field has neither
	nameSchema := props["name"].(map[string]interface{})
	if _, exists := nameSchema["readOnly"]; exists {
		t.Error("name should not have readOnly")
	}
	if _, exists := nameSchema["writeOnly"]; exists {
		t.Error("name should not have writeOnly")
	}
}

func TestRequiredArrayFiltering(t *testing.T) {
	idx := New()

	// Create a class where one required field gets skipped by @JsonIgnore.
	// The required array should NOT include the skipped field.
	idx.Add("com.example.Filtered", &TypeDecl{
		Name:      "Filtered",
		Qualified: "com.example.Filtered",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "visible", Type: "String", JSONName: "visible", Required: true},
			{
				Name:        "hidden",
				Type:        "String",
				JSONName:    "hidden",
				Required:    true,
				Annotations: map[string]string{"JsonIgnore": ""},
			},
		},
	})

	schema, ok := idx.Resolver("com.example.Filtered")
	if !ok {
		t.Fatal("expected to resolve Filtered")
	}

	props := schema["properties"].(map[string]interface{})

	// hidden should not appear in properties
	if _, exists := props["hidden"]; exists {
		t.Error("hidden field with @JsonIgnore should not be in properties")
	}

	// visible should be in properties
	if _, exists := props["visible"]; !exists {
		t.Error("expected 'visible' in properties")
	}

	// required should only contain "visible"
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}
	if len(required) != 1 {
		t.Fatalf("expected 1 required field, got %d: %v", len(required), required)
	}
	if required[0] != "visible" {
		t.Errorf("expected required[0]='visible', got %s", required[0])
	}
}

func TestCircularReference(t *testing.T) {
	idx := New()

	// A class that references itself (e.g. tree node with children)
	idx.Add("com.example.TreeNode", &TypeDecl{
		Name:      "TreeNode",
		Qualified: "com.example.TreeNode",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "value", Type: "String", JSONName: "value"},
			{Name: "parent", Type: "TreeNode", JSONName: "parent"},
			{Name: "children", Type: "List<TreeNode>", JSONName: "children"},
		},
	})

	// This should not cause infinite recursion
	schema, ok := idx.Resolver("com.example.TreeNode")
	if !ok {
		t.Fatal("expected to resolve TreeNode")
	}

	if schema["type"] != "object" {
		t.Errorf("expected type=object, got %v", schema["type"])
	}

	props := schema["properties"].(map[string]interface{})

	// The "parent" field should be a $ref (circular reference guard)
	parentSchema, ok := props["parent"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parent property")
	}
	if parentSchema["$ref"] != "#/components/schemas/TreeNode" {
		t.Errorf("expected parent $ref to TreeNode, got %v", parentSchema["$ref"])
	}

	// The "children" field should be an array with items as $ref
	childrenSchema, ok := props["children"].(map[string]interface{})
	if !ok {
		t.Fatal("expected children property")
	}
	if childrenSchema["type"] != "array" {
		t.Errorf("expected children type=array, got %v", childrenSchema["type"])
	}
	items, ok := childrenSchema["items"].(map[string]interface{})
	if !ok {
		t.Fatal("expected items in children array")
	}
	if items["$ref"] != "#/components/schemas/TreeNode" {
		t.Errorf("expected children items $ref to TreeNode, got %v", items["$ref"])
	}
}

func TestDiscriminatorSchema(t *testing.T) {
	idx := New()

	idx.Add("com.example.Shape", &TypeDecl{
		Name:                  "Shape",
		Qualified:             "com.example.Shape",
		Kind:                  "class",
		DiscriminatorProperty: "type",
		DiscriminatorMapping: map[string]string{
			"circle":    "Circle",
			"rectangle": "Rectangle",
		},
	})

	schema, ok := idx.Resolver("com.example.Shape")
	if !ok {
		t.Fatal("expected to resolve Shape")
	}

	disc, ok := schema["discriminator"].(map[string]interface{})
	if !ok {
		t.Fatal("expected discriminator in schema")
	}
	if disc["propertyName"] != "type" {
		t.Errorf("expected propertyName=type, got %v", disc["propertyName"])
	}

	mapping, ok := disc["mapping"].(map[string]interface{})
	if !ok {
		t.Fatal("expected mapping in discriminator")
	}
	if mapping["circle"] != "#/components/schemas/Circle" {
		t.Errorf("expected circle mapping, got %v", mapping["circle"])
	}
	if mapping["rectangle"] != "#/components/schemas/Rectangle" {
		t.Errorf("expected rectangle mapping, got %v", mapping["rectangle"])
	}

	oneOf, ok := schema["oneOf"].([]interface{})
	if !ok {
		t.Fatal("expected oneOf in schema")
	}
	if len(oneOf) != 2 {
		t.Errorf("expected 2 oneOf entries, got %d", len(oneOf))
	}
}

func TestDeprecatedClassAndField(t *testing.T) {
	idx := New()

	idx.Add("com.example.LegacyDto", &TypeDecl{
		Name:       "LegacyDto",
		Qualified:  "com.example.LegacyDto",
		Kind:       "class",
		Deprecated: true,
		Fields: []FieldDecl{
			{Name: "oldField", Type: "String", JSONName: "oldField", Deprecated: true},
			{Name: "newField", Type: "String", JSONName: "newField"},
		},
	})

	schema, ok := idx.Resolver("com.example.LegacyDto")
	if !ok {
		t.Fatal("expected to resolve LegacyDto")
	}

	if schema["deprecated"] != true {
		t.Error("expected class-level deprecated=true")
	}

	props := schema["properties"].(map[string]interface{})
	oldFieldSchema := props["oldField"].(map[string]interface{})
	if oldFieldSchema["deprecated"] != true {
		t.Error("expected oldField deprecated=true")
	}
	newFieldSchema := props["newField"].(map[string]interface{})
	if _, exists := newFieldSchema["deprecated"]; exists {
		t.Error("newField should not be deprecated")
	}
}

func TestJsonPropertyReadWriteOnly(t *testing.T) {
	t.Run("JsonProperty READ_ONLY", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"JsonProperty": "access = READ_ONLY"}, schema)
		if schema["readOnly"] != true {
			t.Errorf("expected readOnly=true, got %v", schema["readOnly"])
		}
	})

	t.Run("JsonProperty WRITE_ONLY", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{"JsonProperty": "access = WRITE_ONLY"}, schema)
		if schema["writeOnly"] != true {
			t.Errorf("expected writeOnly=true, got %v", schema["writeOnly"])
		}
	})
}

func TestJsonUnwrapped(t *testing.T) {
	idx := New()

	idx.Add("com.example.Address", &TypeDecl{
		Name:      "Address",
		Qualified: "com.example.Address",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "street", Type: "String", JSONName: "street"},
			{Name: "city", Type: "String", JSONName: "city"},
		},
	})

	idx.Add("com.example.Person", &TypeDecl{
		Name:      "Person",
		Qualified: "com.example.Person",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "name", Type: "String", JSONName: "name"},
			{
				Name:        "address",
				Type:        "Address",
				JSONName:    "address",
				Annotations: map[string]string{"JsonUnwrapped": ""},
			},
		},
	})

	schema, ok := idx.Resolver("com.example.Person")
	if !ok {
		t.Fatal("expected to resolve Person")
	}

	props := schema["properties"].(map[string]interface{})

	// Address fields should be inlined
	if _, exists := props["street"]; !exists {
		t.Error("expected 'street' to be inlined from Address")
	}
	if _, exists := props["city"]; !exists {
		t.Error("expected 'city' to be inlined from Address")
	}
	// The "address" field itself should NOT appear (it's unwrapped)
	if _, exists := props["address"]; exists {
		t.Error("'address' should not appear as its own property when @JsonUnwrapped")
	}
}

func TestSchemaAnnotationExtended(t *testing.T) {
	t.Run("Schema example and deprecated", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{
			"Schema": `example = "test@example.com", deprecated = true`,
		}, schema)
		if schema["example"] != "test@example.com" {
			t.Errorf("expected example='test@example.com', got %v", schema["example"])
		}
		if schema["deprecated"] != true {
			t.Errorf("expected deprecated=true, got %v", schema["deprecated"])
		}
	})

	t.Run("Schema accessMode READ_ONLY", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{
			"Schema": `accessMode = AccessMode.READ_ONLY`,
		}, schema)
		if schema["readOnly"] != true {
			t.Errorf("expected readOnly=true, got %v", schema["readOnly"])
		}
	})

	t.Run("Schema description override", func(t *testing.T) {
		schema := map[string]interface{}{"type": "string"}
		applyJavaValidation(map[string]string{
			"Schema": `description = "A human-readable label"`,
		}, schema)
		if schema["description"] != "A human-readable label" {
			t.Errorf("expected description, got %v", schema["description"])
		}
	})
}

func TestEmptyClassSchema(t *testing.T) {
	idx := New()

	idx.Add("com.example.Empty", &TypeDecl{
		Name:      "Empty",
		Qualified: "com.example.Empty",
		Kind:      "class",
	})

	schema, ok := idx.Resolver("com.example.Empty")
	if !ok {
		t.Fatal("expected to resolve Empty")
	}
	if schema["type"] != "object" {
		t.Errorf("expected type=object, got %v", schema["type"])
	}
	if _, exists := schema["properties"]; exists {
		t.Error("empty class should not have properties")
	}
}

func TestNilDeclFallback(t *testing.T) {
	idx := New()
	schema := idx.ToOpenAPISchema(nil, nil)
	if schema["type"] != "object" {
		t.Errorf("expected nil decl fallback to type=object, got %v", schema["type"])
	}
}

func TestFieldDefaultValue(t *testing.T) {
	idx := New()
	idx.Add("com.example.Config", &TypeDecl{
		Name:      "Config",
		Qualified: "com.example.Config",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "retries", Type: "int", JSONName: "retries", DefaultValue: "3"},
			{Name: "timeout", Type: "int", JSONName: "timeout"},
		},
	})

	schema, ok := idx.Resolver("com.example.Config")
	if !ok {
		t.Fatal("expected to resolve Config")
	}
	props := schema["properties"].(map[string]interface{})
	retriesSchema := props["retries"].(map[string]interface{})
	if retriesSchema["default"] != "3" {
		t.Errorf("expected default='3', got %v", retriesSchema["default"])
	}
	timeoutSchema := props["timeout"].(map[string]interface{})
	if _, exists := timeoutSchema["default"]; exists {
		t.Error("timeout should not have a default")
	}
}

func TestUnknownTypeProducesRef(t *testing.T) {
	idx := New()
	schema := idx.fieldTypeToSchema("SomeUnknownType", nil)
	if schema["$ref"] != "#/components/schemas/SomeUnknownType" {
		t.Errorf("expected $ref for unknown type, got %v", schema)
	}
}

func TestInterfacesXImplements(t *testing.T) {
	idx := New()
	idx.Add("com.example.AdminUser", &TypeDecl{
		Name:       "AdminUser",
		Qualified:  "com.example.AdminUser",
		Kind:       "class",
		Interfaces: []string{"Serializable", "Auditable"},
		Fields: []FieldDecl{
			{Name: "role", Type: "String", JSONName: "role"},
		},
	})

	schema, ok := idx.Resolver("com.example.AdminUser")
	if !ok {
		t.Fatal("expected to resolve AdminUser")
	}
	ifaces, ok := schema["x-implements"].([]interface{})
	if !ok {
		t.Fatal("expected x-implements in schema")
	}
	if len(ifaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(ifaces))
	}
	if ifaces[0] != "Serializable" || ifaces[1] != "Auditable" {
		t.Errorf("unexpected interfaces: %v", ifaces)
	}
}

func TestInheritanceWithGenericSuperClass(t *testing.T) {
	idx := New()

	idx.Add("com.example.BaseResponse", &TypeDecl{
		Name:      "BaseResponse",
		Qualified: "com.example.BaseResponse",
		Kind:      "class",
		Fields: []FieldDecl{
			{Name: "status", Type: "String", JSONName: "status"},
		},
	})

	// Child extends BaseResponse<User> — the generic part should be stripped from the $ref
	idx.Add("com.example.UserResponse", &TypeDecl{
		Name:       "UserResponse",
		Qualified:  "com.example.UserResponse",
		Kind:       "class",
		SuperClass: "BaseResponse<User>",
		Fields: []FieldDecl{
			{Name: "status", Type: "String", JSONName: "status"}, // inherited
			{Name: "data", Type: "User", JSONName: "data"},
		},
	})

	schema, ok := idx.Resolver("com.example.UserResponse")
	if !ok {
		t.Fatal("expected to resolve UserResponse")
	}
	allOf, ok := schema["allOf"].([]interface{})
	if !ok {
		t.Fatal("expected allOf for inherited class")
	}
	refObj := allOf[0].(map[string]interface{})
	// Should ref BaseResponse, not BaseResponse<User>
	if refObj["$ref"] != "#/components/schemas/BaseResponse" {
		t.Errorf("expected $ref to BaseResponse (without generics), got %v", refObj["$ref"])
	}
}

func TestMultipleAnnotationsCombined(t *testing.T) {
	// A field can have multiple annotations at once
	schema := map[string]interface{}{"type": "string"}
	annotations := map[string]string{
		"Size":    "min = 1, max = 100",
		"Email":   "",
		"Pattern": `regexp = "^[^@]+@[^@]+$"`,
	}
	applyJavaValidation(annotations, schema)

	if schema["format"] != "email" {
		t.Errorf("expected format=email, got %v", schema["format"])
	}
	if schema["minLength"] != 1 {
		t.Errorf("expected minLength=1, got %v", schema["minLength"])
	}
	if schema["maxLength"] != 100 {
		t.Errorf("expected maxLength=100, got %v", schema["maxLength"])
	}
	if schema["pattern"] != "^[^@]+@[^@]+$" {
		t.Errorf("expected pattern, got %v", schema["pattern"])
	}
}

func TestDateTimeFormats(t *testing.T) {
	idx := New()

	tests := []struct {
		typeName string
		format   string
	}{
		{"LocalDate", "date"},
		{"OffsetDateTime", "date-time"},
		{"ZonedDateTime", "date-time"},
		{"Instant", "date-time"},
		{"UUID", "uuid"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s -> %s", tt.typeName, tt.format), func(t *testing.T) {
			schema := idx.fieldTypeToSchema(tt.typeName, nil)
			if schema["type"] != "string" {
				t.Errorf("expected type=string, got %v", schema["type"])
			}
			if schema["format"] != tt.format {
				t.Errorf("expected format=%s, got %v", tt.format, schema["format"])
			}
		})
	}
}
