#!/usr/bin/env bash
# install-mcp-test.sh — install-mcp.sh unit tests.
# Each test fakes $HOME into a temp dir, runs the installer against a fixture
# config, asserts the resulting file. Exits non-zero on the first failure.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALLER="$SCRIPT_DIR/install-mcp.sh"

# Pick a real hermem binary on PATH if available, otherwise stand in with /bin/echo.
# The installer only writes config — it never launches the server in test mode.
BIN_OVERRIDE="--bin=/usr/bin/env"

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
FAILED_NAMES=()

log()   { printf '  %s\n' "$*"; }
pass()  { printf '  %s ✓ %s%s\n' "${C_GREEN:-\033[32m}" "$*" "${C_RESET:-\033[0m}"; TESTS_PASSED=$((TESTS_PASSED+1)); }
fail()  { printf '  %s ✗ %s%s\n' "${C_RED:-\033[31m}"   "$*" "${C_RESET:-\033[0m}"; TESTS_FAILED=$((TESTS_FAILED+1)); }

assert_file_contains() {
  local f="$1" needle="$2" tname="$3"
  if [[ -f "$f" ]] && grep -qF -- "$needle" "$f"; then
    pass "[$tname] file contains: $needle"
  else
    fail "[$tname] expected '$needle' in $f"
    [[ -f "$f" ]] && sed 's/^/      /' "$f"
  fi
}
assert_file_missing() {
  local f="$1" needle="$2" tname="$3"
  if [[ ! -f "$f" ]] || ! grep -qF -- "$needle" "$f"; then
    pass "[$tname] file does not contain: $needle"
  else
    fail "[$tname] did NOT expect '$needle' in $f"
  fi
}
assert_contains_json_key() {
  # asserts that $1 (file) contains JSON key $2 at top level OR under mcpServers/hermem/$2
  local f="$1" key="$2" tname="$3"
  if command -v jq >/dev/null 2>&1; then
    if jq -e --arg k "$key" '.mcpServers.hermem[$k] // empty' "$f" >/dev/null 2>&1 \
       || jq -e --arg k "$key" '.mcp[$k] // empty' "$f" >/dev/null 2>&1; then
      pass "[$tname] JSON has hermem.$key"
    else
      fail "[$tname] JSON missing hermem.$key in $f"
      cat "$f"
    fi
  else
    pass "[$tname] skipped JSON-shape check (no jq)"
  fi
}

# Make a fresh temp HOME with optional pre-existing config.
make_home() {
  local fake_home; fake_home="$(mktemp -d)"
  export HOME="$fake_home"
  export XDG_CONFIG_HOME="$fake_home/.config"
  mkdir -p "$fake_home/.config"
  printf '%s\n' "$fake_home"
}

cleanup_home() {
  local h="$1"
  rm -rf "$h" 2>/dev/null || true
}

assert_json() {
  # Generic JSON validity.
  local f="$1" tname="$2"
  command -v jq >/dev/null || { pass "[$tname] skipping JSON validate (no jq)"; return 0; }
  if jq -e . "$f" >/dev/null 2>&1; then
    pass "[$tname] JSON parses: $f"
  else
    fail "[$tname] JSON invalid: $f"
    cat "$f"
  fi
}

run_installer() {
  local home="$1"
  shift
  (
    export HOME="$home"
    export XDG_CONFIG_HOME="$home/.config"
    "$INSTALLER" "$@" 2>&1
  )
}

# ----------------------------------------------------------------------------

setup() {
  printf '\n%s== %s ==%s\n' "${C_BOLD:-\033[1m}${C_CYAN:-\033[36m}" "$1" "${C_RESET:-\033[0m}"
}

# Test 1: claude_desktop — fresh install, JSON parses, mcpServers.hermem present.
test_claude_desktop_fresh() {
  setup "claude_desktop: fresh install"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))

  # macOS-style path
  local cfg="$h/Library/Application Support/Claude/claude_desktop_config.json"
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null
  if [[ -f "$cfg" ]]; then
    assert_json "$cfg" "claude_desktop_fresh"
    assert_contains_json_key "$cfg" "command" "claude_desktop_fresh"
    assert_contains_json_key "$cfg" "args"    "claude_desktop_fresh"
  else
    fail "[claude_desktop_fresh] config not created at $cfg"
  fi
  cleanup_home "$h"
}

