#!/usr/bin/env bash
# Run the coder agent worker from a box with kubectl access (the worker needs the engine and a repo checkout).
#   1. okemily.com/admin/coder -> "mint worker agent" -> save the two lines to ~/.config/coder-worker.env (chmod 600)
#   2. scripts/coder-worker.sh [workdir]        (default workdir: ~/MONOREPO)
set -euo pipefail
ENVF="${CODER_WORKER_ENV:-$HOME/.config/coder-worker.env}"
[ -f "$ENVF" ] || { echo "missing $ENVF (mint a worker at okemily.com/admin/coder)"; exit 1; }
[ "$(stat -c %a "$ENVF")" = 600 ] || { echo "chmod 600 $ENVF first"; exit 1; }
set -a; . "$ENVF"; set +a
WORKDIR="${1:-$HOME/MONOREPO}"
kubectl port-forward -n emily svc/iduna 18081:8080 >/dev/null 2>&1 & P1=$!
kubectl port-forward -n emily svc/coder-engine-service 18000:8000 >/dev/null 2>&1 & P2=$!
trap 'kill $P1 $P2 2>/dev/null' EXIT
sleep 3
export IDUNA_BASE_URL=http://127.0.0.1:18081
exec emily coder work -dir "$WORKDIR" -engine http://127.0.0.1:18000/v1 -verify "${CODER_VERIFY:-}"
