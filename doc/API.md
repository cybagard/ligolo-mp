# Ligolo-MP Operator API

The operator API is the gRPC service `Ligolo`, defined in
[`protobuf/ligolo.proto`](../protobuf/ligolo.proto). It is the single source of
truth for the contract; this document summarizes it. The TUI client, and the
[MCP server](./MCP_PLAN.md), are both ordinary clients of this API.

## Transport & authentication

- gRPC over mTLS on the operator listener (default `0.0.0.0:58008`).
- The client presents an **operator certificate**. The operator identity is the
  certificate's CommonName; the certificate thumbprint must match the stored
  operator (`operatorFromContext` in `cmd/server/rpc/rpc.go`).
- Authorization is per-RPC: admin-only RPCs require the operator to have admin
  privileges. The server enforces this on every call regardless of client.

The operator credential is the config JSON produced by `ExportOperator`
(`Name`, `Server`, `CA`, `Cert` incl. private key). It is a sensitive secret: it
grants that operator's full access.

## Error semantics (gRPC status codes)

Handlers return typed gRPC status codes so clients can react programmatically:

| Code | Meaning |
|---|---|
| `Unauthenticated` | Client certificate missing, unknown, or revoked. |
| `PermissionDenied` | A non-admin operator called an admin-only RPC. |
| `InvalidArgument` | Malformed input (e.g. bad IP for `Traceroute`, malformed operator server address). |
| `NotFound` | Referenced session does not exist. |
| `FailedPrecondition` | Refused by a server-side invariant (e.g. deleting the last admin operator). |
| `Unknown` / other | Unclassified service error; the message carries the detail. |

These are additive: existing clients that only render the error message continue
to work unchanged.

## RPC surface

### Read / query

| RPC | Admin | Purpose |
|---|:--:|---|
| `GetMetadata` | – | Calling operator's identity + server config. |
| `GetSessions` | – | All agent sessions: hostname, interfaces, routes, redirectors, relay state. |
| `Traceroute` | – | Which session/interface routes to an IP. `InvalidArgument` on a malformed IP. |
| `GetOperators` | ✔ | All operators and their online status. |
| `GetCerts` | ✔ | Certificate metadata (never private keys). |
| `Join` | – | Server → client stream of activity `Event`s. |

### Session & relay

| RPC | Admin | Notes |
|---|:--:|---|
| `RenameSession` | – | Set alias. `NotFound` if unknown. |
| `KillSession` | – | Disconnect agent and remove session. Destructive. |
| `StartRelay` / `StopRelay` | – | Bring the TUN pivot up / down. `NotFound` if unknown. |

### Routing

| RPC | Admin | Notes |
|---|:--:|---|
| `AddRoute` | – | Add a CIDR route to a session. |
| `EditRoute` | – | Replace a route's CIDR/metric/loopback. |
| `MoveRoute` | – | Move a route between sessions. |
| `DelRoute` | – | Remove a route. Destructive. |

### Redirectors

| RPC | Admin | Notes |
|---|:--:|---|
| `AddRedirector` | – | Create a listener that forwards to a destination via the agent. |
| `DelRedirector` | – | Remove a redirector. Destructive. |

### Admin

| RPC | Admin | Notes |
|---|:--:|---|
| `AddOperator` | ✔ | Create operator; server generates its cert. |
| `DelOperator` | ✔ | Delete + revoke. Destructive. `FailedPrecondition` on last (admin) operator. |
| `PromoteOperator` / `DemoteOperator` | ✔ | Change admin privilege. `FailedPrecondition` on last admin. |
| `ExportOperator` | ✔ | Returns a private-key-bearing operator config. **Not exposed via MCP.** |
| `RegenCert` | ✔ | Regenerate a certificate by name. Destructive. |
| `GenerateAgent` | ✔ | Compile an agent binary for a target platform. |

## Events

`Join` streams `Event { Type, Data }` where `Type` is `0` (info), `1` (error),
or `2` (warning) and `Data` is a human-readable line (e.g. "started relay to
X"). The MCP server exposes recent events as the `ligolo://events/recent`
resource.
