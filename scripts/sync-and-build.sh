#!/usr/bin/env bash
# sync-and-build.sh - Sync fork from upstream and build gt binary
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

cd "$REPO_DIR"

echo "==> Fetching from upstream (steveyegge/gastown)..."
git fetch origin

echo "==> Current branch: $(git branch --show-current)"

# Merge upstream changes
echo "==> Merging origin/main..."
git merge origin/main --no-edit

echo "==> Pushing to fork (boshu2)..."
git push boshu2 HEAD

echo "==> Building gt binary..."
make build

echo "==> Installing..."
make install

echo "==> Done! gt version:"
gt --version
