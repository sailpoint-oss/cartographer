package pythonextract

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// --- FastAPI fixture ---

const fastapiControllerSource = `"""User management routes."""
from typing import Annotated, Optional
from uuid import UUID

from fastapi import APIRouter, Depends, HTTPException, Path, Query, Response
from pydantic import BaseModel, Field

from .auth import get_current_user


router = APIRouter(prefix="/api/v1/users", tags=["users"])


class UserDto(BaseModel):
    """A user in the system."""
    id: UUID
    email: str = Field(..., description="Contact email")
    name: str
    age: int | None = None
    tags: list[str] = []


class CreateUserDto(BaseModel):
    """Payload for creating a user."""
    email: str
    name: str
    age: Optional[int] = None


@router.get("/")
async def list_users(
    response: Response,
    offset: int = 0,
    limit: Annotated[int, Query(le=100)] = 20,
    filter: str | None = None,
    _user=Depends(get_current_user),
) -> list[UserDto]:
    """List all users."""
    response.headers["X-Total-Count"] = "0"
    return []


@router.get("/{user_id}")
async def get_user(user_id: UUID) -> UserDto:
    """Get a user by ID."""
    raise HTTPException(status_code=404)


@router.post("/", status_code=201)
async def create_user(payload: CreateUserDto) -> UserDto:
    """Create a new user."""
    return None


@router.delete("/{user_id}", status_code=204)
async def delete_user(user_id: UUID) -> None:
    """Delete a user."""
    return None
`

// --- Flask fixture ---

const flaskAppSource = `from flask import Flask, Blueprint, jsonify, request

app = Flask(__name__)
bp = Blueprint("users", __name__, url_prefix="/users")


@bp.route("/", methods=["GET", "POST"])
def users():
    """List or create users."""
    response = jsonify([])
    response.headers["X-Total-Count"] = "0"
    if request.method == "POST":
        return jsonify({}), 201
    return response


@bp.route("/<user_id>", methods=["DELETE"])
def delete_user(user_id):
    """Delete a user."""
    return ("", 204)


app.register_blueprint(bp)
`

// --- Starlette fixture ---

const starletteAppSource = `from starlette.applications import Starlette
from starlette.responses import JSONResponse
from starlette.routing import Route


async def health(request):
    """Liveness probe."""
    return JSONResponse({"status": "ok"})


async def version(request):
    """Service version."""
    return JSONResponse({"version": "1.0"})


routes = [
    Route("/health", health, methods=["GET"]),
    Route("/version", version, methods=["GET"]),
]

app = Starlette(routes=routes)
`

// --- Ariadne (GraphQL-only) fixture ---

const ariadneAppSource = `from ariadne import QueryType, make_executable_schema
from ariadne.asgi import GraphQL


type_defs = """
    type Query {
        hello: String
    }
"""

query = QueryType()


@query.field("hello")
def resolve_hello(_, __):
    return "world"


schema = make_executable_schema(type_defs, query)
app = GraphQL(schema)
`

// --- Generic agent-server fixture ---

const agentServerSource = `from example_agents import ExampleAgent, ExampleServer


primary_agent = ExampleAgent("primary")
embedded_agent = ExampleAgent("embedded")


server = ExampleServer("example")
server.register_agent("/", primary_agent)
server.register_agent("/embedded", embedded_agent)
`

// --- Shared helpers ---

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- FastAPI tests ---

func TestFastAPIExtraction(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "app"), "routes.py", fastapiControllerSource)

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

	if result.Framework != "fastapi" {
		t.Errorf("expected framework=fastapi, got %q", result.Framework)
	}

	byKey := make(map[string]*Operation)
	for _, op := range result.Operations {
		byKey[op.Method+" "+op.Path] = op
	}

	if op, ok := byKey["GET /api/v1/users"]; ok {
		if op.OperationID != "list_users" {
			t.Errorf("expected operationId=list_users, got %q", op.OperationID)
		}
		if !op.RequiresAuth {
			t.Error("list_users should require auth (Depends)")
		}
		queryCount := 0
		for _, p := range op.Parameters {
			if p.In == "query" {
				queryCount++
			}
		}
		if queryCount < 3 {
			t.Errorf("expected >=3 query params, got %d: %+v", queryCount, op.Parameters)
		}
		if _, ok := op.ResponseHeaders["X-Total-Count"]; !ok {
			t.Errorf("expected X-Total-Count response header, got %#v", op.ResponseHeaders)
		}
	} else {
		t.Errorf("missing GET /api/v1/users operation (router prefix not applied); got keys: %v", keysOfOps(byKey))
	}

	if op, ok := byKey["GET /api/v1/users/{user_id}"]; ok {
		if op.OperationID != "get_user" {
			t.Errorf("expected operationId=get_user, got %q", op.OperationID)
		}
		foundPathParam := false
		for _, p := range op.Parameters {
			if p.In == "path" && p.Name == "user_id" {
				foundPathParam = true
			}
		}
		if !foundPathParam {
			t.Error("expected user_id path parameter")
		}
	} else {
		t.Error("missing GET /api/v1/users/{user_id} operation")
	}

	if op, ok := byKey["POST /api/v1/users"]; ok {
		if op.ResponseStatus != 201 {
			t.Errorf("expected status 201, got %d", op.ResponseStatus)
		}
		if op.RequestBodyType == "" {
			t.Error("POST /api/v1/users should have a request body type")
		}
		if !strings.Contains(op.RequestBodyType, "CreateUserDto") {
			t.Errorf("expected CreateUserDto body, got %q", op.RequestBodyType)
		}
	} else {
		t.Error("missing POST /api/v1/users operation")
	}

	if op, ok := byKey["DELETE /api/v1/users/{user_id}"]; ok {
		if op.ResponseStatus != 204 {
			t.Errorf("expected status 204, got %d", op.ResponseStatus)
		}
	} else {
		t.Error("missing DELETE /api/v1/users/{user_id} operation")
	}

	// Pydantic model should be indexed
	found := false
	for name := range result.Types {
		if name == "UserDto" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected UserDto in type index")
	}
}

