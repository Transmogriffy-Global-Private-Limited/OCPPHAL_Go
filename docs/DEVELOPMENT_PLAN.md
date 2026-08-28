# Development plan

## Objective

Maintain a compatibility-focused OCPP 1.6J HAL with durable transaction truth,
CMS callback delivery, frontend live status, and hardware-independent regression
and operator tooling.

## Permanent invariants

- The HAL owns OCPP transport, charger state, transaction persistence and callbacks.
- Exact Central System transaction IDs remain stable across HAL, CMS and frontend flows.
- Only charger-originated StartTransaction and StopTransaction open and close normal sessions.
- Durable PostgreSQL state and callback outbox state remain authoritative.
- Simulator and smoke clients must use the same OCPP library and public HAL contracts as hardware.

## Current execution

Current phase: compatibility verification and operational tooling.

Active feature: none.

Last completed slice: deterministic accepted remote-stop completion for the terminal-controlled virtual charger.

Next approved work: none; select the next slice with the human.

## Feature registry

### Terminal-controlled virtual charger

Status: Implemented

Objective: Allow realistic local or hosted end-to-end charging tests without
physical hardware, using a standalone configurable charge point executable.

Implemented surfaces:

- OCPP connection, boot and connector state flow;
- local and remote transaction flow;
- coherent cumulative meter generation;
- remote command, failure-policy and firmware/diagnostic handling;
- Windows and Linux builds;
- focused tests and operator documentation.

Follow-up correction: accepted remote stops now own a single deterministic
completion path through StopTransaction, Finishing, and Available; focused
lifecycle tests cover duplicate commands and failed Finishing notification.

Verification: focused tests, `go test ./...`, repository build, Linux cross-build,
and a loopback memory-store charge flow passed. The canonical PostgreSQL
regression remains required before changing this status to Verified.

## Remediation backlog

- Inventory all active REST routes into an authoritative OpenAPI document.
- Serve the same OpenAPI through environment-controlled Swagger UI and raw schema routes.
- Add route/schema drift checks without changing the existing compatibility contracts.

These items are recorded work, not implemented behavior.
