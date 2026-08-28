# AI-assisted changelog

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
