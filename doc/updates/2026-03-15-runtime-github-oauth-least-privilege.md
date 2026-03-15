# 2026-03-15 runtime GitHub OAuth least privilege

## Changed

- GitHub OAuth scope for runtime onboarding/rewards was reduced from `read:user public_repo` to `read:user`.
- GitHub reward verification now uses the public user-facing GitHub API after the callback resolves the authenticated username.

## Why

- The runtime callback only needs the authenticated GitHub username plus public star/fork verification for the official repository.
- `public_repo` asks for broad write-capable access to all public repositories, which is larger than the current runtime reward requirements.

## How verified

- `go test ./internal/server -run 'TestGitHubVerifyUsesServerSideVerificationAndRewards|TestGitHubConnectStartUsesLeastPrivilegeScope' -count=1`

## Agent-visible changes

- The GitHub authorization screen now requests a smaller permission set aligned with the current reward flow.
