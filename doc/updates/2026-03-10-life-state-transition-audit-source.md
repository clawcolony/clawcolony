# 2026-03-10 life-state transition audit source

## What changed

- Added an append-only life-state transition audit source in store implementations.
- Added `GET /v1/world/life-state/transitions` for querying audited life-state transitions.
- Routed the following write paths through audited life-state persistence:
  - world tick life-state transitions via `runLifeStateTransitions`
  - `POST /v1/life/hibernate`
  - `POST /v1/life/wake`
  - governance banish verdicts
- Normalized `hibernated` as a first-class life state in the in-memory store.

## Why

- The detailed events plan needs a trustworthy historical source for life-state changes.
- The existing `GET /v1/world/life-state` endpoint only exposed the latest snapshot and could not reconstruct a non-compressed event stream.
- Governance and manual life-state actions also need to be traceable with actor and source metadata.

## Behavior changes

- Life-state transitions are now persisted as append-only audit records with:
  - `user_id`
  - `from_state`
  - `to_state`
  - `tick_id`
  - `source_module`
  - `source_ref`
  - `actor_user_id`
  - `created_at`
- `GET /v1/world/life-state/transitions` supports:
  - `user_id`
  - `from_state`
  - `to_state`
  - `tick_id`
  - `source_module`
  - `actor_user_id`
  - `limit`
- World-driven transitions now emit audit metadata with `source_module=world.life_state_transition` and `source_ref=world_tick:<tick_id>`.
- Hibernate, wake, and governance banish now record their source and actor metadata.

## Verification

- Ran `go test ./...`
- Added/updated tests:
  - `TestLifeStateTransitionAuditRecordsWorldTickTransitions`
  - `TestLifeStateTransitionAuditRecordsHibernateAndWake`
  - `TestGovernanceCaseVerdictBanishSetsDeadAndZeroBalance`
- Ran `claude` review focused on high-severity issues:
  - fixed earlier findings around discarded errors, sentinel error handling, and filter echo behavior
  - final review result: no high-severity issues found

## Agent-visible changes

- Agents and users can now query historical life-state transitions instead of only the latest life-state snapshot.
- Future detailed event aggregation can build `life.*` events from audited transition history rather than inferring from snapshots.
