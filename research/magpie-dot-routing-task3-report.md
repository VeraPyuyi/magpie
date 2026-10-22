# Task 3 report â€” Magpie Dot routing: gateway text adapter and replay barrier

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

**Waiting and responses.** 120 s answer wait (config 1â€“600) with 2 s polls
through `ByRequest`, which revalidates receipt and result identity. Group
`FirstToken` is not armed for Dot and `watchDot` sends only SSE comments under
the existing hold-writer mutex â€” no `keepAlive` from the poll goroutine.
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
head â€” it fails identically. CGO is unavailable on this box (`-race requires
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

## Review fix pass ¡ª 2026-10-19

This append supersedes inaccurate claims in the original report. The original
Task3 code did not write ledger rows for failed/pending/unknown jobs, and CGO
was available through C:\msys64\mingw64\bin\gcc.exe. The earlier claim that
race testing was impossible was incorrect. The controller's sparse race PASS
(72.52 s) did not exercise completed streams and did not rule out C1.

Addressed C1 and I2¨CI8 from the external Task3 review:

- C1: headers are initialized on the real holdWriter before its watch starts;
  serveDot writes no raw response headers. Completed streams call WriteHeader
  under the hold writer lock and emit through wholeAsStream's already-begun
  SSE writer. Both Chat and Responses completion paths are exercised under
  concurrent keepQuiet, including the real Handler. Loop detection is disabled
  for Dot's buffered output so it cannot turn a verified result into a reask.
- I2: dotScope uses access.Caller(ctx).KeyID, with fixed local scope only when
  no named key authenticated. Real lanGuard + Handler requests with identical
  stale Authorization and distinct valid x-api-key values produce two POSTs;
  repeating the first caller performs lookup only.
- I3: raw Chat messages, Responses items and text parts reject extra keys,
  including assistant tool_calls/function_call, name/tool_call_id and unknown
  nested fields. service_tier is unsupported rather than silently discarded.
- I4: call.To is set before Enqueue. Actual usage.Load rows for pending,
  failed and uncertain attempts retain errors and zero unknown token counts.
- I5: the exact rendered response ID is read once from the native answer and
  recorded; Chat chatcmpl- and Responses resp_ IDs now match wire and ledger.
- I6: Try.Status/Call.Status retain HTTP 200 after keepalives; Try.DotError
  separately carries the semantic code, and Fail is other (canceled for client
  disconnect). Whole-response errors retain 409. Error text persists in usage
  even with HTTP 200. Successful results also run rule/sealedBy bookkeeping.
- I7/I8: tests cover completed native streams and one finish marker, no usage,
  keepalive-before-error ordering controlled by Flush notification, zero Cancel
  POSTs on disconnect after submit, 0->120-second configuration default and
  rejected bounds without sleeping, a prior busy provider plus nested patience
  group, and an eligible small rescue member that is never dispatched after
  acceptance. Counting bridge assertions retain exactly one POST.

Related minors: snapshot JSON keys are lower snake case; IsDigestConflict in
Attempt uses errors.Is (a narrow exported query, no mutation hook); poll receipt
failures become unknown, unknown states stop waiting, pl.left is included in
preflight detection, and dead test journal setup was removed. Pre-submit
configuration/journal availability failures may fall back; invalid IDs and
reused IDs with conflicting content intentionally stop locally. They do not
permit another provider to answer under a conflicting durable identity.

RED evidence before production edits:

- TestDotReviewNestedWireFields: assistant history was queued (1 POST, 409)
  instead of locally rejected; unknown item/part and service_tier admitted.
- TestDotReviewAuthenticatedScopes: 1 POST/2 GETs instead of 2 POSTs/1 GET.
- TestDotReviewFailureLedger: no Dot ledger rows for all three failure modes.
- TestDotReviewCompletedStreams: recorded raw job IDs differed from wire IDs.
- TestDotReviewKeepaliveErrorAndCancel: Try reported 409 and empty Fail after
  actual HTTP 200/comments.
- Forced overlap TestDotReviewConcurrentHeader under MinGW -race reproduced
  sseWriter.begin header writes racing keepAlive's map iteration, followed by
  fatal error: concurrent map iteration and map write. Full retained log:
  C:/Users/13678/AppData/Local/Temp/opencode/magpie-context-verification/magpie-dot-task3-header-red-race.log
  Earlier attempts did not schedule the overlapping map iteration and did not
  detect the race; those PASS runs were not treated as proof of safety.

GREEN evidence on the final edits:

- external run_checks.py --race ... go test -race -tags nogui
  ./internal/gateway ./internal/provider ./internal/dotbridge -run
  '^TestDot|^TestGroupFirstTokenFailsOver$|^TestNestedGroupFirstToken$|^TestMemberTooSmall|KeepAlive|Keepalive|^TestLANGuard$'
  -count=1 -timeout 120s: exit 0, 78.26 seconds.
  Log: C:/Users/13678/AppData/Local/Temp/opencode/magpie-context-verification/magpie-dot-task3-review-final-race.log
- go test -tags nogui ./internal/gateway ./internal/dotbridge -run '^TestDot'
  -count=1 -timeout 90s: PASS (23.345 s and 2.866 s).
- go vet -tags nogui ./internal/gateway ./internal/dotbridge: exit 0.
- gofmt -l on all owned changed Go files and git diff --check: empty output.

No full repository validation, live Dot/cloud acceptance, deployment or overall
project completion is claimed by this fix pass. No subagents. Active CLI/GUI
and provider configuration work was not edited or staged. Polling continues
through validated ByRequest because Get returns an unchecked DTO.
- After adding the explicit completed-stream ledger ResponseID equality
  assertion, the focused completed/concurrent-header/keepalive -race run also
  passed: exit 0, 33.14 seconds (magpie-dot-task3-ledger-stream-race.log).
