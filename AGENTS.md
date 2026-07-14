# AGENTS.md

This file is the canonical agent context for Cartographer. Tools that look for `CLAUDE.md` will find a one-line redirect pointing here.

## Project overview

**Cartographer** is the public, org-neutral extraction repository in the OpenAPI toolchain. It owns service-local extraction:

- `cartographer extract`
- `cartographer init`
- the public GitHub Action at `action.yml`
- the reusable extraction API in `extraction/`
- language-specific extraction internals in `extract/`

Cartographer emits generic OpenAPI with neutral `x-source.stubName` placeholders on stub schemas, and standard `security.oauth2` requirements for every auth-requirement string it finds in source. It ships no vendor-branded vocabulary, no auth-flavoured extensions of any kind, and no right→scope mapping logic. Downstream consumers may layer their own naming, stub catalogues, and rights translation on top during a post-extract overlay step.

## Build and test commands

```bash
make build   # Build cartographer binary (output: ./cartographer)
make test    # Run all Go tests: go test ./...
```

Single-service extraction examples:

```bash
make extract-go ROOT=../example-go-service TITLE="Example Go Service"
make extract-java ROOT=../example-java-service TITLE="Example Java Service"
make extract-ts ROOT=../example-ts-service TITLE="Example TS Service"
```

Run a single Go test:

```bash
go test ./extract/goextract/ -run TestFunctionName -v
```

Local multi-repo development (optional — `go.work` is gitignored):

```bash
go work init . ../your-consumer
go work use ../navigator ../barrelman
```

## Architecture

### Go module and entry point

- Module: `github.com/sailpoint-oss/cartographer`
- Entry: `main.go` → `cmd.Execute()`
- Build injects version/commit/date via ldflags

### Package layout (repository root)

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra CLI commands for `extract`, `init`, `completion`, and `version` |
| `extraction/` | Stable extraction API used by Cartographer and downstream callers |
| `extract/` | Language-specific extractors |
| `extract/goextract/` | Go extraction via `go/ast` + `go/types` |
| `extract/javaextract/` | Java extraction via tree-sitter (Spring MVC, JAX-RS, interface/nested controllers) |
| `extract/tsextract/` | TypeScript extraction via tree-sitter |
| `extract/pythonextract/` | Python extraction (FastAPI / Flask-style routes) |
| `extract/csharpextract/` | C# extraction (minimal APIs / MVC controllers) |
| `extract/extractionopts/` | Shared code-derived extraction options |
| `extract/sharedspec/` | Cross-language OpenAPI assembly + the framework-neutral auth-function name set (`AuthFunctionNames`, `IsAuthFunctionName`) |
| `extract/parser/` | Tree-sitter query files |
| `extract/specgen/` | OpenAPI spec generation from extracted route/schema data |
| `extract/authscope/` | Standard OAuth2 security-block emission + Spring `ROLE_` normalisation |
| `sourcemap/` | Sourcemap reader/writer |
| `sourceloc/` | Shared source location types |
| `extraction/canonical.go` | In-repo OpenAPI/Swagger passthrough |

### Stable downstream import surface

