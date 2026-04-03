// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

// Package extract provides static analysis tools for extracting OpenAPI metadata from Go source code.
package goextract

import (
	"encoding/json"
	"fmt"
	"go/types"
)

// ExtractedMetadata represents all metadata extracted from source code analysis.
type ExtractedMetadata struct {
	// Operations maps operation IDs to their metadata
	Operations map[string]*OperationInfo `json:"operations"`

	// Types maps fully qualified type names to their schema information
	Types map[string]*TypeInfo `json:"types"`

	// Tags contains enhanced tag definitions (OpenAPI 3.2)
	Tags map[string]*TagInfo `json:"tags,omitempty"`

	// Webhooks maps webhook names to their metadata (OpenAPI 3.1+)
	Webhooks map[string]*WebhookInfo `json:"webhooks,omitempty"`

	// Package information
	Package string `json:"package"`

	// Source files analyzed
	Files []string `json:"files"`
}

// WebhookInfo represents a webhook event that the service can publish.
// Used to generate the OpenAPI webhooks section (OpenAPI 3.1+).
type WebhookInfo struct {
	// Name is the webhook identifier, e.g., "governanceGroupCreated"
	Name string `json:"name"`

	// EventType is the event type string, e.g., "GOVERNANCE_GROUP"
	EventType string `json:"eventType"`

	// Topic is the Kafka/Iris topic name, e.g., "governance-group-v1"
	Topic string `json:"topic"`

	// Summary is a short description of the webhook
	Summary string `json:"summary,omitempty"`

	// Description provides detailed information about when this webhook fires
	Description string `json:"description,omitempty"`

	// PayloadType is the fully qualified type name of the event payload
	PayloadType string `json:"payloadType"`

	// Direction indicates whether this is a published or consumed event
	Direction string `json:"direction"` // "publish" or "consume"

	// Tags for categorizing the webhook
	Tags []string `json:"tags,omitempty"`

	// Source location where the webhook was detected
	File string `json:"file"`
	Line int    `json:"line"`
}

// TagInfo represents an enhanced tag definition (OpenAPI 3.2).
// Supports hierarchical tags with parent relationships and classification.
type TagInfo struct {
	Name         string        `json:"name"`
	Summary      string        `json:"summary,omitempty"`      // OpenAPI 3.2: short summary
	Description  string        `json:"description,omitempty"`
	Parent       string        `json:"parent,omitempty"`       // OpenAPI 3.2: hierarchical parent tag
	Kind         string        `json:"kind,omitempty"`         // OpenAPI 3.2: resource, action, collection, etc.
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty"`
}

// ExternalDocs represents external documentation reference.
type ExternalDocs struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

// ExampleInfo represents an example with OpenAPI 3.2 enhanced fields.
type ExampleInfo struct {
	Summary         string      `json:"summary,omitempty"`
	Description     string      `json:"description,omitempty"`
	Value           interface{} `json:"value,omitempty"`
	DataValue       interface{} `json:"dataValue,omitempty"`       // OpenAPI 3.2
	SerializedValue string      `json:"serializedValue,omitempty"` // OpenAPI 3.2
	ExternalValue   string      `json:"externalValue,omitempty"`
}

// DocSource tracks provenance for a specific documentation field.
// Emitted into OpenAPI as x-doc-sources.
type DocSource struct {
	SourceKind     string `json:"sourceKind"`               // COMMENT | OPENAPI_DIRECTIVE | SWAGGER_ANNOTATION | VERIFIED_CONVENTION | ...
	Field          string `json:"field"`                    // e.g. operation.summary
	SourceLocation string `json:"sourceLocation,omitempty"` // file:line
	Confidence     string `json:"confidence,omitempty"`     // high|medium|low
	DetectorId     string `json:"detectorId,omitempty"`
}

