#!/usr/bin/env bash
# install-mcp.sh — wire `hermem mcp` into detected AI coding tools.
#
# Usage:
#   ./scripts/install-mcp.sh                       # interactive (toggle checkboxes)
#   ./scripts/install-mcp.sh --all                # install into every detected tool
#   ./scripts/install-mcp.sh --tools=claude_desktop,cursor
#   ./scripts/install-mcp.sh --all --dry-run      # show what would change
#   ./scripts/install-mcp.sh --all --yes          # non-interactive install
#   ./scripts/install-mcp.sh --unset=claude_desktop
#
# Detected tools (auto-checked when present):
#   claude_desktop, claude_code, codex, cursor, opencode, aider, hermes, openclaw
# Skipped:
#   pi (no native MCP config), kimchi (delegates to underlying tool)
#
# Idempotent — re-running with hermem already configured is a no-op.

set -euo pipefail

# -------- Constants ----------------------------------------------------------

SCRIPT_NAME="$(basename "$0")"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Server spec — the canonical command + args the MCP clients will launch.
SERVER_NAME="hermem"
SERVER_COMMAND="${HERMEM_BIN:-hermem}"
SERVER_ARGS_DEFAULT=("mcp")

# Tool registry: id|label|detection_probe|config_path|config_format|merge_fn
# Detection probe: command run; non-empty stdout + exit 0 = detected.
# Config format: json|toml|yaml
# Merge fn: writes the hermem entry into the config (function name below).
TOOLS=(
  "claude_desktop|Claude Desktop|test -f \"\$CFGP\"|%CLAUDE_DESKTOP_CFG%|json|merge_json_mcpServers"
  "claude_code|Claude Code|test -f \"\$CFGP\" -o -x claude >/dev/null 2>&1|%CLAUDE_CODE_CFG%|json|merge_json_mcpServers"
  "codex|Codex (OpenAI)|test -f \"\$CFGP\" -o -x codex >/dev/null 2>&1|%CODEX_CFG%|toml|merge_toml_mcpServers"
  "cursor|Cursor|test -f \"\$CFGP\" -o -x cursor >/dev/null 2>&1|%CURSOR_CFG%|json|merge_json_mcpServers"
  "opencode|opencode|command -v opencode >/dev/null 2>&1|%OPENCODE_CFG%|json|merge_jsonc_opencode"
  "aider|aider|command -v aider >/dev/null 2>&1|%AIDER_CFG%|yaml|merge_yaml_aider"
  "hermes|Hermes Agent|command -v hermes >/dev/null 2>&1|%HERMES_CFG%|hermes_mcp|merge_hermes_mcp"
  "openclaw|openclaw|command -v openclaw >/dev/null 2>&1|%OPENCLAW_CFG%|json|merge_json_mcpServers"
)

# -------- Colors -------------------------------------------------------------

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  C_RESET=$'\033[0m'
  C_BOLD=$'\033[1m'
  C_DIM=$'\033[2m'
  C_RED=$'\033[31m'
  C_GREEN=$'\033[32m'
  C_YELLOW=$'\033[33m'
  C_BLUE=$'\033[34m'
  C_CYAN=$'\033[36m'
else
  C_RESET="" C_BOLD="" C_DIM="" C_RED="" C_GREEN="" C_YELLOW="" C_BLUE="" C_CYAN=""
fi

# -------- Logging helpers ----------------------------------------------------

log()  { printf '%s\n' "$*"; }
info() { printf '%s==>%s %s\n' "${C_BLUE}" "${C_RESET}" "$*"; }
ok()   { printf '%s ✓ %s%s\n' "${C_GREEN}" "${C_RESET}" "$*"; }
warn() { printf '%s ! %s%s\n' "${C_YELLOW}" "${C_RESET}" "$*" >&2; }
err()  { printf '%s ✗ %s%s\n' "${C_RED}" "${C_RESET}" "$*" >&2; }
hdr()  { printf '\n%s%s%s\n' "${C_BOLD}${C_CYAN}" "$*" "${C_RESET}"; }

# -------- Path resolution ----------------------------------------------------

resolve_paths() {
  local home="${HOME:-}"
  [[ -z "$home" ]] && { err "HOME is not set"; exit 1; }

  case "$(uname -s 2>/dev/null || echo unknown)" in
    Darwin)
      CLAUDE_DESKTOP_CFG="$home/Library/Application Support/Claude/claude_desktop_config.json"
      ;;
    Linux)
      CLAUDE_DESKTOP_CFG="${XDG_CONFIG_HOME:-$home/.config}/Claude/claude_desktop_config.json"
      ;;
    *)
      CLAUDE_DESKTOP_CFG="${XDG_CONFIG_HOME:-$home/.config}/Claude/claude_desktop_config.json"
      ;;
  esac

  CLAUDE_CODE_CFG="$home/.claude.json"
  CODEX_CFG="$home/.codex/config.toml"
  CURSOR_CFG="$home/.cursor/mcp.json"
  OPENCODE_CFG="${XDG_CONFIG_HOME:-$home/.config}/opencode/opencode.json"
  AIDER_CFG="$home/.aider.conf.yml"
  HERMES_CFG="$home/.hermes/config.yaml"
  OPENCLAW_CFG="$home/.openclaw/openclaw.json"
}

