#!/usr/bin/env bash
set -euo pipefail

ROUNDS="${ROUNDS:-3}"
SLEEP_SECONDS="${SLEEP_SECONDS:-2}"

log() { printf '[genesis-dialog-stress] %s\n' "$*"; }

if ! [[ "${ROUNDS}" =~ ^[0-9]+$ ]] || (( ROUNDS <= 0 )); then
  log "invalid ROUNDS=${ROUNDS}"
  exit 1
fi

pass=0
for ((i=1; i<=ROUNDS; i++)); do
  log "round ${i}/${ROUNDS} start"
  if scripts/genesis_real_agent_dialog_actions.sh; then
    pass=$((pass+1))
    log "round ${i}/${ROUNDS} PASS"
  else
    log "round ${i}/${ROUNDS} FAIL"
    exit 1
  fi
  if (( i < ROUNDS )); then
    sleep "${SLEEP_SECONDS}"
  fi
done

log "PASS rounds=${pass}/${ROUNDS}"
