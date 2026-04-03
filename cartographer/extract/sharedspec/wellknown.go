package sharedspec

import "strings"

// WellKnownHeaderTypes provides typed schemas for well-known response headers.
var WellKnownHeaderTypes = map[string]map[string]any{
	"X-Total-Count":          {"type": "integer", "format": "int64"},
	"X-Aggregate-Count":      {"type": "integer", "format": "int64"},
	"X-Rate-Limit":           {"type": "integer", "format": "int32"},
	"X-Rate-Limit-Remaining": {"type": "integer", "format": "int32"},
	"Retry-After":            {"type": "integer"},
}

// HeaderTypeSchema returns the appropriate schema for a response header.
func HeaderTypeSchema(name string) map[string]any {
	if s, ok := WellKnownHeaderTypes[name]; ok {
		return s
	}
	if strings.HasSuffix(name, "-Count") || strings.HasSuffix(name, "-Limit") ||
		strings.HasSuffix(name, "-Remaining") {
		return map[string]any{"type": "integer"}
	}
	return map[string]any{"type": "string"}
}

// DownloadContentTypes lists content types that trigger Content-Disposition header.
var DownloadContentTypes = map[string]bool{
	"text/csv":                 true,
	"application/octet-stream": true,
	"application/zip":          true,
	"application/pdf":          true,
	"application/vnd.ms-excel": true,
}

// WellKnownHeaderInfo enriches well-known HTTP header parameters.
type WellKnownHeaderInfo struct {
	Description string
	Example     string
}

// WellKnownHeaders maps header names to enrichment info.
var WellKnownHeaders = map[string]WellKnownHeaderInfo{
	"Accept-Language": {
		Description: "Language preference for localized responses (BCP 47)",
		Example:     "en-US",
	},
	"X-SailPoint-Experimental": {
		Description: "Required header to access experimental API endpoints",
		Example:     "true",
	},
	"X-Total-Count": {
		Description: "Total number of results matching the query",
		Example:     "250",
	},
	"Authorization": {
		Description: "Bearer token for OAuth2 authentication",
		Example:     "Bearer {access_token}",
	},
	"Content-Type": {
		Description: "Media type of the request body",
		Example:     "application/json",
	},
	"X-Request-ID": {
		Description: "Unique identifier for request tracing",
		Example:     "d290f1ee-6c54-4b01-90e6-d701748f0851",
	},
	"SLPT-Request-ID": {
		Description: "SailPoint request correlation identifier",
		Example:     "d290f1ee-6c54-4b01-90e6-d701748f0851",
	},
}

// WellKnownParamInfo provides descriptions for commonly-named parameters.
type WellKnownParamInfo struct {
	Description string
	Example     string
}

// WellKnownParamDescriptions maps parameter names to enrichment info.
var WellKnownParamDescriptions = map[string]WellKnownParamInfo{
	"filters": {
		Description: "Filter expression (e.g. `name eq \"value\"`)",
		Example:     `name eq "example"`,
	},
	"sorters": {
		Description: "Comma-separated sort fields (e.g. `name,-created`)",
		Example:     "name",
	},
	"offset": {
		Description: "Offset into the full result set. Usually specified with `limit` to paginate through the results.",
		Example:     "0",
	},
	"limit": {
		Description: "Max number of results to return.",
		Example:     "250",
	},
	"count": {
		Description: "If `true`, include the total result count in the response headers.",
		Example:     "true",
	},
	"id": {
		Description: "The unique identifier of the resource.",
		Example:     "2c91808a7813090a017814121919ecca",
	},
	"query": {
		Description: "Free-text search query.",
	},
	"search": {
		Description: "Search expression to filter results.",
	},
	"page": {
		Description: "Zero-based page index.",
		Example:     "0",
	},
	"size": {
		Description: "Number of items per page.",
		Example:     "20",
	},
	"sort": {
		Description: "Sort criteria (e.g. `field,asc`).",
		Example:     "name,asc",
	},
	"cursor": {
		Description: "Cursor token for the next page of results.",
	},
	"token": {
		Description: "Continuation token for paginated requests.",
	},
}
