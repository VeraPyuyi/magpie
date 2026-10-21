# Dot bridge client (Magpie)

Magpie talks to DotControl through a dedicated loopback HTTP client in `internal/dotbridge`. This package is intentionally separate from provider routing, gateway handlers, and UI work.

Readiness uses `Status.Authorized`: the authenticated version 1 server's
future proof expiry and active subscription expiry, with a nonfuture verified
timestamp. Magpie imposes no additional ten-minute proof age and never extends
either expiry. Optional `refresh_before`, `last_refreshed_at`, `renewal_state`
and `subscribe_failure` diagnostics retain version 1 compatibility. The GUI
shows renewal failure as a warning while the original grant remains authorized;
ChatGPT alone requests subscription renewal.

`RecoveryPairingSnapshot` allows original journal GET recovery after local off.
It binds every read to the current original pairing credential and rejects all
POST operations before reservation. An accepted job ID is immutable: a later
receipt naming a different job fails validation, including concurrent lookups
whose local snapshots both initially lack a job ID. `SaveAttempt` compares the
original fingerprint, digest, mode, session and established job ID under the
journal lock. Persistence conflicts propagate before any result is returned.

New gateway stable-header reservations atomically store `original_fingerprint`
alongside the final wire digest. The fingerprint hashes exact decoded request
bytes plus caller/agent, protocol, model, path and session before any hooks or
rewrites. JSON whitespace changes therefore conflict deliberately. No original
prompt bytes are stored. Records lacking this fingerprint require explicit
original-ID recovery and cannot authorize an automatic stable-header replay.

## Configuration

- File: `settings.Dir()/dot-bridge.json`
- Fields: `enabled`, `address`, `credential` (pairing reference only), `pairing_generation`, `readiness_revision`, `answer_wait_seconds` (default 120, range 1..600), `task_wait_seconds` (default 600, range 1..3600)
- Pairing generation changes on replacement; off/re-enable preserves it and pending request identities. A separate readiness revision invalidates cached proof across processes.
- `BoundPairingSnapshot` captures configuration and connection together. Journal scopes include the bound `PairingIdentity`, derived from canonical address, credential reference and generation.
- Pairing secret: keyring service `Magpie/DotBridge`, account = SHA-256 of absolute config directory + pairing reference
- Dependency: `github.com/zalando/go-keyring` v0.2.6 (pinned)

## Request journal

- File: `settings.Dir()/dot-bridge-requests.json` with cross-process `internal/filelock`
- Stores identity only: scope, request_id, digest, mode, session_id, state, optional job_id
- Never stores prompts, tokens, or results
- Pending/unknown records are not auto-erased (10,000 record cap)

## Transport rules

- Numeric loopback host:port only (IPv4 or bracketed IPv6)
- Dedicated `Authorization: Bearer <paired-client-token>` header
- No redirects, proxy, retries, or legacy MCP/admin credentials
- 10s per-call budget (or remaining caller deadline if shorter)
- Encoded request and response budgets are 2 MiB; responses are read with a one-byte overflow check. Raw prompt/result budgets remain 256 KiB.

## Submission state

`Attempt` owns the acceptance barrier:

- `beginSubmit()` runs immediately before the single POST and sets `acceptance_unknown` with `BlockReplay=true`
- Verified HTTP 202 + `accepted:true` receipt calls `accept()`
- 404/timeouts/malformed receipts never clear the barrier
- `Enqueue()` always `Reserve()`s journal identity before I/O; existing identities use `ByRequest()` instead of POST

## Digest

`Digest(submission)` matches DotControl `dotqueue/submissionDigest` field order and empty `messages: []` encoding. Wire bodies also encode empty messages as `[]`. Independent golden hashes and canonical bytes in `internal/dotbridge/testdata/queue-canonical.json` were generated from the actual DotControl digest function in a temporary harness; tests do not duplicate that function.

Local validation follows the queue: at most 32 messages, valid UTF-8, supported roles (system/user/assistant), and exactly 262144 bytes maximum for Task plus all message contents combined. Answer mode requires a message; task mode requires nonblank Task. Session ID may be empty; supplied IDs follow the queue ID pattern. Ports must be decimal 1..65535.

## Readiness

Authenticated status includes subscription state/expiry and text/task capabilities. Only a bound paired connection can publish in-memory readiness after credential revalidation and a version-1 response. Publication and consumption require the current pairing identity and readiness revision, so an obsolete connection cannot make a replacement pairing ready; errors invalidate its cache. `CachedReady(now, capability)` requires enabled, paired, ready, the requested text or tasks capability, active subscription, nonfuture verification, cache age at most ten seconds, and unexpired proof and subscription. Each routed model requires its own capability. A later refresh invalidates an earlier in-flight refresh. There is no readiness setter or persisted synthetic proof. Missing subscription expiry fails closed.

## Routed task contract

`dot/task` uses the final nonblank user turn as Task and preceding explicit
messages as context; `dot/text` retains its original answer digest. Original
and final request admission reject caller tool definitions/results, developer
messages, and known caller environment formats. Dot may use its own cloud
computer, browser and execution tools. Materials must be explicitly supplied or
accessible to the Dot account; local repositories are not implicitly synced.

Validated results have a combined 256 KiB budget including text, error code,
artifact URLs and labels; at most 16 artifacts, 256-byte labels and 2048-byte
URLs. URLs require valid UTF-8, HTTPS without credentials, valid ports, canonical
public literal IPs or public-shaped DNS hosts under the producer's policy.
Numeric host aliases, local suffixes, mapped/scoped IPs and special IP ranges
are rejected without DNS or URL fetches. Markdown links escape labels and
preserve the validated destination.

`magpie_dot` metadata on Chat messages or Responses objects survives streaming.
Pending errors include actual job progress, mode, bounded summary, request/job
IDs and a recovery URL. `GET /v1/magpie/dot/requests/{internal_request_id}`
accepts that exact ID, authenticates caller and agent scope and mode permission,
and only resumes through the original pairing. Group-only restricted keys fail
closed. Acceptance state and job progress remain separate; reservation or
uncertain acceptance prevents automatic fallback even on a non-202 response.

## Local retention and backup

Both JSON files and vault credentials stay on this machine, excluded even from backups that include keys. `TestDotBackupExcludesMachineLocalState` exercises `backup.Collect` with and without keys. No automatic journal cleanup is provided: at 10000 records new reservations fail closed. An explicit cleanup API remains a future policy decision; unknown records must never be automatically removed.

`TestDotResumeChildProcess` starts an OS child using the sandboxed test binary, reloads the identity journal, and verifies recovery uses one authenticated GET with zero POSTs. Transport tests retain the production HTTP client and transport, injecting only socket dialing.
