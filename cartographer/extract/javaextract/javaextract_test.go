package javaextract

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/generics"
	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
)

// --- Spring Boot test fixtures ---

const springControllerSource = `package com.example.api;

import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import java.util.List;

/**
 * User management endpoints.
 */
@RestController
@RequestMapping("/api/v1/users")
public class UserController {

    /**
     * List all users.
     * Returns a paginated list of users matching the given criteria.
     */
    @GetMapping
    public ResponseEntity<List<UserDTO>> listUsers(
            @RequestParam(value = "offset", required = false, defaultValue = "0") int offset,
            @RequestParam(value = "limit", required = false, defaultValue = "50") int limit,
            @RequestParam(value = "filters", required = false) String filters) {
        return null;
    }

    /**
     * Get a user by ID.
     */
    @GetMapping("/{id}")
    public ResponseEntity<UserDTO> getUser(@PathVariable("id") String id) {
        return null;
    }

    /**
     * Create a new user.
     */
    @PostMapping
    public ResponseEntity<UserDTO> createUser(@RequestBody CreateUserRequest body) {
        return null;
    }

    /**
     * Update an existing user.
     */
    @PutMapping("/{id}")
    public ResponseEntity<UserDTO> updateUser(
            @PathVariable("id") String id,
            @RequestBody UpdateUserRequest body) {
        return null;
    }

    /**
     * Delete a user.
     */
    @DeleteMapping("/{id}")
    public void deleteUser(@PathVariable("id") String id) {
    }

    @Deprecated
    @PatchMapping("/{id}")
    public ResponseEntity<UserDTO> patchUser(
            @PathVariable("id") String id,
            @RequestBody Map<String, Object> patch) {
        return null;
    }
}
`

const springDTOSource = `package com.example.api;

import com.fasterxml.jackson.annotation.JsonProperty;
import javax.validation.constraints.*;

public class UserDTO {
    @NotNull
    @JsonProperty("id")
    private String id;

    @NotBlank
    @Size(min = 1, max = 255)
    private String name;

    @Email
    private String email;

    private boolean active;
}
`

const springCreateReqSource = `package com.example.api;

import javax.validation.constraints.*;

public class CreateUserRequest {
    @NotBlank
    private String name;

    @Email
    @NotNull
    private String email;
}
`

const springUpdateReqSource = `package com.example.api;

public class UpdateUserRequest {
    private String name;
    private String email;
    private boolean active;
}
`

// --- JAX-RS test fixtures ---

const jaxrsResourceSource = `package com.example.api;

import javax.ws.rs.*;
import javax.ws.rs.core.*;

@Path("/api/v1/entitlements")
@Produces("application/json")
@Consumes("application/json")
public class EntitlementResource {

    @GET
    public Response listEntitlements(
            @QueryParam("offset") int offset,
            @QueryParam("limit") int limit) {
        return null;
    }

    @GET
    @Path("/{id}")
    public EntitlementDTO getEntitlement(
            @PathParam("id") String id,
            @Context SecurityContext ctx) {
        return null;
    }

    @POST
    public Response createEntitlement(CreateEntitlementRequest body) {
        return null;
    }

    @PUT
    @Path("/{id}")
    public EntitlementDTO updateEntitlement(
            @PathParam("id") String id,
            UpdateEntitlementRequest body) {
        return null;
    }

    @DELETE
    @Path("/{id}")
    public void deleteEntitlement(@PathParam("id") String id) {
    }

    @PATCH
    @Path("/{id}")
    public EntitlementDTO patchEntitlement(
            @PathParam("id") String id,
            @HeaderParam("If-Match") String ifMatch,
            PatchRequest body) {
        return null;
    }
}
`

const jaxrsDTOSource = `package com.example.api;

public class EntitlementDTO {
    private String id;
    private String name;
    private String description;
    private boolean active;
}
`

// --- Test Helpers ---

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupSpringProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pkg := filepath.Join(dir, "src", "main", "java", "com", "example", "api")
	writeTestFile(t, pkg, "UserController.java", springControllerSource)
	writeTestFile(t, pkg, "UserDTO.java", springDTOSource)
	writeTestFile(t, pkg, "CreateUserRequest.java", springCreateReqSource)
	writeTestFile(t, pkg, "UpdateUserRequest.java", springUpdateReqSource)
	return dir
}

func setupJaxrsProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	pkg := filepath.Join(dir, "src", "main", "java", "com", "example", "api")
	writeTestFile(t, pkg, "EntitlementResource.java", jaxrsResourceSource)
	writeTestFile(t, pkg, "EntitlementDTO.java", jaxrsDTOSource)
	return dir
}

// --- Spring Boot Extraction Tests ---

func TestSpringBootExtraction(t *testing.T) {
	dir := setupSpringProject(t)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
		Verbose:    true,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	t.Logf("Extracted %d operations", len(result.Operations))
	for _, op := range result.Operations {
		t.Logf("  %s %s  (operationId=%s, requestBody=%s, response=%s)",
			op.Method, op.Path, op.OperationID, op.RequestBodyType, op.ResponseType)
		for _, p := range op.Parameters {
			t.Logf("    param: %s in=%s type=%s required=%v", p.Name, p.In, p.Type, p.Required)
		}
	}

	if len(result.Operations) == 0 {
		t.Fatal("no operations extracted from Spring Boot controller")
	}

	// Check that we got the expected operations
	opsByMethod := make(map[string]*Operation)
	for _, op := range result.Operations {
		key := op.Method + " " + op.Path
		opsByMethod[key] = op
	}

	// GET /api/v1/users
	if op, ok := opsByMethod["GET /api/v1/users"]; ok {
		if op.OperationID != "listUsers" {
			t.Errorf("expected operationId 'listUsers', got %q", op.OperationID)
		}
		// Should have query params: offset, limit, filters
		queryParams := 0
		for _, p := range op.Parameters {
			if p.In == "query" {
				queryParams++
			}
		}
		if queryParams < 2 {
			t.Errorf("expected at least 2 query params, got %d", queryParams)
		}
	} else {
		t.Error("missing GET /api/v1/users operation")
	}

	// GET /api/v1/users/{id}
	if op, ok := opsByMethod["GET /api/v1/users/{id}"]; ok {
		if op.OperationID != "getUser" {
			t.Errorf("expected operationId 'getUser', got %q", op.OperationID)
		}
		// Should have path param: id
		pathParams := 0
		for _, p := range op.Parameters {
			if p.In == "path" && p.Name == "id" {
				pathParams++
			}
		}
		if pathParams != 1 {
			t.Errorf("expected 1 path param 'id', got %d", pathParams)
		}
	} else {
		t.Error("missing GET /api/v1/users/{id} operation")
	}

	// POST /api/v1/users
	if op, ok := opsByMethod["POST /api/v1/users"]; ok {
		if op.RequestBodyType == "" {
			t.Error("POST should have a request body type")
		}
		if op.ResponseStatus != 201 {
			t.Errorf("expected POST response status 201, got %d", op.ResponseStatus)
		}
	} else {
		t.Error("missing POST /api/v1/users operation")
	}

	// DELETE /api/v1/users/{id}
	if op, ok := opsByMethod["DELETE /api/v1/users/{id}"]; ok {
		if op.ResponseStatus != 204 {
			t.Errorf("expected DELETE response status 204, got %d", op.ResponseStatus)
		}
	} else {
		t.Error("missing DELETE /api/v1/users/{id} operation")
	}

	// PATCH (deprecated)
	if op, ok := opsByMethod["PATCH /api/v1/users/{id}"]; ok {
		if !op.Deprecated {
			t.Error("PATCH operation should be marked as deprecated")
		}
	} else {
		t.Error("missing PATCH /api/v1/users/{id} operation")
	}
}

func TestSpringBootTypes(t *testing.T) {
	dir := setupSpringProject(t)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	t.Logf("Indexed %d types", len(result.Types))
	for name, decl := range result.Types {
		t.Logf("  %s (%s, %d fields)", name, decl.Kind, len(decl.Fields))
	}

	// Should have indexed UserDTO, CreateUserRequest, UpdateUserRequest
	found := 0
	for _, decl := range result.Types {
		switch decl.Name {
		case "UserDTO":
			found++
			if len(decl.Fields) < 3 {
				t.Errorf("UserDTO should have at least 3 fields, got %d", len(decl.Fields))
			}
			// Check @NotNull on id
			for _, f := range decl.Fields {
				if f.Name == "id" && !f.Required {
					t.Error("UserDTO.id should be required (@NotNull)")
				}
				if f.Name == "id" && f.JSONName != "id" {
					t.Errorf("UserDTO.id should have JSONName 'id', got %q", f.JSONName)
				}
			}
		case "CreateUserRequest":
			found++
		case "UpdateUserRequest":
			found++
		}
	}

	if found < 3 {
		t.Errorf("expected at least 3 DTO types indexed, found %d", found)
	}
}

// --- JAX-RS Extraction Tests ---

func TestJaxRsExtraction(t *testing.T) {
	dir := setupJaxrsProject(t)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
		Verbose:    true,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	t.Logf("Extracted %d operations", len(result.Operations))
	for _, op := range result.Operations {
		t.Logf("  %s %s  (operationId=%s, requestBody=%s, response=%s)",
			op.Method, op.Path, op.OperationID, op.RequestBodyType, op.ResponseType)
		for _, p := range op.Parameters {
			t.Logf("    param: %s in=%s type=%s required=%v", p.Name, p.In, p.Type, p.Required)
		}
	}

	if len(result.Operations) == 0 {
		t.Fatal("no operations extracted from JAX-RS resource")
	}

	opsByMethod := make(map[string]*Operation)
	for _, op := range result.Operations {
		key := op.Method + " " + op.Path
		opsByMethod[key] = op
	}

	// GET /api/v1/entitlements
	if op, ok := opsByMethod["GET /api/v1/entitlements"]; ok {
		if op.OperationID != "listEntitlements" {
			t.Errorf("expected operationId 'listEntitlements', got %q", op.OperationID)
		}
		queryParams := 0
		for _, p := range op.Parameters {
			if p.In == "query" {
				queryParams++
			}
		}
		if queryParams < 2 {
			t.Errorf("expected at least 2 query params, got %d", queryParams)
		}
	} else {
		t.Error("missing GET /api/v1/entitlements operation")
	}

	// GET /api/v1/entitlements/{id} - should filter SecurityContext
	if op, ok := opsByMethod["GET /api/v1/entitlements/{id}"]; ok {
		for _, p := range op.Parameters {
			if p.Type == "SecurityContext" {
				t.Error("SecurityContext should be filtered out as a context type")
			}
		}
		pathParams := 0
		for _, p := range op.Parameters {
			if p.In == "path" {
				pathParams++
			}
		}
		if pathParams != 1 {
			t.Errorf("expected 1 path param, got %d", pathParams)
		}
	} else {
		t.Error("missing GET /api/v1/entitlements/{id} operation")
	}

	// POST /api/v1/entitlements - request body from unannotated param
	if op, ok := opsByMethod["POST /api/v1/entitlements"]; ok {
		if op.RequestBodyType == "" {
			t.Error("POST should have inferred request body type from unannotated param")
		}
	} else {
		t.Error("missing POST /api/v1/entitlements operation")
	}

	// PATCH with @HeaderParam
	if op, ok := opsByMethod["PATCH /api/v1/entitlements/{id}"]; ok {
		headerParams := 0
		for _, p := range op.Parameters {
			if p.In == "header" && p.Name == "If-Match" {
				headerParams++
			}
		}
		if headerParams != 1 {
			t.Errorf("expected 1 header param 'If-Match', got %d", headerParams)
		}
	} else {
		t.Error("missing PATCH /api/v1/entitlements/{id} operation")
	}

	// DELETE - void return
	if op, ok := opsByMethod["DELETE /api/v1/entitlements/{id}"]; ok {
		if op.ResponseStatus != 204 {
			t.Errorf("expected DELETE response status 204, got %d", op.ResponseStatus)
		}
	} else {
		t.Error("missing DELETE /api/v1/entitlements/{id} operation")
	}
}

