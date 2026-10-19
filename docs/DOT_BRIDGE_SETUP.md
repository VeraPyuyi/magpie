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
- directory-relative handle traversal forbids symlinks, junctions and reparse points; the opened file must have exactly one hard link
- current-user ownership and private access: Windows requires a protected DACL with only explicit current-user allow ACEs; Unix requires owner-only permissions
- strict JSON with a single object, canonical field spelling, no duplicate keys (including nested messages), no unknown fields or trailing values
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
  journal scope `cli:<pairing-identity>`, bound to the same connection snapshot.

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
pending jobs. Re-enabling the same pairing preserves its request namespace but requires fresh readiness. Replacing a pairing creates a new namespace.

Use `magpie dot archives` to inspect retained records from previous pairings without contacting the current queue. Legacy gateway stable-header records block new submission and report their original recovery identity. They require manual reconciliation with their original queue; they are never automatically reassigned or replayed through a new pairing.

## Security notes

- Loopback client only: no proxy, no redirects.
- Never pass tokens on the command line or through shell history.
- Pairing and task files must be private to the current user.
- The CLI uses the existing loopback client in `internal/dotbridge`.
- Windows inputs must be on a local drive. Create private files with a protected
  DACL granting only the current user's SID; inherited ACLs are rejected even
  if the inherited entries currently appear harmless. This policy also applies
  to task files. The CLI queries owner, DACL and link count on the read handle,
  and fails closed when any query fails.
- Traversal is relative to held directory handles, so ancestor renames cannot
  redirect later opens. Windows leaf handles deny write/delete sharing. This
  protects against ordinary path substitution and access by other users; it
  does not prove isolation from malicious code running as the current user,
  administrators, pre-existing writable mappings or kernel access. Unix
  directory-relative no-follow opens similarly bind traversal, but do not
  prevent the owner from concurrently modifying an already-open file.
- `job` and `cancel` validate version, exact job ID, request/digest/mode/session,
  legal states and result consistency. Known requests use journal identity and
  `Resume` validation. For unknown jobs, identity has only the authenticated
  bridge as its source. Cancellation first validates the current job, then
  requires a matching receipt with cancellation intent set; malformed or
  unrelated responses produce an error with GET recovery guidance. A valid
  cancellation acknowledgement makes no claim that cloud work has stopped.
