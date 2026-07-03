#!/usr/bin/env bash
# pin-actions.sh — pin GitHub Actions by SHA in workflow files.
#
# Rewrites every `uses: owner/repo@<ref>` line in .github/workflows/*.yml
# (and *.yaml) to `uses: owner/repo@<resolved-sha>  # <orig-ref>`. The 40-hex
# SHA eliminates the OpenSSF Scorecard `Pinned-Dependencies` finding.
#
# Resolution: `gh api repos/{o}/{r}/git/ref/tags/<ref>` → `.object.sha`.
#   - Annotated tag → .object.sha is the underlying commit's SHA.
#   - Lightweight tag → .object.sha is the (commit) SHA it points at.
#   - Full semver refs (v3.1.0) resolve the same way.
#
# Idempotent. Re-running does nothing on already-pinned lines.
#
# Usage:
#   scripts/pin-actions.sh              # apply in place
#   scripts/pin-actions.sh --dry-run    # preview only
#   scripts/pin-actions.sh --help       # this message

set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DRY_RUN=0
if [[ ${1:-} == "--dry-run" ]]; then
  DRY_RUN=1
elif [[ ${1:-} =~ ^-h|--help$ ]]; then
  sed -n '2,28p' "$0"
  exit 0
fi

# -------- Colors --------

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_RESET=$'\033[0m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_BLUE=$'\033[34m'
else
  C_RESET=""; C_GREEN=""; C_YELLOW=""; C_BLUE=""
fi
info() { printf '%s==>%s %s\n' "${C_BLUE}" "${C_RESET}" "$*"; }
ok()   { printf '%s ✓ %s%s\n' "${C_GREEN}" "${C_RESET}" "$*"; }
warn() { printf '%s ! %s%s\n' "${C_YELLOW}" "${C_RESET}" "$*" >&2; }

# -------- Resolve a ref to a 40-hex SHA via gh api --------

declare -A CACHE=()
gh_resolve() {
  local owner="$1" repo="$2" ref="$3"
  local key="${owner}/${repo}@${ref}"
  if [[ -n "${CACHE[$key]+set}" ]]; then
    echo "${CACHE[$key]}"; return 0
  fi
  # 1. Tag endpoint (handles both annotated and lightweight refs).
  local sha
  sha=$(gh api "repos/${owner}/${repo}/git/ref/tags/${ref}" \
          --jq '.object.sha // empty' 2>/dev/null || true)
  # 2. Branch fallback (rare for GitHub Actions).
  if [[ -z "$sha" ]]; then
    sha=$(gh api "repos/${owner}/${repo}/git/ref/heads/${ref}" \
            --jq '.object.sha // empty' 2>/dev/null || true)
  fi
  # 3. Commit lookup (last resort).
  if [[ -z "$sha" ]]; then
    sha=$(gh api "repos/${owner}/${repo}/commits/${ref}" \
            --jq '.sha // empty' 2>/dev/null || true)
  fi
  CACHE[$key]="$sha"
  echo "$sha"
}

# -------- Collect workflow files --------

cd "$PROJECT_ROOT"

mapfile -t FILES < <(find .github/workflows -maxdepth 1 -type f \( -name '*.yml' -o -name '*.yaml' \) | sort)
if [[ ${#FILES[@]} -eq 0 ]]; then
  echo "no workflow files found under .github/workflows" >&2; exit 1
fi

# -------- Phase 1: collect distinct (owner/repo@ref) tuples and resolve --------

declare -A RESOLVED=()
distinct=0
warn_count=0
already=0
local_docker_skip=0

for f in "${FILES[@]}"; do
  while IFS= read -r raw; do
    # Pull out the first whitespace-delimited token after `uses:`.
    uses=$(printf '%s' "$raw" \
           | sed -E 's/^[[:space:]]*-?[[:space:]]*uses:[[:space:]]+//' \
           | awk '{print $1}')
    [[ -z "$uses" ]] && continue
    # Skip already SHA-pinned lines.
    if [[ "$uses" =~ @[0-9a-f]{40}$ ]]; then
      already=$((already + 1)); continue
    fi
    # Skip local-action (`./foo`) and docker-based (`docker://foo`).
    if [[ "$uses" == ./* || "$uses" == docker:* ]]; then
      local_docker_skip=$((local_docker_skip + 1)); continue
    fi
    [[ "$uses" != *@* ]] && continue
    if [[ -n "${RESOLVED[$uses]+set}" ]]; then
      continue  # already resolved in this run
    fi
    owner_repo=${uses%@*}
    ref=${uses#*@}
    # Split owner/repo on `/`. Sub-action refs like `github/codeql-action/init@v3`
    # resolve against the *global* `v3` tag in the codeql-action repo (verified
    # via gh api — the nested `init/v3`, `autobuild/v3` tag paths 404), so the
    # subpath is dropped entirely for the `git/ref/tags/<ref>` call.
    IFS='/' read -r -a _parts <<<"$owner_repo"
    owner="${_parts[0]}"
    repo="${_parts[1]}"
    sha=$(gh_resolve "$owner" "$repo" "$ref")
    if [[ -z "$sha" ]]; then
      warn "could not resolve $uses"
      warn_count=$((warn_count + 1))
      continue
    fi
    RESOLVED[$uses]=$sha
    info "pin: ${uses} → ${sha:0:7}"
    distinct=$((distinct + 1))
  done < <(grep -E '^[[:space:]]*-?[[:space:]]*uses:[[:space:]]+' "$f" 2>/dev/null || true)
done

echo
if [[ $warn_count -gt 0 ]]; then
  warn "$warn_count ref(s) could not be resolved — aborting without changes"
  exit 1
fi
ok "resolved ${#RESOLVED[@]} distinct ref(s) across ${#FILES[@]} file(s)  (already-pinned: ${already}, local/docker skipped: ${local_docker_skip})"

# -------- Phase 2: write out a sidecar file with the SHA map --------

LOOKUP="$(mktemp)"
trap 'rm -f "$LOOKUP"' EXIT
for k in "${!RESOLVED[@]}"; do
  printf '%s\t%s\n' "$k" "${RESOLVED[$k]}" >> "$LOOKUP"
done

# -------- Phase 3: rewrite each file in place via awk --------

changed_count=0
for f in "${FILES[@]}"; do
  tmp="$(mktemp)"
  awk -v lookup="$LOOKUP" '
    BEGIN {
      while ((getline rec < lookup) > 0) {
        # rec = "owner/repo@ref<TAB>sha"
        i = index(rec, "\t")
        key = substr(rec, 1, i - 1)
        sha = substr(rec, i + 1)
        PIN[key] = sha
      }
      close(lookup)
    }
    function emit_pinned(raw, uses, sha) {
      # uses = "owner/repo@ref"; sha = "abc...40hex"
      n = split(uses, parts, "@")
      owner_repo = parts[1]
      ref        = parts[2]
      # Replace "@ref" with "@<sha>" + trailing "  # ref".
      idx = index(raw, "@")
      prefix = substr(raw, 1, idx)       # include the "@" — idx points AT it
      printf("%s%s  # %s\n", prefix, sha, ref)
    }
    /^[[:space:]]*-?[[:space:]]*uses:[[:space:]]+/ {
      # Extract the `owner/repo@ref` token.
      tail = $0
      sub(/^[[:space:]]*-?[[:space:]]*uses:[[:space:]]+/, "", tail)
      n = split(tail, parts, /[[:space:]]+/)
      uses = parts[1]
      # Already pinned: 40 hex.
      if (uses ~ /@[0-9a-f]{40}$/) { print; next }
      # Local or docker: leave unchanged.
      if (uses ~ /^\.\// || uses ~ /^docker:/) { print; next }
      # In our lookup table?
      if (!(uses in PIN)) { print; next }
      emit_pinned($0, uses, PIN[uses])
      next
    }
    { print }
  ' "$f" > "$tmp"

  if cmp -s "$f" "$tmp"; then
    rm -f "$tmp"
  else
    if [[ $DRY_RUN -eq 1 ]]; then
      echo "----- diff (dry-run) ${f} -----"
      diff -u "$f" "$tmp" | head -100 || true
      rm -f "$tmp"
    else
      mv "$tmp" "$f"
      ok "wrote ${f}"
      changed_count=$((changed_count + 1))
    fi
  fi
done

[[ $DRY_RUN -eq 1 ]] && { echo; ok "DRY_RUN complete — would write 0 file(s) for real"; exit 0; }
ok "done — rewrote ${changed_count} file(s)"
