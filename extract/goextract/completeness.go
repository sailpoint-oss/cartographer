// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"encoding/json"
	"fmt"
)

// CompletenessReport contains metrics about how complete an OpenAPI spec is.
type CompletenessReport struct {
	// Overall score (0-100)
	Score float64 `json:"score"`

	// Component scores
	PathsScore       float64 `json:"pathsScore"`
	SchemasScore     float64 `json:"schemasScore"`
	OperationsScore  float64 `json:"operationsScore"`
	ResponsesScore   float64 `json:"responsesScore"`
	ParametersScore  float64 `json:"parametersScore"`
	SecurityScore    float64 `json:"securityScore"`
	DocumentionScore float64 `json:"documentationScore"`

	// Metrics
	TotalPaths          int `json:"totalPaths"`
	TotalOperations     int `json:"totalOperations"`
	TotalSchemas        int `json:"totalSchemas"`
	TotalParameters     int `json:"totalParameters"`
	TotalResponses      int `json:"totalResponses"`
	TotalSecuritySchemes int `json:"totalSecuritySchemes"`

	// Missing elements
	MissingDescriptions   []string `json:"missingDescriptions,omitempty"`
	MissingSummaries      []string `json:"missingSummaries,omitempty"`
	MissingResponses      []string `json:"missingResponses,omitempty"`
	MissingRequestBodies  []string `json:"missingRequestBodies,omitempty"`
	MissingSchemas        []string `json:"missingSchemas,omitempty"`
	MissingSecurity       []string `json:"missingSecurity,omitempty"`

	// Warnings
	Warnings []string `json:"warnings,omitempty"`

	// Quality indicators
	OperationsWithSummary     int `json:"operationsWithSummary"`
	OperationsWithDescription int `json:"operationsWithDescription"`
	OperationsWithResponse    int `json:"operationsWithResponse"`
	OperationsWithTags        int `json:"operationsWithTags"`
	SchemasWithDescription    int `json:"schemasWithDescription"`
	SchemasWithExamples       int `json:"schemasWithExamples"`
}

// CompletenessChecker evaluates the completeness of extracted metadata.
type CompletenessChecker struct{}

// NewCompletenessChecker creates a new completeness checker.
func NewCompletenessChecker() *CompletenessChecker {
	return &CompletenessChecker{}
}

// Evaluate evaluates the completeness of extracted metadata.
func (cc *CompletenessChecker) Evaluate(metadata *ExtractedMetadata) *CompletenessReport {
	report := &CompletenessReport{
		MissingDescriptions:  make([]string, 0),
		MissingSummaries:     make([]string, 0),
		MissingResponses:     make([]string, 0),
		MissingRequestBodies: make([]string, 0),
		MissingSchemas:       make([]string, 0),
		MissingSecurity:      make([]string, 0),
		Warnings:             make([]string, 0),
	}

	// Count total elements
	report.TotalPaths = len(cc.countUniquePaths(metadata))
	report.TotalOperations = len(metadata.Operations)
	report.TotalSchemas = len(metadata.Types)
	report.TotalParameters = cc.countTotalParameters(metadata)
	report.TotalResponses = cc.countTotalResponses(metadata)

	// Evaluate operations
	cc.evaluateOperations(metadata, report)

	// Evaluate schemas
	cc.evaluateSchemas(metadata, report)

	// Evaluate security
	cc.evaluateSecurity(metadata, report)

	// Calculate component scores
	cc.calculateScores(report)

	return report
}

// countUniquePaths returns unique paths from operations.
func (cc *CompletenessChecker) countUniquePaths(metadata *ExtractedMetadata) map[string]bool {
	paths := make(map[string]bool)
	for _, op := range metadata.Operations {
		paths[op.Path] = true
	}
	return paths
}

