# Ligolo-MP: API & MCP Server — Design Plan

Status: **Proposed** · Branch: `feature/mcp-server` · Base: `main`

This document plans two related additions to Ligolo-MP:

1. A **stable, documented operator API** (the existing gRPC contract, formalized and lightly extended).
2. An **MCP (Model Context Protocol) server** that lets an LLM/agent drive Ligolo-MP
   pivoting operations through that API.

The guiding principle is **zero changes to the security model**: the MCP server is
just another operator client. It authenticates with an operator certificate over
mTLS exactly like the TUI, and it cannot do anything an operator with that
certificate could not already do.

---

## 1. Current architecture (as-is)

Ligolo-MP is already a client–server system with a clean gRPC boundary.

```
                         ┌────────────────────────────────────────┐
                         │              ligolo-mp (server)          │
   agents ──mTLS:11601──►│  agent server  ──┐                       │
                         │                  ▼                       │
                         │   internal/ services (business logic):   │
   TUI  ──mTLS:58008────►│   session · route · redirector ·         │
   client   (gRPC)       │   certificate · operator · asset · crl   │
                         │                  ▲                       │
                         │   gRPC operator server (rpc.go) ─────────┘
                         └────────────────────────────────────────┘
```

Key facts that shape this design (verified against the code):

| Concern | Where | Notes |
|---|---|---|
| API contract | `protobuf/ligolo.proto` | gRPC service `Ligolo`, ~25 RPCs. This *is* the API. |
| RPC handlers | `cmd/server/rpc/rpc.go` | Thin wrappers over `internal/*` services. |
| Auth | `rpc.go` `operatorFromContext` + `unaryAuthInterceptor` | mTLS; operator identity = client-cert CommonName; cert thumbprint must match stored operator. |
| Authorization | per-handler `oper.IsAdmin` checks | Operator/cert/agent-generation ops are admin-only. |
| Client connection | `internal/operator/entity.go` `Operator.Connect()` | Dials gRPC over mTLS from an operator config; returns a `pb.LigoloClient`. |
| Operator credential | operator config JSON (`Operator.ToBytes`/`ToFile`, `NewOperatorFromFile`) | Contains `Name`, `Server`, `CA`, and `Cert` (incl. private key). This is the portable credential a client imports. |
| Event stream | `Join` RPC → `stream Event`; `internal/events` | Server pushes human-readable activity events to all connected operators. |

### The existing RPC surface

Read/query: `GetMetadata`, `GetSessions`, `GetCerts`, `GetOperators`, `Traceroute`, `Join` (event stream).

Session/relay: `RenameSession`, `KillSession`, `StartRelay`, `StopRelay`.

Routing: `AddRoute`, `EditRoute`, `MoveRoute`, `DelRoute`.

Redirectors: `AddRedirector`, `DelRedirector`.

Admin: `AddOperator`, `DelOperator`, `PromoteOperator`, `DemoteOperator`, `ExportOperator`, `RegenCert`, `GenerateAgent`.

---

## 2. Design decision: MCP as a gRPC client, not a server plugin

Two options were considered:

**Option A — MCP server as an independent gRPC client (chosen).**
A new binary `ligolo-mp-mcp` that loads an operator config, connects via the
existing `operator.Operator.Connect()`, and exposes the `pb.LigoloClient` calls
as MCP tools over stdio (and optionally streamable HTTP).

- ✅ No changes to `ligolo-mp` server or its security model.
- ✅ Reuses the exact mTLS + operator-cert auth the TUI uses.
- ✅ Inherits admin/non-admin authorization for free (server enforces it).
- ✅ Can ship/upgrade independently; safe to run on an operator's workstation.
- ✅ Naturally composes with Claude Code / any MCP host.

**Option B — Embed MCP into the server process.**

- ❌ Widens the server's trust boundary and attack surface.
- ❌ Auth becomes ambiguous (which operator identity does an in-process MCP act as?).
- ❌ Couples MCP release cadence to server releases.

**We choose Option A.** The MCP server is a thin, auditable adapter:
`MCP tool call → pb.LigoloClient method → existing gRPC handler → internal service`.

```
┌─────────────┐   MCP     ┌──────────────────┐  gRPC/mTLS  ┌──────────────┐
│  MCP host   │◄────────► │  ligolo-mp-mcp   │◄──────────► │  ligolo-mp   │
│ (Claude etc)│  stdio /  │ (operator client)│   :58008    │   (server)   │
└─────────────┘  http     └──────────────────┘             └──────────────┘
                              loads operator config (Name/Server/CA/Cert)
```

---

## 3. Part 1 — Formalizing the API

No breaking changes. The gRPC service in `protobuf/ligolo.proto` remains the
single source of truth. Work items:

