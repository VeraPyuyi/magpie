# Dot bridge client wire limits

[`Connection`](../internal/dotbridge/client.go) uses a 2 MiB encoded JSON
request budget and a 2 MiB response budget. The existing combined task and
message content limit remains 256 KiB, with at most 32 messages and IDs up
to 128 characters. JSON HTML and control escaping can expand each content
byte to six wire bytes; the envelope budget accommodates that expansion
and metadata without reducing the content budget.

`Submit` validates and encodes the full request before `Attempt.beginSubmit`.
`Enqueue` performs the same preflight before reserving journal identity.
Invalid input returns locally without starting a POST or setting a new
acceptance barrier. Once submission starts, non-202 responses, malformed
receipts and uncertain transport outcomes retain the replay barrier. Error
status or a structured gate error alone does not prove that no durable write
occurred.

Responses of exactly 2 MiB are allowed. The reader consumes at most the budget
plus one byte and rejects an oversized response, including a complete JSON
object followed by excessive whitespace. It never accepts a truncated prefix.
`ByRequest` continues validating request ID, digest, mode, session, state and
result using the existing receipt validator; increasing the wire budget does
not increase its 256 KiB result text limit or 16-artifact count limit.

`go test ./internal/dotbridge -run '^TestDotWire' -count=1` exercises the
actual HTTP client against local `httptest` handlers. The tests reconstruct
the independent cross-repository audit's escaped-result recipe and verify
its SHA-256 before decoding it, test artifact delivery and identity rejection,
exercise exact and excessive encoded response sizes, and compare maximal
escaped requests with independently specified bytes. These synthetic handlers
do not execute DotControl's real HTTP producer or establish a full cross-repository
integration result.