// countTotalParameters counts total parameters across all operations.
func (cc *CompletenessChecker) countTotalParameters(metadata *ExtractedMetadata) int {
	total := 0
	for _, op := range metadata.Operations {
		total += len(op.PathParams)
		total += len(op.PathParamDetails)
		total += len(op.QueryParamDetails)
		total += len(op.HeaderParamDetails)
	}
	return total
}

// countTotalResponses counts total responses across all operations.
func (cc *CompletenessChecker) countTotalResponses(metadata *ExtractedMetadata) int {
	total := 0
	for _, op := range metadata.Operations {
		total += len(op.ErrorResponses)
		total += len(op.SuccessResponses)
		if op.ResponseType != "" {
			total++
		}
	}
	return total
}

// evaluateOperations evaluates the completeness of operations.
func (cc *CompletenessChecker) evaluateOperations(metadata *ExtractedMetadata, report *CompletenessReport) {
	for _, op := range metadata.Operations {
		opID := fmt.Sprintf("%s %s", op.Method, op.Path)

		// Check summary
		if op.Summary != "" {
			report.OperationsWithSummary++
		} else {
			report.MissingSummaries = append(report.MissingSummaries, opID)
		}

		// Check description
		if op.Description != "" {
			report.OperationsWithDescription++
		} else {
			report.MissingDescriptions = append(report.MissingDescriptions, opID)
		}

		// Check response
		if op.ResponseType != "" || len(op.SuccessResponses) > 0 {
			report.OperationsWithResponse++
		} else {
			report.MissingResponses = append(report.MissingResponses, opID)
		}

		// Check tags
		if len(op.Tags) > 0 {
			report.OperationsWithTags++
		}

		// Check request body for non-GET operations
		if op.Method != "GET" && op.Method != "DELETE" && op.Method != "HEAD" {
			if op.RequestType == "" {
				report.MissingRequestBodies = append(report.MissingRequestBodies, opID)
			}
		}

		// Check security
		if !op.RequiresAuth && len(op.Rights) == 0 && !op.Unprotected {
			report.MissingSecurity = append(report.MissingSecurity, opID)
		}
	}
}

// evaluateSchemas evaluates the completeness of schemas.
func (cc *CompletenessChecker) evaluateSchemas(metadata *ExtractedMetadata, report *CompletenessReport) {
	for _, typ := range metadata.Types {
		// Check for description
		hasDescription := false
		hasExample := false

		for _, field := range typ.Fields {
			if field.Description != "" {
				hasDescription = true
			}
			if field.Example != "" {
				hasExample = true
			}
		}

		if hasDescription {
			report.SchemasWithDescription++
		}
		if hasExample {
			report.SchemasWithExamples++
		}
	}
}

// evaluateSecurity evaluates security information.
func (cc *CompletenessChecker) evaluateSecurity(metadata *ExtractedMetadata, report *CompletenessReport) {
	// Count operations requiring auth
	authOps := 0
	for _, op := range metadata.Operations {
		if op.RequiresAuth || len(op.Rights) > 0 {
			authOps++
		}
	}
	report.TotalSecuritySchemes = 1 // Assume OAuth2 is configured
	
	// Add warning if no auth operations
	if authOps == 0 && len(metadata.Operations) > 0 {
		report.Warnings = append(report.Warnings, "No operations require authentication")
	}
}

