// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"strings"
	"testing"
)

func TestNewCompletenessChecker(t *testing.T) {
	checker := NewCompletenessChecker()
	if checker == nil {
		t.Fatal("Expected non-nil checker")
	}
}

func TestCompletenessChecker_EvaluateEmptyMetadata(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	report := checker.Evaluate(metadata)

	if report.TotalOperations != 0 {
		t.Errorf("Expected 0 operations, got %d", report.TotalOperations)
	}

	if report.Score != 0 {
		t.Errorf("Expected score 0 for empty metadata, got %.2f", report.Score)
	}
}

func TestCompletenessChecker_EvaluateFullyDocumentedOperation(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	// Add a fully documented operation
	metadata.Operations["getUsers"] = &OperationInfo{
		ID:           "getUsers",
		Path:         "/users",
		Method:       "GET",
		Summary:      "Get all users",
		Description:  "Returns a list of all users in the system",
		Tags:         []string{"Users"},
		ResponseType: "[]User",
		RequiresAuth: true,
		Rights:       []string{"sp:users:read"},
	}

	report := checker.Evaluate(metadata)

	if report.TotalOperations != 1 {
		t.Errorf("Expected 1 operation, got %d", report.TotalOperations)
	}

	if report.OperationsWithSummary != 1 {
		t.Errorf("Expected 1 operation with summary, got %d", report.OperationsWithSummary)
	}

	if report.OperationsWithDescription != 1 {
		t.Errorf("Expected 1 operation with description, got %d", report.OperationsWithDescription)
	}

	if report.OperationsWithResponse != 1 {
		t.Errorf("Expected 1 operation with response, got %d", report.OperationsWithResponse)
	}

	if report.OperationsWithTags != 1 {
		t.Errorf("Expected 1 operation with tags, got %d", report.OperationsWithTags)
	}

	// Score should be high for fully documented operation
	if report.Score < 50 {
		t.Errorf("Expected score >= 50 for fully documented operation, got %.2f", report.Score)
	}
}

func TestCompletenessChecker_EvaluatePoorlyDocumentedOperation(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	// Add a poorly documented operation
	metadata.Operations["getUsers"] = &OperationInfo{
		ID:     "getUsers",
		Path:   "/users",
		Method: "GET",
		// No summary, description, tags, or response type
	}

	report := checker.Evaluate(metadata)

	if len(report.MissingSummaries) != 1 {
		t.Errorf("Expected 1 missing summary, got %d", len(report.MissingSummaries))
	}

	if len(report.MissingDescriptions) != 1 {
		t.Errorf("Expected 1 missing description, got %d", len(report.MissingDescriptions))
	}

	if len(report.MissingResponses) != 1 {
		t.Errorf("Expected 1 missing response, got %d", len(report.MissingResponses))
	}

	// Score should be low for poorly documented operation
	if report.Score > 50 {
		t.Errorf("Expected score < 50 for poorly documented operation, got %.2f", report.Score)
	}
}

func TestCompletenessChecker_EvaluateMixedOperations(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	// Add a fully documented operation
	metadata.Operations["getUsers"] = &OperationInfo{
		ID:           "getUsers",
		Path:         "/users",
		Method:       "GET",
		Summary:      "Get all users",
		Description:  "Returns a list of all users",
		Tags:         []string{"Users"},
		ResponseType: "[]User",
		RequiresAuth: true,
	}

	// Add a poorly documented operation
	metadata.Operations["createUser"] = &OperationInfo{
		ID:     "createUser",
		Path:   "/users",
		Method: "POST",
		// Missing summary, description
	}

	report := checker.Evaluate(metadata)

	if report.TotalOperations != 2 {
		t.Errorf("Expected 2 operations, got %d", report.TotalOperations)
	}

	// Should have 1 fully documented, 1 poorly documented
	if report.OperationsWithSummary != 1 {
		t.Errorf("Expected 1 operation with summary, got %d", report.OperationsWithSummary)
	}

	if len(report.MissingSummaries) != 1 {
		t.Errorf("Expected 1 missing summary, got %d", len(report.MissingSummaries))
	}
}

func TestCompletenessChecker_EvaluateSchemas(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	// Add a schema with descriptions and examples
	metadata.Types["User"] = &TypeInfo{
		Name: "User",
		Kind: "struct",
		Fields: []FieldInfo{
			{Name: "id", Description: "User ID", Example: "123"},
			{Name: "name", Description: "User name"},
		},
	}

	// Add a schema without descriptions
	metadata.Types["Request"] = &TypeInfo{
		Name: "Request",
		Kind: "struct",
		Fields: []FieldInfo{
			{Name: "data"},
		},
	}

	report := checker.Evaluate(metadata)

	if report.TotalSchemas != 2 {
		t.Errorf("Expected 2 schemas, got %d", report.TotalSchemas)
	}

	if report.SchemasWithDescription != 1 {
		t.Errorf("Expected 1 schema with description, got %d", report.SchemasWithDescription)
	}

	if report.SchemasWithExamples != 1 {
		t.Errorf("Expected 1 schema with examples, got %d", report.SchemasWithExamples)
	}
}

