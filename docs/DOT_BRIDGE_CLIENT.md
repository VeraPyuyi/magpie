# Dot bridge client (Magpie)

Magpie talks to DotControl through a dedicated loopback HTTP client in `internal/dotbridge`. This package is intentionally separate from provider routing, gateway handlers, and UI work.

## Configuration

- File: `settings.Dir()/dot-bridge.json`
- Fields: `enabled`, `address`, `credential` (pairing reference only), `answer_wait_seconds` (default 120, range 1..600)
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
- Response bodies capped at 1 MiB + 1 byte

## Submission state

`Attempt` owns the acceptance barrier:

- `beginSubmit()` runs immediately before the single POST and sets `acceptance_unknown` with `BlockReplay=true`
- Verified HTTP 202 + `accepted:true` receipt calls `accept()`
- 404/timeouts/malformed receipts never clear the barrier
- `Enqueue()` always `Reserve()`s journal identity before I/O; existing identities use `ByRequest()` instead of POST

## Digest

`Digest(submission)` matches DotControl `dotqueue/submissionDigest` field order and empty `messages: []` encoding.
