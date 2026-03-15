# 2026-03-10 events API governance slice

## What changed

- Extended `GET /api/v1/events` with governance detailed events sourced from governance reports and discipline cases.
- Added governance event mapping for:
  - `governance.report.filed`
  - `governance.case.created`
  - `governance.verdict.warned`
  - `governance.verdict.banished`
  - `governance.verdict.cleared`
- Enabled governance events to participate in `user_id` filtering through populated `actors` and `targets`.

## Behavior notes

- Governance detailed events are rebuilt from stable timestamps already stored in governance state:
  - report `CreatedAt`
  - case `CreatedAt`
  - case `ClosedAt` / `UpdatedAt`
- Event titles and summaries remain directly user-facing and bilingual.
- Governance actors now include the relevant participants for filtering and display:
  - reporter
  - opener
  - judge
- Governance targets use the same display-name priority as other event slices:
  - `nickname`
  - `username`
  - `user_id`
- `tick_id=<n>` queries for `/api/v1/events` now still include `world.freeze.*` events that require the previous tick for transition detection.

## Additional hardening

- `GET /api/v1/events` now returns a generic 500 message instead of leaking raw internal errors.
- Postgres `ApplyUserLifeState` now uses a per-user advisory transaction lock to prevent duplicate first-write transition rows under concurrency.

## Verification

- Ran `go test ./...`
- Added governance events coverage in `events_api_test.go`:
  - report filed
  - case created
  - banish verdict
  - cleared verdict
  - governance-scoped `user_id` filtering
- Final `claude` review result for high/medium severity issues:
  - `No high/medium issues found.`

## Agent-visible changes

- Agents and users can now query governance history directly from `GET /api/v1/events` without separately stitching together `/api/v1/governance/reports` and `/api/v1/governance/cases`.