// --- Spec Generation Tests ---

func TestGenerateSpec(t *testing.T) {
	dir := setupSpringProject(t)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	spec := GenerateSpec(result, SpecConfig{
		Title:           "User Service",
		Version:         "1.0.0",
		OpenAPIVersion:  "3.2",
		ServiceTemplate: "atlas-boot",
		TreeShake:       true,
	})

	// Check top-level structure
	if spec["openapi"] != "3.2.0" {
		t.Errorf("expected openapi 3.2.0, got %v", spec["openapi"])
	}

	info, ok := spec["info"].(map[string]interface{})
	if !ok {
		t.Fatal("spec missing info object")
	}
	if info["title"] != "User Service" {
		t.Errorf("expected title 'User Service', got %v", info["title"])
	}
	if info["x-service-template"] != "atlas-boot" {
		t.Errorf("expected x-service-template 'atlas-boot', got %v", info["x-service-template"])
	}

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("spec missing paths object")
	}
	if len(paths) == 0 {
		t.Error("spec has no paths")
	}

	t.Logf("Generated spec with %d paths", len(paths))
	for path := range paths {
		t.Logf("  %s", path)
	}

	// Check responses have standard error codes
	for _, pathItem := range paths {
		pi, ok := pathItem.(map[string]interface{})
		if !ok {
			continue
		}
		for method, opObj := range pi {
			op, ok := opObj.(map[string]interface{})
			if !ok {
				continue
			}
			responses, ok := op["responses"].(map[string]interface{})
			if !ok {
				t.Errorf("%s operation missing responses", method)
				continue
			}
			for _, code := range []string{"400", "401", "403", "500"} {
				if _, ok := responses[code]; !ok {
					t.Errorf("%s operation missing %s error response", method, code)
				}
			}
		}
	}
}

// --- Edge Cases ---

func TestEmptyProject(t *testing.T) {
	dir := t.TempDir()

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed on empty dir: %v", err)
	}

	if len(result.Operations) != 0 {
		t.Errorf("expected 0 operations from empty dir, got %d", len(result.Operations))
	}
}

func TestNonControllerClass(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example")
	writeTestFile(t, pkg, "Util.java", `package com.example;

public class Util {
    public static String format(String s) {
        return s.trim();
    }
}
`)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Operations) != 0 {
		t.Errorf("expected 0 operations from non-controller class, got %d", len(result.Operations))
	}
}

func TestRequestMappingWithMethod(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example")
	writeTestFile(t, pkg, "LegacyController.java", `package com.example;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/legacy")
public class LegacyController {

    @RequestMapping(path = "/items", method = RequestMethod.POST)
    public String createItem(@RequestBody String body) {
        return "ok";
    }

    @RequestMapping(value = "/items/{id}", method = RequestMethod.GET)
    public String getItem(@PathVariable("id") String id) {
        return "item";
    }
}
`)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	t.Logf("Extracted %d operations", len(result.Operations))
	for _, op := range result.Operations {
		t.Logf("  %s %s (id=%s)", op.Method, op.Path, op.OperationID)
	}

	if len(result.Operations) < 2 {
		t.Errorf("expected at least 2 operations, got %d", len(result.Operations))
	}

	for _, op := range result.Operations {
		if op.OperationID == "createItem" && op.Method != "POST" {
			t.Errorf("createItem should be POST, got %s", op.Method)
		}
		if op.OperationID == "getItem" && op.Method != "GET" {
			t.Errorf("getItem should be GET, got %s", op.Method)
		}
	}
}

// --- Annotation Parsing Unit Tests ---

