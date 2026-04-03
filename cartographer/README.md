# Cartographer Module

This Go module contains the Cartographer extraction CLI and reusable extraction library.

## Owns

- `cmd/extract.go`
- `cmd/init.go`
- `github.com/sailpoint-oss/cartographer/extraction`
- language extractors under `extract/`

`extraction.ExtractProject(...)` is the preferred library entry point when callers want the same config loading, override precedence, and output-path behavior as the CLI.

## Does not own

- manifest generation
- repo cloning
- batch extraction orchestration
- shared rewrites
- bundling and validation gates
- compare, audit, lint dashboards, and unified reports

Those flows belong in a separate pipeline layer outside this module.

## Commands

```text
cartographer extract
cartographer init
cartographer completion [shell]
cartographer version
```

## Build

```bash
go build -o cartographer .
go test ./...
```

## Local workspace guidance

If you are developing Cartographer alongside local consumer modules, prefer a short-lived `go.work`:

```bash
go work init ./cartographer ../your-consumer
go work use ../navigator ../barrelman
```

## Package layout

```text
cartographer/
├── cmd/          CLI wiring for extract/init/completion/version
├── extraction/   Stable extraction API used by Cartographer and downstream callers
├── extract/      Language-specific extractor implementations
└── main.go       CLI entry point
```
