# Repository boundaries

Cartographer is a **public, org-neutral** extraction library and CLI. It turns **one service codebase** into OpenAPI using generic patterns only.

## Cartographer owns

- Per-language extractors under `extract/`
- OpenAPI assembly (`extract/sharedspec`, `extract/specgen`)
- Framework-preset auth detection (`sharedspec.IsAuthFunctionName`): any call or annotation whose simple name matches the generic Spring Security, JAX-RS, FastAPI, NestJS, ASP.NET, or "Require* / Authorize / CheckScope" naming family has its string-literal arguments emitted as standard OAuth2 scope requirements
- `cartographer extract` / `init` and the extraction GitHub Action

## Cartographer must not own

- Fleet service inventory (catalog snapshots, gateway fleet lists)
- Cloning or syncing many service repos
- Building production right→scope tables from any upstream authorization model
- Translating extracted auth tokens into anything other than raw OAuth2 scope strings (right→scope translation, mapping JSON load, "minimal scope set" set-cover logic, etc. — all of that lives downstream)
- Post-extraction rewrite/reconcile/gateway projection
- Committed specs for real internal services
- Vendor-specific type catalogs (the public extractor emits stub-marked placeholders; consumers resolve them via overlay)
- Vendor-specific error envelope shapes (same pattern as type stubs)
- Vendor-branded lint rules or OpenAPI extension names

## Output contract

Cartographer's output uses only the standard OpenAPI surface plus a single neutral extension:

| Surface | Purpose |
|---------|---------|
| `security` block on every operation | Auth requirements extracted from source, surfaced as `oauth2` scope tokens. No vendor branding, no translation. |
| `components.securitySchemes.oauth2` | Union of every scope token observed across operations. Token URL defaults to a fictional placeholder; downstream tooling typically rewrites it. |
| `x-source.stubName` | Marker on placeholder schemas that downstream consumers may resolve to a concrete shape. Source metadata is grouped under the single `x-source` object. |

Cartographer no longer emits any auth-flavoured extension. Right-vs-scope translation, vendor-specific stub catalogues, and branded extension renames live in consumer overlays.

## Test and doc policy

See [ANONYMIZATION.md](./ANONYMIZATION.md). Testdata uses `com.example`, `example.com`, and `api:resource:action` scope examples — never fleet slugs or production resource IDs.

## Allowed dependencies

- Public OSS module path `github.com/sailpoint-oss/cartographer`
- `github.com/sailpoint-oss/navigator` for spec normalization
- Documenting that downstream orchestration lives in a private consumer toolchain (no fleet paths in code)

## Stable import surface for downstream tooling

The following subpackages are part of the **stable downstream contract** and may be imported by Telescope's generation adapter, an IDE LSP, or a private orchestration pipeline:

- `github.com/sailpoint-oss/cartographer/extraction` — high-level entry point
- `github.com/sailpoint-oss/cartographer/sourcemap` — sourcemap reader/writer
- `github.com/sailpoint-oss/cartographer/sourceloc` — source location types
- `github.com/sailpoint-oss/cartographer/extract/extractionopts` — extractor option types

Every other subpackage under `extract/*` and everything under `cmd/` is implementation detail. Downstream consumers must NOT import them; new dependencies are accepted only by extending this list explicitly.

## Leak-guard

Cartographer ships `.github/leak-guard/` (Bloom filter + shape-only patterns + check tool). PR CI fails on any hit. The denylist itself lives only in the downstream private orchestrator that produced the artefacts; the public repo cannot enumerate the underlying terms.
