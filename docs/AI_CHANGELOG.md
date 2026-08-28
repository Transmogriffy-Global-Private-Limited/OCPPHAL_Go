# AI-assisted changelog

## 2026-08-28 — Deterministic accepted remote-stop completion

- Corrected `cpconsole` so an accepted RemoteStopTransaction owns its lifecycle:
  one StopTransaction, cleared local transaction state, then Finishing followed
  immediately by Available.
- Duplicate matching RemoteStopTransaction requests are accepted while the
  claimed stop is in progress but cannot emit another StopTransaction.
- A failed Finishing notification is followed by an Available attempt, so the
  simulator does not remain indefinitely in Finishing.
- Updated remote-command guidance and added focused lifecycle tests.

Compatibility: this changes only the dummy charger client. It does not alter
HAL or CMS source, data, service state, or contracts.

## 2026-08-03 — Terminal-controlled virtual charger

- Added `cmd/cpconsole`, a standalone configurable OCPP 1.6J virtual EV charger.
- Added coherent terminal-controlled local/remote transactions and meter progression.
- Added fault, suspension, remote-policy, diagnostics, firmware and trigger behavior.
- Added Windows build registration and Linux amd64/arm64 cross-build tooling.
- Added focused parser tests and the complete operator/integration guide.
- Registered canonical project-memory files and recorded existing OpenAPI remediation.

Compatibility: no HAL server route, database schema, callback payload, or runtime
ownership boundary changed. The new program is an additional test client.

Verification: focused tests, `go test ./...`, all Windows builds, Linux amd64
cross-build, and a loopback memory-store charge flow passed. The canonical
PostgreSQL regression could not run non-interactively because its password prompt
received no value; this entry does not claim that regression passed.
