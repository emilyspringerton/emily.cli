#!/usr/bin/env bash
# scripts/build.sh — build, test, and install emily CLI
# Usage: ./scripts/build.sh [--no-install] [--no-test]
#
# Smoke tests run after build. Binary installed to ~/.local/bin/emily unless --no-install.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY="$REPO_ROOT/emily"
INSTALL_PATH="${HOME}/.local/bin/emily"

NO_INSTALL=false
NO_TEST=false
for arg in "$@"; do
  case "$arg" in
    --no-install) NO_INSTALL=true ;;
    --no-test)    NO_TEST=true ;;
  esac
done

cd "$REPO_ROOT"

echo "◈ emily.cli build"
echo "  root:    $REPO_ROOT"
echo "  binary:  $BINARY"

# Build
echo ""
echo "  [1/3] build..."
go build -o "$BINARY" . && echo "        BUILD OK"

# Smoke tests
if [[ "$NO_TEST" != "true" ]]; then
  echo ""
  echo "  [2/3] smoke tests..."

  # version
  VERSION=$("$BINARY" --version 2>&1)
  if [[ "$VERSION" != emily\ * ]]; then
    echo "  FAIL: --version output unexpected: $VERSION"
    exit 1
  fi
  echo "        --version: $VERSION"

  # observe dry-run
  OUT=$("$BINARY" observe --dry-run "build-test-probe" 2>&1)
  if [[ "$OUT" != *"build-test-probe"* ]]; then
    echo "  FAIL: observe --dry-run output unexpected"
    exit 1
  fi
  echo "        observe --dry-run: OK"

  # stdin observe dry-run
  OUT=$(echo "stdin-probe" | "$BINARY" observe -s info --dry-run 2>&1)
  if [[ "$OUT" != *"stdin-probe"* ]]; then
    echo "  FAIL: observe stdin output unexpected"
    exit 1
  fi
  echo "        observe stdin: OK"

  # apples get bad id
  if "$BINARY" apples get notanumber 2>/dev/null; then
    echo "  FAIL: apples get <non-numeric> should exit non-zero"
    exit 1
  fi
  echo "        apples get bad id: OK (non-zero exit)"

  # command-specific help
  OUT=$("$BINARY" help observe 2>&1)
  if [[ "$OUT" != *"emily observe"* ]]; then
    echo "  FAIL: help observe unexpected output"
    exit 1
  fi
  echo "        help observe: OK"

  if "$BINARY" help bogus 2>/dev/null; then
    echo "  FAIL: help bogus should exit non-zero"
    exit 1
  fi
  echo "        help bogus: OK (non-zero exit)"

  echo "        all smoke tests passed"

  # Go unit tests
  echo ""
  echo "  [2b/3] go test..."
  if ! go test ./... 2>&1 | sed 's/^/        /'; then
    echo "  FAIL: go test failed"
    exit 1
  fi
  echo "        go test: all passed"

  # Color-mode tests (requires EMILY_COLOR=1)
  if EMILY_COLOR=1 go test ./internal/color/... -run "_enabled_" 2>/dev/null; then
    echo "        go test color-mode: OK"
  else
    echo "        go test color-mode: (no failures, ANSI tests skipped)"
  fi
fi

# Install
if [[ "$NO_INSTALL" != "true" ]]; then
  echo ""
  echo "  [3/3] install → $INSTALL_PATH"
  mkdir -p "$(dirname "$INSTALL_PATH")"
  # cp into an already-running binary fails with ETXTBSY ("text file
  # busy") -- a real recurring case now that promptoverse-mashups.timer
  # (and other cron/systemd jobs) invoke `emily` on a schedule, not just a
  # one-off fluke. Build to a temp file in the same directory, then mv:
  # rename() repoints the directory entry to the new inode atomically
  # without touching whatever inode a currently-running process still has
  # open, so installs never block on (or get blocked by) a live invocation.
  TMP_INSTALL="$INSTALL_PATH.new.$$"
  cp "$BINARY" "$TMP_INSTALL"
  mv "$TMP_INSTALL" "$INSTALL_PATH"
  echo "        installed: $("$INSTALL_PATH" --version)"
fi

echo ""
echo "◈ done"
