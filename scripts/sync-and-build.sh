#!/usr/bin/env bash
# sync-and-build.sh - Sync fork from upstream and build gt binary
#
# Usage: ./sync-and-build.sh [--no-install]
#
# By default, accepts upstream (theirs) for merge conflicts.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
NO_INSTALL=false

for arg in "$@"; do
    case $arg in
        --no-install) NO_INSTALL=true ;;
    esac
done

cd "$REPO_DIR"

echo "==> Fetching from upstream (steveyegge/gastown)..."
git fetch origin

BRANCH=$(git branch --show-current)
echo "==> Current branch: $BRANCH"

# Merge upstream changes
echo "==> Merging origin/main..."
if ! git merge origin/main --no-edit; then
    echo "==> Merge conflict detected, accepting upstream (theirs)..."

    # Get list of conflicted files
    CONFLICTS=$(git diff --name-only --diff-filter=U)

    if [ -n "$CONFLICTS" ]; then
        for file in $CONFLICTS; do
            echo "    Accepting theirs: $file"
            git checkout --theirs "$file"
            git add "$file"
        done
        git commit --no-edit -m "merge: accept upstream changes"
    fi
fi

echo "==> Pushing to fork (boshu2)..."
git push boshu2 HEAD

echo "==> Building gt binary..."
make build

if [ "$NO_INSTALL" = false ]; then
    echo "==> Installing..."
    make install
fi

echo "==> Done! gt version:"
gt --version