// --- Flask tests ---

func TestFlaskExtraction(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "app.py", flaskAppSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
		Verbose:    false,
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Framework != "flask" {
		t.Errorf("expected framework=flask, got %q", result.Framework)
	}

	// users() takes methods=["GET","POST"], we emit one op for the first method
	foundUsers := false
	foundDelete := false
	for _, op := range result.Operations {
		if op.OperationID == "users" {
			foundUsers = true
			if op.Method != "GET" {
				t.Errorf("expected users first method=GET, got %q", op.Method)
			}
			if op.Path != "/users" {
				t.Errorf("expected Blueprint url_prefix to apply: /users, got %q", op.Path)
			}
		}
		if op.OperationID == "delete_user" {
			foundDelete = true
			if op.Method != "DELETE" {
				t.Errorf("expected delete_user method=DELETE, got %q", op.Method)
			}
			if op.Path != "/users/<user_id>" {
				t.Errorf("expected Blueprint url_prefix to apply: /users/<user_id>, got %q", op.Path)
			}
		}
	}
	if !foundUsers {
		t.Error("missing users operation")
	}
	if !foundDelete {
		t.Error("missing delete_user operation")
	}
}

// --- FastAPI app.include_router(prefix=...) -----------------------------------

const fastapiMountedRouterSource = `from fastapi import APIRouter, FastAPI

app = FastAPI()
users = APIRouter(prefix="/users", tags=["users"])


@users.get("/")
async def list_users():
    """List users."""
    return []


@users.get("/{user_id}")
async def get_user(user_id: str):
    """Get a user."""
    return None


app.include_router(users, prefix="/api/v1")
`

// TestFastAPIIncludeRouterPrefix verifies that the prefix supplied to
// app.include_router is combined with the router's own prefix, so a route
// defined as @users.get("/{user_id}") on users = APIRouter(prefix="/users")
// included via app.include_router(users, prefix="/api/v1") resolves to the
// fully qualified path "/api/v1/users/{user_id}".
func TestFastAPIIncludeRouterPrefix(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "routes.py", fastapiMountedRouterSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	byKey := make(map[string]*Operation)
	for _, op := range result.Operations {
		byKey[op.Method+" "+op.Path] = op
	}

	wants := []string{
		"GET /api/v1/users",
		"GET /api/v1/users/{user_id}",
	}
	for _, k := range wants {
		if _, ok := byKey[k]; !ok {
			t.Errorf("missing operation %q; include_router prefix not combined with router prefix. got keys: %v", k, keysOfOps(byKey))
		}
	}
}

func keysOfOps(m map[string]*Operation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestPydanticFieldExampleAndDescription verifies that Pydantic Field(...)
// description and example kwargs (both `example=` and `examples=[...]`) are
// captured on the resulting type's fields. This is what ultimately lets the
// OpenAPI schema emit `description` and `example` entries for Pydantic DTOs.
const pydanticFixture = `from pydantic import BaseModel, Field


class Contact(BaseModel):
    """A contact entry."""

    email: str = Field(..., description="Primary contact email", example="alice@example.com")
    verified: bool = Field(False, description="Whether the contact has verified their email")
    tags: list[str] = Field(default_factory=list, examples=[["friend", "colleague"]])
`

func TestPydanticFieldExampleAndDescription(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "models.py", pydanticFixture)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	contact, ok := result.Types["Contact"]
	if !ok {
		t.Fatalf("Contact type not found; got: %v", keysOfTypes(result.Types))
	}

	byName := map[string]struct {
		Description string
		Example     string
	}{}
	for _, f := range contact.Fields {
		byName[f.Name] = struct {
			Description string
			Example     string
		}{f.Description, f.Example}
	}

	if got := byName["email"].Description; got != "Primary contact email" {
		t.Errorf("email description = %q, want 'Primary contact email'", got)
	}
	if got := byName["email"].Example; got != "alice@example.com" {
		t.Errorf("email example = %q, want 'alice@example.com'", got)
	}
	if got := byName["verified"].Description; got != "Whether the contact has verified their email" {
		t.Errorf("verified description = %q, want Pydantic Field description", got)
	}
	if got := byName["tags"].Example; got == "" {
		t.Error("expected tags example from examples=[[...]] kwarg, got empty")
	}
}

