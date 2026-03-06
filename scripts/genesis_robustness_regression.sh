#!/usr/bin/env bash
set -euo pipefail

log() { printf '[genesis-robustness] %s\n' "$*"; }

PATTERN='TestKBAutoProgressDiscussingNoEnrollmentRejects|TestKBAutoProgressDiscussingStartsVote|TestGovernanceDisciplineAndReputationFlow|TestGovernanceCaseVerdictBanishSetsDeadAndZeroBalance|TestWorldTickExtinctionFreeze|TestWorldTickMinPopulationRevivalAutoRegistersUsers|TestToolInvokeExecModeUsesSandboxRunner|TestToolSandboxProfileTierPolicy|TestToolInvokeURLPolicyByTier|TestGenesisBootstrapAndMetabolismAndNPC'

log "run targeted robustness tests"
go test ./internal/server -run "${PATTERN}" -count=1

log "run full server tests"
go test ./internal/server -count=1

log "PASS"
