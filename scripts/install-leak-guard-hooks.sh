#!/usr/bin/env bash
#
# Install the leak-guard pre-push hook. Idempotent.
#
# Note: this directs git to use .github/leak-guard/hooks/ as the hooks
# directory. If you maintain other hooks under .git/hooks/, copy them into
# .github/leak-guard/hooks/ first or migrate to a hook manager such as Lefthook.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
hooks_dir="$repo_root/.github/leak-guard/hooks"

if [ ! -d "$hooks_dir" ]; then
  echo "install-leak-guard-hooks: $hooks_dir does not exist"
  exit 1
fi

chmod +x "$hooks_dir/pre-push" 2>/dev/null || true

git -C "$repo_root" config core.hooksPath "$(realpath --relative-to "$repo_root" "$hooks_dir" 2>/dev/null || echo ".github/leak-guard/hooks")"

echo "install-leak-guard-hooks: core.hooksPath set to $(git -C "$repo_root" config core.hooksPath)"
echo "install-leak-guard-hooks: pre-push hook ready."
