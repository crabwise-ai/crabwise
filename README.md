# Crabwise — Infrastructure-level control for your AI agents

Local-first monitoring, audit, and commandment(policy) enforcement for AI agents.

Crabwise runs as a daemon plus CLI on your machine. It watches local agent activity, can proxy provider traffic for enforcement, stores a hash-chained audit trail in SQLite, and gives you a fast way to see what your agents are doing without sending that data to a hosted control plane.

Built for solo developers, builders, and OpenClaw users. Currently supports Claude Code, Codex CLI, and OpenClaw. (More coming soon)

## What You Get

- Local monitoring for Claude Code and Codex CLI sessions
- Proxy-based governance for wrapped agents and provider calls
- Declarative policy rules ("commandments") for block and warn behavior
- Hash-chained audit history with live watch and historical queries
- Terminal UIs for status, agents, live activity, audit history, and policies
- Optional OpenClaw attribution and optional OpenTelemetry export


## Quick Start

Install Crabwise:

```bash
curl -sSfL https://raw.githubusercontent.com/crabwise-ai/crabwise/main/install.sh | bash
```

Initialize config and generate the local CA:

```bash
crabwise init
crabwise cert trust --copy
```

Start the daemon:

```bash
crabwise start
```

Launch an agent through Crabwise:

```bash
crabwise wrap -- codex
# or
crabwise wrap -- claude
# or
crabwise wrap -- openclaw gateway
```

In another terminal, inspect what is happening:

```bash
crabwise status
crabwise agents
crabwise watch
crabwise audit
```

When you are done:

```bash
crabwise stop
```

## How Requests Flow

![How Crabwise Works](assets/crabwise-howitworks.gif)

Crabwise sits between the wrapped agent and the model provider. Requests and responses flow through the local proxy, policies are evaluated locally, and audit history stays on your machine.

## Main Features

### Local-first audit trail

Crabwise records normalized agent and proxy events in a local SQLite database with hash chaining, so you can inspect activity and verify integrity without depending on a remote service.

### Policy enforcement

Crabwise can evaluate requests before they reach the model provider and evaluate tool-use payloads in supported JSON responses before they reach the agent. Policies are defined in YAML and support both `warn` and `block` outcomes.

### Fast terminal workflows

The CLI is built for day-to-day use:

- `crabwise status` shows daemon and proxy health
- `crabwise agents` shows discovered agents
- `crabwise watch` streams activity live
- `crabwise audit` queries historical events
- `crabwise commandments list` shows active policy rules

### Works with builders

Use `crabwise wrap -- <command>` to route a local agent, script, or tool runner through Crabwise without permanently changing your shell environment.

### OpenClaw support

When enabled, Crabwise connects to a local OpenClaw Gateway and correlates activity with OpenClaw sessions. Current enforcement is focused on provider-side governance through the Crabwise proxy.

## How It Works

1. `crabwise init` writes default config, policy, and proxy mapping files under `~/.config/crabwise/`.
2. `crabwise start` runs the daemon, log watcher, local IPC socket, and proxy.
3. Crabwise watches supported local agent logs and records events.
4. Wrapped agents send provider traffic through the Crabwise proxy for monitoring and enforcement.
5. You inspect activity with `status`, `agents`, `watch`, `audit`, and `commandments`.

## Notes

- The proxy uses a local CA certificate to inspect HTTPS traffic.
- Crabwise is usable now, but still a pragmatic v1 focused on Claude Code, Codex CLI, wrapped local agents, and OpenClaw-aware workflows.
- A dedicated documentation site will cover the full command reference, flags, configuration, and deeper operational details.

## Development

```bash
make build
make test
```

## License

Licensed under AGPL-3.0. See [LICENSE](LICENSE).
