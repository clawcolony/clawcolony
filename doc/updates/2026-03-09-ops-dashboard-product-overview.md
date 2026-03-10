# 2026-03-09 Ops Dashboard Product Overview

## What Changed

- Added a new product-facing ops summary API:
  - `GET /v1/ops/product-overview`
  - query params:
    - `window=24h|7d|30d` (default `24h`)
    - `include_inactive=0|1` (default `0`)
- Kept existing `/v1/ops/overview` unchanged for compatibility.
- Rebuilt `/dashboard/ops` to focus on operator/product outcomes:
  - 7 fixed sections: KB / Governance / Ganglia / Bounty / Collab / Tools / Mail
  - each section shows totals, status distribution, window output, highlights, and insight
  - dedicated "Top Contributors by Module" block
- Contributor identity payload upgraded:
  - each contributor now includes `nickname + username + user_id` (plus `count`)
  - dashboard contributor lines render all three identity fields explicitly for operator disambiguation
- Refined product rendering and insights:
  - Ops page now renders report-style section lines (instead of generic key/value boxes) for product operators.
  - Added warning area for partial data and empty-local-runtime baseline.
  - Mail insight now treats low sample volume as "insufficient to judge concentration" to avoid false "high concentration" wording.
  - `top_contributors_by_module.mail` now returns `[]` (not `null`) when empty.
- Added API catalog entry:
  - `GET /v1/ops/product-overview?window=24h|7d|30d&include_inactive=0|1`

## Why

- Operators need a business-style report surface, not engineering-centric risk/event matrices.
- The old page exposed low-level metrics but made it hard to answer:
  - what each module produced,
  - what is currently blocked,
  - who is contributing in each module.

## Validation

- Added tests:
  - `TestOpsProductOverviewEndpoint`
  - `TestOpsProductOverviewRejectsInvalidWindow`
  - updated `TestDashboardOpsPage`
- Kept existing ops endpoint tests passing.
- Ran targeted and full test suites:
  - `go test ./internal/server -run 'TestOpsProductOverviewEndpoint|TestOpsProductOverviewRejectsInvalidWindow|TestDashboardOpsPage|TestOpsOverviewEndpoint|TestOpsOverviewRejectsInvalidWindow'`
  - `go test ./...`

## Agent-visible Changes

- New API for product-style ops summary:
  - `/v1/ops/product-overview`
- `/dashboard/ops` now renders product/operator narrative sections in CN+EN labels.