func keysOfTypes[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- Starlette tests ---

func TestStarletteExtraction(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "app.py", starletteAppSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Framework != "starlette" {
		t.Errorf("expected framework=starlette, got %q", result.Framework)
	}

	paths := map[string]bool{}
	for _, op := range result.Operations {
		paths[op.Path] = true
	}
	if !paths["/health"] {
		t.Error("expected /health route from Route(...) constructor")
	}
	if !paths["/version"] {
		t.Error("expected /version route from Route(...) constructor")
	}
}

// --- Ariadne / GraphQL-only service ---

func TestAriadneGraphQLStub(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "app.py", ariadneAppSource)
	// pyproject.toml so we also exercise ProjectMetadata parsing
	writeTestFile(t, dir, "pyproject.toml", `
[build-system]
requires = ["hatchling"]

[project]
name = "example-graphql-test"
version = "1.2.3"
description = "Test GraphQL service"
`)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Framework != "ariadne" {
		t.Errorf("expected framework=ariadne, got %q", result.Framework)
	}

	// Should have the fabricated /graphql stub because there are no REST routes
	if len(result.Operations) == 0 {
		t.Fatal("expected at least one operation (the /graphql stub)")
	}
	found := false
	for _, op := range result.Operations {
		if op.Path == "/graphql" && op.Method == "POST" {
			found = true
		}
	}
	if !found {
		t.Error("expected POST /graphql stub operation")
	}

	// pyproject metadata should be populated
	if result.Metadata.Name != "example-graphql-test" {
		t.Errorf("expected name=example-graphql-test, got %q", result.Metadata.Name)
	}
	if result.Metadata.Version != "1.2.3" {
		t.Errorf("expected version=1.2.3, got %q", result.Metadata.Version)
	}
	if result.Metadata.Description != "Test GraphQL service" {
		t.Errorf("expected description, got %q", result.Metadata.Description)
	}
}

// --- Generic agent-server registration ---

func TestAgentServerRegistrationExtraction(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "app.py", agentServerSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	if result.Framework != "agent" {
		t.Errorf("expected framework=agent, got %q", result.Framework)
	}

	byKey := make(map[string]*Operation)
	for _, op := range result.Operations {
		byKey[op.Method+" "+op.Path] = op
	}
	for _, want := range []string{
		"POST /",
		"GET /.well-known/agent.json",
		"POST /embedded",
		"GET /embedded/.well-known/agent.json",
	} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("missing %q; got keys: %v", want, keysOfOps(byKey))
		}
	}
	if op := byKey["POST /"]; op == nil || op.ProducesContentType != "text/event-stream" {
		t.Errorf("POST / should produce text/event-stream, got %#v", op)
	}
}

func TestAgentTaskRegistrationExtraction(t *testing.T) {
	dir := t.TempDir()
	const taskServerSource = `from example_agents import ExampleServer
from example_tasks import create_identify_task, create_generate_task


def create_server(app):
    server = ExampleServer("example")
    server.register_task(
        path="/example-agent/identify-target",
        task=create_identify_task(),
    )
    server.register_task(path="/example-agent/generate-config", task=create_generate_task())
    return server
`
	writeTestFile(t, dir, "server.py", taskServerSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	if result.Framework != "agent" {
		t.Errorf("expected framework=agent, got %q", result.Framework)
	}
	byKey := make(map[string]*Operation)
	for _, op := range result.Operations {
		byKey[op.Method+" "+op.Path] = op
	}
	for _, want := range []string{
		"POST /example-agent/identify-target",
		"POST /example-agent/generate-config",
	} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("missing task op %q; got keys: %v", want, keysOfOps(byKey))
		}
	}
	// A task-only server must NOT synthesise a root A2A agent surface.
	if _, ok := byKey["POST /"]; ok {
		t.Errorf("task-only server should not emit a root agent stream operation")
	}
}