func mergeDocSources(existing []DocSource, incoming []DocSource) []DocSource {
	if len(incoming) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return incoming
	}
	seen := make(map[string]bool, len(existing)+len(incoming))
	key := func(ds DocSource) string {
		return ds.SourceKind + "|" + ds.Field + "|" + ds.SourceLocation + "|" + ds.Confidence + "|" + ds.DetectorId
	}
	for _, ds := range existing {
		seen[key(ds)] = true
	}
	for _, ds := range incoming {
		k := key(ds)
		if !seen[k] {
			existing = append(existing, ds)
			seen[k] = true
		}
	}
	return existing
}

// OperationInfo contains metadata extracted for a single API operation.
type OperationInfo struct {
	// Operation identification
	ID          string   `json:"id"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
	DocSources  []DocSource `json:"docSources,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Deprecated  bool     `json:"deprecated,omitempty"`

	// HTTP details
	Path       string   `json:"path"`
	Method     string   `json:"method"`
	PathParams []string `json:"pathParams,omitempty"`

	// Detailed parameter information with types
	PathParamDetails   []OperationParamInfo `json:"pathParamDetails,omitempty"`
	QueryParamDetails  []OperationParamInfo `json:"queryParamDetails,omitempty"`
	HeaderParamDetails []OperationParamInfo `json:"headerParamDetails,omitempty"`
	FormParamDetails   []OperationParamInfo `json:"formParamDetails,omitempty"`

	// Request/Response
	RequestType     string `json:"requestType,omitempty"`
	RequestContent  string `json:"requestContent,omitempty"` // Content-Type
	ResponseType    string `json:"responseType,omitempty"`
	ResponseStatus  int    `json:"responseStatus,omitempty"`
	ResponseContent string `json:"responseContent,omitempty"` // Content-Type

	// Streaming response support (OpenAPI 3.2)
	IsStreaming     bool   `json:"isStreaming,omitempty"`     // OpenAPI 3.2: indicates streaming response
	StreamMediaType string `json:"streamMediaType,omitempty"` // OpenAPI 3.2: text/event-stream, application/jsonl, etc.
	StreamItemType  string `json:"streamItemType,omitempty"`  // OpenAPI 3.2: type of each streamed item

	// Query string schema (OpenAPI 3.2)
	QueryStringSchema string `json:"queryStringSchema,omitempty"` // OpenAPI 3.2: schema for all query params

	// Security
	Rights          []string `json:"rights,omitempty"`
	RequiresAuth    bool     `json:"requiresAuth"`
	UserAuth        bool     `json:"userAuth,omitempty"`
	ApplicationAuth bool     `json:"applicationAuth,omitempty"`
	Unprotected     bool     `json:"unprotected,omitempty"`
	UserLevels      []string `json:"userLevels,omitempty"`

	// Additional flags
	Experimental bool `json:"experimental,omitempty"`
	Private      bool `json:"private,omitempty"`

	// Possible error responses
	PossibleErrors []int `json:"possibleErrors,omitempty"`

	// Detailed response information (with schemas and examples)
	ErrorResponses   []ErrorResponseInfo   `json:"errorResponses,omitempty"`
	SuccessResponses []SuccessResponseInfo `json:"successResponses,omitempty"`

	// Source location
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`

	// Handler function name
	HandlerFunc string `json:"handlerFunc,omitempty"`

	// Operation-level examples
	Examples []ExampleInfo `json:"examples,omitempty"`
}

// OperationParamInfo contains detailed information about an operation parameter.
type OperationParamInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Required     bool   `json:"required,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
	Description  string `json:"description,omitempty"`
	ValidateTag  string `json:"validateTag,omitempty"`
	Example      string `json:"example,omitempty"`
	File         string `json:"file,omitempty"`
	Line         int    `json:"line,omitempty"`
	Column       int    `json:"column,omitempty"`
}

