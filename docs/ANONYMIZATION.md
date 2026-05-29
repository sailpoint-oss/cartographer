# Anonymization policy

Cartographer is a **public** repository. Committed sources — especially
`extract/testdata/**`, inline `*_test.go` strings, and README or config
examples — must read as **fictional** and must not tie fixtures to a specific
production service estate.

## Principles

1. **Fictional packages and hosts** — Java under `com.example`, Go imports under
   `example.com/…`, generic API titles like "Example Service API".
2. **Generic domain language** — DTOs, paths, topics, and scopes should look
   like textbook REST/event examples (`OrderDto`, `/api/v1/orders`, `api:resource:read`).
3. **No fleet inventory** — do not embed real service slugs, gateway routes,
   private repo paths, team channels, or orchestration metadata from internal pipelines.
4. **No internal auth-model build in this repo** — cloning or parsing
   the upstream authorization model (or any private rights catalog) belongs
   in the private consumer toolchain, not Cartographer. This repo only
   **loads** a JSON mapping path supplied by the consumer and ships a
   fictional embedded fixture for unit tests.
5. **No internal product names in committed sources** — use neutral terms such as
   "service rights", `rightsMappingPath`, and `api:resource:action` examples.
6. **Consumer vs contributor** — extraction may recognize patterns that appear
   in downstream codebases, but Cartographer's own tests and golden files stay
   anonymous. Document behavior in neutral terms; do not copy internal fixtures.

## Allowed in this repo

- Module path `github.com/sailpoint-oss/cartographer` and other public OSS URLs
- Copyright header `Sailpoint Technologies, Inc.`
- Public toolchain names documented in README
- Ordinary OpenAPI vocabulary (`users`, `workgroups`, `/internal/**` path segments)

## Golden files

When changing extraction output, refresh snapshots locally (not in CI):

```bash
go test ./extract -run TestGoldenSpecs -update -count=1
```

Committed goldens under `extract/testdata/golden/e2e/` must use the same fictional
packages and scopes as other testdata. Do not paste production service specs into
golden YAML.

## Before you open a PR

- Scan new testdata and test strings: would a reader infer a real internal service?
- Search for banned tokens in new/changed files: internal auth acronyms, production
  scope prefixes, and fleet service slugs.
- Prefer extending existing `com.example` / `example.com` fixtures over pasting
  snippets from private repos.
- Keep YAML and comment examples aligned with the generic samples in README.

See also [CLAUDE.md](../CLAUDE.md) working boundaries.
