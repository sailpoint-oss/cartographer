# Cartographer

Cartographer extracts OpenAPI documents from Go, Java, TypeScript, Python, and C# services. It is the public extraction surface in the toolchain: a CLI, a reusable Go library, and a GitHub Action for service-local generation.

The repository is intentionally focused on per-service extraction. Service teams decide how and where to publish the generated spec in their own repositories and CI workflows.

## Toolchain role

- `tree-sitter-openapi` owns syntax grammars and bindings.
- `navigator` owns OpenAPI and Arazzo parsing, indexes, pointers, and shared document validation.
- `barrelman` owns generic OpenAPI lint rules and a `RulePack` plug-in surface so downstream consumers can attach their own rule packs.
- `cartographer` owns service-local source extraction. It emits generic OpenAPI plus neutral `x-source` metadata and ships no vendor-branded vocabulary; consumers layer their own naming and stub catalogs on top.
- `telescope` owns spec-side linting, LSP/editor UX, and the in-process generation loop that wraps cartographer extraction.
- `barometer` owns runtime contract execution.

## Install

```bash
git clone https://github.com/sailpoint-oss/cartographer.git
cd cartographer
make build
```

The binary is written to `./cartographer` at the repository root (the clone directory is commonly named `cartographer`).

## Quick start

From a service repository:

```bash
cartographer init
cartographer extract
```

That creates `.cartographer/cartographer.yaml` and writes the generated spec to `.cartographer/openapi.yaml`.

## Commands

```text
cartographer extract
cartographer init
cartographer completion [bash|zsh|fish|powershell]
cartographer version
```

## Extraction library

The stable library surface for downstream callers lives in `github.com/sailpoint-oss/cartographer/extraction`.

It owns:

- `.cartographer/cartographer.yaml` parsing
- language/template detection
- single-service extraction for Go, Java, TypeScript, Python, and C#
- in-repo canonical OpenAPI/Swagger passthrough when configured
- service-local shaping such as `pathRewrites`, `excludePaths`, and `servers`
- code-derived extraction options under `service.extraction` (error schema, signature pagination types, co-located OpenAPI merge)
- writing the final spec document

For CLI-aligned behavior from Go callers, prefer `extraction.ExtractProject(...)` and then `result.Write()`. That path resolves `--root`-style paths, loads `.cartographer/cartographer.yaml`, applies service-local shaping, and preserves the same default output behavior as `cartographer extract`.

## Code-derived extraction

Cartographer emits only contract detail that source code supports:

- response headers from method bodies, annotations, resolved constants, and inherited helpers
- error schemas from `@ControllerAdvice`, typed return types, and handler signatures — not blanket 400/401/403/500 stubs
- pagination parameters from signature types and indexed DTO fields when configured — not from query-param name heuristics alone
- RFC 7807 ProblemDetails only when source references those types

Optional `service.extraction` settings opt into legacy shapes or hybrid-repo merging when needed.

## Service templates

Auto-detected template labels are written to `info.x-service-template`:

| Language | Template |
| --- | --- |
| Go | `go-web` |
| Java | `java-spring` |
| TypeScript (NestJS) | `typescript-node` |
| Python | `python-fastapi` |
| C# | `csharp-web` |

## `.cartographer/cartographer.yaml`

Cartographer keeps only service-local config in `.cartographer/cartographer.yaml`.

```yaml
service:
  name: "Example Service API"
  description: "OpenAPI extracted from a single service codebase"
  version: "2.0.0"
  language: "java"
  template: "java-spring"
  team: "API Platform"
  slack: "#api-platform"
  contact:
    name: "API Platform"
    email: "platform@example.com"
  license:
    name: "MIT"
  termsOfService: "https://example.com/terms"
  servers:
    - url: "https://{stage}.api.example.com"
      description: "Production API"
      variables:
        stage:
          description: "Deployment stage"
          default: "prod"
  pathRewrites:
    - from: /internal
      to: /api
  excludePaths:
    - /debug/**
    - /internal/**
  extraction:
    # Optional code-derived behavior (defaults emit only what source declares):
    errorSchema: legacy-error-response  # or problem-details when handlers reference RFC 7807 types
    signaturePaginationTypes: []        # expand indexed DTO fields when type appears in a method signature
    mergeCoLocatedOpenAPI: false        # merge in-repo OpenAPI path fragments for hybrid repos
```

## Auth extraction

Cartographer surfaces every auth-requirement string it finds in source as a standard OpenAPI `security.oauth2` scope on the operation. There is no translation, no consumer-supplied mapping JSON, and no vendor-flavoured `x-*` extension. Downstream consumers decide what each token means: tokens that are actually rights, permissions, or role names can be remapped to PAT scopes in a post-extract overlay step the consumer owns.