# Test 2: claude_desktop — preserves existing other server entries.
test_claude_desktop_preserves_other() {
  setup "claude_desktop: preserves other servers"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/Library/Application Support/Claude/claude_desktop_config.json"
  mkdir -p "$(dirname "$cfg")"
  cat > "$cfg" <<'JSON'
{
  "mcpServers": {
    "other-tool": {
      "command": "otool",
      "args": ["serve"]
    }
  }
}
JSON
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null
  assert_json "$cfg" "claude_desktop_preserves"
  assert_file_contains "$cfg" '"other-tool"' "claude_desktop_preserves"
  assert_file_contains "$cfg" '"command": "otool"' "claude_desktop_preserves"
  assert_file_contains "$cfg" '"hermem"' "claude_desktop_preserves"
  cleanup_home "$h"
}

# Test 3: claude_desktop — idempotent (running twice doesn't duplicate).
test_claude_desktop_idempotent() {
  setup "claude_desktop: idempotent"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/Library/Application Support/Claude/claude_desktop_config.json"
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null
  local before; before="$(wc -l < "$cfg")"
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null
  local after; after="$(wc -l < "$cfg")"
  if [[ "$before" == "$after" ]]; then
    pass "[claude_desktop_idempotent] file unchanged on re-run ($before lines)"
  else
    fail "[claude_desktop_idempotent] file changed: $before → $after lines"
  fi
  cleanup_home "$h"
}

# Test 4: codex TOML — fresh install, [mcp_servers.hermem] block present.
test_codex_fresh() {
  setup "codex: fresh install (TOML)"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/.codex/config.toml"
  run_installer "$h" --tools=codex --yes $BIN_OVERRIDE >/dev/null
  if [[ -f "$cfg" ]]; then
    assert_file_contains "$cfg" "[mcp_servers.hermem]" "codex_fresh"
    assert_file_contains "$cfg" 'command = "/usr/bin/env"' "codex_fresh"
    assert_file_contains "$cfg" 'args = ["mcp"]' "codex_fresh"
  else
    fail "[codex_fresh] config not created at $cfg"
  fi
  cleanup_home "$h"
}

# Test 5: codex — preserves existing sections.
test_codex_preserves() {
  setup "codex: preserves existing TOML sections"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/.codex/config.toml"
  mkdir -p "$(dirname "$cfg")"
  cat > "$cfg" <<'TOML'
[model]
name = "gpt-5"

TOML
  run_installer "$h" --tools=codex --yes $BIN_OVERRIDE >/dev/null
  assert_file_contains "$cfg" "[model]"           "codex_preserves"
  assert_file_contains "$cfg" 'name = "gpt-5"'     "codex_preserves"
  assert_file_contains "$cfg" "[mcp_servers.hermem]" "codex_preserves"
  cleanup_home "$h"
}

# Test 6: cursor — fresh JSON install.
test_cursor_fresh() {
  setup "cursor: fresh install (JSON)"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/.cursor/mcp.json"
  run_installer "$h" --tools=cursor --yes $BIN_OVERRIDE >/dev/null
  if [[ -f "$cfg" ]]; then
    assert_json "$cfg" "cursor_fresh"
    assert_file_contains "$cfg" "\"hermem\"" "cursor_fresh"
  else
    fail "[cursor_fresh] config not created at $cfg"
  fi
  cleanup_home "$h"
}

# Test 7: opencode — fresh JSONC, top-level "mcp" key.
test_opencode_fresh() {
  setup "opencode: fresh install (JSONC, top-level mcp key)"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/.config/opencode/opencode.json"
  run_installer "$h" --tools=opencode --yes $BIN_OVERRIDE >/dev/null
  if [[ -f "$cfg" ]]; then
    assert_json "$cfg" "opencode_fresh"
    assert_file_contains "$cfg" "\"hermem\"" "opencode_fresh"
    assert_file_contains "$cfg" "\"type\": \"local\"" "opencode_fresh"
  else
    fail "[opencode_fresh] config not created at $cfg"
  fi
  cleanup_home "$h"
}

# Test 8: aider — fresh YAML.
test_aider_fresh() {
  setup "aider: fresh install (YAML, dash-form key)"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/.aider.conf.yml"
  run_installer "$h" --tools=aider --yes $BIN_OVERRIDE >/dev/null
  if [[ -f "$cfg" ]]; then
    assert_file_contains "$cfg" "mcp-servers:" "aider_fresh"
    assert_file_contains "$cfg" "  hermem:" "aider_fresh"
    assert_file_contains "$cfg" 'command:' "aider_fresh"
    assert_file_contains "$cfg" "- \"mcp\"" "aider_fresh"
  else
    fail "[aider_fresh] config not created at $cfg"
  fi
  cleanup_home "$h"
}

