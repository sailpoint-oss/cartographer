# Leak Guard

Pre-merge gate that scans files for tokens matching an internal-sensitivity
filter and for shape-only patterns that catch common classes of accidental
disclosure (private-key blocks, JWTs, RFC 1918 addresses, etc.).

This directory is **fully self-contained**:

| File | Purpose |
|------|---------|
| `bloom.bin` | Salted Bloom filter; opaque. Cannot be reversed to enumerate the underlying list. |
| `salt.bin` | 16-byte random salt used to seed the filter hashes. |
| `patterns.yaml` | Shape-only regexes (no internal vocabulary). |
| `allow.txt` | Generic vocabulary the filter should ignore. |
| `check/` | Standalone Go program that runs the scan. |
| `hooks/pre-push` | Optional developer-side git hook. |
| `../workflows/leak-guard.yml` | PR-blocking CI workflow. |

## Running locally

```bash
cd .github/leak-guard/check
go run . -root ../../..
```

Add `-verbose` for the maintainer-only output that includes the literal token
matched. The default output redacts the token so failed-PR CI logs do not echo
sensitive substrings.

## Installing the pre-push hook

```bash
./scripts/install-leak-guard-hooks.sh
```

To bypass the hook for a single push (e.g. on a machine without Go):

```bash
HOOKS_BYPASS=1 git push
```

## Refreshing the filter

The filter is built privately and a new version arrives as a binary-only PR
from the leak-guard automation. Do not hand-edit these files; if you believe
a generic identifier is being false-flagged, add it to `allow.txt` and
mention this in the PR description.
