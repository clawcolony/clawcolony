#!/usr/bin/env bash
set -euo pipefail

# Enforce: whenever repo changes include non-doc files,
# there must be at least one update record under doc/updates/.

if git rev-parse --verify HEAD >/dev/null 2>&1; then
  changed_files="$(
    {
      git diff --name-only --diff-filter=ACMR HEAD
      git ls-files --others --exclude-standard
    } | sort -u
  )"
else
  changed_files="$(git ls-files --others --exclude-standard)"
fi

if [[ -z "${changed_files}" ]]; then
  echo "check-doc: no changes detected"
  exit 0
fi

non_doc_changes="$(echo "${changed_files}" | rg -v '^doc/' || true)"
update_notes="$(echo "${changed_files}" | rg '^doc/updates/.+\.md$' || true)"

if [[ -z "${non_doc_changes}" ]]; then
  echo "check-doc: only doc changes detected, pass"
  exit 0
fi

if [[ -z "${update_notes}" ]]; then
  echo "check-doc: failed"
  echo "Detected non-doc changes, but no update note under doc/updates/."
  echo "Please add one file like: doc/updates/YYYY-MM-DD-<topic>.md"
  exit 1
fi

echo "check-doc: pass"
