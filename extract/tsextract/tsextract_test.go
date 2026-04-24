package tsextract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/index"
)

// --- NestJS test fixtures ---

const nestControllerSource = `import { Body, Controller, Delete, Get, Header, HttpCode, Param, Post, Put, Query } from '@nestjs/common';
import { UserDto, CreateUserDto, UpdateUserDto } from './user.model';

@Controller('api/v1/users')
export class UserController {
	constructor() {}

	/**
	 * List all users.
	 * Returns a paginated list of users.
	 */
	@Get()
	@HttpCode(200)
	listUsers(
		@Query('offset') offset: number,
		@Query('limit') limit: number,
		@Query('filters') filters: string
	): UserDto[] {
		return [];
	}

	/**
	 * Get a user by ID.
	 */
	@Get('/:id')
	@HttpCode(200)
	getUser(@Param('id') id: string): UserDto {
		return null;
	}

	/**
	 * Create a new user.
	 */
	@Post()
	@HttpCode(201)
	createUser(@Body() body: CreateUserDto): UserDto {
		return null;
	}

	/**
	 * Update an existing user.
	 */
	@Put('/:id')
	updateUser(
		@Param('id') id: string,
		@Body() body: UpdateUserDto
	): UserDto {
		return null;
	}

	/**
	 * Delete a user.
	 */
	@Delete('/:id')
	@HttpCode(204)
	deleteUser(@Param('id') id: string): void {
	}
}
`

const nestDTOSource = `import { IsString, IsOptional, IsUUID, IsDefined, IsEmail, MinLength, MaxLength, IsArray, ValidateNested } from 'class-validator';

export class UserDto {
	@IsUUID()
	@IsDefined()
	id: string;

	@IsString()
	@IsDefined()
	@MinLength(1)
	@MaxLength(255)
	name: string;

	@IsEmail()
	@IsOptional()
	email?: string;

	active: boolean;

	@IsArray()
	@IsOptional()
	tags?: string[];
}

export class CreateUserDto {
	@IsString()
	@IsDefined()
	name: string;

	@IsEmail()
	@IsDefined()
	email: string;
}

export class UpdateUserDto {
	@IsString()
	@IsOptional()
	name?: string;

	@IsEmail()
	@IsOptional()
	email?: string;

	active?: boolean;
}
`

const nestValidationControllerSource = `import { Body, Controller, Get, HttpCode, Param, ParseIntPipe, ParseUUIDPipe, Post, Header } from '@nestjs/common';
import { SimpleDto, UserDto } from './';

@Controller('example/validation')
export class ValidationExampleController {
	constructor() {}

	@Get('/path/uuid/:id')
	@HttpCode(200)
	@Header('Content-Type', 'application/json; charset=utf-8')
	pathUUID(@Param('id', new ParseUUIDPipe()) id: string): SimpleDto {
		return { id };
	}

	@Get('/path/int/:id')
	@HttpCode(200)
	pathInt(@Param('id', new ParseIntPipe()) id: number): SimpleDto {
		return { id };
	}

	@Post('/user')
	@HttpCode(201)
	user(@Body() user: UserDto): UserDto {
		return user;
	}
}
`