func TestCompletenessChecker_EvaluateRequestBodies(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	// POST without request body
	metadata.Operations["createUser"] = &OperationInfo{
		ID:     "createUser",
		Path:   "/users",
		Method: "POST",
		// No request type
	}

	// POST with request body
	metadata.Operations["updateUser"] = &OperationInfo{
		ID:          "updateUser",
		Path:        "/users/{id}",
		Method:      "PUT",
		RequestType: "UpdateUserRequest",
	}

	// GET (should not need request body)
	metadata.Operations["getUser"] = &OperationInfo{
		ID:     "getUser",
		Path:   "/users/{id}",
		Method: "GET",
	}

	report := checker.Evaluate(metadata)

	// Only POST without request body should be flagged
	if len(report.MissingRequestBodies) != 1 {
		t.Errorf("Expected 1 missing request body, got %d", len(report.MissingRequestBodies))
	}

	if report.MissingRequestBodies[0] != "POST /users" {
		t.Errorf("Expected 'POST /users' in missing request bodies, got '%s'", report.MissingRequestBodies[0])
	}
}

func TestCompletenessReport_MeetsThreshold(t *testing.T) {
	report := &CompletenessReport{
		Score: 75.5,
	}

	if !report.MeetsThreshold(75.0) {
		t.Error("Expected 75.5 to meet threshold of 75.0")
	}

	if report.MeetsThreshold(80.0) {
		t.Error("Expected 75.5 to NOT meet threshold of 80.0")
	}
}

func TestCompletenessReport_Summary(t *testing.T) {
	report := &CompletenessReport{
		Score:                     75.5,
		TotalOperations:          5,
		TotalSchemas:             3,
		OperationsWithSummary:    4,
		OperationsWithDescription: 3,
		MissingSummaries:         []string{"POST /test"},
	}

	summary := report.Summary()

	if !strings.Contains(summary, "75.5%") {
		t.Error("Expected summary to contain overall score")
	}

	if !strings.Contains(summary, "Total Operations: 5") {
		t.Error("Expected summary to contain total operations")
	}
}

func TestCompletenessReport_ToJSON(t *testing.T) {
	report := &CompletenessReport{
		Score:            75.5,
		TotalOperations: 5,
	}

	json := report.ToJSON()

	if !strings.Contains(json, "\"score\"") {
		t.Error("Expected JSON to contain 'score' field")
	}

	if !strings.Contains(json, "75.5") {
		t.Error("Expected JSON to contain score value")
	}
}

func TestCompletenessChecker_SecurityEvaluation(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	// Operation with auth
	metadata.Operations["secureOp"] = &OperationInfo{
		ID:           "secureOp",
		Path:         "/secure",
		Method:       "GET",
		RequiresAuth: true,
		Rights:       []string{"sp:read"},
	}

	// Operation without auth (but explicitly unprotected)
	metadata.Operations["publicOp"] = &OperationInfo{
		ID:          "publicOp",
		Path:        "/public",
		Method:      "GET",
		Unprotected: true,
	}

	// Operation without auth (not explicitly unprotected - should be flagged)
	metadata.Operations["unknownOp"] = &OperationInfo{
		ID:     "unknownOp",
		Path:   "/unknown",
		Method: "GET",
	}

	report := checker.Evaluate(metadata)

	// Only the last operation should be flagged
	if len(report.MissingSecurity) != 1 {
		t.Errorf("Expected 1 missing security, got %d", len(report.MissingSecurity))
	}
}

func TestCompletenessChecker_CountUniquePaths(t *testing.T) {
	checker := NewCompletenessChecker()
	metadata := NewExtractedMetadata()

	metadata.Operations["listUsers"] = &OperationInfo{ID: "listUsers", Path: "/users", Method: "GET"}
	metadata.Operations["createUser"] = &OperationInfo{ID: "createUser", Path: "/users", Method: "POST"}
	metadata.Operations["getUser"] = &OperationInfo{ID: "getUser", Path: "/users/{id}", Method: "GET"}
	metadata.Operations["updateUser"] = &OperationInfo{ID: "updateUser", Path: "/users/{id}", Method: "PUT"}

	report := checker.Evaluate(metadata)

	// Should count 2 unique paths: /users and /users/{id}
	if report.TotalPaths != 2 {
		t.Errorf("Expected 2 unique paths, got %d", report.TotalPaths)
	}
}

