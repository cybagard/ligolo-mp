# ligolo-mp — Security Assessment (Threat Model + Findings)

Scope: static review of `cybagard/ligolo-mp` (server, agent, operator client).
Focus: external attack surface and remotely reachable vulnerabilities.
Defensive review of the project's own code.

> **Status:** All findings below have been remediated on the `docs/security-assessment`
> branch. See the *Remediation* section at the end for the fix locations.

---

## 1. Architecture & trust boundaries

Three components, three trust zones:

```
 operator client  --mTLS gRPC-->  [ ligolo-mp SERVER ]  <--mTLS/yamux--  agent
 (trusted user)   :58008          (the C2, trusted)      :11601          (runs on TARGET,
                                        |                                  HOSTILE zone)
                                   gvisor netstack (tun)
                                        ^
                                        | relays raw IP packets
                                   TARGET NETWORK (HOSTILE)
```

External / attacker-adjacent surfaces, most-exposed first:

| # | Surface | Bind | Auth | Peer trust |
|---|---------|------|------|------------|
| S1 | Agent listener | `0.0.0.0:11601` | mTLS (optional `-insecure-agents`) | **Agent runs in hostile territory.** Certs + binary sit on the target; a defender who owns the box owns the agent end. |
| S2 | Operator gRPC | `0.0.0.0:58008` | mTLS (client cert + revocation) | Operators — semi-trusted, but authz bugs matter. |
| S3 | gvisor netstack / tun | n/a | none | Processes **raw IP packets relayed from the target network** = fully attacker-controlled. |
| S4 | Agent binary | outbound | mTLS to server | Server is trusted by agent; agent hardened with `recover()`. |

The critical asymmetry driving the top findings: **the agent protects itself**
(`artifacts/agent/agent.go:69` wraps its handler loop in `defer func(){ recover() }()`),
but **the server does not** — no `recover()` exists anywhere in `internal/` or
`cmd/`. Any panic in a server goroutine that processes agent/target data
terminates the entire C2 process, dropping every session.

---

## 2. Findings

### F1 — Remote server crash (DoS) via unchecked type assertions on agent data — **HIGH**

The server decodes protocol messages from the agent and then does *single-value*
type assertions on the decoded payload:

- `internal/session/entity.go:306` — `.(protocol.InfoReplyPacket)`
- `internal/session/entity.go:362` — `.(protocol.RedirectorResponsePacket)`
- `internal/session/entity.go:400-401` — `.(protocol.RedirectorCloseResponsePacket)`
- `internal/netstack/netstack.go:261` — `.(protocol.ConnectResponsePacket)`
- `internal/netstack/netstack.go:370` — `.(protocol.HostPingResponsePacket)`

The decoder (`internal/protocol/decoder.go`) accepts **any** of the 14 valid
message types and fills `Envelope.Payload` accordingly. Nothing forces the reply
type to match the request. A malicious or compromised agent that answers, e.g.,
an `InfoRequest` with a well-formed `ConnectResponse` envelope makes
`gobdecoder.Decode` succeed but the assertion `x.(InfoReplyPacket)` **panic**.

Because no server goroutine has a `recover()`, an unrecovered panic **crashes the
whole ligolo-mp server**, disconnecting all operators and all other sessions.

Reachability is trivial and pre-auth-of-operator:
`remoteGetInfo` is called from `Session.Connect` (`entity.go:159`), which runs
inside `SessionService.NewSession` — invoked the moment an agent connects
(`cmd/server/agents/agents.go:156`, in the `startHandler` goroutine). So **an
attacker who realizes a box is a ligolo agent (or who captures the agent binary
+ its embedded cert) can connect a hand-rolled "agent" and crash the operator's
C2 at session-establishment time**, with no operator interaction.

Contrast: the same class of assertion on the agent side
(`agent.go:193,237,289,315`) is survivable because of the `recover()` at
`agent.go:69`. The server needs the same protection.

**Fix:** use the two-value form everywhere (`reply, ok := x.(T); if !ok { return err }`),
and/or validate `Envelope.Type` matches the expected response before asserting,
and wrap the per-connection/per-packet server goroutines
(`startHandler`, `HandlePacket`, `handleICMP`) in a `recover()`.