func TestExtractStringFromAnnotationArgs(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`("hello")`, "hello"},
		{`(value = "world")`, "world"},
		{`("/api/v1")`, "/api/v1"},
		{`(path = "/items")`, "/items"},
		{`("")`, ""},
		{`()`, ""},
	}

	for _, tt := range tests {
		got := extractStringFromAnnotationArgs(tt.input, nil)
		if got != tt.expected {
			t.Errorf("extractStringFromAnnotationArgs(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractMappingPathFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`("/users")`, "/users"},
		{`(path = "/users")`, "/users"},
		{`(value = "/users")`, "/users"},
		{`(value = "/users", method = RequestMethod.GET)`, "/users"},
		{`()`, ""},
		{``, ""},
	}

	for _, tt := range tests {
		got := extractMappingPathFromString(tt.input, nil)
		if got != tt.expected {
			t.Errorf("extractMappingPathFromString(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractMappingPathFromString_QualifiedConstant(t *testing.T) {
	constants := map[string]string{
		"USERS_PATH": "/v3/users",
	}
	got := extractMappingPathFromString(`(value = ApiPaths.USERS_PATH)`, constants)
	if got != "/v3/users" {
		t.Fatalf("expected resolved qualified constant, got %q", got)
	}
}

func TestExtractRequestMappingMethod(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`(method = RequestMethod.POST)`, "POST"},
		{`(method = RequestMethod.GET)`, "GET"},
		{`(value = "/path", method = RequestMethod.PUT)`, "PUT"},
		{`("/path")`, "GET"}, // default
	}

	for _, tt := range tests {
		got := extractRequestMappingMethod(tt.input)
		if got != tt.expected {
			t.Errorf("extractRequestMappingMethod(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestInferResponseStatus(t *testing.T) {
	tests := []struct {
		method     string
		returnType string
		expected   int
	}{
		{"POST", "UserDTO", 201},
		{"GET", "UserDTO", 200},
		{"DELETE", "void", 204},
		{"DELETE", "", 204},
		{"PUT", "UserDTO", 200},
		{"PATCH", "UserDTO", 200},
	}

	for _, tt := range tests {
		got := inferResponseStatus(tt.method, tt.returnType)
		if got != tt.expected {
			t.Errorf("inferResponseStatus(%q, %q) = %d, want %d", tt.method, tt.returnType, got, tt.expected)
		}
	}
}

func TestCamelCaseToWords(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"listUsers", "List Users"},
		{"getUser", "Get User"},
		{"createUser", "Create User"},
		{"deleteEntitlement", "Delete Entitlement"},
	}

	for _, tt := range tests {
		got := sharedspec.CamelCaseToWords(tt.input)
		if got != tt.expected {
			t.Errorf("CamelCaseToWords(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsJaxRsContextType(t *testing.T) {
	tests := []struct {
		typeName string
		expected bool
	}{
		{"SecurityContext", true},
		{"HttpHeaders", true},
		{"UriInfo", true},
		{"HttpServletRequest", true},
		{"AsyncResponse", true},
		{"UserDTO", false},
		{"String", false},
	}

	for _, tt := range tests {
		got := isJaxRsContextType(tt.typeName)
		if got != tt.expected {
			t.Errorf("isJaxRsContextType(%q) = %v, want %v", tt.typeName, got, tt.expected)
		}
	}
}

func TestParamTypeToSchema(t *testing.T) {
	tests := []struct {
		typeName string
		expected string // expected "type" field
	}{
		{"String", "string"},
		{"int", "integer"},
		{"long", "integer"},
		{"boolean", "boolean"},
		{"double", "number"},
		{"UUID", "string"},
	}

	for _, tt := range tests {
		adapter := &javaAdapter{}
		schema := adapter.ParamTypeToSchema(tt.typeName)
		if schema["type"] != tt.expected {
			t.Errorf("ParamTypeToSchema(%q).type = %v, want %q", tt.typeName, schema["type"], tt.expected)
		}
	}
}

func TestExtractSpringRequestParam(t *testing.T) {
	tests := []struct {
		input          string
		expectedName   string
		expectedReq    bool
		expectedDefVal string
	}{
		{`("limit")`, "limit", true, ""},
		{`(value = "offset", required = false, defaultValue = "0")`, "offset", false, "0"},
		{`(name = "q", required = true)`, "q", true, ""},
		{`()`, "", true, ""},
	}

	for _, tt := range tests {
		name, req, def := extractSpringRequestParam(tt.input, nil)
		if name != tt.expectedName {
			t.Errorf("extractSpringRequestParam(%q).name = %q, want %q", tt.input, name, tt.expectedName)
		}
		if req != tt.expectedReq {
			t.Errorf("extractSpringRequestParam(%q).required = %v, want %v", tt.input, req, tt.expectedReq)
		}
		if def != tt.expectedDefVal {
			t.Errorf("extractSpringRequestParam(%q).defaultValue = %q, want %q", tt.input, def, tt.expectedDefVal)
		}
	}
}

// =============================================================================
// Phase 1: JavaDoc parsing tests
// =============================================================================

func TestParseJavaDocParams(t *testing.T) {
	comment := `/**
	 * Retrieves a user by their identifier.
	 *
	 * This method looks up a user in the database.
	 *
	 * @param id The unique user identifier
	 * @param includeDeleted Whether to include soft-deleted users
	 * @return The user object matching the given ID
	 * @throws NotFoundException If no user exists with the given ID
	 * @throws BadRequestException If the ID format is invalid
	 */`

	result := parseJavaDoc(comment)

	if result.Summary != "Retrieves a user by their identifier." {
		t.Errorf("summary = %q, want 'Retrieves a user by their identifier.'", result.Summary)
	}
	if result.Description == "" {
		t.Error("description should not be empty")
	}
	if desc, ok := result.Params["id"]; !ok || desc != "The unique user identifier" {
		t.Errorf("@param id = %q, want 'The unique user identifier'", desc)
	}
	if desc, ok := result.Params["includeDeleted"]; !ok || desc != "Whether to include soft-deleted users" {
		t.Errorf("@param includeDeleted = %q, want 'Whether to include soft-deleted users'", desc)
	}
	if result.Return != "The user object matching the given ID" {
		t.Errorf("@return = %q, want 'The user object matching the given ID'", result.Return)
	}
	if desc, ok := result.Throws["NotFoundException"]; !ok || desc != "If no user exists with the given ID" {
		t.Errorf("@throws NotFoundException = %q, want 'If no user exists with the given ID'", desc)
	}
	if desc, ok := result.Throws["BadRequestException"]; !ok || desc != "If the ID format is invalid" {
		t.Errorf("@throws BadRequestException = %q, want 'If the ID format is invalid'", desc)
	}
}

func TestParseJavaDocEmpty(t *testing.T) {
	result := parseJavaDoc("/** */")
	if result.Summary != "" {
		t.Errorf("summary should be empty, got %q", result.Summary)
	}
	if len(result.Params) != 0 {
		t.Errorf("params should be empty, got %v", result.Params)
	}
}

func TestCleanJavaDoc(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`See {@link User#getName()} for details`, "See User.getName for details"},
		{`Returns {@code true} when valid`, "Returns true when valid"},
		{`Uses {@value MAX_RETRIES} attempts`, "Uses MAX_RETRIES attempts"},
		{`{@link ClassName} reference`, "ClassName reference"},
		{`Plain text`, "Plain text"},
		{`{@code null}`, "null"},
	}
	for _, tt := range tests {
		got := cleanJavaDoc(tt.input)
		if got != tt.expected {
			t.Errorf("cleanJavaDoc(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExceptionToStatusCode(t *testing.T) {
	tests := []struct {
		exception string
		expected  int
	}{
		{"NotFoundException", 404},
		{"ResourceNotFoundException", 404},
		{"BadRequestException", 400},
		{"IllegalArgumentException", 400},
		{"ValidationException", 400},
		{"UnauthorizedException", 401},
		{"AuthenticationException", 401},
		{"ForbiddenException", 403},
		{"AccessDeniedException", 403},
		{"ConflictException", 409},
		{"RuntimeException", 500},
	}
	for _, tt := range tests {
		got := exceptionToStatusCode(tt.exception)
		if got != tt.expected {
			t.Errorf("exceptionToStatusCode(%q) = %d, want %d", tt.exception, got, tt.expected)
		}
	}
}

// =============================================================================
// Phase 1 + Phase 2: End-to-end extraction with JavaDoc and annotations
// =============================================================================

const javadocControllerSource = `package com.example.api;

import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import io.swagger.v3.oas.annotations.responses.*;
import io.swagger.v3.oas.annotations.Parameter;

@RestController
@RequestMapping("/api/v1/items")
public class ItemController {

    /**
     * Retrieve an item by ID.
     *
     * Looks up a single item from the catalog.
     *
     * @param id The unique item identifier
     * @return The item matching the requested ID
     * @throws NotFoundException If the item does not exist
     */
    @GetMapping("/{id}")
    @ApiResponses({
        @ApiResponse(responseCode = "200", description = "Item found successfully"),
        @ApiResponse(responseCode = "404", description = "Item not found")
    })
    public ResponseEntity<ItemDTO> getItem(
            @PathVariable("id") @Parameter(description = "Unique item ID", required = true) String id) {
        return null;
    }

    /**
     * Delete an item.
     *
     * @param id The item to delete
     * @throws ForbiddenException If the user lacks permission
     */
    @DeleteMapping("/{id}")
    @ApiResponse(responseCode = "204", description = "Item deleted")
    public void deleteItem(@PathVariable("id") String id) {
    }
}
`

func TestJavaDocParamDescriptions(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example", "api")
	writeTestFile(t, pkg, "ItemController.java", javadocControllerSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, op := range result.Operations {
		if op.OperationID == "getItem" {
			// @Parameter annotation should override JavaDoc
			for _, p := range op.Parameters {
				if p.Name == "id" && p.Description == "" {
					t.Error("getItem param 'id' should have a description from @Parameter or @param")
				}
			}

			if op.ReturnDescription == "" {
				t.Error("getItem should have a ReturnDescription from @return")
			}

			if len(op.AnnotatedResponses) < 2 {
				t.Errorf("getItem should have 2 annotated responses, got %d", len(op.AnnotatedResponses))
			}
		}

		if op.OperationID == "deleteItem" {
			if op.ErrorResponses == nil {
				t.Error("deleteItem should have error responses from @throws")
			} else if _, ok := op.ErrorResponses[403]; !ok {
				t.Error("deleteItem should have a 403 error from @throws ForbiddenException")
			}
		}
	}
}

func TestAnnotatedResponsesInSpec(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example", "api")
	writeTestFile(t, pkg, "ItemController.java", javadocControllerSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	spec := GenerateSpec(result, SpecConfig{
		Title:   "Item Service",
		Version: "1.0.0",
	})

	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items/{id}"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})

	// @ApiResponse should have overridden defaults
	if resp200, ok := responses["200"].(map[string]interface{}); ok {
		if resp200["description"] != "Item found successfully" {
			t.Errorf("200 description = %q, want 'Item found successfully'", resp200["description"])
		}
	} else {
		t.Error("missing 200 response")
	}

	if resp404, ok := responses["404"].(map[string]interface{}); ok {
		if resp404["description"] != "Item not found" {
			t.Errorf("404 description = %q, want 'Item not found'", resp404["description"])
		}
	} else {
		t.Error("missing 404 response")
	}
}

// =============================================================================
// Phase 2.4: @Hidden tests
// =============================================================================

const hiddenControllerSource = `package com.example.api;

import org.springframework.web.bind.annotation.*;
import io.swagger.v3.oas.annotations.Hidden;

@Hidden
@RestController
@RequestMapping("/internal")
public class InternalController {

    @GetMapping("/health")
    public String health() {
        return "ok";
    }
}
`

const hiddenMethodSource = `package com.example.api;

import org.springframework.web.bind.annotation.*;
import io.swagger.v3.oas.annotations.Hidden;

@RestController
@RequestMapping("/api")
public class MixedController {

    @GetMapping("/public")
    public String publicEndpoint() {
        return "ok";
    }

    @Hidden
    @GetMapping("/private")
    public String privateEndpoint() {
        return "secret";
    }
}
`

func TestHiddenController(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example", "api")
	writeTestFile(t, pkg, "InternalController.java", hiddenControllerSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Operations) != 0 {
		t.Errorf("@Hidden controller should produce 0 operations, got %d", len(result.Operations))
	}
}

func TestHiddenMethod(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example", "api")
	writeTestFile(t, pkg, "MixedController.java", hiddenMethodSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Operations) != 1 {
		t.Errorf("expected 1 non-hidden operation, got %d", len(result.Operations))
	}

	if len(result.Operations) > 0 && result.Operations[0].OperationID != "publicEndpoint" {
		t.Errorf("expected operationId 'publicEndpoint', got %q", result.Operations[0].OperationID)
	}
}

// =============================================================================
// Phase 3: Security tests
// =============================================================================

const securityControllerSource = `package com.example.api;

import org.springframework.web.bind.annotation.*;
import org.springframework.security.access.prepost.PreAuthorize;
import io.swagger.v3.oas.annotations.security.SecurityRequirement;

@RestController
@RequestMapping("/api/v1/reports")
public class ReportController {

    @GetMapping
    @PreAuthorize("hasAuthority('sp:reports:read')")
    public String listReports() {
        return "[]";
    }

    @PostMapping
    @SecurityRequirement(name = "oauth2", scopes = {"sp:reports:write"})
    public String createReport(@RequestBody String body) {
        return "ok";
    }
}
`

func TestSecurityExtraction(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example", "api")
	writeTestFile(t, pkg, "ReportController.java", securityControllerSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	for _, op := range result.Operations {
		if op.OperationID == "listReports" {
			if len(op.Security) == 0 {
				t.Error("listReports should have security from @PreAuthorize")
			} else if op.Security[0] != "sp:reports:read" {
				t.Errorf("listReports security = %v, want [sp:reports:read]", op.Security)
			}
		}
		if op.OperationID == "createReport" {
			if len(op.Security) == 0 {
				t.Error("createReport should have security from @SecurityRequirement")
			} else if op.Security[0] != "sp:reports:write" {
				t.Errorf("createReport security = %v, want [sp:reports:write]", op.Security)
			}
		}
	}

	// Verify security schemes in spec
	spec := GenerateSpec(result, SpecConfig{Title: "Reports", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	secSchemes, ok := components["securitySchemes"].(map[string]interface{})
	if !ok || secSchemes == nil {
		t.Error("spec should have securitySchemes")
	}
	if _, ok := secSchemes["oauth2"]; !ok {
		t.Error("spec should have oauth2 security scheme")
	}
}

func TestParsePreAuthorize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`("hasAuthority('sp:api:read')")`, []string{"sp:api:read"}},
		{`("hasRole('ADMIN')")`, []string{"ROLE_ADMIN"}},
		{`("hasAnyAuthority('sp:read', 'sp:write')")`, []string{"sp:read", "sp:write"}},
	}
	for _, tt := range tests {
		got := parsePreAuthorize(tt.input, nil)
		if len(got) != len(tt.expected) {
			t.Errorf("parsePreAuthorize(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("parsePreAuthorize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestParseSecuredAnnotation(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`("ROLE_ADMIN")`, []string{"ROLE_ADMIN"}},
		{`({"ROLE_USER", "ROLE_ADMIN"})`, []string{"ROLE_USER", "ROLE_ADMIN"}},
	}
	for _, tt := range tests {
		got := parseSecuredAnnotation(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("parseSecuredAnnotation(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("parseSecuredAnnotation(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

// =============================================================================
// Phase 4: Tags and types tests
// =============================================================================

func TestParsePluralTags(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`({@Tag(name = "Users"), @Tag(name = "Admin")})`, []string{"Users", "Admin"}},
		{`({@Tag(name = "Single")})`, []string{"Single"}},
	}
	for _, tt := range tests {
		got := parsePluralTags(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("parsePluralTags(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("parsePluralTags(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestWildcardTypeSchema(t *testing.T) {
	schema := generics.Parse("?").ToOpenAPISchema(nil)
	if schema["type"] != "object" {
		t.Errorf("? should be object, got %v", schema)
	}

	schema = generics.Parse("? extends UserDTO").ToOpenAPISchema(nil)
	if _, ok := schema["$ref"]; !ok {
		t.Errorf("? extends UserDTO should produce $ref, got %v", schema)
	}

	schema = generics.Parse("? super UserDTO").ToOpenAPISchema(nil)
	if _, ok := schema["$ref"]; !ok {
		t.Errorf("? super UserDTO should produce $ref to bound type, got %v", schema)
	}
}

func TestPageTypeSchema(t *testing.T) {
	schema := generics.Parse("Page<UserDTO>").ToOpenAPISchema(nil)
	if schema["type"] != "object" {
		t.Fatalf("Page<UserDTO> should be object, got %v", schema)
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Page<UserDTO> should have properties")
	}
	content, ok := props["content"].(map[string]interface{})
	if !ok {
		t.Fatal("Page<UserDTO> should have content property")
	}
	if content["type"] != "array" {
		t.Errorf("content should be array, got %v", content["type"])
	}
	items, ok := content["items"].(map[string]interface{})
	if !ok {
		t.Fatal("content items should exist")
	}
	if items["$ref"] != "#/components/schemas/UserDTO" {
		t.Errorf("items.$ref = %v, want #/components/schemas/UserDTO", items["$ref"])
	}
}

// =============================================================================
// Phase 4.3: Pageable parameter expansion test
// =============================================================================

const pageableControllerSource = `package com.example.api;

import org.springframework.web.bind.annotation.*;
import org.springframework.data.domain.Page;
import org.springframework.data.domain.Pageable;

@RestController
@RequestMapping("/api/v1/items")
public class PageableController {

    @GetMapping
    public Page<ItemDTO> listItems(Pageable pageable) {
        return null;
    }
}
`

func TestPageableExpansion(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example", "api")
	writeTestFile(t, pkg, "PageableController.java", pageableControllerSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Operations) == 0 {
		t.Fatal("expected at least 1 operation")
	}

	op := result.Operations[0]
	paramNames := make(map[string]bool)
	for _, p := range op.Parameters {
		paramNames[p.Name] = true
	}

	for _, expected := range []string{"page", "size", "sort"} {
		if !paramNames[expected] {
			t.Errorf("expected Pageable expansion to include %q parameter, got params: %v", expected, paramNames)
		}
	}
}

// =============================================================================
// Phase 5: Field JavaDoc and @Schema tests
// =============================================================================

const schemaAnnotatedDTO = `package com.example.api;

import io.swagger.v3.oas.annotations.media.Schema;

@Schema(description = "Represents a catalog item")
public class ItemDTO {
    @Schema(description = "Unique identifier for the item")
    private String id;

    /** Display name shown to users. */
    private String name;

    private int quantity;
}
`

func TestSchemaAnnotationsOnDTO(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "com", "example", "api")
	writeTestFile(t, pkg, "ItemDTO.java", schemaAnnotatedDTO)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// Check type-level description
	for _, decl := range result.Types {
		if decl.Name == "ItemDTO" {
			if decl.Description != "Represents a catalog item" {
				t.Errorf("ItemDTO description = %q, want 'Represents a catalog item'", decl.Description)
			}

			for _, f := range decl.Fields {
				switch f.Name {
				case "id":
					if f.Description != "Unique identifier for the item" {
						t.Errorf("field id description = %q, want 'Unique identifier for the item'", f.Description)
					}
				case "name":
					if f.Description != "Display name shown to users." {
						t.Errorf("field name description = %q, want 'Display name shown to users.'", f.Description)
					}
				}
			}
			return
		}
	}
	t.Error("ItemDTO type not found in results")
}

func TestReturnDescriptionInSpec(t *testing.T) {
	op := &Operation{
		Path:              "/test",
		Method:            "GET",
		OperationID:       "testOp",
		ResponseType:      "String",
		ResponseStatus:    200,
		ReturnDescription: "The greeting message",
	}
	result := &Result{
		Operations: []*Operation{op},
		Schemas:    make(map[string]interface{}),
		Types:      make(map[string]*index.TypeDecl),
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0"})
	paths := spec["paths"].(map[string]any)
	pathItem := paths["/test"].(map[string]any)
	getOp := pathItem["get"].(map[string]any)
	responses := getOp["responses"].(map[string]any)
	resp200, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatal("missing 200 response")
	}
	if resp200["description"] != "The greeting message" {
		t.Errorf("200 description = %q, want 'The greeting message'", resp200["description"])
	}
}

func TestApiResponseAnnotationParsing(t *testing.T) {
	tests := []struct {
		input      string
		statusCode int
		desc       string
	}{
		{`(responseCode = "200", description = "Success")`, 200, "Success"},
		{`(responseCode = "404", description = "Not found")`, 404, "Not found"},
		{`()`, 0, ""},
	}
	for _, tt := range tests {
		ar := parseApiResponseAnnotation(tt.input)
		if tt.statusCode == 0 && tt.desc == "" {
			if ar != nil {
				t.Errorf("parseApiResponseAnnotation(%q) should return nil", tt.input)
			}
			continue
		}
		if ar == nil {
			t.Errorf("parseApiResponseAnnotation(%q) returned nil", tt.input)
			continue
		}
		if ar.StatusCode != tt.statusCode {
			t.Errorf("parseApiResponseAnnotation(%q).StatusCode = %d, want %d", tt.input, ar.StatusCode, tt.statusCode)
		}
		if ar.Description != tt.desc {
			t.Errorf("parseApiResponseAnnotation(%q).Description = %q, want %q", tt.input, ar.Description, tt.desc)
		}
	}
}

func TestParameterAnnotationParsing(t *testing.T) {
	result := parseParameterAnnotation(`(description = "The user ID", example = "abc-123", required = true)`)
	if result.Description != "The user ID" {
		t.Errorf("description = %q, want 'The user ID'", result.Description)
	}
	if result.Example != "abc-123" {
		t.Errorf("example = %q, want 'abc-123'", result.Example)
	}
	if result.Required == nil || !*result.Required {
		t.Error("required should be true")
	}
}

func TestBuildSecuritySchemes(t *testing.T) {
	result := &Result{
		Operations: []*Operation{
			{Security: []string{"sp:read", "sp:write"}},
			{Security: []string{"sp:read"}},
		},
		Schemas: make(map[string]interface{}),
		Types:   make(map[string]*index.TypeDecl),
	}
	adapter := &javaAdapter{}
	schemes := adapter.BuildSecuritySchemes(result.ToUnifiedResult())
	if schemes == nil {
		t.Fatal("security schemes should not be nil")
	}
	oauth2, ok := schemes["oauth2"].(map[string]any)
	if !ok {
		t.Fatal("missing oauth2 scheme")
	}
	if oauth2["type"] != "oauth2" {
		t.Errorf("type = %v, want oauth2", oauth2["type"])
	}
}

func TestBuildSecuritySchemesEmpty(t *testing.T) {
	result := &Result{
		Operations: []*Operation{
			{Security: nil},
		},
		Schemas: make(map[string]interface{}),
		Types:   make(map[string]*index.TypeDecl),
	}
	adapter := &javaAdapter{}
	schemes := adapter.BuildSecuritySchemes(result.ToUnifiedResult())
	if schemes != nil {
		t.Errorf("security schemes should be nil for ops without security, got %v", schemes)
	}
}

func TestFieldValidationAnnotations(t *testing.T) {
	src := `package com.example;
import javax.validation.constraints.*;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @PostMapping
    public ItemDTO createItem(@RequestBody ItemDTO body) {
        return null;
    }
}

public class ItemDTO {
    @Size(min = 2, max = 100)
    private String name;

    @Min(1)
    @Max(1000)
    private int quantity;

    @Email
    private String email;

    @Pattern(regexp = "^[a-z]+$")
    private String code;

    @Positive
    private int score;

    @NotBlank
    private String title;

    @DecimalMin("0.01")
    @DecimalMax("999.99")
    private double price;

    @Future
    private String deadline;
}`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "ItemController.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	itemDTO, ok := schemas["ItemDTO"].(map[string]interface{})
	if !ok {
		t.Fatal("missing ItemDTO schema")
	}
	props := itemDTO["properties"].(map[string]interface{})

	// @Size(min=2, max=100) on name
	nameProp := props["name"].(map[string]interface{})
	if nameProp["minLength"] != 2 {
		t.Errorf("name.minLength = %v, want 2", nameProp["minLength"])
	}
	if nameProp["maxLength"] != 100 {
		t.Errorf("name.maxLength = %v, want 100", nameProp["maxLength"])
	}

	// @Min(1) @Max(1000) on quantity
	qtyProp := props["quantity"].(map[string]interface{})
	if qtyProp["minimum"] != 1 {
		t.Errorf("quantity.minimum = %v, want 1", qtyProp["minimum"])
	}
	if qtyProp["maximum"] != 1000 {
		t.Errorf("quantity.maximum = %v, want 1000", qtyProp["maximum"])
	}

	// @Email on email
	emailProp := props["email"].(map[string]interface{})
	if emailProp["format"] != "email" {
		t.Errorf("email.format = %v, want email", emailProp["format"])
	}

	// @Pattern on code
	codeProp := props["code"].(map[string]interface{})
	if codeProp["pattern"] != "^[a-z]+$" {
		t.Errorf("code.pattern = %v, want ^[a-z]+$", codeProp["pattern"])
	}

	// @Positive on score
	scoreProp := props["score"].(map[string]interface{})
	if scoreProp["exclusiveMinimum"] != 0 {
		t.Errorf("score.exclusiveMinimum = %v, want 0", scoreProp["exclusiveMinimum"])
	}

	// @NotBlank on title
	titleProp := props["title"].(map[string]interface{})
	if titleProp["minLength"] != 1 {
		t.Errorf("title.minLength = %v, want 1", titleProp["minLength"])
	}

	// @Future on deadline
	deadlineProp := props["deadline"].(map[string]interface{})
	if deadlineProp["format"] != "date-time" {
		t.Errorf("deadline.format = %v, want date-time", deadlineProp["format"])
	}
}

func TestJsonIgnoreExcludesField(t *testing.T) {
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/things")
public class ThingController {
    @GetMapping("/{id}")
    public ThingDTO get(@PathVariable("id") String id) { return null; }
}

public class ThingDTO {
    private String visible;

    @JsonIgnore
    private String hidden;
}`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "ThingController.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Things", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	dto := schemas["ThingDTO"].(map[string]interface{})
	props := dto["properties"].(map[string]interface{})

	if _, ok := props["visible"]; !ok {
		t.Error("visible field should be present")
	}
	if _, ok := props["hidden"]; ok {
		t.Error("@JsonIgnore field should be excluded from schema")
	}
}

func TestJsonPropertyReadWriteOnly(t *testing.T) {
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/accounts")
public class AccountController {
    @GetMapping("/{id}")
    public AccountDTO get(@PathVariable("id") String id) { return null; }
}

public class AccountDTO {
    private String name;

    @JsonProperty(access = JsonProperty.Access.READ_ONLY)
    private String createdAt;

    @JsonProperty(access = JsonProperty.Access.WRITE_ONLY)
    private String password;
}`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "AccountController.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Accounts", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	dto := schemas["AccountDTO"].(map[string]interface{})
	props := dto["properties"].(map[string]interface{})

	createdAt := props["createdAt"].(map[string]interface{})
	if createdAt["readOnly"] != true {
		t.Error("createdAt should be readOnly")
	}

	password := props["password"].(map[string]interface{})
	if password["writeOnly"] != true {
		t.Error("password should be writeOnly")
	}
}

func TestRequireRightExtraction(t *testing.T) {
	src := `package com.example;
@Path("/api/v1/tags")
public class TagsResource {
    @GET
    @RequireRight("sp:tags:read")
    public List<TagDTO> list() { return null; }

    @DELETE
    @Path("/{id}")
    @RequireRight({"sp:tags:delete", "sp:tags:manage"})
    public void delete(@PathParam("id") String id) {}
}`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "TagsResource.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(result.Operations))
	}

	for _, op := range result.Operations {
		t.Logf("  %s %s security=%v", op.Method, op.Path, op.Security)
	}

	// Find the GET operation
	var getOp, deleteOp *Operation
	for _, op := range result.Operations {
		if op.Method == "GET" {
			getOp = op
		}
		if op.Method == "DELETE" {
			deleteOp = op
		}
	}

	if getOp == nil {
		t.Fatal("missing GET operation")
	}
	if len(getOp.Security) != 1 || getOp.Security[0] != "sp:tags:read" {
		t.Errorf("GET security = %v, want [sp:tags:read]", getOp.Security)
	}

	if deleteOp == nil {
		t.Fatal("missing DELETE operation")
	}
	if len(deleteOp.Security) != 2 {
		t.Errorf("DELETE security = %v, want [sp:tags:delete, sp:tags:manage]", deleteOp.Security)
	}
}

func TestParsePreAuthorizeHasAnyRole(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`("hasAnyRole('ADMIN', 'USER')")`, []string{"ROLE_ADMIN", "ROLE_USER"}},
		{`("hasAuthority('sp:api:read')")`, []string{"sp:api:read"}},
		{`("hasRole('ADMIN')")`, []string{"ROLE_ADMIN"}},
	}
	for _, tt := range tests {
		got := parsePreAuthorize(tt.input, nil)
		if len(got) != len(tt.expected) {
			t.Errorf("parsePreAuthorize(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("parsePreAuthorize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestParseRequireRight(t *testing.T) {
	tests := []struct {
		input     string
		constants map[string]string
		expected  []string
	}{
		{`("sp:tags:read")`, nil, []string{"sp:tags:read"}},
		{`({"sp:a", "sp:b"})`, nil, []string{"sp:a", "sp:b"}},
		{`(Right.READ)`, map[string]string{"READ": "sp:tags:read"}, []string{"sp:tags:read"}},
	}
	for _, tt := range tests {
		got := parseRequireRight(tt.input, tt.constants)
		if len(got) != len(tt.expected) {
			t.Errorf("parseRequireRight(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("parseRequireRight(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestClassLevelSecurityPropagation(t *testing.T) {
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/reports")
@PreAuthorize("hasAuthority('sp:reports:read')")
public class ReportsController {
    @GetMapping
    public List<ReportDTO> list() { return null; }

    @GetMapping("/{id}")
    public ReportDTO get(@PathVariable("id") String id) { return null; }
}`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "ReportsController.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	for _, op := range result.Operations {
		if len(op.Security) == 0 || op.Security[0] != "sp:reports:read" {
			t.Errorf("%s %s: expected class-level security [sp:reports:read], got %v", op.Method, op.Path, op.Security)
		}
	}
}

func TestErrorResponseSchemas(t *testing.T) {
	result := &Result{
		Operations: []*Operation{
			{
				Path:         "/test",
				Method:       "GET",
				OperationID:  "test",
				ResponseType: "String",
			},
		},
		Schemas: make(map[string]interface{}),
		Types:   make(map[string]*index.TypeDecl),
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	testPath := paths["/test"].(map[string]interface{})
	getOp := testPath["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})

	// Verify error responses have schema content
	for _, code := range []string{"400", "401", "403", "500"} {
		resp := responses[code].(map[string]interface{})
		content, ok := resp["content"].(map[string]interface{})
		if !ok {
			t.Errorf("%s response missing content", code)
			continue
		}
		jsonContent, ok := content["application/json"].(map[string]interface{})
		if !ok {
			t.Errorf("%s response missing application/json content", code)
			continue
		}
		schema, ok := jsonContent["schema"].(map[string]interface{})
		if !ok {
			t.Errorf("%s response missing schema", code)
			continue
		}
		ref, ok := schema["$ref"].(string)
		if !ok || ref != "#/components/schemas/ErrorResponse" {
			t.Errorf("%s response schema $ref = %v, want #/components/schemas/ErrorResponse", code, ref)
		}
	}

	// Verify ErrorResponse schema exists in components
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	if _, ok := schemas["ErrorResponse"]; !ok {
		t.Error("missing ErrorResponse schema in components")
	}
}

func TestDeprecatedSinceExtraction(t *testing.T) {
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/old")
public class OldController {
    @GetMapping
    @Deprecated(since = "2024-01")
    public String getOld() { return null; }
}`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "OldController.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}

	op := result.Operations[0]
	if !op.Deprecated {
		t.Error("operation should be deprecated")
	}
	if op.DeprecatedSince != "2024-01" {
		t.Errorf("DeprecatedSince = %q, want %q", op.DeprecatedSince, "2024-01")
	}

	// Verify spec has x-deprecated-since
	spec := GenerateSpec(result, SpecConfig{Title: "Old", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/old"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	if getOp["deprecated"] != true {
		t.Error("spec operation should be deprecated")
	}
	if getOp["x-deprecated-since"] != "2024-01" {
		t.Errorf("x-deprecated-since = %v, want 2024-01", getOp["x-deprecated-since"])
	}
}

func TestSchemaAnnotationAttributes(t *testing.T) {
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/widgets")
public class WidgetController {
    @GetMapping("/{id}")
    public WidgetDTO get(@PathVariable("id") String id) { return null; }
}

public class WidgetDTO {
    @Schema(example = "my-widget", deprecated = true)
    private String name;
}`

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "WidgetController.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Widgets", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	dto := schemas["WidgetDTO"].(map[string]interface{})
	props := dto["properties"].(map[string]interface{})

	nameProp := props["name"].(map[string]interface{})
	if nameProp["example"] != "my-widget" {
		t.Errorf("name.example = %v, want my-widget", nameProp["example"])
	}
	if nameProp["deprecated"] != true {
		t.Error("name.deprecated should be true")
	}
}

func TestExtractAnnotationNamedParam(t *testing.T) {
	tests := []struct {
		args, param, expected string
	}{
		{"(min = 5, max = 100)", "min", "5"},
		{"(min = 5, max = 100)", "max", "100"},
		{"(regexp = \"^[a-z]+$\")", "regexp", "\"^[a-z]+$\""},
		{"(since = \"2024-01\")", "since", "\"2024-01\""},
		{"(value = 42)", "value", "42"},
		{"(example = \"test\", deprecated = true)", "deprecated", "true"},
	}
	for _, tt := range tests {
		got := extractAnnotationNamedParam(tt.args, tt.param)
		if got != tt.expected {
			t.Errorf("extractAnnotationNamedParam(%q, %q) = %q, want %q", tt.args, tt.param, got, tt.expected)
		}
	}
}

// ===== v3 Overhaul Tests =====

func TestParameterConstraintsInSpec(t *testing.T) {
	// Improvement #1: Parameter validation constraints are emitted in spec
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    public List<String> list(
        @QueryParam("name") @Pattern(regexp = "^[a-z]+$") String name,
        @QueryParam("page") @Min(0) @Max(100) int page,
        @QueryParam("size") @Size(min = 1, max = 50) String size,
        @HeaderParam("Accept-Language") String locale) {
        return null;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	op := pathItem["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})

	for _, p := range params {
		pm := p.(map[string]interface{})
		schema := pm["schema"].(map[string]interface{})
		switch pm["name"] {
		case "name":
			if schema["pattern"] != "^[a-z]+$" {
				t.Errorf("name.pattern = %v, want ^[a-z]+$", schema["pattern"])
			}
		case "page":
			if schema["minimum"] != 0 {
				t.Errorf("page.minimum = %v, want 0", schema["minimum"])
			}
			if schema["maximum"] != 100 {
				t.Errorf("page.maximum = %v, want 100", schema["maximum"])
			}
		case "size":
			if schema["minLength"] != 1 {
				t.Errorf("size.minLength = %v, want 1", schema["minLength"])
			}
			if schema["maxLength"] != 50 {
				t.Errorf("size.maxLength = %v, want 50", schema["maxLength"])
			}
		case "Accept-Language":
			// Improvement #6: Well-known header enrichment
			if _, ok := pm["description"]; !ok {
				t.Error("Accept-Language should have auto-populated description")
			}
		}
	}
}

func TestDefaultValueTyping(t *testing.T) {
	// Improvement #16: Default values are typed according to schema type
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping
    public List<String> list(
        @RequestParam(value = "offset", defaultValue = "0") int offset,
        @RequestParam(value = "limit", defaultValue = "50") int limit,
        @RequestParam(value = "active", defaultValue = "true") boolean active,
        @RequestParam(value = "name", defaultValue = "test") String name) {
        return null;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemController.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	op := pathItem["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})

	for _, p := range params {
		pm := p.(map[string]interface{})
		schema := pm["schema"].(map[string]interface{})
		switch pm["name"] {
		case "offset":
			if schema["default"] != 0 {
				t.Errorf("offset default should be int 0, got %v (%T)", schema["default"], schema["default"])
			}
		case "limit":
			if schema["default"] != 50 {
				t.Errorf("limit default should be int 50, got %v (%T)", schema["default"], schema["default"])
			}
		case "active":
			if schema["default"] != true {
				t.Errorf("active default should be bool true, got %v (%T)", schema["default"], schema["default"])
			}
		case "name":
			if schema["default"] != "test" {
				t.Errorf("name default should be string 'test', got %v", schema["default"])
			}
		}
	}
}

func TestEnumParameterValues(t *testing.T) {
	// Improvement #20: Enum parameter values from type index
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    public List<String> list(@QueryParam("level") DetailLevel level) {
        return null;
    }
}

public enum DetailLevel {
    FULL,
    SUMMARY,
    MINIMAL
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	op := pathItem["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})

	found := false
	for _, p := range params {
		pm := p.(map[string]interface{})
		if pm["name"] == "level" {
			found = true
			schema := pm["schema"].(map[string]interface{})
			if schema["type"] != "string" {
				t.Errorf("enum param type = %v, want string", schema["type"])
			}
			enums, ok := schema["enum"].([]interface{})
			if !ok || len(enums) != 3 {
				t.Errorf("expected 3 enum values, got %v", schema["enum"])
			}
		}
	}
	if !found {
		t.Error("level parameter not found")
	}
}

func TestResourceEndpoint404(t *testing.T) {
	// Improvement #10: 404 for resource endpoints with path params
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    @Path("/{id}")
    public String get(@PathParam("id") String id) { return null; }

    @GET
    public List<String> list() { return null; }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})

	// GET /{id} should have 404
	idPath := paths["/api/v1/items/{id}"].(map[string]interface{})
	getOp := idPath["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})
	if _, ok := responses["404"]; !ok {
		t.Error("GET /{id} should have 404 response")
	}

	// GET /items (no path params) should NOT have 404
	listPath := paths["/api/v1/items"].(map[string]interface{})
	listOp := listPath["get"].(map[string]interface{})
	listResponses := listOp["responses"].(map[string]interface{})
	if _, ok := listResponses["404"]; ok {
		t.Error("GET /items (no path params) should not have 404 response")
	}
}

func TestDeleteReturns204(t *testing.T) {
	// Improvement #17: DELETE with void return defaults to 204
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @DELETE
    @Path("/{id}")
    public void delete(@PathParam("id") String id) {}

    @DELETE
    @Path("/{id}/force")
    public Response forceDelete(@PathParam("id") String id) {
        return Response.noContent().build();
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	for _, op := range result.Operations {
		if op.Method == "DELETE" {
			if op.ResponseStatus != 204 {
				t.Errorf("DELETE %s: ResponseStatus = %d, want 204", op.Path, op.ResponseStatus)
			}
		}
	}
}

func TestConflictResponseForPostPut(t *testing.T) {
	// Improvement #11: 409 Conflict for POST/PUT endpoints
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @PostMapping
    public String create(@RequestBody String body) { return null; }

    @PutMapping("/{id}")
    public String update(@PathVariable("id") String id, @RequestBody String body) { return null; }

    @GetMapping("/{id}")
    public String get(@PathVariable("id") String id) { return null; }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemController.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})

	// POST should have 409
	listPath := paths["/api/v1/items"].(map[string]interface{})
	postOp := listPath["post"].(map[string]interface{})
	postResponses := postOp["responses"].(map[string]interface{})
	if _, ok := postResponses["409"]; !ok {
		t.Error("POST should have 409 response")
	}

	// GET should NOT have 409
	idPath := paths["/api/v1/items/{id}"].(map[string]interface{})
	getOp := idPath["get"].(map[string]interface{})
	getResponses := getOp["responses"].(map[string]interface{})
	if _, ok := getResponses["409"]; ok {
		t.Error("GET should not have 409 response")
	}
}

func TestResponseHeaderExtraction(t *testing.T) {
	// Improvement #2: Response headers from method body
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    public Response list() {
        List<String> items = service.list();
        return Response.ok(items)
            .header("X-Total-Count", items.size())
            .header("X-Aggregate-Count", 42)
            .build();
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}
	op := result.Operations[0]
	if op.ResponseHeaders == nil {
		t.Fatal("ResponseHeaders should not be nil")
	}
	if _, ok := op.ResponseHeaders["X-Total-Count"]; !ok {
		t.Error("missing X-Total-Count header")
	}
	if _, ok := op.ResponseHeaders["X-Aggregate-Count"]; !ok {
		t.Error("missing X-Aggregate-Count header")
	}

	// Verify they appear in the spec
	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})
	successResp := responses["200"].(map[string]interface{})
	headers, ok := successResp["headers"].(map[string]interface{})
	if !ok {
		t.Fatal("success response should have headers")
	}
	if _, ok := headers["X-Total-Count"]; !ok {
		t.Error("missing X-Total-Count in spec response headers")
	}
}

func TestExceptionToStatusMapping(t *testing.T) {
	// Improvement #3: Exception-to-status mapping from throw statements
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    @Path("/{id}")
    public String get(@PathParam("id") String id) {
        if (id == null) {
            throw new NotFoundException("not found");
        }
        if (duplicate) {
            throw new ConflictException("conflict");
        }
        return null;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}
	op := result.Operations[0]
	if op.ErrorResponses == nil {
		t.Fatal("ErrorResponses should not be nil")
	}
	if _, ok := op.ErrorResponses[404]; !ok {
		t.Error("missing 404 from NotFoundException")
	}
	if _, ok := op.ErrorResponses[409]; !ok {
		t.Error("missing 409 from ConflictException")
	}
}

func TestFilterSorterDescriptions(t *testing.T) {
	// Improvement #13: Filter/sorter parameter enrichment
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    public List<String> list(
        @QueryParam("filters") String filters,
        @QueryParam("sorters") String sorters) {
        return null;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	op := pathItem["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})

	for _, p := range params {
		pm := p.(map[string]interface{})
		switch pm["name"] {
		case "filters":
			if _, ok := pm["description"]; !ok {
				t.Error("filters param should have auto-populated description")
			}
			if _, ok := pm["example"]; !ok {
				t.Error("filters param should have auto-populated example")
			}
		case "sorters":
			if _, ok := pm["description"]; !ok {
				t.Error("sorters param should have auto-populated description")
			}
		}
	}
}

func TestContentDispositionHeader(t *testing.T) {
	// Improvement #8: Content-Disposition header for download endpoints
	src := `package com.example;
@Path("/api/v1/reports")
public class ReportResource {
    @GET
    @Path("/export")
    @Produces("text/csv")
    public Response export() {
        return Response.ok().build();
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ReportResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Reports", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/reports/export"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})
	successResp := responses["200"].(map[string]interface{})
	headers, ok := successResp["headers"].(map[string]interface{})
	if !ok {
		t.Fatal("CSV download response should have headers")
	}
	if _, ok := headers["Content-Disposition"]; !ok {
		t.Error("missing Content-Disposition header for CSV download")
	}
}

func TestRequestBodyFromIndex(t *testing.T) {
	// Improvement #7: Request body schema uses pre-computed index schema
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @PostMapping
    public String create(@RequestBody ItemDTO body) { return null; }
}

public class ItemDTO {
    @NotNull
    @Size(min = 1, max = 100)
    private String name;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemController.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	postOp := pathItem["post"].(map[string]interface{})
	reqBody := postOp["requestBody"].(map[string]interface{})
	content := reqBody["content"].(map[string]interface{})
	jsonContent := content["application/json"].(map[string]interface{})
	schema := jsonContent["schema"].(map[string]interface{})
	// The schema should reference the indexed type (which has validation constraints)
	if schema == nil {
		t.Error("request body schema should not be nil")
	}
}

func TestMeteredAnnotation(t *testing.T) {
	// Improvement #14: @Metered → x-rate-limited
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    @Metered
    public List<String> list() { return null; }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}
	if !result.Operations[0].RateLimited {
		t.Error("operation with @Metered should have RateLimited=true")
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	if getOp["x-rate-limited"] != true {
		t.Error("x-rate-limited extension should be true")
	}
}

func TestApiResponseSchemaExtraction(t *testing.T) {
	// Improvement #18: @ApiResponse with content/schema extraction
	input := `(responseCode = "200", description = "Success", content = @Content(schema = @Schema(implementation = ItemDTO.class)))`
	ar := parseApiResponseAnnotation(input)
	if ar == nil {
		t.Fatal("parseApiResponseAnnotation returned nil")
	}
	if ar.SchemaType != "ItemDTO" {
		t.Errorf("SchemaType = %q, want ItemDTO", ar.SchemaType)
	}
}

func TestDiscriminatorExtraction(t *testing.T) {
	// Improvement #4: @JsonTypeInfo/@JsonSubTypes discriminator support
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/modules")
public class ModuleController {
    @GetMapping("/{id}")
    public IModule get(@PathVariable("id") String id) { return null; }
}

@JsonTypeInfo(use = JsonTypeInfo.Id.NAME, property = "type")
@JsonSubTypes({
    @Type(value = PTAModule.class, name = "PTA"),
    @Type(value = BlockModule.class, name = "Block")
})
public interface IModule {}

public class PTAModule implements IModule {
    private String name;
}

public class BlockModule implements IModule {
    private String reason;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ModuleController.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	// Check that IModule has discriminator info
	imoduleDecl, ok := result.Types["com.example.IModule"]
	if !ok {
		t.Fatal("IModule type not found")
	}
	if imoduleDecl.DiscriminatorProperty != "type" {
		t.Errorf("DiscriminatorProperty = %q, want 'type'", imoduleDecl.DiscriminatorProperty)
	}
	if len(imoduleDecl.DiscriminatorMapping) != 2 {
		t.Errorf("DiscriminatorMapping length = %d, want 2", len(imoduleDecl.DiscriminatorMapping))
	}

	// Check generated schema has discriminator
	spec := GenerateSpec(result, SpecConfig{Title: "Modules", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	imodule, ok := schemas["IModule"].(map[string]interface{})
	if !ok {
		t.Fatal("IModule schema not found")
	}
	if imodule["discriminator"] == nil {
		t.Error("IModule schema should have discriminator")
	}
	if imodule["oneOf"] == nil {
		t.Error("IModule schema should have oneOf")
	}
}

func TestCustomPaginationExpansion(t *testing.T) {
	// Improvement #12: Custom pagination types expanded
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    public List<String> list(ChroniclePagingOptions paging) {
        return null;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}
	params := result.Operations[0].Parameters
	if len(params) < 2 {
		t.Fatalf("expected >= 2 expanded params, got %d", len(params))
	}
	names := make(map[string]bool)
	for _, p := range params {
		names[p.Name] = true
	}
	if !names["offset"] || !names["limit"] {
		t.Errorf("expected offset and limit params, got %v", names)
	}
}

func TestThrownExceptionExtraction(t *testing.T) {
	// Unit test for extractThrownExceptions
	body := `{
        if (x == null) throw new NotFoundException("msg");
        if (y) throw new ConflictException("already exists");
        throw new ForbiddenException("nope");
    }`
	exceptions := extractThrownExceptions(body)
	if len(exceptions) != 3 {
		t.Fatalf("expected 3 exceptions, got %d: %v", len(exceptions), exceptions)
	}
}

func TestResponseHeadersExtracted(t *testing.T) {
	// Unit test for extractResponseHeaders
	body := `return Response.ok(list).header("X-Total-Count", count).header("X-Aggregate-Count", 42).build();`
	headers := extractResponseHeaders(body)
	if headers == nil {
		t.Fatal("headers should not be nil")
	}
	if _, ok := headers["X-Total-Count"]; !ok {
		t.Error("missing X-Total-Count")
	}
	if _, ok := headers["X-Aggregate-Count"]; !ok {
		t.Error("missing X-Aggregate-Count")
	}
}

// =============================================================================
// v4 Backfill Tests
// =============================================================================

func TestResponsePreferIndexedSchema(t *testing.T) {
	// v4 #1: Response body uses pre-computed schema from index
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    @Size(min = 1, max = 100)
    private String name;
    @Min(0)
    private int count;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items/{id}"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})
	resp200 := responses["200"].(map[string]interface{})
	content := resp200["content"].(map[string]interface{})
	jsonContent := content["application/json"].(map[string]interface{})
	schema := jsonContent["schema"].(map[string]interface{})

	// Response should use the indexed schema (which has validation annotations)
	// Either as $ref or as inline with properties
	if ref, ok := schema["$ref"].(string); ok {
		if ref != "#/components/schemas/ItemDTO" {
			t.Errorf("response $ref = %v, want #/components/schemas/ItemDTO", ref)
		}
	} else if props, ok := schema["properties"].(map[string]interface{}); ok {
		// Inline schema should have validation from index
		if nameProp, ok := props["name"].(map[string]interface{}); ok {
			if nameProp["minLength"] != 1 {
				t.Errorf("name.minLength = %v, want 1", nameProp["minLength"])
			}
		}
	} else {
		t.Errorf("response schema should be either $ref or inline with properties, got %v", schema)
	}
}

func TestFormParamRequestBody(t *testing.T) {
	// v4 #2: @FormParam produces multipart/form-data request body
	src := `package com.example;
@Path("/api/v1/upload")
public class UploadResource {
    @POST
    public void upload(
        @FormParam("name") String name,
        @FormParam("description") String description) {}
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "UploadResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}

	op := result.Operations[0]
	if len(op.FormParams) != 2 {
		t.Fatalf("expected 2 form params, got %d", len(op.FormParams))
	}
	if op.FormParams[0].Name != "name" {
		t.Errorf("form param 0 name = %q, want 'name'", op.FormParams[0].Name)
	}
}

func TestFormParamFileUpload(t *testing.T) {
	// v4 #2: File types (InputStream) → binary format
	src := `package com.example;
@Path("/api/v1/files")
@Consumes("multipart/form-data")
public class FileResource {
    @POST
    public void uploadFile(
        @FormParam("file") InputStream file,
        @FormParam("name") String name) {}
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "FileResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Files", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/files"].(map[string]interface{})
	postOp := pathItem["post"].(map[string]interface{})
	reqBody := postOp["requestBody"].(map[string]interface{})
	content := reqBody["content"].(map[string]interface{})

	// Should use multipart/form-data
	if _, ok := content["multipart/form-data"]; !ok {
		t.Errorf("expected multipart/form-data content type, got keys: %v", keysOfMap(content))
	}
}

func TestSetUniqueItemsInResolver(t *testing.T) {
	// v4 #3: Set<T>/HashSet<T>/TreeSet<T> → uniqueItems: true
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    private Set<String> tags;
    private HashSet<String> uniqueNames;
    private List<String> items;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	// Check schemas from index resolver
	for _, decl := range result.Types {
		if decl.Name == "ItemDTO" {
			schema := result.Schemas["ItemDTO"].(map[string]interface{})
			props := schema["properties"].(map[string]interface{})

			tagsProp := props["tags"].(map[string]interface{})
			if tagsProp["uniqueItems"] != true {
				t.Errorf("Set<String>.uniqueItems = %v, want true", tagsProp["uniqueItems"])
			}

			uniqueNamesProp := props["uniqueNames"].(map[string]interface{})
			if uniqueNamesProp["uniqueItems"] != true {
				t.Errorf("HashSet<String>.uniqueItems = %v, want true", uniqueNamesProp["uniqueItems"])
			}

			itemsProp := props["items"].(map[string]interface{})
			if _, ok := itemsProp["uniqueItems"]; ok {
				t.Error("List<String> should NOT have uniqueItems")
			}
			return
		}
	}
	t.Error("ItemDTO not found")
}

func TestSizeOnCollectionMinMaxItems(t *testing.T) {
	// v4 #4: @Size on List<T> → minItems/maxItems (not minLength)
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    @Size(min = 1, max = 10)
    private List<String> tags;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	tagsProp := props["tags"].(map[string]interface{})

	if tagsProp["minItems"] != 1 {
		t.Errorf("tags.minItems = %v, want 1", tagsProp["minItems"])
	}
	if tagsProp["maxItems"] != 10 {
		t.Errorf("tags.maxItems = %v, want 10", tagsProp["maxItems"])
	}
	if _, ok := tagsProp["minLength"]; ok {
		t.Error("tags should NOT have minLength (it's an array)")
	}
}

func TestResponseHeaderIntegerType(t *testing.T) {
	// v4 #5: X-Total-Count → integer schema, not string
	op := &Operation{
		Path:         "/test",
		Method:       "GET",
		OperationID:  "test",
		ResponseType: "String",
		ResponseHeaders: map[string]string{
			"X-Total-Count": "Total number of items",
		},
	}
	result := &Result{
		Operations: []*Operation{op},
		Schemas:    make(map[string]interface{}),
		Types:      make(map[string]*index.TypeDecl),
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0"})
	paths := spec["paths"].(map[string]any)
	pathItem := paths["/test"].(map[string]any)
	getOp := pathItem["get"].(map[string]any)
	responses := getOp["responses"].(map[string]any)
	resp200 := responses["200"].(map[string]any)
	headers, ok := resp200["headers"].(map[string]any)
	if !ok {
		t.Fatal("missing headers in 200 response")
	}
	header := headers["X-Total-Count"].(map[string]any)
	schema := header["schema"].(map[string]any)
	if schema["type"] != "integer" {
		t.Errorf("X-Total-Count schema type = %v, want integer", schema["type"])
	}
}

func TestJsonPatchAutoSchema(t *testing.T) {
	// v4 #6: json-patch+json content type → JsonPatch $ref
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @PATCH
    @Path("/{id}")
    @Consumes("application/json-patch+json")
    public ItemDTO patch(@PathParam("id") String id, Object body) { return null; }
}
public class ItemDTO {
    private String name;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items/{id}"].(map[string]interface{})
	patchOp := pathItem["patch"].(map[string]interface{})
	reqBody := patchOp["requestBody"].(map[string]interface{})
	content := reqBody["content"].(map[string]interface{})
	jsonPatch := content["application/json-patch+json"].(map[string]interface{})
	schema := jsonPatch["schema"].(map[string]interface{})

	if schema["$ref"] != "#/components/schemas/JsonPatch" {
		t.Errorf("json-patch+json schema $ref = %v, want #/components/schemas/JsonPatch", schema["$ref"])
	}
}

func TestOperationIdDedup(t *testing.T) {
	// v4 #7: Duplicate method names get path-suffixed operationIds
	src := `package com.example;
@Path("/api/v1")
public class TestResource {
    @GET
    @Path("/items")
    public String list() { return null; }

    @GET
    @Path("/users")
    public String list() { return null; }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "TestResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})

	ids := make(map[string]bool)
	for _, pi := range paths {
		for _, opObj := range pi.(map[string]interface{}) {
			op := opObj.(map[string]interface{})
			opId := op["operationId"].(string)
			if ids[opId] {
				t.Errorf("duplicate operationId: %s", opId)
			}
			ids[opId] = true
		}
	}
}

func TestTopLevelTags(t *testing.T) {
	// v4 #9: Tags array emitted at spec root
	src := `package com.example;
import io.swagger.v3.oas.annotations.tags.Tag;
@Path("/api/v1/items")
@Tag(name = "Items")
public class ItemResource {
    @GET
    public String list() { return null; }
}
@Path("/api/v1/users")
@Tag(name = "Users")
public class UserResource {
    @GET
    public String list() { return null; }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Resources.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	tags, ok := spec["tags"].([]interface{})
	if !ok {
		t.Fatal("spec should have top-level tags array")
	}
	if len(tags) < 2 {
		t.Errorf("expected at least 2 tags, got %d", len(tags))
	}
}

// =============================================================================
// v3 Coverage Gap Tests
// =============================================================================

func TestBeanParamExpansion(t *testing.T) {
	// v3 #9: @BeanParam fields expanded to individual params
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    public List<String> list(@BeanParam PagingParams paging) { return null; }
}
public class PagingParams {
    @QueryParam("offset")
    private int offset;
    @QueryParam("limit")
    private int limit;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}

	paramNames := make(map[string]bool)
	for _, p := range result.Operations[0].Parameters {
		paramNames[p.Name] = true
	}
	if !paramNames["offset"] || !paramNames["limit"] {
		t.Errorf("@BeanParam should expand to offset+limit, got %v", paramNames)
	}
}

func TestNullableResponseSchema(t *testing.T) {
	// v3 #15: @Nullable return type → nullable response
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    @Path("/{id}")
    @Nullable
    public String get(@PathParam("id") String id) { return null; }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}

	if !result.Operations[0].NullableResponse {
		t.Error("@Nullable return type should set NullableResponse = true")
	}
}

func TestLombokValueReadOnly(t *testing.T) {
	// v3 #5: @Value → all fields readOnly
	src := `package com.example;
import lombok.Value;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

@Value
public class ItemDTO {
    String name;
    int count;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	for field, propObj := range props {
		prop := propObj.(map[string]interface{})
		if prop["readOnly"] != true {
			t.Errorf("@Value class: field %q should be readOnly", field)
		}
	}
}

func TestLombokNonNullRequired(t *testing.T) {
	// v3 #5: @NonNull → required
	src := `package com.example;
import lombok.NonNull;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    @NonNull
    private String name;
    private String optional;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("ItemDTO should have required array")
	}
	found := false
	for _, r := range required {
		if r == "name" {
			found = true
		}
	}
	if !found {
		t.Errorf("@NonNull field 'name' should be in required, got %v", required)
	}
}

func TestDiscriminatorOneOf(t *testing.T) {
	// v3 #4: @JsonTypeInfo/@JsonSubTypes → discriminator + oneOf
	src := `package com.example;
import com.fasterxml.jackson.annotation.*;

@RestController
@RequestMapping("/api/v1/shapes")
public class ShapeController {
    @GetMapping("/{id}")
    public IShape get(@PathVariable("id") String id) { return null; }
}

@JsonTypeInfo(use = JsonTypeInfo.Id.NAME, property = "type")
@JsonSubTypes({
    @Type(value = Circle.class, name = "circle"),
    @Type(value = Square.class, name = "square")
})
public interface IShape {}

public class Circle implements IShape {
    private double radius;
}

public class Square implements IShape {
    private double side;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ShapeController.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema, ok := result.Schemas["IShape"].(map[string]interface{})
	if !ok {
		t.Fatal("IShape schema not found")
	}

	if _, ok := schema["discriminator"]; !ok {
		t.Error("IShape should have discriminator")
	}
	if _, ok := schema["oneOf"]; !ok {
		t.Error("IShape should have oneOf")
	}
}

func TestMultipleErrorResponses(t *testing.T) {
	// v3 #3: Multiple thrown exceptions → multiple error status codes
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    @Path("/{id}")
    public String get(@PathParam("id") String id) {
        if (id == null) throw new NotFoundException("not found");
        if (id.isEmpty()) throw new BadRequestException("bad");
        throw new ConflictException("conflict");
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}

	errors := result.Operations[0].ErrorResponses
	if errors == nil {
		t.Fatal("should have error responses from exceptions")
	}

	for _, code := range []int{404, 400, 409} {
		if _, ok := errors[code]; !ok {
			t.Errorf("missing error response %d", code)
		}
	}
}

func TestAcceptLanguageHeader(t *testing.T) {
	// v3 #6: Accept-Language wellKnownHeaders enrichment
	src := `package com.example;
@Path("/api/v1/items")
public class ItemResource {
    @GET
    public String list(@HeaderParam("Accept-Language") String locale) { return null; }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ItemResource.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	params := getOp["parameters"].([]interface{})

	for _, p := range params {
		pm := p.(map[string]interface{})
		if pm["name"] == "Accept-Language" {
			if _, ok := pm["description"]; !ok {
				t.Error("Accept-Language should have auto-populated description")
			}
			return
		}
	}
	t.Error("Accept-Language parameter not found")
}

// =============================================================================
// v5 Feature Tests
// =============================================================================

func TestNullableFieldInSchema(t *testing.T) {
	// v5 #1: @Nullable on field → nullable: true
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    private String id;
    @Nullable
    private String description;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})

	descProp := props["description"].(map[string]interface{})
	if descProp["nullable"] != true {
		t.Errorf("@Nullable field should have nullable: true, got %v", descProp)
	}

	idProp := props["id"].(map[string]interface{})
	if _, ok := idProp["nullable"]; ok {
		t.Error("non-@Nullable field should NOT have nullable")
	}
}

func TestNonnullFieldRequired(t *testing.T) {
	// v5 #1: @Nonnull on field → required list
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    @Nonnull
    private String id;
    private String optional;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("ItemDTO should have required array")
	}
	found := false
	for _, r := range required {
		if r == "id" {
			found = true
		}
	}
	if !found {
		t.Errorf("@Nonnull field 'id' should be in required, got %v", required)
	}
}

func TestNestedEnumExtraction(t *testing.T) {
	// v5 #2: Inner enum types indexed and resolvable
	src := `package com.example;
public class OuterDTO {
    private String id;
    private Status status;

    public enum Status {
        ACTIVE, INACTIVE, PENDING
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "OuterDTO.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	// The nested enum should be indexed
	found := false
	for _, decl := range result.Types {
		if decl.Name == "Status" && decl.Kind == "enum" {
			found = true
			if len(decl.EnumValues) != 3 {
				t.Errorf("Status should have 3 values, got %d", len(decl.EnumValues))
			}
		}
	}
	if !found {
		t.Error("nested enum 'Status' should be indexed")
	}
}

func TestNestedClassExtraction(t *testing.T) {
	// v5 #2: Inner class types indexed and resolvable
	src := `package com.example;
public class OuterDTO {
    private String id;
    private InnerConfig config;

    public static class InnerConfig {
        private String key;
        private String value;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "OuterDTO.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	// The nested class should be indexed
	found := false
	for _, decl := range result.Types {
		if decl.Name == "InnerConfig" && decl.Kind == "class" {
			found = true
			if len(decl.Fields) != 2 {
				t.Errorf("InnerConfig should have 2 fields, got %d", len(decl.Fields))
			}
		}
	}
	if !found {
		t.Error("nested class 'InnerConfig' should be indexed")
	}
}

func TestSpringDefaultValueExtraction(t *testing.T) {
	// v5 #3 (already implemented): @RequestParam(defaultValue="0") → default: 0
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping
    public List<String> list(
        @RequestParam(value = "offset", required = false, defaultValue = "0") int offset,
        @RequestParam(value = "limit", required = false, defaultValue = "50") int limit) {
        return null;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/api/v1/items"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	params := getOp["parameters"].([]interface{})

	for _, p := range params {
		pm := p.(map[string]interface{})
		schema := pm["schema"].(map[string]interface{})
		switch pm["name"] {
		case "offset":
			if schema["default"] != 0 {
				t.Errorf("offset default = %v (type %T), want 0", schema["default"], schema["default"])
			}
		case "limit":
			if schema["default"] != 50 {
				t.Errorf("limit default = %v (type %T), want 50", schema["default"], schema["default"])
			}
		}
	}
}

func TestSingularFieldArraySchema(t *testing.T) {
	// v5 #4: @Singular Set<String> → array with uniqueItems (handled by v4 Set<T>)
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    @Singular
    private Set<String> tags;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	tagsProp := props["tags"].(map[string]interface{})
	if tagsProp["type"] != "array" {
		t.Errorf("Set<String> should be array, got %v", tagsProp["type"])
	}
	if tagsProp["uniqueItems"] != true {
		t.Error("Set<String> should have uniqueItems: true")
	}
}

func TestFieldSchemaAnnotation(t *testing.T) {
	// v5 #5: @Schema(description, example, deprecated, minLength, accessMode) on fields
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    @Schema(description = "Unique ID", example = "abc-123", minLength = 3, maxLength = 50, accessMode = AccessMode.READ_ONLY)
    private String id;

    @Schema(deprecated = true)
    private String legacyField;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})

	idProp := props["id"].(map[string]interface{})
	if idProp["description"] != "Unique ID" {
		t.Errorf("id.description = %v, want 'Unique ID'", idProp["description"])
	}
	if idProp["example"] != "abc-123" {
		t.Errorf("id.example = %v, want 'abc-123'", idProp["example"])
	}
	if idProp["minLength"] != 3 {
		t.Errorf("id.minLength = %v, want 3", idProp["minLength"])
	}
	if idProp["maxLength"] != 50 {
		t.Errorf("id.maxLength = %v, want 50", idProp["maxLength"])
	}
	if idProp["readOnly"] != true {
		t.Error("id should be readOnly from accessMode = READ_ONLY")
	}

	legacyProp := props["legacyField"].(map[string]interface{})
	if legacyProp["deprecated"] != true {
		t.Error("legacyField should be deprecated")
	}
}

func TestJsonFormatDate(t *testing.T) {
	// v5 #6: @JsonFormat(pattern) on date fields
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    @JsonFormat(shape = JsonFormat.Shape.STRING, pattern = "yyyy-MM-dd")
    private LocalDate date;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	dateProp := props["date"].(map[string]interface{})
	if dateProp["type"] != "string" {
		t.Errorf("date.type = %v, want string", dateProp["type"])
	}
	if dateProp["x-format-pattern"] != "yyyy-MM-dd" {
		t.Errorf("date.x-format-pattern = %v, want yyyy-MM-dd", dateProp["x-format-pattern"])
	}
}

func TestRequestParamRequired(t *testing.T) {
	// v5 #7 (already implemented): @RequestParam(required=false) → required: false
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping
    public String list(
        @RequestParam(value = "q", required = false) String q,
        @RequestParam("name") String name) {
        return null;
    }
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(result.Operations))
	}

	for _, p := range result.Operations[0].Parameters {
		switch p.Name {
		case "q":
			if p.Required {
				t.Error("@RequestParam(required=false) should make param not required")
			}
		case "name":
			if !p.Required {
				t.Error("@RequestParam without required=false should default to required=true")
			}
		}
	}
}

func TestResponseEntityUnwrapping(t *testing.T) {
	// v5 #8: ResponseEntity<List<T>> → array of T
	schema := generics.Parse("ResponseEntity<List<UserDTO>>").ToOpenAPISchema(nil)
	if schema["type"] != "array" {
		t.Errorf("ResponseEntity<List<UserDTO>> should produce array, got %v", schema)
	}
	items, ok := schema["items"].(map[string]interface{})
	if !ok {
		t.Fatal("no items in array schema")
	}
	if items["$ref"] != "#/components/schemas/UserDTO" {
		t.Errorf("items.$ref = %v, want #/components/schemas/UserDTO", items["$ref"])
	}
}

func TestEnumFieldDefaultValue(t *testing.T) {
	// v5 #9: Field initializer enum default in schema
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    private Status status = Status.ACTIVE;
    private String name;
}

public enum Status {
    ACTIVE, INACTIVE, PENDING
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	statusProp := props["status"].(map[string]interface{})
	if statusProp["default"] != "ACTIVE" {
		t.Errorf("status.default = %v, want ACTIVE", statusProp["default"])
	}
}

func TestDeprecatedField(t *testing.T) {
	// v5 #10: @Deprecated on field → deprecated: true
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public ItemDTO get(@PathVariable("id") String id) { return null; }
}

public class ItemDTO {
    private String name;
    @Deprecated
    private String legacyField;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["ItemDTO"].(map[string]interface{})
	props := schema["properties"].(map[string]interface{})
	legacyProp := props["legacyField"].(map[string]interface{})
	if legacyProp["deprecated"] != true {
		t.Error("@Deprecated field should have deprecated: true")
	}
}

func TestDeprecatedClass(t *testing.T) {
	// v5 #10: @Deprecated on class → deprecated: true
	src := `package com.example;
@RestController
@RequestMapping("/api/v1/items")
public class ItemController {
    @GetMapping("/{id}")
    public LegacyDTO get(@PathVariable("id") String id) { return null; }
}

@Deprecated
public class LegacyDTO {
    private String name;
}`
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "Controller.java"), []byte(src), 0o644)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema := result.Schemas["LegacyDTO"].(map[string]interface{})
	if schema["deprecated"] != true {
		t.Error("@Deprecated class should have deprecated: true in schema")
	}
}

// keysOfMap returns keys of a map[string]interface{} as a string slice.
func keysOfMap(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Verify the unused import is used
var _ = fmt.Sprintf
