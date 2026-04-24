package generics

import (
	"testing"
)

func TestParseSimple(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"String", "String"},
		{"int", "int"},
		{"void", "void"},
		{"object", "object"},
		{"UUID", "UUID"},
	}
	for _, tt := range tests {
		node := Parse(tt.input)
		if node.Name != tt.want {
			t.Errorf("Parse(%q).Name = %q, want %q", tt.input, node.Name, tt.want)
		}
		if len(node.Params) != 0 {
			t.Errorf("Parse(%q) has %d params, want 0", tt.input, len(node.Params))
		}
		if node.Array {
			t.Errorf("Parse(%q).Array = true, want false", tt.input)
		}
	}
}

func TestParseSingleGeneric(t *testing.T) {
	tests := []struct {
		input    string
		name     string
		paramCnt int
		param0   string
	}{
		{"List<User>", "List", 1, "User"},
		{"Optional<String>", "Optional", 1, "String"},
		{"Set<Integer>", "Set", 1, "Integer"},
		{"ResponseEntity<Foo>", "ResponseEntity", 1, "Foo"},
		{"Promise<Bar>", "Promise", 1, "Bar"},
	}
	for _, tt := range tests {
		node := Parse(tt.input)
		if node.Name != tt.name {
			t.Errorf("Parse(%q).Name = %q, want %q", tt.input, node.Name, tt.name)
		}
		if len(node.Params) != tt.paramCnt {
			t.Fatalf("Parse(%q) has %d params, want %d", tt.input, len(node.Params), tt.paramCnt)
		}
		if node.Params[0].Name != tt.param0 {
			t.Errorf("Parse(%q).Params[0].Name = %q, want %q", tt.input, node.Params[0].Name, tt.param0)
		}
	}
}

func TestParseNestedGenerics(t *testing.T) {
	// ResponseEntity<List<ServiceAPI>>
	node := Parse("ResponseEntity<List<ServiceAPI>>")
	if node.Name != "ResponseEntity" {
		t.Fatalf("got name %q", node.Name)
	}
	if len(node.Params) != 1 {
		t.Fatalf("got %d params", len(node.Params))
	}
	list := node.Params[0]
	if list.Name != "List" {
		t.Fatalf("inner name %q", list.Name)
	}
	if len(list.Params) != 1 || list.Params[0].Name != "ServiceAPI" {
		t.Fatalf("inner params: %v", list.Params)
	}
}

func TestParseMultiParam(t *testing.T) {
	// Map<String, List<User>>
	node := Parse("Map<String, List<User>>")
	if node.Name != "Map" {
		t.Fatalf("got name %q", node.Name)
	}
	if len(node.Params) != 2 {
		t.Fatalf("got %d params, want 2", len(node.Params))
	}
	if node.Params[0].Name != "String" {
		t.Errorf("param0 = %q", node.Params[0].Name)
	}
	if node.Params[1].Name != "List" {
		t.Errorf("param1 name = %q", node.Params[1].Name)
	}
	if len(node.Params[1].Params) != 1 || node.Params[1].Params[0].Name != "User" {
		t.Errorf("param1.Params = %v", node.Params[1].Params)
	}
}

func TestParseArray(t *testing.T) {
	node := Parse("String[]")
	if node.Name != "String" || !node.Array {
		t.Errorf("Parse(\"String[]\") = %+v", node)
	}

	// List<String>[] - array of lists
	node2 := Parse("List<String>[]")
	if node2.Name != "List" || !node2.Array {
		t.Errorf("Parse(\"List<String>[]\") = %+v", node2)
	}
	if len(node2.Params) != 1 || node2.Params[0].Name != "String" {
		t.Errorf("inner: %+v", node2.Params)
	}
}

func TestParseWildcard(t *testing.T) {
	// List<? extends Base>
	node := Parse("List<? extends Base>")
	if node.Name != "List" {
		t.Fatalf("name = %q", node.Name)
	}
	if len(node.Params) != 1 {
		t.Fatalf("params = %d", len(node.Params))
	}
	wc := node.Params[0]
	if wc.Name != "? extends" {
		t.Fatalf("wildcard name = %q", wc.Name)
	}
	if len(wc.Params) != 1 || wc.Params[0].Name != "Base" {
		t.Fatalf("wildcard params = %v", wc.Params)
	}

	// Just ?
	node2 := Parse("List<?>")
	if node2.Params[0].Name != "?" {
		t.Errorf("wildcard ? name = %q", node2.Params[0].Name)
	}
}