expand_tool_paths() {
  local entry="$1"
  printf '%s' "$entry" | sed \
    -e "s|%CLAUDE_DESKTOP_CFG%|${CLAUDE_DESKTOP_CFG}|g" \
    -e "s|%CLAUDE_CODE_CFG%|${CLAUDE_CODE_CFG}|g" \
    -e "s|%CODEX_CFG%|${CODEX_CFG}|g" \
    -e "s|%CURSOR_CFG%|${CURSOR_CFG}|g" \
    -e "s|%OPENCODE_CFG%|${OPENCODE_CFG}|g" \
    -e "s|%AIDER_CFG%|${AIDER_CFG}|g" \
    -e "s|%HERMES_CFG%|${HERMES_CFG}|g" \
    -e "s|%OPENCLAW_CFG%|${OPENCLAW_CFG}|g"
}

# -------- Server probe -------------------------------------------------------

probe_binary() {
  if command -v "$SERVER_COMMAND" >/dev/null 2>&1; then
    # Found on PATH — verify it's actually hermem (best-effort, ignore failure)
    if "$SERVER_COMMAND" version >/dev/null 2>&1; then
      printf '%s%s%s\n' "$SERVER_COMMAND" "" ""
      return 0
    fi
    # Even if version subcommand missing, command exists
    printf '%s\n' "$SERVER_COMMAND"
    return 0
  fi
  # Fall back to local build at project root
  if [[ -x "$PROJECT_ROOT/hermem" ]]; then
    printf '%s\n' "$PROJECT_ROOT/hermem"
    return 0
  fi
  return 1
}

# -------- Detection ----------------------------------------------------------

# detect_tool <id> <probe_cmd> <cfg_path>
# Echoes "1" if detected (probe path or binary), "0" otherwise. Sets DETECTED_CFG.
detect_tool() {
  local id="$1" probe="$2" cfg="$3"
  DETECTED_CFG="$cfg"

  # Build a probe that uses $CFGP for the path token
  local probe_expanded="${probe//\$CFGP/$cfg}"
  if bash -c "$probe_expanded" >/dev/null 2>&1; then
    printf '1'
  else
    printf '0'
  fi
}

# -------- Config-merging primitives -----------------------------------------

require_jq() {
  if ! command -v jq >/dev/null 2>&1; then
    err "jq is required for JSON config edits. Install it (brew install jq / apt install jq)."
    return 1
  fi
}

# merge_json_mcpServers <cfg_path>
# Adds hermem under .mcpServers; preserves all other keys and entries.
merge_json_mcpServers() {
  local cfg="$1"
  require_jq || return 1

  # In dry-run mode, don't create files that don't exist.
  if [[ "$DRY_RUN" == "1" && ! -f "$cfg" ]]; then
    info "[dry-run] would create $cfg with mcpServers.$SERVER_NAME"
    return 0
  fi

  if [[ ! -f "$cfg" ]]; then
    mkdir -p "$(dirname "$cfg")"
    printf '{}\n' > "$cfg"
  fi

  # If file is empty or not valid JSON, recreate it as empty object.
  if ! jq -e . "$cfg" >/dev/null 2>&1; then
    warn "$cfg is empty or invalid JSON — recreating as empty object"
    backup_file "$cfg"
    printf '{}\n' > "$cfg"
  fi

  # If hermem already present with same command/args, no-op.
  local existing
  existing="$(jq -r --arg name "$SERVER_NAME" \
    '.mcpServers[$name].command // empty' "$cfg" 2>/dev/null || true)"
  if [[ "$existing" == "$SERVER_COMMAND" ]]; then
    ok "$cfg already has $SERVER_NAME ($SERVER_COMMAND)"
    return 0
  fi

  # Build jq program that sets .mcpServers.hermem
  local args_json
  args_json="$(printf '%s\n' "${SERVER_ARGS_DEFAULT[@]}" | jq -R . | jq -s .)"
  local tmp
  tmp="$(mktemp)"
  jq --arg cmd "$SERVER_COMMAND" \
     --argjson args "$args_json" \
     --arg name "$SERVER_NAME" \
     '.mcpServers //= {} | .mcpServers[$name] = {command: $cmd, args: $args}' \
     "$cfg" > "$tmp"

  # Verify jq produced valid output before overwriting.
  if ! jq -e . "$tmp" >/dev/null 2>&1; then
    err "jq produced invalid output for $cfg — aborting"
    rm -f "$tmp"
    return 1
  fi

  if [[ "$DRY_RUN" == "1" ]]; then
    info "[dry-run] would update $cfg:"
    diff -u "$cfg" "$tmp" | sed 's/^/    /' || true
    rm -f "$tmp"
    return 0
  fi

  backup_file "$cfg"
  mv "$tmp" "$cfg"
  ok "wrote $SERVER_NAME into $cfg"
}

