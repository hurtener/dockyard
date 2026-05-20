#!/usr/bin/env bash
# Install Dockyard's git hooks into this clone. Run once per clone.
set -euo pipefail
cd "$(dirname "$0")/.."

hookdir=$(git rev-parse --git-path hooks)
cp scripts/hooks/pre-commit "$hookdir/pre-commit"
chmod +x "$hookdir/pre-commit"
echo "installed pre-commit hook -> $hookdir/pre-commit"