func TestSingleAgentBootstrapExtraction(t *testing.T) {
	dir := t.TempDir()
	const mainSource = `import asyncio
from sp_agents.agents import TemplateAgent
from agent import config


async def main() -> None:
    agent: TemplateAgent = TemplateAgent.from_config(config)
    await agent.arun_server_deployed()


if __name__ == "__main__":
    asyncio.run(main())
`
	writeTestFile(t, dir, "__main__.py", mainSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	byKey := make(map[string]*Operation)
	for _, op := range result.Operations {
		byKey[op.Method+" "+op.Path] = op
	}
	for _, want := range []string{"POST /", "GET /.well-known/agent.json"} {
		if _, ok := byKey[want]; !ok {
			t.Errorf("missing implicit agent op %q; got keys: %v", want, keysOfOps(byKey))
		}
	}
}

func TestA2AStarletteBootstrapExtraction(t *testing.T) {
	dir := t.TempDir()
	const serverSource = `from a2a.server.apps import A2AStarletteApplication
from a2a.server.request_handlers import DefaultRequestHandler


def create_app(agent):
    handler = DefaultRequestHandler(agent_executor=agent, task_store=None)
    app = A2AStarletteApplication(agent_card=None, http_handler=handler)
    return app.build()
`
	writeTestFile(t, dir, "a2a_server.py", serverSource)

	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	byKey := make(map[string]*Operation)
	for _, op := range result.Operations {
		byKey[op.Method+" "+op.Path] = op
	}
	if _, ok := byKey["GET /.well-known/agent.json"]; !ok {
		t.Errorf("expected implicit agent card op for A2AStarletteApplication; got keys: %v", keysOfOps(byKey))
	}
}

// --- Spec generation smoke test ---

func TestPythonSpecGeneration(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "app"), "routes.py", fastapiControllerSource)

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	spec := GenerateSpec(result, SpecConfig{
		Title:           "Test Service",
		Version:         "1.0.0",
		Description:     "Test service",
		OpenAPIVersion:  "3.2",
		ServiceTemplate: "python-fastapi",
		TreeShake:       true,
	})

	if spec["openapi"] != "3.2.0" {
		t.Errorf("expected openapi 3.2.0, got %v", spec["openapi"])
	}
	info, _ := spec["info"].(map[string]any)
	if info == nil {
		t.Fatal("missing info block")
	}
	if info["title"] != "Test Service" {
		t.Errorf("expected title=Test Service, got %v", info["title"])
	}
	if info["x-service-template"] != "python-fastapi" {
		t.Errorf("expected x-service-template=python-fastapi, got %v", info["x-service-template"])
	}
	if info["x-service-framework"] != "fastapi" {
		t.Errorf("expected x-service-framework=fastapi, got %v", info["x-service-framework"])
	}
	paths, _ := spec["paths"].(map[string]any)
	if len(paths) == 0 {
		t.Error("expected at least one path in the spec")
	}
}

// --- humaniseOperationID unit tests ---

func TestHumaniseOperationID(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"list_users", "List users"},
		{"get_user_by_id", "Get user by id"},
		{"listUsers", "List users"},
		{"", ""},
	}
	for _, c := range cases {
		got := humaniseOperationID(c.in)
		if got != c.out {
			t.Errorf("humaniseOperationID(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

// --- extractPathParamNames unit tests ---

func TestExtractPathParamNames(t *testing.T) {
	cases := []struct {
		in  string
		out []string
	}{
		{"/users/{id}", []string{"id"}},
		{"/users/{user_id}/posts/{post_id}", []string{"user_id", "post_id"}},
		{"/health", nil},
		{"/items/{name:int}", []string{"name"}},
	}
	for _, c := range cases {
		got := extractPathParamNames(c.in)
		var names []string
		for k := range got {
			names = append(names, k)
		}
		if len(names) != len(c.out) {
			t.Errorf("extractPathParamNames(%q) count = %d, want %d (names=%+v)", c.in, len(names), len(c.out), names)
			continue
		}
		// Check every expected name is present
		exp := make(map[string]bool)
		for _, n := range c.out {
			exp[n] = true
		}
		for _, n := range names {
			if !exp[n] {
				t.Errorf("extractPathParamNames(%q) contains unexpected %q", c.in, n)
			}
		}
	}
}

// --- pyproject.toml parser unit tests ---

func TestParsePyprojectMetadata(t *testing.T) {
	got := parsePyprojectMetadata(`
[build-system]
requires = ["hatchling"]

[project]
name = "my-svc"
version = "2.0.0"
description = "Short description"   # with trailing comment
readme = "README.md"

[tool.ruff]
name = "ignored"
`)
	want := ProjectMetadata{
		Name:        "my-svc",
		Version:     "2.0.0",
		Description: "Short description",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsePyprojectMetadata() = %+v, want %+v", got, want)
	}
}
