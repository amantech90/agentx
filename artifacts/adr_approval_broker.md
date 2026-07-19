# ADR: Provider-neutral blocking approvals

## Context

Agent X runs Claude and Codex on one device while allowing a paired device to control the same workspace. Both providers can pause before a sensitive tool action, but their wire protocols and decision formats differ. A visual-only confirmation would be unsafe because it could drift from the provider process actually waiting for permission.

## Decision

Agent X owns a provider-neutral, in-memory approval broker in the session manager. A runner submits display-only approval metadata and blocks on a single-use decision. The manager persists and broadcasts an approval chat item, validates that a decision belongs to the exact workspace and pending approval, records the result, and then releases the runner.

Claude uses its bidirectional `stream-json` permission callback. Codex uses `codex app-server` approval requests. The UI exposes only **Allow once** and **Deny** in V1. Pending requests are cancelled after process exit or application restart and cannot be replayed.

## Rationale

One broker gives local and paired devices identical behavior without leaking provider-specific protocol details into the UI or bridge. Single-use decisions minimize accidental permission expansion and let the provider remain the authority that executes the requested action.

## Alternatives considered

- Parse terminal prompts: brittle and cannot reliably resume the correct request.
- Provider-specific UI flows: duplicates routing and persistence logic and makes future providers harder to add.
- Session-wide allow rules: convenient, but broader than needed for V1 and harder to explain safely.

## Consequences

- Session snapshots now carry structured approval metadata.
- The bridge adds an authenticated approval-resolution method.
- Provider runners must use their interactive structured protocols instead of fire-and-forget execution.
- Approval cards remain visible in history with their final status.

## Risks

- Provider protocol changes can break an adapter; protocol parsing is isolated and tested.
- A device can disconnect while a request waits; the provider remains blocked until another paired/local client decides or the run is cancelled.
- Previously persisted pending cards are marked cancelled at startup so stale actions cannot be approved.

## References

- OpenAI Codex app-server protocol and generated schemas from the installed CLI.
- Anthropic Claude Agent SDK control-request and permission-result types.