### What gets matched

Auth requirements are detected via **framework presets** — a generic name set that covers the auth conventions of the major web frameworks. No per-service configuration is required.

| Language | Patterns detected |
| --- | --- |
| Go | Any call whose function name matches the generic set (`Require*`, `Authorize`, `CheckScope*`, `CheckPermission*`, `HasScope*`, `HasRole*`, `HasPermission*`, `HasAuthority*`, `*AuthMiddleware`, etc.). String-literal and named-string-constant arguments are extracted. The call-graph is traversed cycle-safely from the route handler, so auth checks inside helper functions are found too. |
| Java | Spring Security `@PreAuthorize`, `@Secured`, `@PermitAll`, `@DenyAll`; JAX-RS `@RolesAllowed`. The `hasAuthority(...)` / `hasRole(...)` Spring expression DSL is parsed. |
| Python | FastAPI `Depends(Security(scopes=[...]))`, `Depends(OAuth2PasswordBearer(...))`. |
| TypeScript | NestJS `@UseGuards(...)`, `@Roles(...)`, `@Scopes(...)`, `@ApiBearerAuth`, `@ApiSecurity`, `@ApiOAuth2`, `@SetMetadata('scopes', [...])`. |
| C# | `[Authorize(Policy = "...")]`, `[Authorize(Roles = "...")]`, `[Authorize("...")]`, `.RequireAuthorization("...")`. |

### What gets emitted

For every operation whose source declares one or more auth requirements:

- The operation's `security` is set to `[{oauth2: [<extracted tokens>]}]`.
- Spring Security `ROLE_` prefixes are stripped when the inner token is a colon-delimited auth id.
- `components.securitySchemes.oauth2` is populated with the union of every observed scope and uses the fictional `https://{tenant}.api.example.com/oauth/token` token URL by default.

No vendor extensions are emitted. If your codebase uses an organisation-specific auth helper whose name is not in the generic preset set, cartographer will not surface a security requirement; rename the helper to match the conventions above, or layer the missing pattern into your own post-extract overlay.

**Public repo policy:** Testdata and tests must use fictional fixtures only — no real service inventory or material copied from private codebases. See [docs/ANONYMIZATION.md](docs/ANONYMIZATION.md).

Supported languages: Go, Java (Spring/JAX-RS), TypeScript (NestJS), Python (FastAPI), and C# (minimal APIs / MVC).

## GitHub Action

Cartographer ships a versioned GitHub Action for service repositories:

```yaml
jobs:
  generate:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
      - uses: sailpoint-oss/cartographer@vX.Y.Z
```

That action runs `cartographer extract`, writes `.cartographer/openapi.yaml` by default, respects custom `project-root`, `cartographer-dir`, and `spec-path` inputs, and can optionally commit the result back to the service repository.

Pin the action to a release tag or immutable commit SHA instead of `main`.

## Releases

Cartographer now publishes from `main` automatically.

- Every push to `main` runs the release workflow after build and test pass.
- The workflow cuts the next patch tag, creates a GitHub Release, and uploads versioned CLI archives plus `SHA256SUMS.txt`.
- GitHub Releases are the public distribution surface for the CLI artifacts.
- The GitHub Action lives at the repository root and should be consumed by release tag or immutable commit SHA.

## Local development

```bash
go test ./...
```

### Golden spec snapshots

Full OpenAPI snapshots live under `extract/testdata/golden/e2e/`. The table-driven
`TestGoldenSpecs` in `extract/golden_specs_test.go` compares extractor output against
those YAML files (source locations and diagnostics are stripped for stability).

Update goldens after intentional extractor changes:

```bash
go test ./extract -run TestGoldenSpecs -update -count=1
```

`-update` is refused when `CI` is set. New fixtures must follow
[docs/ANONYMIZATION.md](docs/ANONYMIZATION.md) (`com.example`, `example.com`,
generic scopes such as `api:resource:read`).

For local multi-repo development, prefer a short-lived `go.work` instead of long-lived `replace` directives:

```bash
go work init . ../telescope/server
go work use ../navigator ../barrelman
```

## Related repositories

This repo is part of a six-repo OpenAPI toolchain:

- [tree-sitter-openapi](https://github.com/sailpoint-oss/tree-sitter-openapi) — grammar and tree-sitter bindings
- [navigator](https://github.com/sailpoint-oss/navigator) — parse, index, `$ref` resolution, document validation
- [barrelman](https://github.com/sailpoint-oss/barrelman) — generic OpenAPI lint rules and plug-in surface
- [telescope](https://github.com/sailpoint-oss/telescope) — VS Code extension, language server, and CLI built on the above
- [barometer](https://github.com/sailpoint-oss/barometer) — live HTTP contract testing and Arazzo runner