// TypeInfo contains schema information for a Go type.
type TypeInfo struct {
	// Package and name
	Package  string `json:"package"`
	Name     string `json:"name"`
	FullName string `json:"fullName"` // package.Name

	// Type classification
	Kind     string `json:"kind"`               // struct, slice, map, basic, etc.
	ElemType string `json:"elemType,omitempty"` // For slices/arrays

	// Description from type-level godoc comment
	Description string `json:"description,omitempty"`

	// Struct fields (if Kind == "struct")
	Fields []FieldInfo `json:"fields,omitempty"`

	// Source location
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// FieldInfo represents a struct field.
type FieldInfo struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	JSONName    string            `json:"jsonName,omitempty"`
	Required    bool              `json:"required,omitempty"`
	Description string            `json:"description,omitempty"`
	Example     string            `json:"example,omitempty"`
	Enum        []string          `json:"enum,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	File        string            `json:"file,omitempty"`
	Line        int               `json:"line,omitempty"`
	Column      int               `json:"column,omitempty"`
}

// NewExtractedMetadata creates a new, empty ExtractedMetadata.
func NewExtractedMetadata() *ExtractedMetadata {
	return &ExtractedMetadata{
		Operations: make(map[string]*OperationInfo),
		Types:      make(map[string]*TypeInfo),
		Tags:       make(map[string]*TagInfo),
		Webhooks:   make(map[string]*WebhookInfo),
		Files:      make([]string, 0),
	}
}

// AddTag adds or updates a tag definition (OpenAPI 3.2).
func (m *ExtractedMetadata) AddTag(tag *TagInfo) {
	if tag.Name != "" {
		m.Tags[tag.Name] = tag
	}
}

// GetTag retrieves a tag by name.
func (m *ExtractedMetadata) GetTag(name string) *TagInfo {
	return m.Tags[name]
}

// AddWebhook adds or updates a webhook definition (OpenAPI 3.1+).
func (m *ExtractedMetadata) AddWebhook(webhook *WebhookInfo) {
	if webhook.Name != "" {
		m.Webhooks[webhook.Name] = webhook
	}
}

// GetWebhook retrieves a webhook by name.
func (m *ExtractedMetadata) GetWebhook(name string) *WebhookInfo {
	return m.Webhooks[name]
}

// AddOperation adds an operation to the metadata.
func (m *ExtractedMetadata) AddOperation(op *OperationInfo) error {
	if op.ID == "" {
		return fmt.Errorf("operation ID is required")
	}

	if existing, exists := m.Operations[op.ID]; exists {
		return fmt.Errorf("duplicate operation ID %q (existing: %s:%d, new: %s:%d)",
			op.ID, existing.File, existing.Line, op.File, op.Line)
	}

	m.Operations[op.ID] = op
	return nil
}

// AddType adds a type to the metadata.
func (m *ExtractedMetadata) AddType(ti *TypeInfo) {
	if ti.FullName != "" {
		m.Types[ti.FullName] = ti
	}
}

// GetOperation retrieves an operation by ID.
func (m *ExtractedMetadata) GetOperation(id string) *OperationInfo {
	return m.Operations[id]
}

// GetType retrieves a type by full name.
func (m *ExtractedMetadata) GetType(fullName string) *TypeInfo {
	return m.Types[fullName]
}

// Merge combines another ExtractedMetadata into this one.
// Later values override earlier ones in case of conflicts.
func (m *ExtractedMetadata) Merge(other *ExtractedMetadata) error {
	if other == nil {
		return nil
	}

	// Merge operations
	for id, op := range other.Operations {
		if existing := m.Operations[id]; existing != nil {
			// Merge operation details, preferring non-empty values from 'other'
			mergeOperation(existing, op)
		} else {
			m.Operations[id] = op
		}
	}

	// Merge types
	for name, ti := range other.Types {
		m.Types[name] = ti
	}

	// Merge tags
	for name, tag := range other.Tags {
		m.Tags[name] = tag
	}

	// Merge webhooks
	for name, webhook := range other.Webhooks {
		if existing := m.Webhooks[name]; existing != nil {
			// Merge webhook details, preferring non-empty values from 'other'
			mergeWebhook(existing, webhook)
		} else {
			m.Webhooks[name] = webhook
		}
	}

	// Merge files list
	fileSet := make(map[string]bool)
	for _, f := range m.Files {
		fileSet[f] = true
	}
	for _, f := range other.Files {
		if !fileSet[f] {
			m.Files = append(m.Files, f)
		}
	}

	return nil
}

// mergeOperation merges two operations, preferring non-empty values from 'new'.
func mergeOperation(existing, new *OperationInfo) {
	if new.Summary != "" {
		existing.Summary = new.Summary
	}
	if new.Description != "" {
		existing.Description = new.Description
	}
	if len(new.DocSources) > 0 {
		existing.DocSources = mergeDocSources(existing.DocSources, new.DocSources)
	}
	if len(new.Tags) > 0 {
		existing.Tags = new.Tags
	}
	if new.Path != "" {
		existing.Path = new.Path
	}
	if new.Method != "" {
		existing.Method = new.Method
	}
	if new.RequestType != "" {
		existing.RequestType = new.RequestType
	}
	if new.ResponseType != "" {
		existing.ResponseType = new.ResponseType
	}
	if new.ResponseStatus != 0 {
		existing.ResponseStatus = new.ResponseStatus
	}
	if len(new.Rights) > 0 {
		existing.Rights = new.Rights
	}
	if new.RequiresAuth {
		existing.RequiresAuth = new.RequiresAuth
	}
	if new.UserAuth {
		existing.UserAuth = new.UserAuth
	}
	if new.ApplicationAuth {
		existing.ApplicationAuth = new.ApplicationAuth
	}
	if len(new.PossibleErrors) > 0 {
		existing.PossibleErrors = new.PossibleErrors
	}
	if new.Deprecated {
		existing.Deprecated = new.Deprecated
	}
	if new.Experimental {
		existing.Experimental = new.Experimental
	}
	if new.Private {
		existing.Private = new.Private
	}
	if len(new.Examples) > 0 {
		existing.Examples = append(existing.Examples, new.Examples...)
	}
}

// mergeWebhook merges two webhooks, preferring non-empty values from 'new'.
func mergeWebhook(existing, new *WebhookInfo) {
	if new.EventType != "" {
		existing.EventType = new.EventType
	}
	if new.Topic != "" {
		existing.Topic = new.Topic
	}
	if new.Summary != "" {
		existing.Summary = new.Summary
	}
	if new.Description != "" {
		existing.Description = new.Description
	}
	if new.PayloadType != "" {
		existing.PayloadType = new.PayloadType
	}
	if new.Direction != "" {
		existing.Direction = new.Direction
	}
	if len(new.Tags) > 0 {
		existing.Tags = new.Tags
	}
}

// Validate checks the metadata for consistency and completeness.
func (m *ExtractedMetadata) Validate() error {
	for id, op := range m.Operations {
		if op.Path == "" {
			return fmt.Errorf("operation %q is missing path", id)
		}
		if op.Method == "" {
			return fmt.Errorf("operation %q is missing HTTP method", id)
		}
	}
	return nil
}

// ToJSON serializes the metadata to JSON.
func (m *ExtractedMetadata) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// FromJSON deserializes metadata from JSON.
func FromJSON(data []byte) (*ExtractedMetadata, error) {
	m := NewExtractedMetadata()
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}
	return m, nil
}

// TypeString converts a Go types.Type to a string representation.
func TypeString(t types.Type) string {
	if t == nil {
		return ""
	}
	return types.TypeString(t, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Path()
	})
}

// SimplifyTypeName extracts a simple name from a full type string.
// e.g., "github.com/sailpoint/pkg.Type" -> "Type"
func SimplifyTypeName(fullName string) string {
	// Find last dot
	for i := len(fullName) - 1; i >= 0; i-- {
		if fullName[i] == '.' {
			return fullName[i+1:]
		}
	}
	return fullName
}
