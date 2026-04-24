// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"testing"
)

func TestGetStatusCodeForErrorFunction(t *testing.T) {
	tests := []struct {
		funcName string
		expected int
	}{
		{"BadRequest", StatusBadRequest},
		{"Unauthorized", StatusUnauthorized},
		{"Forbidden", StatusForbidden},
		{"ForbiddenWithError", StatusForbidden},
		{"NotFound", StatusNotFound},
		{"NotFoundWithError", StatusNotFound},
		{"Gone", StatusGone},
		{"InternalServerError", StatusInternalServerError},
		{"ServiceUnavailable", StatusServiceUnavailable},
		{"ContextCanceled", StatusClientClosedRequest},
		{"NoContent", StatusNoContent},
		{"UnknownFunction", StatusInternalServerError}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			result := GetStatusCodeForErrorFunction(tt.funcName)
			if result != tt.expected {
				t.Errorf("GetStatusCodeForErrorFunction(%s) = %d, want %d", tt.funcName, result, tt.expected)
			}
		})
	}
}

func TestIsKnownWebErrorFunction(t *testing.T) {
	knownFunctions := []string{
		"BadRequest", "Unauthorized", "Forbidden", "ForbiddenWithError",
		"NotFound", "NotFoundWithError", "Gone", "InternalServerError",
		"ServiceUnavailable", "ContextCanceled", "NoContent",
	}

	for _, fn := range knownFunctions {
		if !IsKnownWebErrorFunction(fn) {
			t.Errorf("IsKnownWebErrorFunction(%s) = false, want true", fn)
		}
	}

	unknownFunctions := []string{
		"WriteJSON", "RequireRights", "SomeOtherFunc", "HandleRequest",
	}

	for _, fn := range unknownFunctions {
		if IsKnownWebErrorFunction(fn) {
			t.Errorf("IsKnownWebErrorFunction(%s) = true, want false", fn)
		}
	}
}

func TestGetHTTPStatusText(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{StatusOK, "OK"},
		{StatusCreated, "Created"},
		{StatusNoContent, "No Content"},
		{StatusBadRequest, "Bad Request"},
		{StatusUnauthorized, "Unauthorized"},
		{StatusForbidden, "Forbidden"},
		{StatusNotFound, "Not Found"},
		{StatusGone, "Gone"},
		{StatusInternalServerError, "Internal Server Error"},
		{StatusServiceUnavailable, "Service Unavailable"},
		{StatusClientClosedRequest, "Client Closed Request"},
		{999, "Unknown Status"}, // Unknown code
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GetHTTPStatusText(tt.code)
			if result != tt.expected {
				t.Errorf("GetHTTPStatusText(%d) = %s, want %s", tt.code, result, tt.expected)
			}
		})
	}
}

func TestGetHTTPStatusDescription(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{StatusOK, "OK"},
		{StatusBadRequest, "Bad Request - The request was invalid or cannot be served"},
		{StatusUnauthorized, "Unauthorized - Authentication is required and has failed or not been provided"},
		{StatusForbidden, "Forbidden - The request is valid but the server is refusing action (insufficient permissions)"},
		{StatusNotFound, "Not Found - The requested resource could not be found"},
		{StatusInternalServerError, "Internal Server Error - The server encountered an unexpected condition"},
		{StatusServiceUnavailable, "Service Unavailable - The server is currently unavailable (overloaded or down)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GetHTTPStatusDescription(tt.code)
			if result != tt.expected {
				t.Errorf("GetHTTPStatusDescription(%d) = %s, want %s", tt.code, result, tt.expected)
			}
		})
	}
}

func TestGetHTTPStatusDescription_GenericDescriptions(t *testing.T) {
	// Test generic descriptions for unknown codes
	tests := []struct {
		name     string
		code     int
		expected string
	}{
		{"unknown 4xx", 418, "Client Error"},  // I'm a teapot
		{"unknown 5xx", 599, "Server Error"},
		{"unknown 2xx", 299, "Success"},
		{"unknown other", 999, "Server Error"}, // 999 > 500, so "Server Error"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetHTTPStatusDescription(tt.code)
			if result != tt.expected {
				t.Errorf("GetHTTPStatusDescription(%d) = %s, want %s", tt.code, result, tt.expected)
			}
		})
	}
}

func TestStatusCodeConstants(t *testing.T) {
	// Verify status code constants match expected values
	tests := []struct {
		name     string
		constant int
		expected int
	}{
		{"StatusOK", StatusOK, 200},
		{"StatusCreated", StatusCreated, 201},
		{"StatusAccepted", StatusAccepted, 202},
		{"StatusNoContent", StatusNoContent, 204},
		{"StatusBadRequest", StatusBadRequest, 400},
		{"StatusUnauthorized", StatusUnauthorized, 401},
		{"StatusForbidden", StatusForbidden, 403},
		{"StatusNotFound", StatusNotFound, 404},
		{"StatusMethodNotAllowed", StatusMethodNotAllowed, 405},
		{"StatusConflict", StatusConflict, 409},
		{"StatusGone", StatusGone, 410},
		{"StatusUnprocessableEntity", StatusUnprocessableEntity, 422},
		{"StatusTooManyRequests", StatusTooManyRequests, 429},
		{"StatusClientClosedRequest", StatusClientClosedRequest, 499},
		{"StatusInternalServerError", StatusInternalServerError, 500},
		{"StatusNotImplemented", StatusNotImplemented, 501},
		{"StatusBadGateway", StatusBadGateway, 502},
		{"StatusServiceUnavailable", StatusServiceUnavailable, 503},
		{"StatusGatewayTimeout", StatusGatewayTimeout, 504},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestWebErrorFunctionStatusCodes_Coverage(t *testing.T) {
	// Ensure all known error functions have status codes
	for fn := range KnownWebErrorFunctions {
		code := GetStatusCodeForErrorFunction(fn)
		if code == 0 {
			t.Errorf("Missing status code for known function: %s", fn)
		}
	}
}

func TestHTTPStatusText_Coverage(t *testing.T) {
	// Ensure all common status codes have text
	commonCodes := []int{200, 201, 204, 400, 401, 403, 404, 500, 503}

	for _, code := range commonCodes {
		text := GetHTTPStatusText(code)
		if text == "Unknown Status" {
			t.Errorf("Missing status text for common code: %d", code)
		}
	}
}