1. **Document the contract.** Add `doc/API.md` describing every RPC: purpose,
   request/response, admin-only flag, and emitted events. Generated from the
   proto comments where possible.
2. **Add proto comments.** Annotate each RPC and message in `ligolo.proto` so the
   contract is self-documenting and MCP tool descriptions can be derived from it.
3. **Stable error semantics.** RPC handlers currently return raw `error`s. Map
   them to gRPC status codes (`codes.PermissionDenied` for the `IsAdmin` checks,
   `codes.NotFound` for missing sessions/operators, `codes.InvalidArgument` for
   malformed input) so any API client — MCP included — can react programmatically.
   This is additive and backward-compatible for the TUI.
4. **(Optional, later) Read-only REST gateway.** A `grpc-gateway` or hand-written
   HTTP shim exposing the read RPCs (`GetSessions`, `GetMetadata`, `Traceroute`)
   for dashboards. Out of scope for the first MCP milestone; noted for completeness.

No new privileged operations are introduced. The API stays exactly as capable as
it is today.

---

## 4. Part 2 — The MCP server

### 4.1 Layout

```
cmd/mcp/
  main.go            # flags, config load, MCP server bootstrap (stdio / http)
internal/mcp/
  server.go          # MCP server wiring, tool registry
  client.go          # wraps operator.Operator: connect, reconnect, expose pb.LigoloClient
  tools_sessions.go  # session + relay tools
  tools_routes.go    # routing tools
  tools_redirectors.go
  tools_admin.go     # admin-gated tools (operators, certs, agent generation)
  tools_recon.go     # traceroute, metadata
  convert.go         # pb.* <-> MCP JSON shaping (human-friendly output)
  tools_test.go
```

### 4.2 Connection & auth

- Reuse `internal/operator`. The MCP server takes `--config <operator.json>`
  (the same file `ExportOperator` produces and the TUI imports via
  `NewOperatorFromFile`), builds an `*operator.Operator`, and calls `Connect()`.
- No new credential format, no new key handling. The operator private key never
  leaves the config file the user already trusts.
- On disconnect, transparently reconnect with backoff (the operator conn exposes
  `IsConnected()`).