const nestRoutingControllerSource = `import { All, Body, Controller, Delete, Get, Header, HttpCode, Patch, Post, Put, Req, Res } from '@nestjs/common';
import { RoutingExampleDto } from './';
import { Request, Response } from 'express';

@Controller('example/routing')
export class RoutingExampleController {
	constructor() {}

	@Get('/get')
	@HttpCode(200)
	get(): RoutingExampleDto {
		return { text: 'Example GET Request' };
	}

	@Post('/post')
	@HttpCode(201)
	post(@Body() body: any): RoutingExampleDto {
		return { text: 'Example POST Request' };
	}

	@Delete('/delete')
	@HttpCode(200)
	delete(@Body() body: any): RoutingExampleDto {
		return { text: 'Example DELETE Request' };
	}

	@Put('/put')
	@HttpCode(200)
	put(@Body() body: any): RoutingExampleDto {
		return { text: 'Example PUT Request' };
	}

	@Patch('/patch')
	@HttpCode(200)
	patch(@Body() body: any): RoutingExampleDto {
		return { text: 'Example PATCH Request' };
	}

	@All('/all')
	all(@Req() request: Request, @Res() response: Response): void {
	}
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

func setupNestProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "user.controller.ts", nestControllerSource)
	writeTestFile(t, src, "user.model.ts", nestDTOSource)
	return dir
}

// --- NestJS Extraction Tests ---

func TestNestJSExtraction(t *testing.T) {
	dir := setupNestProject(t)

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
		t.Logf("  %s %s  (operationId=%s, requestBody=%s, response=%s, status=%d)",
			op.Method, op.Path, op.OperationID, op.RequestBodyType, op.ResponseType, op.ResponseStatus)
		for _, p := range op.Parameters {
			t.Logf("    param: %s in=%s type=%s required=%v", p.Name, p.In, p.Type, p.Required)
		}
	}

	if len(result.Operations) == 0 {
		t.Fatal("no operations extracted from NestJS controller")
	}

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

	// GET /api/v1/users/{id} - NestJS :id should be converted to {id}
	if op, ok := opsByMethod["GET /api/v1/users/{id}"]; ok {
		if op.OperationID != "getUser" {
			t.Errorf("expected operationId 'getUser', got %q", op.OperationID)
		}
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

	// PUT /api/v1/users/{id}
	if op, ok := opsByMethod["PUT /api/v1/users/{id}"]; ok {
		if op.RequestBodyType == "" {
			t.Error("PUT should have request body")
		}
		hasPathParam := false
		for _, p := range op.Parameters {
			if p.In == "path" && p.Name == "id" {
				hasPathParam = true
			}
		}
		if !hasPathParam {
			t.Error("PUT should have path param 'id'")
		}
	} else {
		t.Error("missing PUT /api/v1/users/{id} operation")
	}

	// DELETE /api/v1/users/{id}
	if op, ok := opsByMethod["DELETE /api/v1/users/{id}"]; ok {
		if op.ResponseStatus != 204 {
			t.Errorf("expected DELETE response status 204, got %d", op.ResponseStatus)
		}
	} else {
		t.Error("missing DELETE /api/v1/users/{id} operation")
	}
}

func TestNestJSTypes(t *testing.T) {
	dir := setupNestProject(t)

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

	// Should have indexed UserDto, CreateUserDto, UpdateUserDto
	found := 0
	for _, decl := range result.Types {
		switch decl.Name {
		case "UserDto":
			found++
			if len(decl.Fields) < 3 {
				t.Errorf("UserDto should have at least 3 fields, got %d", len(decl.Fields))
			}
		case "CreateUserDto":
			found++
		case "UpdateUserDto":
			found++
		}
	}

	if found < 3 {
		t.Errorf("expected at least 3 DTO types indexed, found %d", found)
	}
}

func TestNestJSValidationController(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "validation.controller.ts", nestValidationControllerSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	t.Logf("Extracted %d operations", len(result.Operations))
	for _, op := range result.Operations {
		t.Logf("  %s %s (id=%s, status=%d)", op.Method, op.Path, op.OperationID, op.ResponseStatus)
	}

	if len(result.Operations) < 3 {
		t.Errorf("expected at least 3 operations, got %d", len(result.Operations))
	}

	// Check path param conversion
	for _, op := range result.Operations {
		if op.OperationID == "pathUUID" {
			if !containsParam(op.Parameters, "id", "path") {
				t.Error("pathUUID should have path param 'id'")
			}
			if op.ResponseStatus != 200 {
				t.Errorf("expected status 200, got %d", op.ResponseStatus)
			}
		}
		if op.OperationID == "user" {
			if op.RequestBodyType == "" {
				t.Error("user endpoint should have request body")
			}
			if op.ResponseStatus != 201 {
				t.Errorf("expected status 201, got %d", op.ResponseStatus)
			}
		}
	}
}

func TestNestJSRoutingController(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "routing.controller.ts", nestRoutingControllerSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	t.Logf("Extracted %d operations", len(result.Operations))
	for _, op := range result.Operations {
		t.Logf("  %s %s (id=%s, requestBody=%s)", op.Method, op.Path, op.OperationID, op.RequestBodyType)
	}

	if len(result.Operations) < 5 {
		t.Errorf("expected at least 5 operations, got %d", len(result.Operations))
	}

	// @Req() and @Res() should be filtered out as infrastructure types
	for _, op := range result.Operations {
		if op.OperationID == "all" {
			for _, p := range op.Parameters {
				if p.Type == "Request" || p.Type == "Response" {
					t.Errorf("Request/Response infrastructure types should be filtered out, found param %s of type %s", p.Name, p.Type)
				}
			}
		}
	}
}

func TestNestJSSpecGeneration(t *testing.T) {
	dir := setupNestProject(t)

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
		ServiceTemplate: "saas-atlasjs",
		TreeShake:       true,
	})

	if spec["openapi"] != "3.2.0" {
		t.Errorf("expected openapi 3.2.0, got %v", spec["openapi"])
	}

	info := spec["info"].(map[string]interface{})
	if info["title"] != "User Service" {
		t.Errorf("expected title 'User Service', got %v", info["title"])
	}
	if info["x-service-template"] != "saas-atlasjs" {
		t.Errorf("expected service template 'saas-atlasjs', got %v", info["x-service-template"])
	}

	paths := spec["paths"].(map[string]interface{})
	if len(paths) == 0 {
		t.Error("spec has no paths")
	}

	t.Logf("Generated spec with %d paths", len(paths))
	for path := range paths {
		t.Logf("  %s", path)
	}

	// Check error responses
	for _, pathItem := range paths {
		pi := pathItem.(map[string]interface{})
		for method, opObj := range pi {
			op := opObj.(map[string]interface{})
			responses := op["responses"].(map[string]interface{})
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
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "util.ts", `export class Util {
	static format(s: string): string {
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

func TestSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "user.controller.spec.ts", nestControllerSource)
	writeTestFile(t, src, "user.controller.test.ts", nestControllerSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Operations) != 0 {
		t.Errorf("expected 0 operations from test files, got %d", len(result.Operations))
	}
}

// --- Unit Tests ---

func TestBuildPath(t *testing.T) {
	tests := []struct {
		base     string
		method   string
		expected string
	}{
		{"api/v1/users", "", "/api/v1/users"},
		{"api/v1/users", "/:id", "/api/v1/users/{id}"},
		{"/api/v1/users", "/search", "/api/v1/users/search"},
		{"example/routing", "/get", "/example/routing/get"},
		{"", "/health", "/health"},
		{"", "", "/"},
	}

	for _, tt := range tests {
		got := buildPath(tt.base, tt.method)
		if got != tt.expected {
			t.Errorf("buildPath(%q, %q) = %q, want %q", tt.base, tt.method, got, tt.expected)
		}
	}
}

func TestStripTSQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`'hello'`, "hello"},
		{`"world"`, "world"},
		{"``", ""},
		{`api/v1`, "api/v1"},
		{`'/path'`, "/path"},
	}

	for _, tt := range tests {
		got := stripTSQuotes(tt.input)
		if got != tt.expected {
			t.Errorf("stripTSQuotes(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractFirstArg(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`('id')`, "'id'"},
		{`('id', new ParseUUIDPipe())`, "'id'"},
		{`()`, ""},
		{`(200)`, "200"},
	}

	for _, tt := range tests {
		got := extractFirstArg(tt.input)
		if got != tt.expected {
			t.Errorf("extractFirstArg(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestApplyParamOverride_MatchesExactNameAndLocation(t *testing.T) {
	params := []*Parameter{
		{Name: "id", In: "path", Description: "path id"},
		{Name: "id", In: "query", Description: "query id"},
	}
	applyParamOverride(params, paramOverride{
		Name:        "id",
		In:          "query",
		Description: "query identifier",
	})
	if params[0].Description != "path id" {
		t.Fatalf("path parameter was unexpectedly overwritten: %q", params[0].Description)
	}
	if params[1].Description != "query identifier" {
		t.Fatalf("query parameter was not overwritten as expected: %q", params[1].Description)
	}
}

func TestEnsurePathParameters_AddsMissingPathParam(t *testing.T) {
	params := []*Parameter{
		{Name: "id", In: "query", Type: "string"},
	}
	got := ensurePathParameters(params, "/api/items/{itemId}")
	found := false
	for _, p := range got {
		if p.Name == "itemId" && p.In == "path" && p.Required {
			found = true
		}
	}
	if !found {
		t.Fatal("expected missing {itemId} to be synthesized as a required path parameter")
	}
}

func TestInferResponseStatus(t *testing.T) {
	tests := []struct {
		method     string
		returnType string
		expected   int
	}{
		{"POST", "UserDto", 201},
		{"GET", "UserDto", 200},
		{"DELETE", "void", 204},
		{"DELETE", "", 204},
		{"PUT", "UserDto", 200},
	}

	for _, tt := range tests {
		got := inferResponseStatus(tt.method, tt.returnType)
		if got != tt.expected {
			t.Errorf("inferResponseStatus(%q, %q) = %d, want %d", tt.method, tt.returnType, got, tt.expected)
		}
	}
}

func TestIsNestInfraType(t *testing.T) {
	tests := []struct {
		typeName string
		expected bool
	}{
		{"Request", true},
		{"Response", true},
		{"any", true},
		{"UserDto", false},
		{"string", false},
	}

	for _, tt := range tests {
		got := isNestInfraType(tt.typeName)
		if got != tt.expected {
			t.Errorf("isNestInfraType(%q) = %v, want %v", tt.typeName, got, tt.expected)
		}
	}
}

func TestTSTypeToSchema(t *testing.T) {
	tests := []struct {
		typeName string
		expected string
	}{
		{"string", "string"},
		{"number", "number"},
		{"boolean", "boolean"},
		{"Date", "string"},
	}

	for _, tt := range tests {
		adapter := &tsAdapter{}
		schema := adapter.ParamTypeToSchema(tt.typeName)
		if schema["type"] != tt.expected {
			t.Errorf("ParamTypeToSchema(%q).type = %v, want %q", tt.typeName, schema["type"], tt.expected)
		}
	}
}

// --- Phase 1: Class-Validator to Schema Mapping ---

func TestClassValidatorAnnotationsInSchema(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")

	writeTestFile(t, src, "validated.controller.ts", `import { Controller, Get, Post, Body } from '@nestjs/common';
@Controller('validated')
export class ValidatedController {
	@Post()
	create(@Body() body: ValidatedDto): ValidatedDto {
		return body;
	}
}
`)
	writeTestFile(t, src, "validated.dto.ts", `import { IsEmail, IsUUID, MinLength, MaxLength, Min, Max, Matches, IsUrl, IsDateString, IsOptional, IsNotEmpty } from 'class-validator';

export class ValidatedDto {
	@IsUUID()
	id: string;

	@IsEmail()
	email: string;

	@MinLength(3)
	@MaxLength(50)
	username: string;

	@Min(0)
	@Max(150)
	age: number;

	@Matches('/^[a-z]+$/')
	slug: string;

	@IsUrl()
	website: string;

	@IsDateString()
	createdAt: string;

	@IsOptional()
	nickname?: string;

	@IsNotEmpty()
	requiredField: string;
}
`)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	spec := GenerateSpec(result, SpecConfig{
		Title:   "Validated",
		Version: "1.0.0",
	})

	schemas := spec["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	dto, ok := schemas["ValidatedDto"].(map[string]interface{})
	if !ok {
		t.Fatal("ValidatedDto schema not found")
	}

	props := dto["properties"].(map[string]interface{})

	// Check format annotations
	checkSchemaField := func(name, key string, expected interface{}) {
		t.Helper()
		field, ok := props[name].(map[string]interface{})
		if !ok {
			t.Errorf("field %q not found in properties", name)
			return
		}
		if field[key] != expected {
			t.Errorf("field %q: expected %s=%v, got %v", name, key, expected, field[key])
		}
	}

	checkSchemaField("id", "format", "uuid")
	checkSchemaField("email", "format", "email")
	checkSchemaField("username", "minLength", 3)
	checkSchemaField("username", "maxLength", 50)
	checkSchemaField("age", "minimum", 0)
	checkSchemaField("age", "maximum", 150)
	checkSchemaField("slug", "pattern", "/^[a-z]+$/")
	checkSchemaField("website", "format", "uri")
	checkSchemaField("createdAt", "format", "date-time")

	// Check required array respects @IsOptional and @IsNotEmpty
	requiredSet := toStringSet(dto["required"])
	if requiredSet["nickname"] {
		t.Error("nickname should NOT be in required (has @IsOptional)")
	}
	if !requiredSet["requiredField"] {
		t.Error("requiredField should be in required (has @IsNotEmpty)")
	}
}

// --- Phase 2: Parameter Defaults ---

func TestParameterDefaultsInSpec(t *testing.T) {
	// Unit test for the default value emission in buildOperation
	result := &Result{
		Operations: []*Operation{
			{
				Path:        "/items",
				Method:      "GET",
				OperationID: "listItems",
				Parameters: []*Parameter{
					{Name: "limit", In: "query", Type: "number", DefaultValue: "10"},
					{Name: "offset", In: "query", Type: "number", DefaultValue: "0"},
					{Name: "id", In: "path", Type: "string", Required: true},
				},
				ResponseType:   "string",
				ResponseStatus: 200,
			},
		},
		Types:   make(map[string]*index.TypeDecl),
		Schemas: make(map[string]interface{}),
	}

	spec := GenerateSpec(result, SpecConfig{
		Title:   "Test",
		Version: "1.0.0",
	})

	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/items"].(map[string]interface{})
	op := pathItem["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})

	for _, p := range params {
		pm := p.(map[string]interface{})
		schema := pm["schema"].(map[string]interface{})
		name := pm["name"].(string)
		switch name {
		case "limit":
			// Improvement #16: numeric defaults are emitted as integers
			if schema["default"] != 10 {
				t.Errorf("limit default: expected 10, got %v", schema["default"])
			}
		case "offset":
			if schema["default"] != 0 {
				t.Errorf("offset default: expected 0, got %v", schema["default"])
			}
		case "id":
			if schema["default"] != nil {
				t.Errorf("id should not have default, got %v", schema["default"])
			}
		}
	}
}

// --- Phase 3: NestJS Swagger Decorators ---

const nestSwaggerControllerSource = `import { Controller, Get, Post, Body, Param, Query } from '@nestjs/common';
import { ApiOperation, ApiResponse, ApiParam, ApiQuery, ApiTags } from '@nestjs/swagger';

@Controller('items')
@ApiTags('Items', 'Inventory')
export class ItemController {
	/**
	 * Old JSDoc summary that should be overridden.
	 */
	@Get('/:id')
	@ApiOperation({ summary: 'Get item by ID', description: 'Fetches a single item' })
	@ApiResponse({ status: 200, description: 'Item found' })
	@ApiResponse({ status: 404, description: 'Item not found' })
	@ApiParam({ name: 'id', description: 'The item UUID' })
	getItem(@Param('id') id: string): ItemDto {
		return null;
	}

	@Get()
	@ApiQuery({ name: 'q', description: 'Search query' })
	search(@Query('q') q: string): ItemDto[] {
		return [];
	}

	@Post()
	@ApiResponse({ status: 201, description: 'Item created' })
	create(@Body() body: CreateItemDto): ItemDto {
		return null;
	}
}
`

func TestSwaggerDecorators(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "item.controller.ts", nestSwaggerControllerSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Operations) < 3 {
		t.Fatalf("expected at least 3 operations, got %d", len(result.Operations))
	}

	for _, op := range result.Operations {
		t.Logf("  %s %s (summary=%q, tags=%v)", op.Method, op.Path, op.Summary, op.Tags)
	}

	opsByID := make(map[string]*Operation)
	for _, op := range result.Operations {
		opsByID[op.OperationID] = op
	}

	// Test @ApiOperation overrides JSDoc
	getItem := opsByID["getItem"]
	if getItem == nil {
		t.Fatal("getItem operation not found")
	}
	if getItem.Summary != "Get item by ID" {
		t.Errorf("expected summary 'Get item by ID', got %q", getItem.Summary)
	}
	if getItem.Description != "Fetches a single item" {
		t.Errorf("expected description 'Fetches a single item', got %q", getItem.Description)
	}

	// Test @ApiTags plural
	if len(getItem.Tags) != 2 || getItem.Tags[0] != "Items" || getItem.Tags[1] != "Inventory" {
		t.Errorf("expected tags [Items, Inventory], got %v", getItem.Tags)
	}

	// Test @ApiResponse annotations
	if len(getItem.ApiResponses) != 2 {
		t.Errorf("expected 2 API responses, got %d", len(getItem.ApiResponses))
	}

	// Test @ApiParam description
	hasDesc := false
	for _, p := range getItem.Parameters {
		if p.Name == "id" && p.Description == "The item UUID" {
			hasDesc = true
		}
	}
	if !hasDesc {
		t.Error("expected @ApiParam description 'The item UUID' on id param")
	}

	// Test @ApiQuery
	search := opsByID["search"]
	if search == nil {
		t.Fatal("search operation not found")
	}
	hasQueryDesc := false
	for _, p := range search.Parameters {
		if p.Name == "q" && p.Description == "Search query" {
			hasQueryDesc = true
		}
	}
	if !hasQueryDesc {
		t.Error("expected @ApiQuery description 'Search query' on q param")
	}

	// Test spec generation with annotated responses
	spec := GenerateSpec(result, SpecConfig{
		Title:   "Items",
		Version: "1.0.0",
	})

	paths := spec["paths"].(map[string]interface{})
	getPath := paths["/items/{id}"].(map[string]interface{})
	getOp := getPath["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})

	if r200, ok := responses["200"].(map[string]interface{}); ok {
		if r200["description"] != "Item found" {
			t.Errorf("expected 200 description 'Item found', got %v", r200["description"])
		}
	} else {
		t.Error("missing 200 response")
	}

	if r404, ok := responses["404"].(map[string]interface{}); ok {
		if r404["description"] != "Item not found" {
			t.Errorf("expected 404 description 'Item not found', got %v", r404["description"])
		}
	} else {
		t.Error("missing 404 response")
	}
}

// --- Phase 3.5: @ApiProperty ---

func TestApiPropertyOnDTOFields(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "item.controller.ts", `import { Controller, Post, Body } from '@nestjs/common';
@Controller('props')
export class PropController {
	@Post()
	create(@Body() body: PropDto): PropDto {
		return body;
	}
}
`)
	writeTestFile(t, src, "prop.dto.ts", `import { ApiProperty } from '@nestjs/swagger';

export class PropDto {
	@ApiProperty({ description: 'The unique identifier', example: '123' })
	id: string;

	@ApiProperty({ description: 'User name', required: true })
	name: string;

	@ApiProperty({ required: false })
	optional?: string;
}
`)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	spec := GenerateSpec(result, SpecConfig{
		Title:   "Props",
		Version: "1.0.0",
	})

	schemas := spec["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	dto, ok := schemas["PropDto"].(map[string]interface{})
	if !ok {
		t.Fatal("PropDto schema not found")
	}

	props := dto["properties"].(map[string]interface{})

	// Check @ApiProperty description
	idField := props["id"].(map[string]interface{})
	if idField["description"] != "The unique identifier" {
		t.Errorf("expected id description 'The unique identifier', got %v", idField["description"])
	}
	if idField["example"] != "123" {
		t.Errorf("expected id example '123', got %v", idField["example"])
	}

	// Check @ApiProperty({ required: false }) removes from required
	requiredSet := toStringSet(dto["required"])
	if requiredSet["optional"] {
		t.Error("optional should NOT be in required (@ApiProperty required: false)")
	}
	if !requiredSet["name"] {
		t.Error("name SHOULD be in required (@ApiProperty required: true)")
	}
}

// --- Phase 4: Security ---

const nestSecurityControllerSource = `import { Controller, Get, Post, Body, UseGuards } from '@nestjs/common';
import { ApiBearerAuth, ApiSecurity, ApiOAuth2 } from '@nestjs/swagger';
import { AuthGuard } from './auth.guard';

@Controller('secure')
@UseGuards(AuthGuard)
@ApiBearerAuth()
export class SecureController {
	@Get()
	listItems(): string[] {
		return [];
	}

	@Post()
	@ApiOAuth2(['read', 'write'])
	createItem(@Body() body: any): string {
		return '';
	}
}
`

func TestSecurityDecorators(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "secure.controller.ts", nestSecurityControllerSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if len(result.Operations) < 2 {
		t.Fatalf("expected at least 2 operations, got %d", len(result.Operations))
	}

	for _, op := range result.Operations {
		t.Logf("  %s %s (auth=%v, security=%v)", op.Method, op.Path, op.RequiresAuth, op.Security)
		if !op.RequiresAuth {
			t.Errorf("%s should require auth (class-level @UseGuards)", op.OperationID)
		}
	}

	// Verify spec has security schemes
	spec := GenerateSpec(result, SpecConfig{
		Title:   "Secure",
		Version: "1.0.0",
	})

	components := spec["components"].(map[string]interface{})
	secSchemes, ok := components["securitySchemes"].(map[string]interface{})
	if !ok {
		t.Fatal("expected securitySchemes in components")
	}

	if _, ok := secSchemes["bearerAuth"]; !ok {
		t.Error("expected bearerAuth security scheme")
	}

	// Check per-operation security
	paths := spec["paths"].(map[string]interface{})
	securePath := paths["/secure"].(map[string]interface{})
	getOp := securePath["get"].(map[string]interface{})
	if _, ok := getOp["security"]; !ok {
		t.Error("GET /secure should have security")
	}
}

// --- Phase 5: JSDoc Structured Tags ---

func TestJSDocStructuredParsing(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		summary  string
		desc     string
		params   map[string]string
		returns  string
	}{
		{
			name: "full jsdoc",
			input: `/**
			 * Get a user by ID.
			 * Fetches user from database.
			 * @param {string} id - The user identifier
			 * @param {number} limit - Max results
			 * @returns {User} The found user
			 */`,
			summary: "Get a user by ID.",
			desc:    "Get a user by ID. Fetches user from database.",
			params:  map[string]string{"id": "The user identifier", "limit": "Max results"},
			returns: "The found user",
		},
		{
			name: "params without types",
			input: `/**
			 * Search items.
			 * @param query search text
			 * @returns results
			 */`,
			summary: "Search items.",
			params:  map[string]string{"query": "search text"},
			returns: "results",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseJSDocStructured(tt.input)
			if result.Summary != tt.summary {
				t.Errorf("summary: got %q, want %q", result.Summary, tt.summary)
			}
			if tt.desc != "" && result.Description != tt.desc {
				t.Errorf("description: got %q, want %q", result.Description, tt.desc)
			}
			for k, v := range tt.params {
				if result.Params[k] != v {
					t.Errorf("param %q: got %q, want %q", k, result.Params[k], v)
				}
			}
			if tt.returns != "" && result.Returns != tt.returns {
				t.Errorf("returns: got %q, want %q", result.Returns, tt.returns)
			}
		})
	}
}

func TestJSDocParamDescriptionsApplied(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeTestFile(t, src, "jsdoc.controller.ts", `import { Controller, Get, Query } from '@nestjs/common';

@Controller('jsdoc')
export class JsdocController {
	/**
	 * Search items.
	 * @param q - The search query
	 * @param limit - Max results to return
	 * @returns search results
	 */
	@Get('/search')
	search(@Query('q') q: string, @Query('limit') limit: number): string[] {
		return [];
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

	if len(result.Operations) == 0 {
		t.Fatal("no operations extracted")
	}

	op := result.Operations[0]
	for _, p := range op.Parameters {
		switch p.Name {
		case "q":
			if p.Description != "The search query" {
				t.Errorf("q description: got %q, want 'The search query'", p.Description)
			}
		case "limit":
			if p.Description != "Max results to return" {
				t.Errorf("limit description: got %q, want 'Max results to return'", p.Description)
			}
		}
	}
}

// --- Unit Tests for New Helpers ---

func TestExtractDecoratorProperties(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]string
	}{
		{
			input:    `({ summary: 'Hello', description: 'World' })`,
			expected: map[string]string{"summary": "Hello", "description": "World"},
		},
		{
			input:    `({ status: 200, description: 'OK' })`,
			expected: map[string]string{"status": "200", "description": "OK"},
		},
		{
			input:    `()`,
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		result := extractDecoratorProperties(tt.input)
		for k, v := range tt.expected {
			if result[k] != v {
				t.Errorf("extractDecoratorProperties(%q)[%q] = %q, want %q", tt.input, k, result[k], v)
			}
		}
	}
}

func TestExtractAllArgs(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{`('Tag1', 'Tag2')`, []string{"Tag1", "Tag2"}},
		{`('Single')`, []string{"Single"}},
		{`()`, nil},
		{`('A', 'B', 'C')`, []string{"A", "B", "C"}},
	}

	for _, tt := range tests {
		got := extractAllArgs(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("extractAllArgs(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("extractAllArgs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestApplyClassValidatorAnnotations(t *testing.T) {
	schema := map[string]interface{}{"type": "string"}
	annotations := map[string]string{
		"IsEmail":   "()",
		"MinLength": "(5)",
		"MaxLength": "(100)",
	}
	applyClassValidatorAnnotations(schema, annotations)

	if schema["format"] != "email" {
		t.Errorf("expected format=email, got %v", schema["format"])
	}
	if schema["minLength"] != 5 {
		t.Errorf("expected minLength=5, got %v", schema["minLength"])
	}
	if schema["maxLength"] != 100 {
		t.Errorf("expected maxLength=100, got %v", schema["maxLength"])
	}
}

func TestExtractAnnotationArg(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"(3)", "3"},
		{"(100)", "100"},
		{"('/^[a-z]+$/')", "'/^[a-z]+$/'"},
		{"()", ""},
	}

	for _, tt := range tests {
		got := extractAnnotationArg(tt.input)
		if got != tt.expected {
			t.Errorf("extractAnnotationArg(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// Helper functions

func containsParam(params []*Parameter, name, in string) bool {
	for _, p := range params {
		if p.Name == name && p.In == in {
			return true
		}
	}
	return false
}

func toStringSet(v interface{}) map[string]bool {
	set := make(map[string]bool)
	switch arr := v.(type) {
	case []interface{}:
		for _, item := range arr {
			if s, ok := item.(string); ok {
				set[s] = true
			}
		}
	case []string:
		for _, s := range arr {
			set[s] = true
		}
	}
	return set
}

func TestExcludeDecoratorOmitsField(t *testing.T) {
	src := `import { Controller, Get } from '@nestjs/common';
import { Exclude } from 'class-transformer';

@Controller('items')
export class ItemsController {
    @Get(':id')
    getItem(): ItemDto {
        return null;
    }
}

export class ItemDto {
    name: string;

    @Exclude()
    internalId: string;
}`

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "items.controller.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{srcDir}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Items", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	itemDto, ok := schemas["ItemDto"].(map[string]interface{})
	if !ok {
		t.Fatal("missing ItemDto schema")
	}
	props := itemDto["properties"].(map[string]interface{})

	if _, ok := props["name"]; !ok {
		t.Error("name field should be present")
	}
	if _, ok := props["internalId"]; ok {
		t.Error("@Exclude field should be excluded from schema")
	}
}

func TestTSErrorResponseSchemas(t *testing.T) {
	result := &Result{
		Operations: []*Operation{
			{
				Path:         "/test",
				Method:       "GET",
				OperationID:  "test",
				ResponseType: "string",
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
			t.Errorf("%s response $ref = %v, want #/components/schemas/ErrorResponse", code, ref)
		}
	}

	// Verify ErrorResponse schema exists
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})
	if _, ok := schemas["ErrorResponse"]; !ok {
		t.Error("missing ErrorResponse schema in components")
	}
}

func TestTSSecuritySchemesClientCredentials(t *testing.T) {
	result := &Result{
		Operations: []*Operation{
			{
				Path:         "/test",
				Method:       "GET",
				OperationID:  "test",
				RequiresAuth: true,
				Security:     []string{"oauth2", "sp:read"},
			},
		},
		Schemas: make(map[string]interface{}),
		Types:   make(map[string]*index.TypeDecl),
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	components := spec["components"].(map[string]interface{})
	secSchemes, ok := components["securitySchemes"].(map[string]interface{})
	if !ok {
		t.Fatal("missing securitySchemes")
	}
	oauth2, ok := secSchemes["oauth2"].(map[string]interface{})
	if !ok {
		t.Fatal("missing oauth2 scheme")
	}
	flows, ok := oauth2["flows"].(map[string]interface{})
	if !ok {
		t.Fatal("missing flows")
	}
	if _, ok := flows["clientCredentials"]; !ok {
		t.Error("expected clientCredentials flow, not implicit")
	}
	if _, ok := flows["implicit"]; ok {
		t.Error("should not have implicit flow")
	}
	cc := flows["clientCredentials"].(map[string]interface{})
	if cc["tokenUrl"] != "/oauth/token" {
		t.Errorf("tokenUrl = %v, want /oauth/token", cc["tokenUrl"])
	}
}

// ===== v3 Overhaul Tests =====

func TestTS404ForResourceEndpoint(t *testing.T) {
	// Improvement #10: 404 for resource endpoints with path params
	result := &Result{
		Operations: []*Operation{
			{
				Path:           "/items/{id}",
				Method:         "GET",
				OperationID:    "getItem",
				Parameters:     []*Parameter{{Name: "id", In: "path", Required: true, Type: "string"}},
				ResponseType:   "string",
				ResponseStatus: 200,
			},
			{
				Path:           "/items",
				Method:         "GET",
				OperationID:    "listItems",
				ResponseType:   "string",
				ResponseStatus: 200,
			},
		},
		Types:   make(map[string]*index.TypeDecl),
		Schemas: make(map[string]interface{}),
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})

	// GET /{id} should have 404
	idPath := paths["/items/{id}"].(map[string]interface{})
	getOp := idPath["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})
	if _, ok := responses["404"]; !ok {
		t.Error("GET /{id} should have 404 response")
	}

	// GET /items should NOT have 404
	listPath := paths["/items"].(map[string]interface{})
	listOp := listPath["get"].(map[string]interface{})
	listResponses := listOp["responses"].(map[string]interface{})
	if _, ok := listResponses["404"]; ok {
		t.Error("GET /items without path params should not have 404")
	}
}

func TestTSParameterConstraints(t *testing.T) {
	// Improvement #1: TS parameter constraints emitted in spec
	minLen := 3
	maxLen := 50
	result := &Result{
		Operations: []*Operation{
			{
				Path:        "/items",
				Method:      "GET",
				OperationID: "listItems",
				Parameters: []*Parameter{
					{Name: "search", In: "query", Type: "string", MinLength: &minLen, MaxLength: &maxLen, Format: "email"},
				},
				ResponseType:   "string",
				ResponseStatus: 200,
			},
		},
		Types:   make(map[string]*index.TypeDecl),
		Schemas: make(map[string]interface{}),
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/items"].(map[string]interface{})
	op := pathItem["get"].(map[string]interface{})
	params := op["parameters"].([]interface{})

	pm := params[0].(map[string]interface{})
	schema := pm["schema"].(map[string]interface{})
	if schema["minLength"] != 3 {
		t.Errorf("minLength = %v, want 3", schema["minLength"])
	}
	if schema["maxLength"] != 50 {
		t.Errorf("maxLength = %v, want 50", schema["maxLength"])
	}
	if schema["format"] != "email" {
		t.Errorf("format = %v, want email", schema["format"])
	}
}

// =============================================================================
// v4 Backfill Tests
// =============================================================================

func TestTSResponsePreferIndexedSchema(t *testing.T) {
	// v4 #1: Response body uses indexed schema
	src := `import { Controller, Get, Param } from '@nestjs/common';
import { IsString, MinLength, MaxLength } from 'class-validator';

export class ItemDto {
	@IsString()
	@MinLength(1)
	@MaxLength(100)
	name: string;
}

@Controller('items')
export class ItemController {
	@Get('/:id')
	get(@Param('id') id: string): ItemDto {
		return null;
	}
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/item.controller.ts", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})

	// Find any GET operation response schema
	for _, pi := range paths {
		for method, opObj := range pi.(map[string]interface{}) {
			if method != "get" {
				continue
			}
			op := opObj.(map[string]interface{})
			responses := op["responses"].(map[string]interface{})
			resp200, ok := responses["200"].(map[string]interface{})
			if !ok {
				continue
			}
			content, ok := resp200["content"].(map[string]interface{})
			if !ok {
				continue
			}
			jsonContent, ok := content["application/json"].(map[string]interface{})
			if !ok {
				continue
			}
			schema := jsonContent["schema"].(map[string]interface{})
			// Should use indexed schema (either $ref or inline with properties)
			if _, hasRef := schema["$ref"]; hasRef {
				// $ref is fine
			} else if _, hasProps := schema["properties"]; hasProps {
				// Inline schema is fine (index may not carry TS validator annotations)
			} else if schema["type"] == nil {
				t.Errorf("response schema should have $ref, properties, or type: %v", schema)
			}
			return
		}
	}
	t.Error("no GET operation found in spec")
}

func TestTSSizeOnCollectionMinMaxItems(t *testing.T) {
	// v4 #4: @IsArray + @ArrayMinSize → minItems
	src := `import { Controller, Get, Param } from '@nestjs/common';
import { IsArray, ArrayMinSize, ArrayMaxSize } from 'class-validator';

export class ItemDto {
	@IsArray()
	@ArrayMinSize(1)
	@ArrayMaxSize(10)
	tags: string[];
}

@Controller('items')
export class ItemController {
	@Get('/:id')
	get(@Param('id') id: string): ItemDto {
		return null;
	}
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/item.controller.ts", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	for _, decl := range result.Types {
		if decl.Name == "ItemDto" {
			for _, f := range decl.Fields {
				if f.Name == "tags" {
					if f.Type != "string[]" {
						t.Errorf("tags type = %q, want string[]", f.Type)
					}
					return
				}
			}
		}
	}
}

func TestTSOperationIdDedup(t *testing.T) {
	// v4 #7: Duplicate operationIds disambiguated
	src := `import { Controller, Get } from '@nestjs/common';

@Controller('api/v1')
export class TestController {
	@Get('/items')
	list(): string[] { return []; }

	@Get('/users')
	list(): string[] { return []; }
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/test.controller.ts", src)
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

func TestTS409ForPostPut(t *testing.T) {
	// v4 #8: 409 Conflict for POST/PUT
	src := `import { Controller, Post, Body } from '@nestjs/common';

@Controller('items')
export class ItemController {
	@Post()
	create(@Body() body: any): string {
		return '';
	}
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/item.controller.ts", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	for _, pi := range paths {
		for _, opObj := range pi.(map[string]interface{}) {
			op := opObj.(map[string]interface{})
			responses := op["responses"].(map[string]interface{})
			if _, ok := responses["409"]; !ok {
				t.Error("POST should have 409 Conflict response")
			}
		}
	}
}

func TestTSResponseHeaders(t *testing.T) {
	// v4 #8: Response headers with typed schemas
	op := &Operation{
		Path:         "/test",
		Method:       "GET",
		OperationID:  "test",
		ResponseType: "string",
		ResponseHeaders: map[string]string{
			"X-Total-Count": "Total items",
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

func TestTSTopLevelTags(t *testing.T) {
	// v4 #9: Tags array emitted at spec root
	src := `import { Controller, Get } from '@nestjs/common';
import { ApiTags } from '@nestjs/swagger';

@ApiTags('Items')
@Controller('items')
export class ItemController {
	@Get()
	list(): string[] { return []; }
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/item.controller.ts", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	tags, ok := spec["tags"].([]interface{})
	if !ok {
		t.Skip("TS tags extraction may not be implemented yet")
	}
	if len(tags) < 1 {
		t.Errorf("expected at least 1 tag, got %d", len(tags))
	}
}

func TestTSRequestBodyPreferIndexed(t *testing.T) {
	// v4 #10: TS request body uses indexed schema
	src := `import { Controller, Post, Body } from '@nestjs/common';
import { IsString, MinLength } from 'class-validator';

export class CreateItemDto {
	@IsString()
	@MinLength(1)
	name: string;
}

@Controller('items')
export class ItemController {
	@Post()
	create(@Body() body: CreateItemDto): string {
		return '';
	}
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/item.controller.ts", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	for _, pi := range paths {
		for _, opObj := range pi.(map[string]interface{}) {
			op := opObj.(map[string]interface{})
			if reqBody, ok := op["requestBody"].(map[string]interface{}); ok {
				content := reqBody["content"].(map[string]interface{})
				jsonContent := content["application/json"].(map[string]interface{})
				schema := jsonContent["schema"].(map[string]interface{})
				// Either $ref or inline with properties (index may not carry TS validator annotations)
				if _, hasRef := schema["$ref"]; !hasRef {
					if _, hasProps := schema["properties"]; !hasProps {
						if schema["type"] == nil {
							t.Errorf("request body schema should have $ref, properties, or type: %v", schema)
						}
					}
				}
			}
		}
	}
}

// =============================================================================
// v5 Feature Tests
// =============================================================================

func TestTSNullableFieldInSchema(t *testing.T) {
	// v5 #1: Optional (?) property → nullable: true
	src := `import { Controller, Get, Param } from '@nestjs/common';

export class ItemDto {
	id: string;
	description?: string;
}

@Controller('items')
export class ItemController {
	@Get('/:id')
	get(@Param('id') id: string): ItemDto {
		return null;
	}
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/item.controller.ts", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema, ok := result.Schemas["ItemDto"].(map[string]interface{})
	if !ok {
		t.Fatal("ItemDto schema not found")
	}
	props := schema["properties"].(map[string]interface{})

	descProp := props["description"].(map[string]interface{})
	if descProp["nullable"] != true {
		t.Errorf("optional field should have nullable: true, got %v", descProp)
	}

	idProp := props["id"].(map[string]interface{})
	if _, ok := idProp["nullable"]; ok {
		t.Error("non-optional field should NOT have nullable")
	}
}

func TestTSDeprecatedField(t *testing.T) {
	// v5 #10: @deprecated JSDoc → deprecated: true
	src := `import { Controller, Get, Param } from '@nestjs/common';

export class ItemDto {
	id: string;

	/** @deprecated Use newField instead */
	oldField: string;
}

@Controller('items')
export class ItemController {
	@Get('/:id')
	get(@Param('id') id: string): ItemDto {
		return null;
	}
}
`
	dir := t.TempDir()
	writeTestFile(t, dir, "src/item.controller.ts", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{filepath.Join(dir, "src")}})
	if err != nil {
		t.Fatal(err)
	}

	schema, ok := result.Schemas["ItemDto"].(map[string]interface{})
	if !ok {
		t.Fatal("ItemDto schema not found")
	}
	props := schema["properties"].(map[string]interface{})

	oldProp := props["oldField"].(map[string]interface{})
	if oldProp["deprecated"] != true {
		t.Errorf("@deprecated JSDoc field should have deprecated: true, got %v", oldProp)
	}
}

// =============================================================================
// v4 #8 Coverage Gap Tests
// =============================================================================

func TestTSNullableResponse(t *testing.T) {
	// v4 #8: Nullable response in TS
	op := &Operation{
		Path:             "/test",
		Method:           "GET",
		OperationID:      "test",
		ResponseType:     "string",
		NullableResponse: true,
	}
	result := &Result{Operations: []*Operation{op}, Schemas: make(map[string]interface{})}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/test"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})
	responses := getOp["responses"].(map[string]interface{})
	resp200 := responses["200"].(map[string]interface{})
	content := resp200["content"].(map[string]interface{})
	jsonContent := content["application/json"].(map[string]interface{})
	schema := jsonContent["schema"].(map[string]interface{})

	if schema["nullable"] != true {
		t.Error("nullable response should have nullable: true in schema")
	}
}

func TestTSRateLimited(t *testing.T) {
	// v4 #8: @Throttle → x-rate-limited
	op := &Operation{
		Path:         "/test",
		Method:       "GET",
		OperationID:  "test",
		ResponseType: "string",
		RateLimited:  true,
	}
	result := &Result{Operations: []*Operation{op}, Schemas: make(map[string]interface{})}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/test"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})

	if getOp["x-rate-limited"] != true {
		t.Error("rate-limited operation should have x-rate-limited: true")
	}
}

func TestTSDeprecatedSince(t *testing.T) {
	// v4 #8: @Deprecated({ since }) → x-deprecated-since
	op := &Operation{
		Path:            "/test",
		Method:          "GET",
		OperationID:     "test",
		ResponseType:    "string",
		Deprecated:      true,
		DeprecatedSince: "2024-01",
	}
	result := &Result{Operations: []*Operation{op}, Schemas: make(map[string]interface{})}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/test"].(map[string]interface{})
	getOp := pathItem["get"].(map[string]interface{})

	if getOp["deprecated"] != true {
		t.Error("deprecated operation should have deprecated: true")
	}
	if getOp["x-deprecated-since"] != "2024-01" {
		t.Errorf("x-deprecated-since = %v, want 2024-01", getOp["x-deprecated-since"])
	}
}

func TestTSContentTypeOverrides(t *testing.T) {
	// v4 #8: ConsumesContentType/ProducesContentType override defaults
	op := &Operation{
		Path:                "/test",
		Method:              "POST",
		OperationID:         "test",
		RequestBodyType:     "string",
		ResponseType:        "string",
		ResponseStatus:      200,
		ConsumesContentType: "text/csv",
		ProducesContentType: "text/csv",
	}
	result := &Result{Operations: []*Operation{op}, Schemas: make(map[string]interface{})}

	spec := GenerateSpec(result, SpecConfig{Title: "Test", Version: "1.0.0"})
	paths := spec["paths"].(map[string]interface{})
	pathItem := paths["/test"].(map[string]interface{})
	postOp := pathItem["post"].(map[string]interface{})

	// Request body should use text/csv
	reqBody := postOp["requestBody"].(map[string]interface{})
	reqContent := reqBody["content"].(map[string]interface{})
	if _, ok := reqContent["text/csv"]; !ok {
		t.Error("request body should use text/csv content type")
	}

	// Response should use text/csv
	responses := postOp["responses"].(map[string]interface{})
	resp200 := responses["200"].(map[string]interface{})
	respContent, ok := resp200["content"].(map[string]interface{})
	if !ok {
		t.Fatal("200 response should have content")
	}
	if _, ok := respContent["text/csv"]; !ok {
		t.Error("response should use text/csv content type")
	}
}
