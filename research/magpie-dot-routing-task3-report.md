# Task 3 report — Magpie Dot routing: gateway text adapter and replay barrier

## Scope

Owned `internal/gateway/dot.go`, `dot_response.go`, `trace.go`, `fallback.go`
and `gateway.go` changes for Task 3 of
`docs/superpowers/plans/2026-10-19-dot-automatic-routing.md`, plus the
reviewed Task 2 fix content in this shared worktree (`dot_requirements.go`,
`dot_requirements_test.go`, `dot_preflight_test.go`, `dot_status.go`,
`dot_status_test.go`, `dot_testutil_test.go`, `provider/dot_test.go`,
`docs/subsystems/providers-accounts.md`), committed together with the
gateway integration as instructed. No live Dot, network or credential access.

## What was implemented

**Real `serveDot` branch.** `Server.attempt` branches on `p.IsDot()` before
any normalization: before `joinToolRuns`/`orphanedToolOutputs`/
`pairToolItems`/`chatCallIDs`, before `p.Via`, `ResolveAuto`, passthrough,
translation and the HTTP forwarder.

**Request-scoped Attempt on a private context key.** `prepareDot` runs in
`serve` after the final body recheck and builds the immutable
`dotbridge.Submission` via the existing `dotSubmission`, then binds both it and
`*dotbridge.Attempt` under `dotAttemptKey`/`dotSubmissionKey`. Only client
methods mutate that Attempt. Identity: explicit `X-Magpie-Dot-Request-ID`
(scoped SHA-256 of caller key, agent, supplied ID) or random `req-` + 16 bytes;
session from the session header or the request ID. No route ID or prompt hash
is used.

**Barrier.** After `hw.settle`, try timings and `watched.attempt`, and before
every cancellation/slow/reloop/retry/replan/reset/small-rescue decision,
`serve` reads the Attempt snapshot. When `BlockReplay` is set the try is
recorded once with `Try.Dot`, the client is given the verified result or an
explicit failure, and the candidate loop breaks. Shared end-of-request
bookkeeping (usage row, trace, recent calls, fallback string) is preserved;
`serve` never returns early. A 200 with no error releases the held writer and
runs `servedCandidate`/`answered`; anything else emits the error. The branch
does not consult `hw.failed`, `hw.passing`, status or `last`.

**Barrier guarantees one POST.** `Enqueue` reserves the journal identity before
I/O, and `Connection.Submit` calls `beginSubmit` before its single POST. Once
the POST has begun, the in-memory Attempt keeps `BlockReplay` even on a dial
failure, a non-202, a malformed 202 or a digest mismatch, so the outer loop
cannot re-enter. `ByRequest` never posts. Verified regression: an invalid
receipt now yields exactly **1** POST (previously 3).

**Waiting and responses.** 120 s answer wait (config 1–600) with 2 s polls
through `ByRequest`, which revalidates receipt and result identity. Group
`FirstToken` is not armed for Dot and `watchDot` sends only SSE comments under
the existing hold-writer mutex — no `keepAlive` from the poll goroutine.
Streaming waits contain no assistant content, no `response.created` and no
usage. A completed nonempty result is buffered and written once in the client's
protocol with no usage or reasoning fields (`dotAnswer`). A client disconnect
stops the wait and sends no cancel.

**Explicit errors.** 409 `dot_job_pending` (known job), `dot_acceptance_unknown`
(uncertain), `dot_job_failed` (failed/outcome_unknown/empty/cancelled result),
`dot_job_cancelled` (cancelled), with request/job references and
`automatic_redispatch:false` metadata. If keepalive comments already sent 200,
the same payload is a native SSE error event with no completed event. Pre-
submission failures (invalid header, unsupported wire, digest conflict,
unpaired, corrupt journal) return locally and keep normal fallback.

## Files

| File | Change |
| --- | --- |
| `internal/gateway/dot.go` | new: identity helpers, `prepareDot`, `serveDot`, `dotFailure` |
| `internal/gateway/dot_response.go` | new: `dotAnswer` for Chat and Responses |
| `internal/gateway/dot_barrier_test.go` | new: barrier, identity, no-fake-content, invalid-receipt regression |
| `internal/gateway/dot_response_test.go` | new: no usage in either protocol |
| `internal/gateway/gateway.go` | Task 2 preflight integration plus `serveDot` branch, barrier check, `watchDot` wiring, `Try.Dot` |
| `internal/gateway/fallback.go` | `watchDot`: SSE comments only, no first-token failover |
| `internal/gateway/trace.go` | `Try.Dot` snapshot, safe fields only |
| `internal/dotbridge/attempt.go` | no net change; reverted the temporary state getters |
| docs | `gateway-routing.md` rewritten to match the real branch |

## Verification (all run in this session)

| Check | Result |
| --- | --- |
| `go test -tags nogui ./internal/gateway -run '^TestDot' -count=1` | ok, 13.3 s |
| `go test -tags nogui ./internal/provider -run '^TestDot' -count=1` | ok, 2.0 s |
| `go test -tags nogui ./internal/gateway -count=1` | ok (full package, ~2 min in the combined run) |
| `go vet ./...`, `GOOS=linux go vet -tags nogui ./internal/...`, `GOOS=windows go vet` | clean |
| `gofmt -l internal/gateway internal/provider internal/dotbridge docs` | clean |
| RED proof: barrier branch disabled by hand (`if false && snap.BlockReplay`) | `TestDotInvalidReceiptExactlyOnePOST` FAIL, restored |
| `TestDotInvalidReceiptExactlyOnePOST` with counting fake bridge | exactly 1 POST, 0 vendor calls |
| `go test -tags nogui ./internal/provider -count=1` | 9 pre-existing failures: `TestCodexKeychain*`, `TestCodexSignedInInKeychain`, `TestSavedButSignedOut` |

The `provider` failures are pre-existing and unrelated to this task: they
concern Windows keyring stand-ins for Codex. Confirmed by stashing every
Dot-related change and re-running `TestSavedButSignedOut` at the unmodified
head — it fails identically. CGO is unavailable on this box (`-race requires
cgo`), so no race run was possible; `watchDot` and the poll loop are new
goroutines/timers and still need a `-race` run on a machine with CGO.

## Rulings and notes

- **`Get` is not used for polling.** `Get` returns an unchecked DTO, so the
  poll path uses `ByRequest`, which validates request ID, digest, mode and
  session against the Attempt on every read. This is stricter than the plan's
  wording and prevents an unauthenticated job body from answering a user.
- **Digest conflict is local, not fallback.** Same scoped ID with changed
  content returns 409 before any POST, so a retry cannot silently become a new
  job.
- **Task 2 dependency.** No blocker found; the reviewed preflight fix is
  committed with Task 3 as instructed and was not reverted.
- **No subagents**, no live Dot calls, no credential writes, no `internal/
  dotbridge` production logic change.