Downstream consumers (Telescope's generation adapter, IDEs, private orchestration pipelines) may import:

- `github.com/sailpoint-oss/cartographer/extraction`
- `github.com/sailpoint-oss/cartographer/sourcemap`
- `github.com/sailpoint-oss/cartographer/sourceloc`
- `github.com/sailpoint-oss/cartographer/extract/extractionopts`

Anything else under `extract/*` and everything under `cmd/` is implementation detail. New downstream-stable packages should be added to this list explicitly.

### Key configuration

- `.cartographer/cartographer.yaml` — service-local extraction config

### Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/sailpoint-oss/navigator` — OpenAPI parsing and document validation
- `github.com/tree-sitter/go-tree-sitter` — Java/TypeScript parsing
- `golang.org/x/tools` — Go AST analysis
- `charmbracelet/log` — structured logging

## Working boundaries

- Do not reintroduce orchestration packages into this repo.
- Do not add upstream-authorization-model checkout/build tooling here — Cartographer does not load any consumer mapping JSON, does not translate rights to scopes, and does not know what a "right" is. Auth extraction surfaces raw strings as standard OAuth2 scopes; downstream consumers own any translation.
- Keep `.cartographer/cartographer.yaml` service-local.
- **Public-repo anonymization** — Cartographer must not contain internal service identifiers, fleet inventory, or fixtures copied from private codebases. All `extract/testdata/**`, inline tests, and examples use fictional `com.example` / `example.com` material and generic API vocabulary. See [docs/ANONYMIZATION.md](docs/ANONYMIZATION.md).
- Auth detection is framework-preset only. The shared name set (`sharedspec.AuthFunctionNames` / `IsAuthFunctionName`) covers the common Spring Security, JAX-RS, FastAPI, NestJS, ASP.NET, and generic "Require* / Authorize / CheckScope" conventions. No per-service config knob.
- Auth output uses the standard `security.oauth2` block. No vendor-flavoured auth extensions are emitted from this repository under any circumstances. Branded names belong in a downstream overlay, not here.
- Stub schemas (`extract/sharedspec.GenerateStubSchema`) emit only universal placeholders plus `x-source.stubName`. Vendor-specific shapes (IAM reference types, legacy error envelopes) are resolved by a consumer overlay, not here.

## Leak guard

`.github/leak-guard/` ships a salted-bloom filter + shape-only patterns + a small standalone Go scanner. PR CI fails on any hit. The denylist itself lives only in the downstream private orchestrator that produced the artefacts; this repo cannot enumerate the underlying terms.

To install the pre-push hook locally:

```bash
./scripts/install-leak-guard-hooks.sh
```

## CI/CD

- `.github/workflows/cartographer.yml` — build and test extraction-only Cartographer
- `.github/workflows/pr-check.yml` — PR checks for extraction CLI and action changes
- `.github/workflows/openapi-release.yml` — auto-cut a patch release from `main`, create the GitHub Release, and upload multi-platform extraction CLI archives plus checksums
- `.github/workflows/leak-guard.yml` — required leak-guard check on every PR
- `action.yml` — extraction-focused GitHub Action for service repos

GitHub Releases are the public publishing surface for the Cartographer CLI, and the root `action.yml` is the public GitHub Action surface for service repos.

## Related repositories

This repo is part of a six-repo OpenAPI toolchain:

- [tree-sitter-openapi](https://github.com/sailpoint-oss/tree-sitter-openapi) — grammar and tree-sitter bindings
- [navigator](https://github.com/sailpoint-oss/navigator) — parse, index, `$ref` resolution, document validation
- [barrelman](https://github.com/sailpoint-oss/barrelman) — generic OpenAPI lint rules and plug-in surface
- [telescope](https://github.com/sailpoint-oss/telescope) — VS Code extension, language server, and CLI built on the above
- [barometer](https://github.com/sailpoint-oss/barometer) — live HTTP contract testing and Arazzo runner

## Cursor Cloud specific instructions

Cartographer is a batch Go CLI (module `github.com/sailpoint-oss/cartographer`) — there is no long-running server, database, or dev server to keep alive. "Running the app" means building the `cartographer` binary and invoking an extraction. Standard commands live in the **Build and test commands** section above; a few non-obvious notes:

- **Toolchain:** Requires Go 1.25.x (see `go.mod`). The startup update script runs `go mod download`; a plain `make build` also fetches deps.
- **Lint/CI gate:** CI (`.github/workflows/cartographer.yml`) only gates on `go build` + `go test ./...`. `go vet ./...` is clean, but `gofmt -l .` reports many pre-existing unformatted files — this is expected, is NOT a CI gate, and you should NOT run `gofmt -w` broadly to "fix" it.
- **Quick end-to-end smoke test** (no external service needed — fixtures are bundled): `./cartographer extract --lang go --root extract/testdata/go-http --title "Go HTTP Demo" --output /tmp/go-http-openapi.yaml`. Other languages have fixtures under `extract/testdata/` (`python-fastapi`, `csharp-minimal`, `java-*`, `ts-*`).
- **Nested module:** `extract/testdata/go-http/` has its own `go.mod`; `go test ./...` from the repo root intentionally does not descend into it.
- Golden snapshots live in `extract/testdata/golden/e2e/`; refresh only via `make test-golden-update` when output changes are intended.