# merge_toml_mcpServers <cfg_path>
# Appends [mcp_servers.hermem] block; preserves existing top-level sections.
merge_toml_mcpServers() {
  local cfg="$1"

  if [[ ! -f "$cfg" ]]; then
    mkdir -p "$(dirname "$cfg")"
    if [[ "$DRY_RUN" == "1" ]]; then
      info "[dry-run] would create $cfg with [mcp_servers.$SERVER_NAME] block"
      return 0
    fi
    : > "$cfg"
  fi

  # Idempotent check
  if grep -qE "^\[mcp_servers\.${SERVER_NAME}\]" "$cfg" 2>/dev/null; then
    ok "$cfg already declares [mcp_servers.$SERVER_NAME]"
    return 0
  fi

  local tmp
  tmp="$(mktemp)"
  {
    # Preserve existing content
    [[ -s "$cfg" ]] && cat "$cfg" && printf '\n'
    printf '[mcp_servers.%s]\n' "$SERVER_NAME"
    printf 'command = "%s"\n' "$SERVER_COMMAND"
    printf 'args = ["%s"]\n' "${SERVER_ARGS_DEFAULT[*]}"
  } > "$tmp"

  if [[ "$DRY_RUN" == "1" ]]; then
    info "[dry-run] would append to $cfg:"
    if [[ -s "$cfg" ]]; then
      diff -u "$cfg" "$tmp" | sed 's/^/    /' || true
    else
      sed 's/^/    /' "$tmp"
    fi
    rm -f "$tmp"
    return 0
  fi

  backup_file "$cfg"
  mv "$tmp" "$cfg"
  ok "wrote [mcp_servers.$SERVER_NAME] into $cfg"
}