---

### F2 — Unbounded / negative allocation in protocol decoder — **HIGH**

`internal/protocol/decoder.go:24-34`:

```go
binary.Read(d.reader, binary.LittleEndian, &d.Envelope.Size) // Size is int32
payload := make([]byte, d.Envelope.Size)
io.ReadFull(d.reader, payload)
```

`Envelope.Size` is a **signed `int32`** (`packets.go:12`) read directly off the
wire with no bounds check.

- **Negative size** → `make([]byte, negative)` panics (`makeslice: len out of
  range`) → server crash via the same no-`recover` path as F1.
- **Large positive size** (up to ~2 GiB) → the server eagerly allocates that
  buffer per message → memory exhaustion / OOM-kill. A few concurrent yamux
  streams multiply it.

There is no maximum-message cap and no `io.LimitReader`. Same reachability as F1
(malicious agent, or `-insecure-agents` mode where anyone can be an agent).

**Fix:** reject `Size < 0` and `Size > MaxMessageSize`; read through a bounded
`io.LimitReader`; consider allocating incrementally.

---

### F3 — `encoding/gob` on untrusted input — **MEDIUM**

The decoder feeds attacker-controlled bytes to `gob.NewDecoder(...).Decode`.
`gob` is not designed to be robust against adversarial input: nested/recursive
type descriptions and large declared lengths can drive CPU and memory
independently of F2's outer cap. This compounds F2.

**Fix:** enforce the outer size cap first (F2), and treat gob purely as a
performance/DoS concern — a length-prefixed, field-bounded codec would be safer
for a security tool whose peer lives in hostile territory.

---

### F4 — Agent listener does not check certificate revocation — **MEDIUM**

`cmd/server/agents/agents.go:74-92` `VerifyPeerCertificate` verifies the agent
cert chains to the CA but **never calls `certService.IsRevoked`**. The operator
path does (`cmd/server/rpc/rpc.go:650`).

All agents are minted off the same `__AGENTS`/CA lineage with **empty
CommonName** (`GenerateAgent` → `GenerateCert("", CACert)`), so certs are not
individually distinguishable and revocation is effectively the *only* kill
switch for a leaked agent credential — and it is not enforced on the agent
listener. A revoked/leaked agent cert keeps working.

**Fix:** add the same `IsRevoked` check to the agent `VerifyPeerCertificate`.

---

### F5 — `-insecure-agents` removes agent authentication entirely — **MEDIUM (config)**

`agents.go:61-64`: with `config.InsecureAgents`, `ClientAuth` becomes
`tls.NoClientCert`. Combined with the default bind `0.0.0.0:11601`, **anyone who
can reach the port can register as an agent**, create sessions, feed the gvisor
netstack, and trigger F1/F2. This is a documented flag, but its blast radius
(full pre-auth access to the agent plane + the DoS primitives above) warrants a
loud warning and, ideally, binding to a restricted interface when set.

---

### F6 — `GenerateAgent` has no authorization gate — **LOW/MEDIUM**

Every other sensitive RPC checks `oper.IsAdmin` (`GetOperators`, `AddOperator`,
`Del/Promote/DemoteOperator`, `GetCerts`, `RegenCert`). **`GenerateAgent`
(`rpc.go:286`) does not.** Any authenticated operator can:

1. Trigger a full server-side `go build` / `garble` compile (CPU/mem/disk heavy;
   `internal/asset/service.go`, `internal/gogo/go.go`) — resource-amplification
   DoS from a low-priv operator.
2. Mint an unlimited number of valid agent certificates off the CA.

**Fix:** decide whether agent generation is admin-only; if not, rate-limit and
cap concurrent compiles.

---

### F7 — String interpolation into compiled agent source — **LOW**

`internal/asset/service.go:96-147` renders `agent.go` with `text/template`,
injecting `ProxyServer`, `Servers`, and the certs into backtick-delimited Go
string literals (`agent.go:45-49`). Inputs are partly validated
(`net.SplitHostPort`, proxy scheme allowlist) and `CGO_ENABLED=0`, so build-time
server RCE is not demonstrated — but a value containing a backtick breaks out of
the raw-string literal, and raw interpolation into source that is then compiled
is fragile. Harden with `strconv.Quote`/`%q` or pass values via `-ldflags`/
embedded assets rather than source templating.

