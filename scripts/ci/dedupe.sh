#!/usr/bin/env bash
# Decide whether CI can be skipped on a push to main because the merged tree is exactly
# what the pull request already tested. Fail safe: every doubt means "do not skip".
set -euo pipefail

# Inputs from the workflow (or a dry run):
#   REPO          owner/name
#   SHA           commit being built
#   REF           refs/heads/main for a main push
#   EVENT_NAME    push, pull_request, ...
#   BEFORE        github.event.before (may be empty or all zeroes)
#   WORKFLOW_REF  github.workflow_ref (optional, used to find this workflow)
#   GITHUB_OUTPUT optional file for the skip/pr outputs

PR_NUMBER=""

finish() {
  local skip="$1"
  local message="$2"
  printf 'dedupe: %s\n' "$message"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    {
      printf 'skip=%s\n' "$skip"
      if [ -n "$PR_NUMBER" ]; then
        printf 'pr=%s\n' "$PR_NUMBER"
      fi
    } >> "$GITHUB_OUTPUT"
  fi
  exit 0
}

# Any unexpected failure (including a jq or gh surprise) still means "run CI".
trap 'finish false "unexpected error on line $LINENO; not skipping"' ERR

if [ "${EVENT_NAME:-}" != "push" ] || [ "${REF:-}" != "refs/heads/main" ]; then
  finish false "not a push to main (event=${EVENT_NAME:-?}, ref=${REF:-?}); not skipping"
fi

if [ -z "${REPO:-}" ] || [ -z "${SHA:-}" ]; then
  finish false "missing repository or commit; not skipping"
fi

# 1. The merged pull request whose base is main.
pulls=""
if ! pulls=$(gh api "repos/${REPO}/commits/${SHA}/pulls" --paginate 2>/dev/null); then
  finish false "cannot list the pull requests for ${SHA}; not skipping"
fi

pr_count=$(jq '[.[] | select(.base.ref == "main" and .merged_at != null)] | length' <<<"$pulls" 2>/dev/null) || {
  finish false "cannot read the pull request list; not skipping"
}
if [ "$pr_count" -ne 1 ]; then
  finish false "found ${pr_count} merged pull requests based on main for ${SHA}; not skipping"
fi

PR_NUMBER=$(jq -r '[.[] | select(.base.ref == "main" and .merged_at != null)][0].number' <<<"$pulls")
HEAD_SHA=$(jq -r '[.[] | select(.base.ref == "main" and .merged_at != null)][0].head.sha' <<<"$pulls")
if [ -z "$PR_NUMBER" ] || [ "$PR_NUMBER" = "null" ] || [ -z "$HEAD_SHA" ] || [ "$HEAD_SHA" = "null" ]; then
  finish false "the pull request data is incomplete; not skipping"
fi

# 2./3. The merge commit and the tested PR head must have the same tree.
tree_of() {
  gh api "repos/${REPO}/git/commits/$1" --jq .tree.sha 2>/dev/null
}

sha_tree=""
head_tree=""
if ! sha_tree=$(tree_of "$SHA"); then
  finish false "cannot read the tree of ${SHA}; not skipping"
fi
if ! head_tree=$(tree_of "$HEAD_SHA"); then
  finish false "cannot read the tree of the PR head ${HEAD_SHA}; not skipping"
fi
if [ -z "$sha_tree" ] || [ -z "$head_tree" ]; then
  finish false "the tree data is incomplete; not skipping"
fi
if [ "$sha_tree" != "$head_tree" ]; then
  finish false "main tree ${sha_tree} differs from PR #${PR_NUMBER} head ${HEAD_SHA} tree ${head_tree}; not skipping"
fi

# 4. A successful pull_request run of this workflow for the PR head.
workflow_path=".github/workflows/ci.yml"
if [ -n "${WORKFLOW_REF:-}" ]; then
  candidate="${WORKFLOW_REF%%@*}"
  case "$candidate" in
    */.github/*) workflow_path=".github/${candidate#*/.github/}" ;;
  esac
fi

workflow_id=0
if workflow_id_json=$(gh api "repos/${REPO}/actions/workflows/$(basename "$workflow_path")" --jq .id 2>/dev/null); then
  case "$workflow_id_json" in
    '' | *[!0-9]*) workflow_id=0 ;;
    *) workflow_id="$workflow_id_json" ;;
  esac
fi

runs=""
if ! runs=$(gh api "repos/${REPO}/actions/runs?head_sha=${HEAD_SHA}&event=pull_request&status=success" --paginate --slurp 2>/dev/null); then
  finish false "cannot list the pull request runs for ${HEAD_SHA}; not skipping"
fi

run_count=$(jq --arg path "$workflow_path" --argjson id "$workflow_id" \
  '[.[] | .workflow_runs[] | select(.status == "completed" and .conclusion == "success" and (.path == $path or ($id > 0 and .workflow_id == $id)))] | length' \
  <<<"$runs" 2>/dev/null) || {
  finish false "cannot read the pull request runs; not skipping"
}
if [ "$run_count" -eq 0 ]; then
  finish false "no successful pull_request run of ${workflow_path} for PR #${PR_NUMBER} head ${HEAD_SHA}; not skipping"
fi
RUN_ID=$(jq -r --arg path "$workflow_path" --argjson id "$workflow_id" \
  '[.[] | .workflow_runs[] | select(.status == "completed" and .conclusion == "success" and (.path == $path or ($id > 0 and .workflow_id == $id)))][0].id' \
  <<<"$runs")
if [ -z "$RUN_ID" ] || [ "$RUN_ID" = "null" ]; then
  finish false "the workflow run data is incomplete; not skipping"
fi

# 5. A dependency change on main needs its own run so the main-only cache save refreshes.
if [ -z "${BEFORE:-}" ] || [ "$BEFORE" = "0000000000000000000000000000000000000000" ]; then
  finish false "the push has no usable before commit; assuming dependencies changed, not skipping"
fi

compare=""
if ! compare=$(gh api "repos/${REPO}/compare/${BEFORE}...${SHA}" 2>/dev/null); then
  finish false "cannot compare ${BEFORE}...${SHA}; not skipping"
fi

file_count=$(jq '.files | length' <<<"$compare" 2>/dev/null) || {
  finish false "cannot read the changed files; not skipping"
}
dependency_change=$(jq -r '.files[]? | .filename, (.previous_filename // empty)' <<<"$compare" 2>/dev/null | grep -Ex 'go\.mod|go\.sum|frontend/package-lock\.json' || true)
if [ -n "$dependency_change" ]; then
  names=$(printf '%s\n' "$dependency_change" | paste -sd ',' -)
  finish false "dependency files changed in this push (${names}); not skipping"
fi
if [ "$file_count" -ge 300 ]; then
  finish false "the compare lists ${file_count} files (truncated); assuming dependencies changed, not skipping"
fi

finish true "main ${SHA} tree ${sha_tree} equals PR #${PR_NUMBER} head ${HEAD_SHA} tree ${head_tree}, tested by run ${RUN_ID}; skipping CI"