# merge_yaml_aider <cfg_path>
# Appends mcp-servers.hermem block. YAML — naive append is acceptable because
# the section key (`mcp-servers`) and the inner key (`hermem`) are unlikely
# to collide with non-MCP config blocks in these tools.
merge_yaml_aider() {
  local cfg="$1"

  # aider uses `mcp-servers:` (dash form); hermes uses `mcp_servers:` (underscore).
  # We support both via environment override; default to dash (aider).
  local outer_key="${HERMEM_YAML_KEY:-mcp-servers}"
  local label="${HERMEM_YAML_LABEL:-Aider-style YAML}"

  if [[ ! -f "$cfg" ]]; then
    mkdir -p "$(dirname "$cfg")"
    if [[ "$DRY_RUN" == "1" ]]; then
      info "[dry-run] would create $cfg"
      return 0
    fi
    : > "$cfg"
  fi

  # No idempotent fast-path here — the strip+insert pipeline below is itself
  # idempotent (same input → same output across N runs). Fast-path would
  # short-circuit cleanup of orphan / duplicate / legacy blocks.

  # Stage 1: Surgical AWK pass to strip ONLY hermem-related content while
  # preserving (a) the FIRST mcpServers/mcp-servers/mcp_servers block header,
  # (b) any sibling tool entries under the SAME block, (c) all unrelated user
  # content. Also strips orphan `command: "hermem"` blocks that leaked into
  # unrelated parent blocks during prior broken runs.
  local tmp
  tmp="$(mktemp)"
  awk -v n="$SERVER_NAME" -v cmd="$SERVER_COMMAND" '
    BEGIN { in_outer=0; in_subtree=0; outer_count=0; in_orphan=0; skip_dup=0 }

    # Match any mcp-key outer block. First occurrence: print header + enter.
    # Subsequent occurrences: skip the entire duplicate block.
    /^(mcpServers|mcp-servers|mcp_servers):/ {
      outer_count++
      if (outer_count > 1) { skip_dup=1; next }
      print; in_outer=1; next
    }

    # End duplicate block on next top-level key.
    skip_dup && /^[a-zA-Z_-]/ { skip_dup=0; in_outer=0 }
    skip_dup { next }

    # Inside first outer: end-of-block + hermem subtree skipping.
    in_outer && /^[a-zA-Z_-]/ { in_outer=0; in_subtree=0 }
    in_outer && $0 ~ "^[[:space:]]+" n ":" { in_subtree=1; next }
    in_outer && in_subtree && /^[[:space:]]/ { next }
    in_outer && in_subtree && /^[a-zA-Z_-]/ { in_subtree=0 }

    # Outside any outer: detect orphan hermem config block (no proper parent).
    !in_outer && in_orphan == 0 && $0 ~ "^[[:space:]]+command:[[:space:]]*\"" cmd "\"" {
      in_orphan=1; next
    }
    !in_outer && in_orphan && /^[[:space:]]/ { next }
    !in_outer && in_orphan && /^[a-zA-Z_-]/ { in_orphan=0 }

    { if (in_subtree || in_orphan) next; print }
  ' "$cfg" > "$tmp"

  # Stage 2: Build hermem subtree as a single string for splicing.
  # NOTE: We use a temp file instead of a variable because:
  # 1. awk -v can't handle multi-line strings
  # 2. Bash command substitution strips trailing newlines
  local subtree_file
  subtree_file="$(mktemp)"
  printf '  %s:\n    command: "%s"\n    args:\n' \
    "$SERVER_NAME" "$SERVER_COMMAND" > "$subtree_file"
  for arg in "${SERVER_ARGS_DEFAULT[@]}"; do
    printf '      - "%s"\n' "$arg" >> "$subtree_file"
  done

  # Stage 3: Splice hermem subtree — at END of existing outer_key block if
  # present (before next column-0 key or at EOF), else append at end-of-file
  # with a fresh outer_key wrapper.
  local tmp2
  tmp2="$(mktemp)"
  if grep -qE "^${outer_key}:" "$tmp"; then
    awk -v ok="$outer_key" -v sf="$subtree_file" '
      BEGIN {
        in_outer=0; inserted=0; hb=""
        while ((getline line < sf) > 0) { hb = hb line "\n" }
        close(sf)
      }
      $0 ~ "^" ok ":" { print; in_outer=1; next }
      in_outer && /^[a-zA-Z_-]/ {
        printf "%s", hb
        inserted=1
        in_outer=0
      }
      { print }
      END { if (in_outer && !inserted) printf "%s", hb }
    ' "$tmp" > "$tmp2"
  else
    cat "$tmp" > "$tmp2"
    printf '\n%s:\n' "$outer_key" >> "$tmp2"
    cat "$subtree_file" >> "$tmp2"
  fi
  rm -f "$subtree_file"

  if [[ "$DRY_RUN" == "1" ]]; then
    info "[dry-run] would rewrite (surgical strip+insert, $label) of $cfg:"
    diff -u "$cfg" "$tmp2" | sed 's/^/    /' || true
    rm -f "$tmp" "$tmp2"
    return 0
  fi

  backup_file "$cfg"
  mv "$tmp2" "$cfg"
  rm -f "$tmp"
  ok "wrote $outer_key.$SERVER_NAME into $cfg"
}

# merge_hermes_mcp <cfg_path>
# Uses `hermes mcp add` CLI instead of writing config.yaml directly.
# Hermes manages its own MCP config and ignores the mcpServers key in config.yaml.
merge_hermes_mcp() {
  local cfg="$1"

  if ! command -v hermes >/dev/null 2>&1; then
    err "hermes binary not found on PATH — cannot configure MCP"
    return 1
  fi

  if [[ "$DRY_RUN" == "1" ]]; then
    info "[dry-run] would run: hermes mcp add $SERVER_NAME --command $SERVER_COMMAND --args ${SERVER_ARGS_DEFAULT[*]}"
    return 0
  fi

  # Idempotent: check if already configured.
  if hermes mcp list 2>/dev/null | grep -qF "$SERVER_NAME"; then
    ok "hermes already has MCP server '$SERVER_NAME'"
    return 0
  fi

  # Pipe "y" to auto-accept all tools during interactive prompt.
  echo "y" | hermes mcp add "$SERVER_NAME" --command "$SERVER_COMMAND" --args "${SERVER_ARGS_DEFAULT[@]}" >/dev/null 2>&1
  ok "configured hermes MCP server '$SERVER_NAME'"
}

