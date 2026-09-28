#!/usr/bin/env bash
#
# mcp-smoke.sh — end-to-end smoke test for the ligolo-mp MCP server.
#
# Drives ligolo-mp-mcp over stdio (newline-delimited JSON-RPC) against a REAL,
# running ligolo-mp server and asserts the core read path works: initialize,
# tools/list, ligolo_get_metadata, ligolo_list_sessions, and the events
# resource. Optionally checks that write tools appear only with --allow-writes.
#
# Prerequisites:
#   - a running ligolo-mp server
#   - an operator config JSON (the file the TUI exports/imports:
#     <name>_<server>_ligolo-mp.json) — it holds the operator's private key
#   - the ligolo-mp-mcp binary (build with `make mcp`)
#
# Usage:
#   scripts/mcp-smoke.sh -c /path/operator_ligolo-mp.json [-b ./ligolo-mp-mcp] [--writes]
#
set -euo pipefail

BIN="./ligolo-mp-mcp"
CONFIG=""
CHECK_WRITES=0
TIMEOUT="${MCP_SMOKE_TIMEOUT:-20}"

usage() {
	sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		-c|--config) CONFIG="${2:-}"; shift 2 ;;
		-b|--bin)    BIN="${2:-}"; shift 2 ;;
		--writes)    CHECK_WRITES=1; shift ;;
		-h|--help)   usage 0 ;;
		*) echo "unknown argument: $1" >&2; usage 2 ;;
	esac
done

fail() { echo "FAIL: $*" >&2; exit 1; }

[[ -n "$CONFIG" ]] || { echo "error: -c/--config is required" >&2; usage 2; }
[[ -f "$CONFIG" ]] || fail "operator config not found: $CONFIG"
[[ -x "$BIN" ]] || fail "mcp binary not found or not executable: $BIN (build with 'make mcp')"

HAVE_JQ=0
if command -v jq >/dev/null 2>&1; then HAVE_JQ=1; fi

MCP_ARGS=(--config "$CONFIG")
if [[ "$CHECK_WRITES" -eq 1 ]]; then MCP_ARGS+=(--allow-writes); fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
out="$workdir/out.jsonl"
errlog="$workdir/err.log"

# JSON-RPC request sequence. notifications/initialized carries no id.
requests=(
	'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mcp-smoke","version":"0"}}}'
	'{"jsonrpc":"2.0","method":"notifications/initialized"}'
	'{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
	'{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ligolo_get_metadata","arguments":{}}}'
	'{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ligolo_list_sessions","arguments":{}}}'
	'{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"ligolo://events/recent"}}'
)

echo "== running $BIN ${MCP_ARGS[*]} =="
if ! printf '%s\n' "${requests[@]}" | timeout "$TIMEOUT" "$BIN" "${MCP_ARGS[@]}" >"$out" 2>"$errlog"; then
	rc=$?
	# timeout returns 124; the server exiting cleanly on stdin EOF returns 0.
	if [[ $rc -ne 0 && $rc -ne 124 ]]; then
		echo "--- stderr ---" >&2; cat "$errlog" >&2
		fail "mcp server exited with status $rc (is the ligolo-mp server reachable? check the operator config)"
	fi
fi

[[ -s "$out" ]] || { echo "--- stderr ---" >&2; cat "$errlog" >&2; fail "no JSON-RPC output from server"; }

# --- assertions ------------------------------------------------------------

# line_for_id ID -> prints the response object whose "id" matches.
line_for_id() {
	local id="$1"
	if [[ "$HAVE_JQ" -eq 1 ]]; then
		jq -c --argjson id "$id" 'select(.id == $id)' "$out" 2>/dev/null | head -n1
	else
		grep -E "\"id\"[[:space:]]*:[[:space:]]*$id([^0-9]|$)" "$out" | head -n1
	fi
}

check_contains() { # haystack needle label
	case "$1" in
		*"$2"*) echo "ok: $3" ;;
		*) fail "$3 (missing: $2) — got: $1" ;;
	esac
}

# 1) initialize
init="$(line_for_id 1)"
[[ -n "$init" ]] || fail "no initialize response"
check_contains "$init" '"result"' "initialize returned a result"

# 2) tools/list contains the read tools
tools="$(line_for_id 2)"
[[ -n "$tools" ]] || fail "no tools/list response"
for t in ligolo_get_metadata ligolo_list_sessions ligolo_traceroute; do
	check_contains "$tools" "$t" "tools/list advertises $t"
done

# write tools must be gated by --allow-writes
if [[ "$CHECK_WRITES" -eq 1 ]]; then
	check_contains "$tools" "ligolo_add_route" "write tools present with --allow-writes"
else
	case "$tools" in
		*ligolo_add_route*) fail "write tool advertised without --allow-writes" ;;
		*) echo "ok: write tools absent without --allow-writes" ;;
	esac
fi

assert_tool_ok() { # id label
	local resp; resp="$(line_for_id "$1")"
	[[ -n "$resp" ]] || fail "no response for $2 (id $1)"
	if [[ "$HAVE_JQ" -eq 1 ]]; then
		local is_err; is_err="$(printf '%s' "$resp" | jq -r '.result.isError // false')"
		[[ "$is_err" == "true" ]] && fail "$2 returned a tool error: $resp"
		[[ "$(printf '%s' "$resp" | jq -r 'has("error")')" == "true" ]] && fail "$2 returned a protocol error: $resp"
		echo "ok: $2 succeeded"
	else
		case "$resp" in
			*'"isError":true'*) fail "$2 returned a tool error: $resp" ;;
			*'"error"'*)        fail "$2 returned a protocol error: $resp" ;;
			*) echo "ok: $2 succeeded" ;;
		esac
	fi
}

# 3/4) tool calls succeed
assert_tool_ok 3 "ligolo_get_metadata"
assert_tool_ok 4 "ligolo_list_sessions"

# 5) events resource is readable
events="$(line_for_id 5)"
[[ -n "$events" ]] || fail "no resources/read response"
check_contains "$events" 'ligolo://events/recent' "events resource is readable"

echo
echo "PASS: MCP server end-to-end smoke test succeeded"
