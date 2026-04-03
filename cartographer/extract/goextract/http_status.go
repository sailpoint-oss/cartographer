// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

// HTTP status codes commonly used in web error functions.
// These constants help eliminate duplicate definitions across the codebase.
const (
	StatusOK                  = 200
	StatusCreated             = 201
	StatusAccepted            = 202
	StatusNoContent           = 204
	StatusBadRequest          = 400
	StatusUnauthorized        = 401
	StatusForbidden           = 403
	StatusNotFound            = 404
	StatusMethodNotAllowed    = 405
	StatusNotAcceptable       = 406
	StatusRequestTimeout      = 408
	StatusConflict            = 409
	StatusGone                = 410
	StatusLengthRequired      = 411
	StatusPreconditionFailed  = 412
	StatusRequestEntityTooLarge = 413
	StatusUnsupportedMediaType  = 415
	StatusUnprocessableEntity   = 422
	StatusTooManyRequests       = 429
	StatusClientClosedRequest   = 499
	StatusInternalServerError   = 500
	StatusNotImplemented        = 501
	StatusBadGateway            = 502
	StatusServiceUnavailable    = 503
	StatusGatewayTimeout        = 504
)

// WebErrorFunctionStatusCodes maps web package error function names to HTTP status codes.
// This is the single source of truth for error function to status code mappings.
var WebErrorFunctionStatusCodes = map[string]int{
	"BadRequest":          StatusBadRequest,
	"Unauthorized":        StatusUnauthorized,
	"Forbidden":           StatusForbidden,
	"ForbiddenWithError":  StatusForbidden,
	"NotFound":            StatusNotFound,
	"NotFoundWithError":   StatusNotFound,
	"Gone":                StatusGone,
	"InternalServerError": StatusInternalServerError,
	"ServiceUnavailable":  StatusServiceUnavailable,
	"ContextCanceled":     StatusClientClosedRequest,
	"NoContent":           StatusNoContent,
}

// GetStatusCodeForErrorFunction returns the HTTP status code for a web error function name.
// Returns StatusInternalServerError (500) if the function name is not recognized.
func GetStatusCodeForErrorFunction(funcName string) int {
	if code, ok := WebErrorFunctionStatusCodes[funcName]; ok {
		return code
	}
	return StatusInternalServerError // Default
}

// KnownWebErrorFunctions is a set of known web error function names.
var KnownWebErrorFunctions = map[string]bool{
	"BadRequest":          true,
	"Unauthorized":        true,
	"Forbidden":           true,
	"ForbiddenWithError":  true,
	"NotFound":            true,
	"NotFoundWithError":   true,
	"Gone":                true,
	"InternalServerError": true,
	"ServiceUnavailable":  true,
	"ContextCanceled":     true,
	"NoContent":           true,
}

// IsKnownWebErrorFunction checks if a function name is a known web error function.
func IsKnownWebErrorFunction(funcName string) bool {
	return KnownWebErrorFunctions[funcName]
}

// HTTPStatusText returns the HTTP status text for a status code.
var HTTPStatusText = map[int]string{
	StatusOK:                    "OK",
	StatusCreated:               "Created",
	StatusAccepted:              "Accepted",
	StatusNoContent:             "No Content",
	StatusBadRequest:            "Bad Request",
	StatusUnauthorized:          "Unauthorized",
	StatusForbidden:             "Forbidden",
	StatusNotFound:              "Not Found",
	StatusMethodNotAllowed:      "Method Not Allowed",
	StatusNotAcceptable:         "Not Acceptable",
	StatusRequestTimeout:        "Request Timeout",
	StatusConflict:              "Conflict",
	StatusGone:                  "Gone",
	StatusLengthRequired:        "Length Required",
	StatusPreconditionFailed:    "Precondition Failed",
	StatusRequestEntityTooLarge: "Request Entity Too Large",
	StatusUnsupportedMediaType:  "Unsupported Media Type",
	StatusUnprocessableEntity:   "Unprocessable Entity",
	StatusTooManyRequests:       "Too Many Requests",
	StatusClientClosedRequest:   "Client Closed Request",
	StatusInternalServerError:   "Internal Server Error",
	StatusNotImplemented:        "Not Implemented",
	StatusBadGateway:            "Bad Gateway",
	StatusServiceUnavailable:    "Service Unavailable",
	StatusGatewayTimeout:        "Gateway Timeout",
}

// GetHTTPStatusText returns the HTTP status text for a status code.
// Returns "Unknown Status" if the code is not recognized.
func GetHTTPStatusText(code int) string {
	if text, ok := HTTPStatusText[code]; ok {
		return text
	}
	return "Unknown Status"
}

// HTTPStatusDescription returns a longer description for HTTP status codes.
var HTTPStatusDescription = map[int]string{
	StatusOK:                    "OK",
	StatusCreated:               "Created",
	StatusNoContent:             "No Content",
	StatusBadRequest:            "Bad Request - The request was invalid or cannot be served",
	StatusUnauthorized:          "Unauthorized - Authentication is required and has failed or not been provided",
	StatusForbidden:             "Forbidden - The request is valid but the server is refusing action (insufficient permissions)",
	StatusNotFound:              "Not Found - The requested resource could not be found",
	StatusGone:                  "Gone - The requested resource is no longer available",
	StatusClientClosedRequest:   "Client Closed Request - The client closed the connection before the server responded",
	StatusInternalServerError:   "Internal Server Error - The server encountered an unexpected condition",
	StatusServiceUnavailable:    "Service Unavailable - The server is currently unavailable (overloaded or down)",
}

// GetHTTPStatusDescription returns a description for an HTTP status code.
func GetHTTPStatusDescription(code int) string {
	if desc, ok := HTTPStatusDescription[code]; ok {
		return desc
	}
	// Generic descriptions based on status code range
	if code >= 400 && code < 500 {
		return "Client Error"
	}
	if code >= 500 {
		return "Server Error"
	}
	if code >= 200 && code < 300 {
		return "Success"
	}
	return "Response"
}

