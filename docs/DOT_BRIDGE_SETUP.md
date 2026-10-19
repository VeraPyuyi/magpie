# Dot bridge operator setup

Magpie talks to DotControl through the local `internal/dotbridge` client. The
`magpie dot` commands are the operator surface for pairing, readiness checks,
and background task submission.

## Pairing

1. In DotControl, export a **private version 1 pairing file** for the current
   user. The file uses DotControl's `PairingFile` shape:
   `version`, `client_id`, `queue_id`, `token`, `listen`, `created_at`.
2. `listen` is a numeric loopback `host:port`, not a URL.
3. Run:

```sh
magpie dot pair /path/to/pairing.json
```

Magpie validates the file before writing anything:

- regular file only
- no symlinks, junctions, reparse points, or hard links in the path
- strict JSON with a single object
- version `1` only
- pairing files capped at 16 KiB; task files capped at 256 KiB
- loopback `listen` and authenticated `/dot-bridge/v1/status`

On success the token is stored in the OS keyring through
`dotbridge.SavePairing`. Magpie does **not** print the token. The pairing
file is **retained**; delete it manually after `magpie dot status` looks right.

Configuration is written to `dot-bridge.json` with a credential reference only.
Tokens never live in provider keys or backups.

## Readiness and task capability

```sh
magpie dot status
```

`status` reports the authenticated bridge payload, including
`capabilities.tasks`. Background task submission is allowed only when:

- the bridge is enabled and paired
- `ready` is true
- subscription state is active and unexpired
- `capabilities.tasks` is true

If `capabilities.tasks` is false, `magpie dot submit` fails with
`unsupported/probe_required` and sends **zero** POST requests. Run DotControl's
task probe first; text readiness alone is not enough.

## Background tasks

Task files are strict JSON objects:

```json
{
  "request_id": "cli-request-1",
  "mode": "task",
  "task": "Summarize the supplied context",
  "session_id": "",
  "messages": [
    {"role": "user", "content": "explicit context only"}
  ]
}
```

Rules:

- `mode` must be `task`. Answer mode is separate from background tasks.
- `request_id` must stay stable for recovery.
- No automatic history is attached; only explicit `messages` are sent.
- `magpie dot submit --file task.json` uses `dotbridge.Enqueue` and the CLI
  journal scope `cli:<address>:<credential>`.

Outcomes:

- `202 Accepted` prints the job id and state.
- uncertain acceptance prints `outcome_unknown` with the `request_id` and a
  recovery command; do not resubmit with a new id.
- `magpie dot request <requestID>` resumes with GET only.
- `magpie dot requests` lists known requests for this CLI scope when the
  journal API is available.
- `magpie dot job <id>` fetches status/results.
- `magpie dot cancel <id>` records cancellation intent only; inspect the job
  afterward because completion is not confirmed here.

## Disable locally

```sh
magpie dot off
```

This disables the bridge locally without deleting pairing credentials or
pending jobs.

## Security notes

- Loopback client only: no proxy, no redirects.
- Never pass tokens on the command line or through shell history.
- Pairing and task files must be private to the current user.
- Foreign hosts, origins, and forwarded headers are rejected by the bridge
  server; the CLI uses the existing loopback client in `internal/dotbridge`.
