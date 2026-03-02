# Crabwise

Local-first daemon + CLI that monitors AI agent activity. Watches Claude Code and Codex CLI sessions, builds a hash-chained audit trail in SQLite, and lets you query/stream events in real time.

## Install

```bash
curl -sSfL https://raw.githubusercontent.com/crabwise-ai/crabwise/main/install.sh | bash
```

Or pin a version:

```bash
curl -sSfL https://raw.githubusercontent.com/crabwise-ai/crabwise/main/install.sh | VERSION=0.1.0-alpha.1 bash
```

### Other methods

```bash
# GitHub CLI — Linux (x86_64)
gh release download --repo crabwise-ai/crabwise --pattern '*linux_amd64*' && tar xzf crabwise_*_linux_amd64.tar.gz && sudo mv crabwise /usr/local/bin/

# GitHub CLI — macOS (Apple Silicon)
gh release download --repo crabwise-ai/crabwise --pattern '*darwin_arm64*' && tar xzf crabwise_*_darwin_arm64.tar.gz && sudo mv crabwise /usr/local/bin/

# GitHub CLI — macOS (Intel)
gh release download --repo crabwise-ai/crabwise --pattern '*darwin_amd64*' && tar xzf crabwise_*_darwin_amd64.tar.gz && sudo mv crabwise /usr/local/bin/

# go install
go install github.com/crabwise-ai/crabwise/cmd/crabwise@latest

# from source (requires Go 1.25+)
git clone https://github.com/crabwise-ai/crabwise.git && cd crabwise && make build
sudo cp bin/crabwise /usr/local/bin/

# from a PR (change to pr#)
tmp="$(mktemp -d)" && gh repo clone crabwise-ai/crabwise "$tmp" && cd "$tmp" && gh pr checkout 15 && make build && sudo install -m 0755 bin/crabwise /usr/local/bin/crabwise && cd - >/dev/null && rm -rf "$tmp"
```

## Quick Start

```bash
# Write default config + commandments files (~/.config/crabwise/)
crabwise init

# Trust the Crabwise CA (required for HTTPS interception / policy enforcement)
crabwise cert trust --copy

# Start the daemon (foreground)
crabwise start

# Launch AI agents through the proxy:
crabwise wrap -- codex      # sets HTTPS_PROXY automatically
crabwise wrap -- claude     # works with any AI agent
crabwise wrap -- openclaw gateway

# Or set env vars manually:
eval $(crabwise env)
codex

# In another terminal:
crabwise status          # check daemon is running
crabwise agents          # list discovered AI agents
crabwise watch           # stream events live
crabwise audit           # query event history
crabwise audit --triggered --outcome warned  # show policy-triggered warnings
crabwise audit --verify-integrity  # verify hash chain
crabwise stop            # graceful shutdown
```

## Usage

### `crabwise cert trust`

Prints an OS-specific command to trust Crabwise's local CA certificate (required for HTTPS interception / policy enforcement). Use `--copy` to put the command on your clipboard.

### `crabwise start`

Runs the daemon in the foreground. Discovers Claude Code sessions under `~/.claude/projects/` and Codex CLI sessions under `~/.codex/sessions/`, parses JSONL logs, and writes events to SQLite with hash chaining. When CA certificates are configured (via `crabwise init`), the daemon also runs a forward HTTPS proxy that intercepts AI provider traffic for policy enforcement.

If `adapters.openclaw.enabled` is set, the daemon also connects to the local OpenClaw Gateway for session attribution. Phase 1 only governs provider calls that hit the Crabwise proxy. It does not block local OpenClaw tool execution after a model response is already in-process.

Background it yourself with systemd, `&`, or a process manager.

### `crabwise stop`

Sends SIGTERM to the running daemon for graceful shutdown.

### `crabwise status`

Shows whether the daemon is running and basic stats.

### `crabwise agents`

Lists discovered AI agent sessions and their status.

### `crabwise watch`

`crabwise watch` defaults to a minimal Bubble Tea dashboard. Features:

- live feed of recent audit events
- daemon/status panel (uptime, queue depth, dropped count)
- commandment trigger rate panel
- **event filtering**: press `/` to enter filter mode, type a substring, `Enter` to apply, `Esc` to clear
- **visual indicators**: ⚠ orange for warned outcomes, ✖ red for blocked

Reconnect behavior is intentionally conservative: on stream disconnect, watch performs one reconnect attempt, then exits with an error if reconnect fails.

Use `--text` to force the legacy plain-text stream output mode.

Text fallback example:

```
14:23:05 [claude-code] tool_call           Read  src/main.ts
14:23:07 [codex-cli]   command_execution   Bash  go test ./...
```

### `crabwise audit`

Query the audit trail with filters:

