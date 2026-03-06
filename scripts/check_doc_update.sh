#!/usr/bin/env bash
set -euo pipefail

# Runtime repo policy:
# - Detailed update notes are maintained in deployer private repo.
# - This check keeps runtime side lightweight and only reports local change scope.

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
if [[ -z "${non_doc_changes}" ]]; then
  echo "check-doc: only doc changes detected, pass"
else
  echo "check-doc: pass (runtime mode)"
  echo "note: detailed update notes are tracked in clawcolony-deployer/doc/updates/"
fi
