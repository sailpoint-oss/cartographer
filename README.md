# Cartographer

Cartographer extracts OpenAPI documents from Go, Java, and TypeScript services. It is the public extraction surface in the toolchain: a CLI, a reusable Go library, and a GitHub Action for service-local generation.

The repository is intentionally focused on per-service extraction. Service teams decide how and where to publish the generated spec in their own repositories and CI workflows.

## Toolchain role

- `tree-sitter-openapi` owns syntax grammars and bindings.
- `navigator` owns OpenAPI and Arazzo parsing, indexes, pointers, and shared document validation.
- `barrelman` owns lint rules and diagnostic execution.
- `cartographer` owns service-local source extraction.
- `barometer` owns runtime contract execution.

## Install

```bash
git clone https://github.com/sailpoint-oss/cartographer.git
cd cartographer
make build
```

The binary is written to `cartographer/cartographer`.

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
- single-service extraction for Go, Java, and TypeScript
- service-local shaping such as `pathRewrites`, `excludePaths`, and `servers`
- writing the final spec document

For CLI-aligned behavior from Go callers, prefer `extraction.ExtractProject(...)` and then `result.Write()`. That path resolves `--root`-style paths, loads `.cartographer/cartographer.yaml`, applies service-local shaping, and preserves the same default output behavior as `cartographer extract`.

## `.cartographer/cartographer.yaml`

Cartographer keeps only service-local config in `.cartographer/cartographer.yaml`.

```yaml
service:
  name: "Example Service API"
  description: "OpenAPI extracted from a single service codebase"
  version: "2.0.0"
  language: "java"
  template: "atlas-boot"
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
```

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
cd cartographer && go test ./...
```

For local multi-repo development, prefer a short-lived `go.work` instead of long-lived `replace` directives:

```bash
go work init ./cartographer ../your-consumer
go work use ../navigator ../barrelman
```