func TestParseEmpty(t *testing.T) {
	node := Parse("")
	if node.Name != "object" {
		t.Errorf("Parse(\"\").Name = %q, want \"object\"", node.Name)
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"String", "String"},
		{"List<User>", "List<User>"},
		{"Map<String, List<User>>", "Map<String, List<User>>"},
		{"String[]", "String[]"},
		{"ResponseEntity<List<ServiceAPI>>", "ResponseEntity<List<ServiceAPI>>"},
	}
	for _, tt := range tests {
		got := Parse(tt.input).String()
		if got != tt.want {
			t.Errorf("Parse(%q).String() = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestUnwrapWrappers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ResponseEntity<List<User>>", "List<User>"},
		{"Promise<Observable<Foo>>", "Foo"},
		{"CompletableFuture<String>", "String"},
		{"String", "String"},
		{"List<User>", "List<User>"},
		{"Mono<Flux<Bar>>", "List<Bar>"},
		{"Flux<ItemDto>", "List<ItemDto>"},
	}
	for _, tt := range tests {
		got := Parse(tt.input).UnwrapWrappers().String()
		if got != tt.want {
			t.Errorf("Parse(%q).UnwrapWrappers() = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToOpenAPISchema_SimpleTypes(t *testing.T) {
	tests := []struct {
		input   string
		wantKey string
		wantVal interface{}
	}{
		{"String", "type", "string"},
		{"int", "type", "integer"},
		{"boolean", "type", "boolean"},
		{"object", "type", "object"},
	}
	for _, tt := range tests {
		schema := Parse(tt.input).ToOpenAPISchema(nil)
		if schema[tt.wantKey] != tt.wantVal {
			t.Errorf("Parse(%q).ToOpenAPISchema()[%q] = %v, want %v", tt.input, tt.wantKey, schema[tt.wantKey], tt.wantVal)
		}
	}
}

func TestToOpenAPISchema_List(t *testing.T) {
	schema := Parse("List<User>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Fatalf("expected array type, got %v", schema["type"])
	}
	items, ok := schema["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("items not a map: %T", schema["items"])
	}
	if items["$ref"] != "#/components/schemas/User" {
		t.Errorf("items.$ref = %v", items["$ref"])
	}
}

func TestToOpenAPISchema_Set(t *testing.T) {
	schema := Parse("Set<String>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Fatalf("expected array type, got %v", schema["type"])
	}
	if schema["uniqueItems"] != true {
		t.Errorf("expected uniqueItems=true")
	}
}

func TestToOpenAPISchema_Map(t *testing.T) {
	schema := Parse("Map<String, User>").ToOpenAPISchema(nil)
	if schema["type"] != "object" {
		t.Fatalf("expected object type")
	}
	addl, ok := schema["additionalProperties"].(map[string]interface{})
	if !ok {
		t.Fatalf("additionalProperties not a map: %T", schema["additionalProperties"])
	}
	if addl["$ref"] != "#/components/schemas/User" {
		t.Errorf("additionalProperties.$ref = %v", addl["$ref"])
	}
}

func TestToOpenAPISchema_ResponseEntityListServiceAPI(t *testing.T) {
	// This is the Chronicle bug case
	schema := Parse("ResponseEntity<List<ServiceAPI>>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Fatalf("expected array type, got %v", schema)
	}
	items, ok := schema["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("items not a map: %T", schema["items"])
	}
	if items["$ref"] != "#/components/schemas/ServiceAPI" {
		t.Errorf("items.$ref = %v, want #/components/schemas/ServiceAPI", items["$ref"])
	}
}

func TestToOpenAPISchema_PromiseArray(t *testing.T) {
	// Promise<Observable<Bar[]>> -> array of Bar
	schema := Parse("Promise<Observable<Bar[]>>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Fatalf("expected array type, got %v", schema)
	}
	items, ok := schema["items"].(map[string]interface{})
	if !ok {
		t.Fatalf("items not a map")
	}
	if items["$ref"] != "#/components/schemas/Bar" {
		t.Errorf("items.$ref = %v", items["$ref"])
	}
}

func TestToOpenAPISchema_Optional(t *testing.T) {
	schema := Parse("Optional<String>").ToOpenAPISchema(nil)
	if schema["type"] != "string" {
		t.Errorf("expected string type, got %v", schema)
	}
}

func TestToOpenAPISchema_Page(t *testing.T) {
	schema := Parse("Page<User>").ToOpenAPISchema(nil)
	if schema["type"] != "object" {
		t.Fatalf("expected object type")
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("no properties")
	}
	content, ok := props["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("no content property")
	}
	if content["type"] != "array" {
		t.Errorf("content.type = %v", content["type"])
	}
}

func TestToOpenAPISchema_WildcardExtends(t *testing.T) {
	schema := Parse("List<? extends Base>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Fatalf("expected array")
	}
	items := schema["items"].(map[string]interface{})
	if items["$ref"] != "#/components/schemas/Base" {
		t.Errorf("items = %v", items)
	}
}

func TestToOpenAPISchema_GenericRef(t *testing.T) {
	schema := Parse("ExportedObject<Foo>").ToOpenAPISchema(nil)
	if schema["$ref"] != "#/components/schemas/ExportedObjectFoo" {
		t.Errorf("$ref = %v", schema["$ref"])
	}
}

func TestCollectTypeRefs(t *testing.T) {
	tests := []struct {
		input string
		want  map[string]bool
	}{
		{"String", map[string]bool{}},
		{"User", map[string]bool{"User": true}},
		{"List<User>", map[string]bool{"User": true}},
		{"ResponseEntity<List<ServiceAPI>>", map[string]bool{"ServiceAPI": true}},
		{"Map<String, List<User>>", map[string]bool{"User": true}},
		{"ExportedObject<Foo>", map[string]bool{"ExportedObjectFoo": true, "Foo": true}},
	}
	for _, tt := range tests {
		refs := make(map[string]bool)
		Parse(tt.input).CollectTypeRefs(refs)
		if len(refs) != len(tt.want) {
			t.Errorf("CollectTypeRefs(%q) = %v, want %v", tt.input, refs, tt.want)
			continue
		}
		for k := range tt.want {
			if !refs[k] {
				t.Errorf("CollectTypeRefs(%q) missing %q", tt.input, k)
			}
		}
	}
}

func TestSubstitute(t *testing.T) {
	// List<T> with T=User -> List<User>
	node := Parse("List<T>")
	bindings := map[string]*TypeNode{
		"T": {Name: "User"},
	}
	result := node.Substitute(bindings)
	if result.String() != "List<User>" {
		t.Errorf("Substitute = %q, want List<User>", result.String())
	}

	// Map<K, V> with K=String, V=Integer
	node2 := Parse("Map<K, V>")
	bindings2 := map[string]*TypeNode{
		"K": {Name: "String"},
		"V": {Name: "Integer"},
	}
	result2 := node2.Substitute(bindings2)
	if result2.String() != "Map<String, Integer>" {
		t.Errorf("Substitute = %q, want Map<String, Integer>", result2.String())
	}
}

func TestIsPrimitive(t *testing.T) {
	primitives := []string{"String", "int", "Integer", "long", "Long", "boolean", "Boolean",
		"double", "Double", "float", "Float", "UUID", "void", "Void", "object", "Object",
		"number", "Date", "LocalDate", "OffsetDateTime"}
	for _, p := range primitives {
		if !IsPrimitive(p) {
			t.Errorf("IsPrimitive(%q) = false, want true", p)
		}
	}

	nonPrimitives := []string{"User", "ServiceAPI", "Foo", "List", "Map"}
	for _, p := range nonPrimitives {
		if IsPrimitive(p) {
			t.Errorf("IsPrimitive(%q) = true, want false", p)
		}
	}
}