---

### F8 — Defensive: unchecked auth-info assertion & cert indexing — **INFO**

- `rpc.go:559` `p.AuthInfo.(credentials.TLSInfo)` — unchecked; panics if creds
  aren't TLS.
- `rpc.go:561` `VerifiedChains[0][0]`, `rpc.go:634` / `agents.go:75` `rawCerts[0]`
  — indexed without a length check inside `VerifyPeerCertificate`. Guarded in
  practice by `RequireAndVerifyClientCert`, but an empty `rawCerts` would panic
  (again, no `recover`). Add `ok`/length checks.

---

### F9 — Netstack: `panic(err)` on ICMP write + disabled checksum — **INFO/LOW**

`internal/netstack/netstack.go:457` `ProcessICMP` does `panic(err)` if
`WriteHeaderIncludedPacket` fails — reachable while replying to echo requests for
target hosts, i.e. another target-influenced crash path (no `recover`). The
in-stack ICMP checksum validation is commented out (`netstack.go:394-399`).
The stack also runs with `SetSpoofing(1,true)`, `SetPromiscuousMode(1,true)` and
an allow-all route table — inherent to the tool, but it means the server-side
stack will process whatever src/dst the hostile network emits; keep the DoS
hardening (F1/F2) in mind since this is all reachable from the target side.

---

## 3. Priority

1. **F1 + F2** — remote, pre-interaction, crashes the C2. Fix together: bound the
   decoder, use two-value assertions, add `recover()` to server goroutines.
2. **F4, F5** — agent-plane authentication/revocation gaps.
3. **F6** — authz gap + resource amplification on agent generation.
4. **F3, F7, F8, F9** — hardening.

All top findings share one root cause worth a single systemic fix: **the server
trusts that a TLS-authenticated agent is well-behaved.** In this tool's own
threat model the agent lives on the target — the most hostile place in the
deployment — so agent-supplied bytes must be treated as adversarial: bounded,
type-checked, and panic-isolated.

---

## 4. Remediation (applied on this branch)

| # | Fix | Location |
|---|-----|----------|
| F1 | Server-side payload assertions converted to checked two-value form; per-packet handler (`HandlePacket`) and per-connection agent handler (`handleAgentConn`) wrapped in `recover()` so a hostile agent can no longer panic-crash the server. | `internal/session/entity.go`, `internal/netstack/netstack.go`, `cmd/server/agents/agents.go` |
| F2 | Decoder rejects negative and oversized `Envelope.Size` (cap `MaxEnvelopeSize` = 16 MiB) before allocating; full payload read via `io.ReadFull`. Applied to both server and agent decoders. Regression tests added. | `internal/protocol/{decoder,packets}.go`, `internal/protocol/codec_test.go`, `artifacts/agent/internal/protocol/{decoder,packets}.go` |
| F3 | Mitigated by the F2 size cap bounding gob input. | `internal/protocol/decoder.go` |
| F4 | Agent listener now rejects revoked certificates (`IsRevoked`), matching the operator path. | `cmd/server/agents/agents.go` |
| F5 | Loud warning logged when `-insecure-agents` disables agent authentication. | `cmd/server/agents/agents.go` |
| F6 | `GenerateAgent` now requires an admin operator (consistent with `GetCerts`/`RegenCert`). **Behavioral change** — non-admin operators can no longer generate agents. | `cmd/server/rpc/rpc.go` |
| F7 | Agent-generation inputs rejected if they contain a backtick (would break out of the raw-string literals in the generated source). | `internal/asset/service.go` |
| F8 | Defensive length/type checks on `AuthInfo`, `VerifiedChains`, and `rawCerts` in both TLS verifiers. | `cmd/server/rpc/rpc.go`, `cmd/server/agents/agents.go` |
| F9 | `ProcessICMP` no longer `panic`s on a write error (logs and returns); covered by the `HandlePacket` recover as well. | `internal/netstack/netstack.go` |

Verified: `go build ./cmd/server/`, agent module `go build ./internal/protocol/`, and
`go test ./internal/protocol/ ./internal/session/` all pass.
