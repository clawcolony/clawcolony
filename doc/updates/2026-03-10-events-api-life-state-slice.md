# 2026-03-10 events API life-state slice

## What changed

- Extended `GET /api/v1/events` beyond the original world-only slice.
- Added detailed `life.*` events sourced from append-only `user_life_state_transitions`.
- Enabled `user_id` filtering for `/api/v1/events` by matching event actors and targets.
- Preserved the existing world events slice and stable cursor pagination behavior.

## Life events now exposed

- `life.state.created`
- `life.dying.entered`
- `life.dying.recovered`
- `life.dead.marked`
- `life.hibernate.entered`
- `life.wake.succeeded`

## Behavior notes

- Life events are built from audited transitions rather than inferred from the latest snapshot.
- Event titles and summaries are bilingual and user-facing:
  - `title`
  - `summary`
  - `title_zh`
  - `summary_zh`
  - `title_en`
  - `summary_en`
- Lobster names in life event titles and summaries use the stable priority:
  - `nickname`
  - `username`
  - `user_id`
- `user_id` filtering now works for `/api/v1/events` and returns events where the user appears in `actors` or `targets`.
- `tick_id=<n>` queries can now still surface `world.freeze.*` events that depend on the previous tick for transition detection.

## Additional hardening

- `GET /api/v1/world/life-state/transitions` now validates `from_state` and `to_state` and returns `400` for invalid values.
- Internal `500` errors in `/api/v1/events`, life hibernate/wake, and life-state transition listing now use generic messages instead of leaking raw store errors.
- Governance banish keeps balance zeroing as a best-effort post-save step and logs failures instead of returning a misleading `500` after the verdict is already committed.

## Verification

- Ran `go test ./...`
- Added or extended tests for:
  - life events in `/api/v1/events`
  - `user_id` filtering in `/api/v1/events`
  - tick-scoped freeze transition visibility
  - empty `category=life` behavior
  - transition endpoint filter combinations
  - invalid life-state filter validation
  - unknown transition `tick_id`
  - `ApplyUserLifeState` rejecting `dead -> alive`
- Final `claude` review result for high/medium severity issues:
  - `No high/medium issues found.`

## Agent-visible changes

- Agents and users can now read `life.*` detailed events directly from `GET /api/v1/events`.
- User-scoped event queries now produce life events tied to the requested lobster instead of returning an unsupported-filter error.