# merge_jsonc_opencode <cfg_path>
# opencode uses top-level "mcp" key. Each entry is a discriminated union of
# `{type: "local", command: [], enabled: true, ...}` OR `{type: "remote", url, enabled}`.
# We write a stdio (local) entry. Users' working entries (e.g. codebase-memory-mcp)
# confirm the schema: type + enabled + command as string-array.
merge_jsonc_opencode() {
  local cfg="$1"
  require_jq || return 1

  # In dry-run mode, don't create files that don't exist.
  if [[ "$DRY_RUN" == "1" && ! -f "$cfg" ]]; then
    info "[dry-run] would create $cfg with mcp.$SERVER_NAME"
    return 0
  fi

  if [[ ! -f "$cfg" ]]; then
    mkdir -p "$(dirname "$cfg")"
    printf '{\n}\n' > "$cfg"
  fi

  # If file is empty or not valid JSON, recreate it as empty object.
  if ! jq -e . "$cfg" >/dev/null 2>&1; then
    warn "$cfg is empty or invalid JSON — recreating as empty object"
    backup_file "$cfg"
    printf '{\n}\n' > "$cfg"
  fi

  local cmd_json
  cmd_json="$(printf '%s\n' "$SERVER_COMMAND" "${SERVER_ARGS_DEFAULT[@]}" | jq -R . | jq -s .)"

  # Already present and shape-correct? Compare via jq (string-mash is fragile
  # because jq -r renders JSON arrays with embedded quotes). No-op only when the
  # entry exists with type="local", enabled=true, AND command array matches.
  if jq -e --arg n "$SERVER_NAME" --argjson c "$cmd_json" \
       '.mcp[$n]? | (.type == "local") and (.enabled == true) and (.command == $c)' \
       "$cfg" >/dev/null 2>&1; then
    ok "$cfg already has mcp.$SERVER_NAME"
    return 0
  fi

  local tmp
  tmp="$(mktemp)"
  jq --arg name "$SERVER_NAME" \
     --argjson cmd "$cmd_json" \
     '.mcp //= {} | (if .mcp[$name] != null then .mcp |= del(.[$name]) else . end) | .mcp[$name] = {type: "local", enabled: true, command: $cmd}' \
     "$cfg" > "$tmp"

  # Verify jq produced valid output before overwriting.
  if ! jq -e . "$tmp" >/dev/null 2>&1; then
    err "jq produced invalid output for $cfg — aborting"
    rm -f "$tmp"
    return 1
  fi

  if [[ "$DRY_RUN" == "1" ]]; then
    info "[dry-run] would update $cfg:"
    diff -u "$cfg" "$tmp" | sed 's/^/    /' || true
    rm -f "$tmp"
    return 0
  fi

  backup_file "$cfg"
  mv "$tmp" "$cfg"
  ok "wrote mcp.$SERVER_NAME into $cfg"
}

# -------- Backup -------------------------------------------------------------

backup_file() {
  local f="$1"
  if [[ -f "$f" ]]; then
    # Format: <file>.bak-YYYYMMDD-HHMMSS-<pid> — sortable + second-precision
    # + PID prevents 1-second-collision overwrites under rapid back-to-back runs
    # (e.g. --uninstall immediately followed by reinstall).
    local bname="${f}.bak-$(date +%Y%m%d-%H%M%S)-$$"
    cp -p "$f" "$bname"
    info "backed up → $bname"
  fi
}

# -------- Unset (remove hermem from all configs) -----------------------------

unset_from_config() {
  local cfg="$1" fmt="$2"
  case "$fmt" in
    json)
      require_jq || return 1
      [[ ! -f "$cfg" ]] && return 0
      if ! jq -e --arg n "$SERVER_NAME" '.mcpServers[$n] // empty' "$cfg" >/dev/null 2>&1; then
        ok "$cfg has no $SERVER_NAME entry"
        return 0
      fi
      local tmp; tmp="$(mktemp)"
      jq --arg n "$SERVER_NAME" 'del(.mcpServers[$n])' "$cfg" > "$tmp"
      backup_file "$cfg"; mv "$tmp" "$cfg"
      ok "removed $SERVER_NAME from $cfg"
      ;;
    jsonc)
      require_jq || return 1
      [[ ! -f "$cfg" ]] && return 0
      if ! jq -e --arg n "$SERVER_NAME" '.mcp[$n] // empty' "$cfg" >/dev/null 2>&1; then
        ok "$cfg has no mcp.$SERVER_NAME"
        return 0
      fi
      local tmp; tmp="$(mktemp)"
      jq --arg n "$SERVER_NAME" 'del(.mcp[$n])' "$cfg" > "$tmp"
      backup_file "$cfg"; mv "$tmp" "$cfg"
      ok "removed mcp.$SERVER_NAME from $cfg"
      ;;
    toml)
      [[ ! -f "$cfg" ]] && return 0
      if ! grep -qE "^\[mcp_servers\.${SERVER_NAME}\]" "$cfg"; then
        ok "$cfg has no [mcp_servers.$SERVER_NAME]"
        return 0
      fi
      # Single backup_file + awk block removal (consistent suffix, no sed mess).
      backup_file "$cfg"
      awk -v section="[mcp_servers.${SERVER_NAME}]" '
        $0 ~ "^"section"$" { skip=1; next }
        skip && /^\[/ { skip=0 }
        !skip { print }
      ' "$cfg" > "${cfg}.new" && mv "${cfg}.new" "$cfg"
      ok "removed [mcp_servers.$SERVER_NAME] from $cfg"
      ;;
    yaml)
      [[ ! -f "$cfg" ]] && return 0
      backup_file "$cfg"
      # Strip any mcpServers / mcp-servers / mcp_servers (snake + camel + dash)
      # block including the hermem entry, leaving the rest of the file intact.
      awk -v n="$SERVER_NAME" '
        $0 ~ /^(mcp-servers|mcp_servers|mcpServers):/ { inblock=1; next }
        inblock && /^[a-zA-Z]/ { inblock=0 }
        inblock && $0 ~ "^[[:space:]]+"n":" { next }
        { print }
      ' "$cfg" > "${cfg}.new" && mv "${cfg}.new" "$cfg"
      ok "removed hermem block from $cfg"
      ;;
    hermes_mcp)
      # Hermes manages MCP via its own CLI, not config.yaml edits.
      if ! command -v hermes >/dev/null 2>&1; then
        warn "hermes not on PATH — cannot remove MCP server"
        return 0
      fi
      if ! hermes mcp list 2>/dev/null | grep -qF "$SERVER_NAME"; then
        ok "hermes has no MCP server '$SERVER_NAME'"
        return 0
      fi
      echo "y" | hermes mcp remove "$SERVER_NAME" >/dev/null 2>&1
      ok "removed MCP server '$SERVER_NAME' from hermes"
      ;;
  esac
}