```bash
crabwise audit --since 2026-02-23T00:00:00Z
crabwise audit --action tool_call
crabwise audit --session <id>
crabwise audit --triggered
crabwise audit --triggered --outcome warned
crabwise audit --export json
crabwise audit --verify-integrity
```

Flags: `--since`, `--until`, `--agent`, `--action`, `--session`, `--outcome`, `--triggered`, `--limit`, `--export`, `--verify-integrity`

### `crabwise commandments`

Inspect and test active policy rules:

```bash
crabwise commandments list
crabwise commandments test '{"action_type":"command_execution","action":"Bash","arguments":"git push origin main"}'
crabwise commandments reload
```

Subcommands: `list`, `test <event-json>`, `reload`

### `crabwise wrap`

Launches a command with proxy environment variables configured. All HTTPS traffic from the wrapped process routes through the crabwise proxy for monitoring and enforcement.

```bash
crabwise wrap -- codex
crabwise wrap -- claude
crabwise wrap -- python my_agent.py
crabwise wrap -- openclaw gateway
```

### `crabwise service`

Manages proxy injection for daemon-managed agents running under systemd (Linux) or launchd (macOS). Unlike `crabwise wrap` (which sets environment at exec time), `crabwise service` persists proxy config in the service definition so it survives reboots.

Two scopes:
- `--scope system` (default) — requires root. Linux: `/etc/systemd/system`, macOS: `/Library/LaunchDaemons`
- `--scope user` — no root, rejects sudo. Linux: `~/.config/systemd/user`, macOS: `~/Library/LaunchAgents`

`--agent` resolves via the config registry (e.g. `openclaw` → `openclaw-gateway` on Linux). Unknown names are treated as literal unit names.

```bash
# Production daemon (resolves via registry, system scope default)
sudo crabwise service inject --agent openclaw --restart

# Per-user agent (literal fallback, user scope)
crabwise service inject --scope user --agent my-agent --restart

# Check injection status
crabwise service status --agent openclaw
crabwise service status --scope user --agent my-agent

# Remove injection
sudo crabwise service remove --agent openclaw --restart
crabwise service remove --scope user --agent my-agent --restart
```

### `crabwise env`

Prints proxy environment variables for shell evaluation.

```bash
eval $(crabwise env)              # bash/zsh
crabwise env --shell fish | source  # fish
```

## Config

Default config is embedded in the binary. Override with `~/.config/crabwise/config.yaml`.

Commandments file path is configured at:

```yaml
commandments:
  file: ~/.config/crabwise/commandments.yaml
```

OpenClaw phase-1 attribution config:

```yaml
adapters:
  openclaw:
    enabled: true
    gateway_url: ws://127.0.0.1:18789
    api_token_env: OPENCLAW_API_TOKEN
    session_refresh_interval: 30s
    correlation_window: 3s
```

Notes:

- `gateway_url` points at the local OpenClaw Gateway control surface.
- `OPENCLAW_API_TOKEN` is only needed if your Gateway requires token auth.
- Changes under `adapters.openclaw.*` require a daemon restart in phase 1. `SIGHUP` still only reloads commandments, tool registry, and proxy mappings.
- OpenClaw governance in phase 1 is provider-call governance only. Crabwise blocks upstream model requests, not local tool execution inside the OpenClaw host.

### OpenTelemetry Export

Crabwise can export GenAI spans via OTLP HTTP to any OpenTelemetry collector. Disabled by default.

```yaml
otel:
  enabled: true
  endpoint: localhost:4318
  export_interval: 5s
```

Spans follow the [GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/) with attributes like `gen_ai.system`, `gen_ai.request.model`, `gen_ai.usage.input_tokens`, and Crabwise extensions (`crabwise.outcome`, `crabwise.cost_usd`).

### Install Script

The install script downloads the release archive and verifies its SHA-256 checksum against `checksums.txt` from the release. If the checksum file is missing, the archive is not found in checksums, or no sha256 tool is available, installation fails (fail-closed). Supports Linux and macOS.

## Manual cleanup

To reset or remove Crabwise data and config (e.g. for a clean reinstall):

1. **Delete the database and runtime data** (default location):
   ```bash
   rm -rf ~/.local/share/crabwise/
   ```
   This removes the SQLite database (`crabwise.db`), socket, PID file, and any raw payload files. The daemon will recreate the directory and a new empty database on next `crabwise start`.

2. **Optionally delete config and commandments** (to restore defaults):
   ```bash
   rm -rf ~/.config/crabwise/
   ```
   Then run `crabwise init` to write fresh default config, commandments, and tool registry files. If you use a custom config path (e.g. `--config`), remove that path instead.

## License

See [LICENSE](LICENSE).
