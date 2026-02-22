# Crabwise

Local-first daemon + CLI that monitors AI agent activity. Watches Claude Code sessions, builds a hash-chained audit trail in SQLite, and lets you query/stream events in real time.

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
# go install
go install github.com/crabwise-ai/crabwise/cmd/crabwise@latest

# from source (requires Go 1.25+)
git clone https://github.com/crabwise-ai/crabwise.git && cd crabwise && make build
sudo cp bin/crabwise /usr/local/bin/
```

## Quick Start

```bash
# Start the daemon (foreground)
crabwise start

# In another terminal:
crabwise status          # check daemon is running
crabwise agents          # list discovered AI agents
crabwise watch           # stream events live
crabwise audit           # query event history
crabwise audit --verify-integrity  # verify hash chain
crabwise stop            # graceful shutdown
```

## Usage

### `crabwise start`

Runs the daemon in the foreground. Discovers Claude Code sessions under `~/.claude/projects/`, parses JSONL logs, and writes events to SQLite with hash chaining.

Background it yourself with systemd, `&`, or a process manager.

### `crabwise stop`

Sends SIGTERM to the running daemon for graceful shutdown.

### `crabwise status`

Shows whether the daemon is running and basic stats.

### `crabwise agents`

Lists discovered AI agent sessions and their status.

### `crabwise watch`

Streams audit events in real time as Claude Code generates them.

```
14:23:05 [claude-code] tool_call  Read  src/main.ts
14:23:07 [claude-code] tool_call  Edit  src/main.ts
```

### `crabwise audit`

Query the audit trail with filters:

```bash
crabwise audit --since 1h
crabwise audit --action tool_call
crabwise audit --session <id>
crabwise audit --export json
crabwise audit --verify-integrity
```

Flags: `--since`, `--until`, `--agent`, `--action`, `--session`, `--limit`, `--export`, `--verify-integrity`

## Config

Default config is embedded in the binary. Override with `~/.config/crabwise/config.yaml`.

## License

See [LICENSE](LICENSE).