# -------- Interactive picker (inline in main; bash 3.2-safe) -----------------
# We avoid `local -n` (bash 4.3+) and `${var,,}` (bash 4.0+) by keeping all
# picker state in globals populated by detect_all_tools().

# -------- Tools registry (iterate) -------------------------------------------

get_tool_field() {
  local idx="$1" field="$2"
  local entry="${TOOLS[$idx]}"
  IFS='|' read -r id label probe cfg fmt merge <<<"$entry"
  case "$field" in
    id)      printf '%s' "$id" ;;
    label)   printf '%s' "$label" ;;
    probe)   printf '%s' "$probe" ;;
    cfg) expand_tool_paths "$cfg" ;;
    fmt)     printf '%s' "$fmt" ;;
    merge)   printf '%s' "$merge" ;;
  esac
}

# Populates globals TABLE_IDS, TABLE_LABELS, TABLE_CFGS, TABLE_FMTS, TABLE_MERGES, TABLE_DETECTED.
detect_all_tools() {
  TABLE_IDS=()
  TABLE_LABELS=()
  TABLE_CFGS=()
  TABLE_FMTS=()
  TABLE_MERGES=()
  TABLE_DETECTED=()
  local n=${#TOOLS[@]}
  for ((i=0; i<n; i++)); do
    local id label probe cfg fmt merge det
    id="$(get_tool_field "$i" id)"
    label="$(get_tool_field "$i" label)"
    cfg="$(get_tool_field "$i" cfg)"
    fmt="$(get_tool_field "$i" fmt)"
    merge="$(get_tool_field "$i" merge)"
    CFGP="$cfg" det="$(detect_tool "$id" "$(get_tool_field "$i" probe)" "$cfg")"
    TABLE_IDS+=("$id")
    TABLE_LABELS+=("$label")
    TABLE_CFGS+=("$cfg")
    TABLE_FMTS+=("$fmt")
    TABLE_MERGES+=("$merge")
    TABLE_DETECTED+=("$det")
  done
}

# -------- Apply --------------------------------------------------------------

apply_to_tool() {
  local idx="$1"
  local id cfg fmt merge
  id="${TABLE_IDS[$idx]}"
  cfg="${TABLE_CFGS[$idx]}"
  fmt="${TABLE_FMTS[$idx]}"
  merge="${TABLE_MERGES[$idx]}"

  # Set YAML key per-tool default — hermes uses underscore, others dash.
  case "$id" in
    hermes) export HERMEM_YAML_KEY="mcp_servers" HERMEM_YAML_LABEL="Hermes YAML (underscore)" ;;
    aider)  export HERMEM_YAML_KEY="mcp-servers" HERMEM_YAML_LABEL="aider YAML" ;;
    *)      unset HERMEM_YAML_KEY HERMEM_YAML_LABEL ;;
  esac

  hdr "→ $id ($cfg)"
  "$merge" "$cfg"
}

# -------- Help ---------------------------------------------------------------

