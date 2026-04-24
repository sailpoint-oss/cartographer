// Package specmodel defines a unified data model for Java and TypeScript
// OpenAPI spec generation. Language-specific extractors convert their native
// result types into this model before passing to sharedspec.
package specmodel

import "github.com/sailpoint-oss/cartographer/extract/index"

// Result holds unified extraction results for spec generation.
type Result struct {
	Operations []*Operation
	Schemas    map[string]any // pre-computed schemas (WITH annotations from index resolver)
	Types      map[string]*index.TypeDecl
}

// Operation represents an extracted API endpoint in a language-agnostic form.
type Operation struct {
	Path                   string
	Method                 string
	OperationID            string
	Summary                string
	Description            string
	Tags                   []string
	Parameters             []*Parameter
	RequestBodyType        string
	RequestBodyDescription string
	ResponseType           string
	ResponseStatus         int
	Deprecated             bool
	DeprecatedSince        string
	Security               []SecurityRequirement
	ConsumesContentType    string
	ProducesContentType    string
	ReturnDescription      string
	ErrorResponses         map[int]string
	AnnotatedResponses     []AnnotatedResponse
	ResponseHeaders        map[string]string
	NullableResponse       bool
	RateLimited            bool
	FormParams             []*Parameter
	File                   string
	Line                   int
	Column                 int
}

// SecurityRequirement pairs a scheme name with its scopes.
type SecurityRequirement struct {
	Scheme string // "oauth2", "bearerAuth"
	Scopes []string
}

// AnnotatedResponse represents a response declared via annotation/decorator.
type AnnotatedResponse struct {
	StatusCode  int
	Description string
	SchemaType  string
}

// Parameter represents an API parameter.
type Parameter struct {
	Name         string
	In           string // path, query, header, cookie, form
	Type         string
	Required     bool
	DefaultValue string
	Description  string
	Format       string
	Pattern      string
	Minimum      *int
	Maximum      *int
	MinLength    *int
	MaxLength    *int
	MinItems     *int
	MaxItems     *int
	Example      string
	Enum         []string
	Deprecated   bool
	File         string
	Line         int
	Column       int
}

// SpecConfig holds configuration for OpenAPI spec generation.
type SpecConfig struct {
	Title           string
	Version         string
	Description     string
	OpenAPIVersion  string // "3.1" or "3.2"
	ServiceTemplate string
	TreeShake       bool
}
