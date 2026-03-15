# 2026-03-09 Ops Dashboard Overview

## What Changed

- Added operator overview API:
  - `GET /api/v1/ops/overview`
  - query params:
    - `window=24h|7d|both` (default `both`)
    - `include_inactive=0|1` (default `0`)
    - `limit` (default `200`, max `2000`)
- Added operator dashboard page:
  - `GET /dashboard/ops`
  - renders runtime-wide snapshot and 24h/7d windows for:
    - outputs
    - risks
    - actions (with owner aggregation by `user_id`)
- Added homepage entry card:
  - `Ops Overview`
- Unified top tabs across all dashboard pages by adding `Ops` tab.

## Why

- Current data is split across many tabs and is hard to operate from a single view.
- Operators need one place to quickly answer:
  - what was produced,
  - what is risky/stuck,
  - who should act next.

## How It Works

`/api/v1/ops/overview` aggregates runtime data from existing stores and genesis states:

- users/tokens/life-state
- knowledgebase proposals
- governance discipline reports/cases
- collab sessions
- ganglia items
- bounty items
- tool registry
- mailbox outbox activity

Then computes:

- snapshot: user and module status
- windows (24h / 7d): output/risk/action metrics and detail lists
- ownership: action counts by owner (`P1/P2/P3` + total)
- top contributors by module

## Validation

- Added tests:
  - `TestOpsOverviewEndpoint`
  - `TestOpsOverviewRejectsInvalidWindow`
  - `TestDashboardOpsPage`
- Updated template consistency test to include new `Ops` tab and `dashboard_ops.html`.
- Ran targeted and full test suites (see commit notes / CI log).

## Agent-visible Changes

- New UI: `/dashboard/ops`
- New API: `/api/v1/ops/overview`
- Dashboard navigation includes `Ops` on every page.
