# Agent X

**Your AI agents, on every device.**

Agent X is a local-first desktop workspace for [Claude Code](https://claude.com/claude-code) and [Codex](https://openai.com/codex/) — watch, steer, and approve every AI coding session from any of your trusted machines. Work never stops when you switch computers.

- **CLI in chat** — messages, tool output, errors, and approvals in one readable stream
- **Cross-device control** — send, queue, and approve from any trusted device
- **Local-first** — no account, no cloud; pairing and execution stay on your network

Available for **macOS** and **Windows**.

## Stack

- [Wails v2](https://wails.io) (Go backend + WebView frontend)
- Vanilla JS + Vite frontend (`frontend/`)
- Go domain logic under `internal/`

## Development

Prerequisites: Go 1.22+, Node 18+, and the [Wails CLI](https://wails.io/docs/gettingstarted/installation).

```sh
wails dev        # live development with hot reload
wails build      # production build (see build/bin)
go test ./...    # run the Go test suite
```

## License

Source-available under the [Elastic License 2.0](LICENSE). You may use, copy,
modify, and redistribute the code, but you may not offer it as a hosted or
managed service. Copyright © 2026 Aman Singh.
