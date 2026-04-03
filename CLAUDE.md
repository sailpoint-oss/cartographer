# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## Project overview

**Cartographer** is the public extraction-side repository in the OpenAPI toolchain. It owns service-local extraction:

- `cartographer extract`
- `cartographer init`
- the public GitHub Action at `action.yml`
- the reusable extraction API in `cartographer/extraction`
- language-specific extraction internals in `cartographer/extract`

## Build and test commands

```bash
make build   # Build cartographer binary (output: cartographer/cartographer)
make test    # Run all Go tests: cd cartographer && go test ./...
```

Single-service extraction examples:

```bash
make extract-go ROOT=../example-go-service TITLE="Example Go Service"
make extract-java ROOT=../example-java-service TITLE="Example Java Service"
make extract-ts ROOT=../example-ts-service TITLE="Example TS Service"
```

Run a single Go test:

```bash
cd cartographer && go test ./extract/goextract/ -run TestFunctionName -v
```

Local multi-repo development (optional — `go.work` is gitignored):

```bash
go work init ./cartographer ../your-consumer
go work use ../navigator ../barrelman
```

## Architecture

### Go module and entry point

- Module: `github.com/sailpoint-oss/cartographer`
- Entry: `cartographer/main.go` -> `cmd.Execute()`
- Build injects version/commit/date via ldflags

### Package layout (`cartographer/`)

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra CLI commands for `extract`, `init`, `completion`, and `version` |
| `extraction/` | Stable extraction API used by Cartographer and downstream callers |
| `extract/` | Language-specific extractors |
| `extract/goextract/` | Go extraction via `go/ast` + `go/types` |
| `extract/javaextract/` | Java extraction via tree-sitter |
| `extract/tsextract/` | TypeScript extraction via tree-sitter |
| `extract/parser/` | Tree-sitter query files |
| `extract/specgen/` | OpenAPI spec generation from extracted route/schema data |

### Key configuration

- `.cartographer/cartographer.yaml` — service-local extraction config

### Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/tree-sitter/go-tree-sitter` — Java/TypeScript parsing
- `golang.org/x/tools` — Go AST analysis
- `charmbracelet/log` — structured logging

## Working boundaries

- Do not reintroduce orchestration packages into this repo.
- Keep `.cartographer/cartographer.yaml` service-local.

## CI/CD

- `.github/workflows/cartographer.yml` — build and test extraction-only Cartographer
- `.github/workflows/pr-check.yml` — PR checks for extraction CLI and action changes
- `.github/workflows/openapi-release.yml` — auto-cut a patch release from `main`, create the GitHub Release, and upload multi-platform extraction CLI archives plus checksums
- `action.yml` — extraction-focused GitHub Action for service repos

GitHub Releases are the public publishing surface for the Cartographer CLI, and the root `action.yml` is the public GitHub Action surface for service repos.
