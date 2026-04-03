// Package index maintains a type index for tree-sitter-based extraction.
// It stores type declarations (classes, interfaces, enums) found during scanning
// and resolves them to OpenAPI schemas.
package index

// Index holds type declarations keyed by qualified name (e.g. "com.example.User").
type Index struct {
	types   map[string]*TypeDecl
	imports map[string]map[string]string // file -> simple name -> qualified name
}

// TypeDecl represents a type declaration found in source code.
type TypeDecl struct {
	Name                   string            // simple name (e.g. "User")
	Qualified              string            // fully qualified (e.g. "com.example.User")
	Kind                   string            // "class", "interface", "enum", "struct"
	Package                string            // package/namespace
	SourceFile             string            // path to source file
	Line                   int               // 1-based line number of declaration
	Column                 int               // 1-based column number of declaration
	Fields                 []FieldDecl       // struct/class fields
	EnumValues             []string          // enum constants (for enums)
	SuperClass             string            // parent class if any
	Interfaces             []string          // implemented interfaces
	Generics               []string          // generic type parameters (e.g. ["T", "U"])
	Description            string            // from JavaDoc or @Schema
	DiscriminatorProperty  string            // from @JsonTypeInfo(property = "type")
	DiscriminatorMapping   map[string]string // discriminator value -> subtype name (from @JsonSubTypes)
	ReadOnly               bool              // from @Value (Lombok) — all fields are immutable
	Deprecated             bool              // from @Deprecated annotation on class/enum
}

// FieldDecl represents a field within a type declaration.
type FieldDecl struct {
	Name         string            // field name
	Type         string            // type string (e.g. "String", "List<User>")
	JSONName     string            // JSON serialization name (from @JsonProperty or convention)
	Description  string            // from JavaDoc/comment
	Required     bool              // from @NotNull, @NonNull, etc.
	Nullable     bool              // from @Nullable annotation
	Deprecated   bool              // from @Deprecated annotation
	Annotations  map[string]string // annotation name -> value
	DefaultValue string            // default value if any
	Line         int               // 1-based line number
	Column       int               // 1-based column number
}

// New creates a new type index.
func New() *Index {
	return &Index{
		types:   make(map[string]*TypeDecl),
		imports: make(map[string]map[string]string),
	}
}

// Add registers a type declaration.
func (idx *Index) Add(qualifiedName string, decl *TypeDecl) {
	idx.types[qualifiedName] = decl
}

// Resolve looks up a type by qualified name.
func (idx *Index) Resolve(ref string) (*TypeDecl, bool) {
	d, ok := idx.types[ref]
	return d, ok
}

// ResolveSimple tries to resolve by simple (unqualified) name.
// If there are multiple matches, the first one wins.
func (idx *Index) ResolveSimple(simpleName string) (*TypeDecl, bool) {
	for _, decl := range idx.types {
		if decl.Name == simpleName {
			return decl, true
		}
	}
	return nil, false
}

// AddImport records that sourceFile imports qualifiedName under simpleName.
func (idx *Index) AddImport(sourceFile, simpleName, qualifiedName string) {
	if idx.imports[sourceFile] == nil {
		idx.imports[sourceFile] = make(map[string]string)
	}
	idx.imports[sourceFile][simpleName] = qualifiedName
}

// ResolveInFile resolves a simple type name using the import context of a file.
func (idx *Index) ResolveInFile(sourceFile, simpleName string) (*TypeDecl, bool) {
	// First try the imports for this file
	if fileImports, ok := idx.imports[sourceFile]; ok {
		if qualified, ok := fileImports[simpleName]; ok {
			if decl, ok := idx.types[qualified]; ok {
				return decl, true
			}
		}
	}
	// Fall back to simple name search
	return idx.ResolveSimple(simpleName)
}

// All returns all registered type declarations.
func (idx *Index) All() map[string]*TypeDecl {
	return idx.types
}

// Count returns the number of registered types.
func (idx *Index) Count() int {
	return len(idx.types)
}