// calculateScores calculates component and overall scores.
func (cc *CompletenessChecker) calculateScores(report *CompletenessReport) {
	// Operations score
	if report.TotalOperations > 0 {
		summaryRatio := float64(report.OperationsWithSummary) / float64(report.TotalOperations)
		descRatio := float64(report.OperationsWithDescription) / float64(report.TotalOperations)
		respRatio := float64(report.OperationsWithResponse) / float64(report.TotalOperations)
		tagsRatio := float64(report.OperationsWithTags) / float64(report.TotalOperations)

		report.OperationsScore = (summaryRatio*25 + descRatio*25 + respRatio*25 + tagsRatio*25)
	}

	// Schemas score
	if report.TotalSchemas > 0 {
		descRatio := float64(report.SchemasWithDescription) / float64(report.TotalSchemas)
		exampleRatio := float64(report.SchemasWithExamples) / float64(report.TotalSchemas)

		report.SchemasScore = (descRatio*50 + exampleRatio*50)
	}

	// Responses score
	if report.TotalOperations > 0 {
		report.ResponsesScore = float64(report.OperationsWithResponse) / float64(report.TotalOperations) * 100
	}

	// Parameters score (based on having typed parameters)
	if report.TotalParameters > 0 {
		report.ParametersScore = 100 // If we have parameters, full score
	}

	// Security score
	securityOps := report.TotalOperations - len(report.MissingSecurity)
	if report.TotalOperations > 0 {
		report.SecurityScore = float64(securityOps) / float64(report.TotalOperations) * 100
	}

	// Documentation score (weighted average of summaries and descriptions)
	if report.TotalOperations > 0 {
		summaryRatio := float64(report.OperationsWithSummary) / float64(report.TotalOperations)
		descRatio := float64(report.OperationsWithDescription) / float64(report.TotalOperations)
		report.DocumentionScore = (summaryRatio*60 + descRatio*40) * 100
	}

	// Paths score (based on having unique paths)
	if report.TotalPaths > 0 && report.TotalOperations > 0 {
		report.PathsScore = 100 // Full score if we have paths
	}

	// Overall score (weighted average)
	report.Score = (report.OperationsScore*0.25 +
		report.SchemasScore*0.15 +
		report.ResponsesScore*0.20 +
		report.ParametersScore*0.10 +
		report.SecurityScore*0.15 +
		report.DocumentionScore*0.15)
}

// ToJSON returns the report as JSON.
func (report *CompletenessReport) ToJSON() string {
	data, _ := json.MarshalIndent(report, "", "  ")
	return string(data)
}

// Summary returns a human-readable summary of the report.
func (report *CompletenessReport) Summary() string {
	return fmt.Sprintf(`Completeness Report
==================
Overall Score: %.1f%%

Component Scores:
- Operations: %.1f%%
- Schemas: %.1f%%
- Responses: %.1f%%
- Parameters: %.1f%%
- Security: %.1f%%
- Documentation: %.1f%%

Metrics:
- Total Paths: %d
- Total Operations: %d
- Total Schemas: %d
- Total Parameters: %d
- Total Responses: %d

Quality:
- Operations with Summary: %d/%d
- Operations with Description: %d/%d
- Operations with Response: %d/%d
- Operations with Tags: %d/%d
- Schemas with Description: %d/%d
- Schemas with Examples: %d/%d

Missing Elements:
- Missing Summaries: %d
- Missing Descriptions: %d
- Missing Responses: %d
- Missing Request Bodies: %d
- Missing Security: %d

Warnings: %d
`,
		report.Score,
		report.OperationsScore,
		report.SchemasScore,
		report.ResponsesScore,
		report.ParametersScore,
		report.SecurityScore,
		report.DocumentionScore,
		report.TotalPaths,
		report.TotalOperations,
		report.TotalSchemas,
		report.TotalParameters,
		report.TotalResponses,
		report.OperationsWithSummary, report.TotalOperations,
		report.OperationsWithDescription, report.TotalOperations,
		report.OperationsWithResponse, report.TotalOperations,
		report.OperationsWithTags, report.TotalOperations,
		report.SchemasWithDescription, report.TotalSchemas,
		report.SchemasWithExamples, report.TotalSchemas,
		len(report.MissingSummaries),
		len(report.MissingDescriptions),
		len(report.MissingResponses),
		len(report.MissingRequestBodies),
		len(report.MissingSecurity),
		len(report.Warnings),
	)
}

// MeetsThreshold checks if the report meets a minimum score threshold.
func (report *CompletenessReport) MeetsThreshold(minScore float64) bool {
	return report.Score >= minScore
}

