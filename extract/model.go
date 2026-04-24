package extract

// Operation represents an extracted API operation.
type Operation struct {
	Path        string
	Method      string
	OperationID string
	Summary     string
	Description string
	Tags        []string
	Parameters  []Parameter
	RequestBody *RequestBody
	Responses   map[string]Response
	Security    []map[string]interface{}
}

// Parameter represents a request parameter.
type Parameter struct {
	Name     string
	In       string
	Required bool
	Schema   interface{}
}

// RequestBody represents a request body.
type RequestBody struct {
	Content map[string]MediaType
}

// MediaType represents a media type with schema.
type MediaType struct {
	Schema interface{}
}

// Response represents a response.
type Response struct {
	Description string
	Content     map[string]MediaType
	Headers     map[string]interface{}
}

// Metadata is the result of extraction (unified across languages).
type Metadata struct {
	Operations []Operation
	Schemas    map[string]interface{}
	Tags       map[string]TagInfo
	Webhooks   map[string]interface{}
}

// TagInfo holds tag metadata.
type TagInfo struct {
	Summary string
	Parent  string
	Kind    string
}