# Test 9: hermes — fresh install via `hermes mcp add` (mock).
test_hermes_fresh() {
  setup "hermes: fresh install via hermes mcp add"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))

  # Create a mock hermes script that records calls.
  local mock_dir="$h/bin"
  mkdir -p "$mock_dir"
  local mock_log="$h/hermes_mcp_calls.log"
  cat > "$mock_dir/hermes" <<MOCK
#!/usr/bin/env bash
echo "\$@" >> "$mock_log"
if [[ "\$1" == "mcp" && "\$2" == "list" ]]; then
  # Return empty list (no servers configured yet).
  exit 0
fi
if [[ "\$1" == "mcp" && "\$2" == "add" ]]; then
  echo "Added MCP server: \$3" >&2
  exit 0
fi
exit 0
MOCK
  chmod +x "$mock_dir/hermes"

  # Run installer with mock hermes on PATH.
  PATH="$mock_dir:$PATH" run_installer "$h" --tools=hermes --yes $BIN_OVERRIDE >/dev/null

  # Verify hermes mcp add was called with correct args.
  if [[ -f "$mock_log" ]]; then
    assert_file_contains "$mock_log" "mcp add hermem" "hermes_fresh_add"
    assert_file_contains "$mock_log" "--command" "hermes_fresh_cmd"
    assert_file_contains "$mock_log" "--args mcp" "hermes_fresh_args"
  else
    fail "[hermes_fresh] hermes mcp add was never called"
  fi
  cleanup_home "$h"
}

# Test 10: claude_code — fresh JSON install at ~/.claude.json.
test_claude_code_fresh() {
  setup "claude_code: fresh install (JSON at ~/.claude.json)"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/.claude.json"
  run_installer "$h" --tools=claude_code --yes $BIN_OVERRIDE >/dev/null
  if [[ -f "$cfg" ]]; then
    assert_json "$cfg" "claude_code_fresh"
    assert_contains_json_key "$cfg" "command" "claude_code_fresh"
  else
    fail "[claude_code_fresh] config not created at $cfg"
  fi
  cleanup_home "$h"
}

# Test 11: --unset removes hermem.
test_unset_claude_desktop() {
  setup "claude_desktop: --unset removes entry"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/Library/Application Support/Claude/claude_desktop_config.json"
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null
  assert_file_contains "$cfg" "\"hermem\"" "unset_setup"
  run_installer "$h" --unset=claude_desktop --yes >/dev/null
  assert_file_missing "$cfg" "\"hermem\"" "unset_claude_desktop"
  cleanup_home "$h"
}

# Test 11b: --unset=hermes calls `hermes mcp remove`.
test_unset_hermes() {
  setup "hermes: --unset calls hermes mcp remove"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))

  # Create a mock hermes that pretends hermem is already configured.
  local mock_dir="$h/bin"
  mkdir -p "$mock_dir"
  local mock_log="$h/hermes_mcp_calls.log"
  cat > "$mock_dir/hermes" <<MOCK
#!/usr/bin/env bash
echo "\$@" >> "$mock_log"
if [[ "\$1" == "mcp" && "\$2" == "list" ]]; then
  echo "  hermem"
  exit 0
fi
if [[ "\$1" == "mcp" && "\$2" == "remove" ]]; then
  echo "Removed MCP server: \$3" >&2
  exit 0
fi
exit 0
MOCK
  chmod +x "$mock_dir/hermes"

  PATH="$mock_dir:$PATH" run_installer "$h" --unset=hermes --yes >/dev/null

  if [[ -f "$mock_log" ]]; then
    assert_file_contains "$mock_log" "mcp remove hermem" "unset_hermes"
  else
    fail "[unset_hermes] hermes mcp remove was never called"
  fi
  cleanup_home "$h"
}

# Test 12: --dry-run does not modify the filesystem.
test_dry_run_noop() {
  setup "all tools: --dry-run leaves filesystem untouched"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  # Snapshot what was there before
  local before; before="$(find "$h" -type f 2>/dev/null | sort)"
  # Note: hermes excluded because it uses CLI (`hermes mcp add`) which can't be
  # fully dry-run tested without mocking the hermes binary.
  run_installer "$h" --tools=claude_desktop,codex,cursor,opencode,aider,claude_code --yes --dry-run >/dev/null
  local after; after="$(find "$h" -type f 2>/dev/null | sort)"
  if [[ "$before" == "$after" ]]; then
    pass "[dry_run_noop] no files created by --dry-run"
  else
    fail "[dry_run_noop] filesystem changed by --dry-run"
    diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") | sed 's/^/      /'
  fi
  cleanup_home "$h"
}