print_help() {
  cat <<EOF
$SCRIPT_NAME — wire \`hermem mcp\` (stdio) into detected AI coding tools.

USAGE
  $SCRIPT_NAME                              Interactive toggle picker
  $SCRIPT_NAME --all [--yes] [--dry-run]    Install into every detected tool
  $SCRIPT_NAME --tools=<id1,id2,...>        Install into the given tools
  $SCRIPT_NAME --unset=<id1,id2,...>        Remove hermem from the given tools
  $SCRIPT_NAME --uninstall                  Remove hermem from EVERY registered tool
  $SCRIPT_NAME --list                       List detected tools + configs
  $SCRIPT_NAME --help                       Show this message

OPTIONS
  --all              Install into every detected tool.
  --tools=<list>     Comma-separated tool ids (see --list).
  --unset=<list>     Remove hermem entry from each tool's config.
                     Pass "all" to target every registered tool.
  --uninstall        Synonym for --unset=all. Removes hermem everywhere.
  --list             Print detected tools and exit.
  --yes, -y          Assume "yes" in interactive mode (auto-confirm).
  --dry-run          Show what would change, but don't write any files.
  --bin=<path>       Override server command (default: $SERVER_COMMAND).
  --no-color         Disable colored output.
  --help, -h         Show this message.

DETECTED TOOLS
  claude_desktop  Claude Desktop (macOS / Linux config)
  claude_code     Claude Code (CLI)
  codex           OpenAI Codex (TOML config)
  cursor          Cursor editor
  opencode        opencode (JSONC, top-level "mcp" key)
  aider           aider chat (YAML)
  hermes          Hermes Agent (YAML; see --notes on key naming)
  openclaw        openclaw (JSON)

EXAMPLES
  # Interactive — pre-checks detected tools, you toggle and confirm
  $SCRIPT_NAME

  # Non-interactive — set up everything that we can detect
  $SCRIPT_NAME --all --yes

  # Just Claude Code + Cursor
  $SCRIPT_NAME --tools=claude_code,cursor --yes

  # Preview changes only
  $SCRIPT_NAME --all --dry-run

  # Roll back hermem from Claude + Cursor
  $SCRIPT_NAME --unset=claude_desktop,cursor --yes

NOTES
  - Idempotent: re-running with hermem already configured is a no-op.
  - Every write creates \$cfg.bak-YYYYMMDD-HHMM-SS (sortable, second-precision).
  - JSON edits require jq (install via brew/apt).
  - Bulk rollback: $SCRIPT_NAME --uninstall  (or --unset=all).
EOF
}

list_tools() {
  printf '%sDetected AI tools%s\n\n' "${C_BOLD}" "${C_RESET}"
  printf '%-15s %-22s %-9s %s\n' "ID" "LABEL" "FOUND" "CONFIG"
  printf '%-15s %-22s %-9s %s\n' "---" "-----" "-----" "------"
  for ((i=0; i<${#TABLE_IDS[@]}; i++)); do
    local mark="no"
    [[ "${TABLE_DETECTED[$i]}" == "1" ]] && mark="${C_GREEN}yes${C_RESET}"
    printf '%-15s %-22s %-9s %s\n' \
      "${TABLE_IDS[$i]}" "${TABLE_LABELS[$i]}" "$mark" "${TABLE_CFGS[$i]}"
  done
  printf '\n'
}

# -------- Main ---------------------------------------------------------------

main() {
  resolve_paths

  local action="interactive" list_only=0 dry_run=0 auto_yes=0
  local tools_csv="" unset_csv="" bin_override=""

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --help|-h)           print_help; exit 0 ;;
      --list)              list_only=1 ;;
      --all)               action="all"; tools_csv="all" ;;
      --tools=*)           action="select"; tools_csv="${1#--tools=}" ;;
      --unset=*)           action="unset"; unset_csv="${1#--unset=}" ;;
      --uninstall)         action="unset"; unset_csv="all" ;;
      --dry-run)           dry_run=1 ;;
      --yes|-y)            auto_yes=1 ;;
      --bin=*)             bin_override="${1#--bin=}"; SERVER_COMMAND="$bin_override" ;;
      --no-color|NO_COLOR=1) NO_COLOR=1 ;;
      *)                   err "unknown flag: $1"; print_help; exit 2 ;;
    esac
    shift
  done

  DRY_RUN=$dry_run
  detect_all_tools

  if (( list_only )); then
    list_tools
    exit 0
  fi

  # Probe hermem binary
  local resolved
  if resolved="$(probe_binary)"; then
    ok "hermem binary: $resolved"
  else
    warn "hermem not on PATH and not in $PROJECT_ROOT — config will still be written."
    warn "Build with: $PROJECT_ROOT/make build  (or set --bin=/path/to/hermem)"
  fi

  case "$action" in
    all)
      local fails=0
      for ((i=0; i<${#TABLE_IDS[@]}; i++)); do
        if [[ "${TABLE_DETECTED[$i]}" == "1" ]]; then
          apply_to_tool "$i" || fails=$((fails + 1))
        else
          warn "skipping ${TABLE_IDS[$i]} (not detected)"
        fi
      done
      printf '\n'
      if (( fails == 0 )); then
        ok "all done"
      else
        err "$fails tool(s) failed"
        exit 1
      fi
      ;;
    select)
      IFS=',' read -ra wanted <<<"$tools_csv"
      local fails=0
      for want in "${wanted[@]}"; do
        want="${want// /}"
        local found=-1
        for ((i=0; i<${#TABLE_IDS[@]}; i++)); do
          if [[ "${TABLE_IDS[$i]}" == "$want" ]]; then
            found=$i
            break
          fi
        done
        if (( found < 0 )); then
          warn "unknown tool id: $want"
          fails=$((fails + 1))
          continue
        fi
        apply_to_tool "$found" || fails=$((fails + 1))
      done
      printf '\n'
      if (( fails == 0 )); then ok "all done"; else err "$fails tool(s) failed"; exit 1; fi
      ;;
    unset)
      local wanted
      if [[ "$unset_csv" == "all" ]]; then
        # Bulk uninstall — every registered tool, regardless of detection.
        # Each unset_from_config is idempotent (no-op if no entry present).
        info "bulk uninstall: removing hermem from every registered tool config"
        wanted=("${TABLE_IDS[@]}")
      else
        IFS=',' read -ra wanted <<<"$unset_csv"
      fi
      for want in "${wanted[@]}"; do
        want="${want// /}"
        local found=-1
        for ((i=0; i<${#TABLE_IDS[@]}; i++)); do
          if [[ "${TABLE_IDS[$i]}" == "$want" ]]; then found=$i; break; fi
        done
        if (( found < 0 )); then
          warn "unknown tool id: $want"
          continue
        fi
        hdr "✗ ${TABLE_IDS[$found]} (${TABLE_CFGS[$found]})"
        unset_from_config "${TABLE_CFGS[$found]}" "${TABLE_FMTS[$found]}"
      done
      ;;
    interactive)
      # Pre-check detected tools. PICK index list = indices where detected=1.
      PICK=()
      for ((i=0; i<${#TABLE_IDS[@]}; i++)); do
        [[ "${TABLE_DETECTED[$i]}" == "1" ]] && PICK+=("$i")
      done

      while :; do
        # Render: build map "idx|mark|label|cfg" for each tool, then sort visible first.
        printf '\n'
        printf '%sDetected AI tools:%s\n' "${C_BOLD}" "${C_RESET}"
        printf '%sToggle with space-separated numbers, "a" all, "u" none, "ok" to apply, "q" quit:%s\n\n' "${C_DIM}" "${C_RESET}"
        for ((i=0; i<${#TABLE_IDS[@]}; i++)); do
          local_mark="[ ]"
          for p in "${PICK[@]}"; do
            [[ "$p" == "$i" ]] && local_mark="[✓]" && break
          done
          printf '  %s %2d. %-22s %s%s%s\n' "$local_mark" \
            $((i+1)) "${TABLE_LABELS[$i]}" "${C_DIM}" "${TABLE_CFGS[$i]}" "${C_RESET}"
        done
        printf '\n  %sChoice:%s ' "${C_BOLD}" "${C_RESET}"

        local line
        if (( auto_yes )); then
          line="ok"
          printf '%s\n' "$line"
        else
          IFS= read -r -e -p "" line || line="ok"
        fi

        # Lowercase via tr (bash 3.2-safe).
        local lc
        lc="$(printf '%s' "$line" | tr 'A-Z' 'a-z')"

        case "$lc" in
          ok|done|yes|install|apply|"")
            if (( ${#PICK[@]} == 0 )); then
              warn "nothing selected, exiting"
              exit 0
            fi
            for idx in "${PICK[@]}"; do
              apply_to_tool "$idx" || warn "tool ${TABLE_IDS[$idx]} failed"
            done
            printf '\n'
            ok "done"
            exit 0
            ;;
          q|quit|exit)
            info "aborted"
            exit 0
            ;;
          a|all)
            PICK=()
            for ((i=0; i<${#TABLE_IDS[@]}; i++)); do PICK+=("$i"); done
            continue
            ;;
          u|none)
            PICK=()
            continue
          ;;
        esac

        # Toggle list of space-separated digits.
        local changed=0
        for tok in $line; do
          if [[ "$tok" =~ ^[0-9]+$ ]]; then
            local idx=$((tok - 1))
            local n=${#TABLE_IDS[@]}
            if (( idx >= 0 && idx < n )); then
              # Toggle: remove if present, append if not.
              local new_pick=()
              local found_pick=0
              for p in "${PICK[@]}"; do
                if [[ "$p" == "$idx" ]]; then
                  found_pick=1
                else
                  new_pick+=("$p")
                fi
              done
              if (( found_pick == 0 )); then
                new_pick+=("$idx")
              fi
              PICK=("${new_pick[@]}")
              changed=1
            else
              warn "out of range: $tok"
            fi
          else
            warn "ignoring: $tok"
          fi
        done
        # Loop re-renders either way.
        : "${changed:=0}"
      done
      ;;
  esac
}

main "$@"