- The MCP process should be run locally by the operator; document that the config
  file is a sensitive credential (grants that operator's full access).

### 4.3 MCP library

Use the official Go MCP SDK: **`github.com/modelcontextprotocol/go-sdk/mcp`**
(add to `go.mod`, vendored per the repo's `-mod=vendor` build). Rationale: matches
the repo's Go-only, vendored toolchain; first-party protocol support; stdio +
streamable-HTTP transports out of the box.

### 4.4 Tool catalog

Each gRPC RPC maps to one MCP tool. Names use a `ligolo_` prefix; inputs are JSON
schemas derived from the proto messages; outputs are shaped for LLM readability
(e.g. sessions rendered with resolved IPs, route CIDRs, relay/connection state).

| MCP tool | Backing RPC | Admin | Notes |
|---|---|:--:|---|
| `ligolo_get_metadata` | `GetMetadata` | – | Who am I, server config. |
| `ligolo_list_sessions` | `GetSessions` | – | Core recon: agents, interfaces, routes, redirectors, relay state. |
| `ligolo_rename_session` | `RenameSession` | – | |
| `ligolo_kill_session` | `KillSession` | – | **Destructive** (see 4.5). |
| `ligolo_start_relay` | `StartRelay` | – | **State-changing** — brings up the TUN pivot. |
| `ligolo_stop_relay` | `StopRelay` | – | State-changing. |
| `ligolo_add_route` | `AddRoute` | – | State-changing (network routing). |
| `ligolo_edit_route` | `EditRoute` | – | |
| `ligolo_move_route` | `MoveRoute` | – | |
| `ligolo_del_route` | `DelRoute` | – | **Destructive.** |
| `ligolo_add_redirector` | `AddRedirector` | – | |
| `ligolo_del_redirector` | `DelRedirector` | – | **Destructive.** |
| `ligolo_traceroute` | `Traceroute` | – | Recon: which session/iface reaches an IP. |
| `ligolo_list_operators` | `GetOperators` | ✔ | |
| `ligolo_list_certs` | `GetCerts` | ✔ | Metadata only; avoid dumping private keys. |
| `ligolo_add_operator` | `AddOperator` | ✔ | |
| `ligolo_del_operator` | `DelOperator` | ✔ | Destructive. |
| `ligolo_promote_operator` | `PromoteOperator` | ✔ | Privilege change. |
| `ligolo_demote_operator` | `DemoteOperator` | ✔ | Privilege change. |
| `ligolo_regen_cert` | `RegenCert` | ✔ | |
| `ligolo_generate_agent` | `GenerateAgent` | ✔ | Produces an agent binary (bytes). |

**Event stream → MCP resource.** `Join` (server → client `stream Event`) is
exposed as an MCP **resource** `ligolo://events/recent` backed by an in-memory
ring buffer the MCP server fills from the `Join` stream. This gives the agent
recent activity ("X started relay to Y") without a long-lived tool call. (MCP
notifications/subscriptions can be added later if a host supports them.)

### 4.5 Safety rails (MCP-side, defense in depth)

The server already authorizes every call, but the MCP layer adds guardrails
appropriate for an autonomous agent operating an offensive-security tool:

- **Tool annotations.** Mark read tools `readOnlyHint: true`; mark `kill_session`,
  `del_route`, `del_redirector`, `del_operator` `destructiveHint: true`.
- **Default read-only mode.** `--allow-writes=false` by default. Write/destructive
  and admin tools are only *registered* when explicitly enabled
  (`--allow-writes`, `--allow-admin`). An agent given the default config can
  observe but not change the engagement.
- **`generate_agent` handling.** Returns a saved file path + metadata rather than
  streaming a large binary blob into the model context; gated behind
  `--allow-admin`.
- **Large-output shaping.** Certs never emit private-key bytes through MCP; agent
  binaries are written to disk, not returned inline.
- **Auditability.** Every tool invocation logs to the MCP server's slog with the
  operator name; server-side events already record the acting operator.

### 4.6 Configuration surface (`cmd/mcp` flags)

```
--config <path>       operator config JSON (required)
--transport stdio|http (default stdio)
--http-addr <addr>    when transport=http (default 127.0.0.1:0)
--allow-writes        register state-changing/destructive tools (default false)
--allow-admin         register admin-only tools (default false)
--agent-out <dir>     where generate_agent writes binaries
-v                    verbose logging
```

Example MCP host registration (Claude Code / any MCP client):

```json
{
  "mcpServers": {
    "ligolo-mp": {
      "command": "ligolo-mp-mcp",
      "args": ["--config", "/path/to/operator_ligolo-mp.json", "--allow-writes"]
    }
  }
}
```

---

## 5. Build & tooling

- Add a `mcp` target to the `Makefile` mirroring the `client`/`server` targets
  (`CGO_ENABLED=0`, `-mod=vendor`, `-trimpath`, version ldflags).
- Add the MCP SDK to `go.mod` and `go mod vendor`.
- Add `ligolo-mp-mcp` to `.goreleaser.yaml` build matrix.
- Unit-test the tool layer with a mocked `pb.LigoloClient` (the interface makes
  this straightforward — no live server needed).

---

## 6. Milestones

1. **M1 — Read-only MCP (MVP).** ✅ *Implemented.* `cmd/mcp` + connection reuse
   (`internal/mcp`) + read tools (`ligolo_get_metadata`, `ligolo_list_sessions`,
   `ligolo_traceroute`, and `ligolo_list_operators`/`ligolo_list_certs`
   registered only when the connected operator is admin) + the
   `ligolo://events/recent` resource. Default-safe, read-only. No server changes.
   Adds the `github.com/modelcontextprotocol/go-sdk` dependency (which raises the
   module's Go directive to 1.25).
2. **M2 — Write tools.** ✅ *Implemented.* Relay (`ligolo_start_relay`/
   `ligolo_stop_relay`), routing (`ligolo_add_route`/`edit`/`move`/`del`),
   redirectors (`ligolo_add_redirector`/`del`), and `ligolo_rename_session`/
   `ligolo_kill_session`, all registered only behind `--allow-writes`.
   Destructive tools (kill session, del route, del redirector) carry
   `destructiveHint`; the rest are marked non-read-only. Tests cover flag
   gating, argument propagation, and required-argument validation.
3. **M3 — Admin tools.** ✅ *Implemented.* Operator management
   (`ligolo_add_operator`/`del`/`promote`/`demote`), `ligolo_regen_cert`, and
   `ligolo_generate_agent`, behind `--allow-admin` and registered only for an
   admin operator. `generate_agent` writes the binary to `--agent-out` and
   returns a path + size (never the bytes). Operator export (private-key-bearing
   credential) is intentionally not exposed.
4. **M4 — API hardening.** gRPC status codes, proto comments, `doc/API.md`.
5. **M5 — (Optional) REST read gateway** for dashboards.

M1–M3 require **no changes to the ligolo-mp server**; only M4 touches server code
(additively). This keeps the risky surface minimal and lets the MCP feature land
and be reviewed independently.

---

## 7. Out of scope / non-goals

- No new operator capabilities beyond what the gRPC API already exposes.
- No change to the mTLS / operator-cert trust model.
- No embedding of MCP into the server process.
- No exposure of raw private-key material through MCP tools.
