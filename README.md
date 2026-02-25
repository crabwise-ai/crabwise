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
```

## Quick Start

```bash
# Write default config + commandments files (~/.config/crabwise/)
crabwise init

# Start the daemon (foreground)
crabwise start

# Launch AI agents through the proxy:
crabwise wrap -- codex      # sets HTTPS_PROXY automatically
crabwise wrap -- claude     # works with any AI agent

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

### `crabwise start`

Runs the daemon in the foreground. Discovers Claude Code sessions under `~/.claude/projects/` and Codex CLI sessions under `~/.codex/sessions/`, parses JSONL logs, and writes events to SQLite with hash chaining. When CA certificates are configured (via `crabwise init`), the daemon also runs a forward HTTPS proxy that intercepts AI provider traffic for policy enforcement.

Background it yourself with systemd, `&`, or a process manager.

### `crabwise stop`

Sends SIGTERM to the running daemon for graceful shutdown.

### `crabwise status`

Shows whether the daemon is running and basic stats.

### `crabwise agents`

Lists discovered AI agent sessions and their status.

### `crabwise watch`

Streams audit events in real time as supported agents generate them (Claude Code, Codex CLI).

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