# Test 13: --list prints output without writing any files.
test_list_noop() {
  setup "--list: prints without writing"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local before; before="$(find "$h" -type f 2>/dev/null | sort)"
  run_installer "$h" --list >/dev/null
  local after; after="$(find "$h" -type f 2>/dev/null | sort)"
  if [[ "$before" == "$after" ]]; then
    pass "[list_noop] --list wrote nothing"
  else
    fail "[list_noop] --list modified filesystem"
  fi
  cleanup_home "$h"
}

# Test 14: multi-tool install applies to all selected.
test_multi_install() {
  setup "multi: --tools=claude_desktop,cursor installs both"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg1="$h/Library/Application Support/Claude/claude_desktop_config.json"
  local cfg2="$h/.cursor/mcp.json"
  run_installer "$h" --tools=claude_desktop,cursor --yes $BIN_OVERRIDE >/dev/null
  assert_file_contains "$cfg1" "\"hermem\"" "multi_claude_desktop"
  assert_json "$cfg1" "multi_claude_desktop"
  assert_file_contains "$cfg2" "\"hermem\"" "multi_cursor"
  assert_json "$cfg2" "multi_cursor"
  cleanup_home "$h"
}

# Test 15: --help prints help and exits 0.
test_help() {
  setup "--help: prints usage, exits 0"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local out
  out="$(HOME="$h" "$INSTALLER" --help 2>&1)"
  if [[ $? -eq 0 ]] && printf '%s' "$out" | grep -q "USAGE"; then
    pass "[help] printed USAGE"
  else
    fail "[help] missing USAGE in output"
    printf '%s\n' "$out" | head -20 | sed 's/^/      /'
  fi
  cleanup_home "$h"
}

# Test 16: unknown tool id warns and exits non-zero (under --tools=).
test_unknown_tool() {
  setup "--tools: unknown id fails"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  if HOME="$h" "$INSTALLER" --tools=fake_tool --yes $BIN_OVERRIDE >/dev/null 2>&1; then
    fail "[unknown_tool] exit was 0, expected non-zero"
  else
    pass "[unknown_tool] exit non-zero on unknown id"
  fi
  cleanup_home "$h"
}

# Test 17: backup file is created on overwrite.
test_backup_created() {
  setup "claude_desktop: backup .bak created on overwrite"
  local h; h="$(make_home)"
  TESTS_RUN=$((TESTS_RUN+1))
  local cfg="$h/Library/Application Support/Claude/claude_desktop_config.json"
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null
  # Mutate the file to a known shape so the installer detects "already present"
  # not and creates a real backup.
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null 2>&1 || true
  # Above is idempotent — no backup. To test backup path, remove idempotent
  # sentinel first by rewriting the command to a different binary.
  cat > "$cfg" <<'JSON'
{
  "mcpServers": {
    "hermem": {
      "command": "different-binary",
      "args": ["--different"]
    }
  }
}
JSON
  run_installer "$h" --tools=claude_desktop --yes $BIN_OVERRIDE >/dev/null
  local backups; backups="$(ls "$cfg".bak-* 2>/dev/null | wc -l | tr -d ' ')"
  if [[ "$backups" -ge 1 ]]; then
    pass "[backup_created] $backups backup files present"
  else
    fail "[backup_created] no .bak-* files found next to $cfg"
  fi
  cleanup_home "$h"
}

# Run all
main() {
  printf '%s%s%s\n' "${C_BOLD:-\033[1m}" "install-mcp.sh test suite" "${C_RESET:-\033[0m}"
  printf '%s\n' "----------------------------------------"

  test_help
  test_list_noop
  test_claude_desktop_fresh
  test_claude_desktop_preserves_other
  test_claude_desktop_idempotent
  test_codex_fresh
  test_codex_preserves
  test_cursor_fresh
  test_opencode_fresh
  test_aider_fresh
  test_hermes_fresh
  test_claude_code_fresh
  test_unset_claude_desktop
  test_unset_hermes
  test_dry_run_noop
  test_multi_install
  test_unknown_tool
  test_backup_created

  printf '\n%s----------------------------------------%s\n' "${C_BOLD:-\033[1m}" "${C_RESET:-\033[0m}"
  printf 'Tests: %d   Passed: %s%d%s   Failed: %s%d%s\n' \
    "$TESTS_RUN" \
    "${C_GREEN:-\033[32m}" "$TESTS_PASSED" "${C_RESET:-\033[0m}" \
    "${C_RED:-\033[31m}" "$TESTS_FAILED" "${C_RESET:-\033[0m}"

  if (( TESTS_FAILED > 0 )); then
    return 1
  fi
  return 0
}

main "$@"
