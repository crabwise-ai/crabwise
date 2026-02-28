Here's the full user experience end to end:

**One-time setup (about 60 seconds):**

```bash
# 1. Install and init -- generates config + CA certificate
crabwise init

# 2. Trust the CA certificate so HTTPS interception works
# (prints an OS-specific, copy/paste command; use --copy to put it on your clipboard)
crabwise cert trust --copy

# 3. Start the daemon
crabwise start
```

**Every session -- launch your agent through crabwise:**

```bash
# Option A: wrap command (recommended)
crabwise wrap -- codex

# Option B: set env vars yourself
eval $(crabwise env)
codex
```

That's it. Under the hood, `crabwise wrap` sets `HTTPS_PROXY=http://127.0.0.1:9119` and `NODE_EXTRA_CA_CERTS=~/.local/share/crabwise/ca.crt`, then runs Codex. All of Codex's API calls to `api.openai.com` flow through crabwise. Non-AI traffic (git, npm, etc.) tunnels through transparently.

**OpenClaw phase 1 uses the same proxy path:**

```bash
crabwise wrap -- openclaw gateway
```

If you want OpenClaw session attribution in `crabwise watch`, `agents`, and `audit`, enable the read-only Gateway observer:

```yaml
adapters:
  openclaw:
    enabled: true
    gateway_url: ws://127.0.0.1:18789
    api_token_env: OPENCLAW_API_TOKEN
    session_refresh_interval: 30s
    correlation_window: 3s
```

Phase 1 scope is intentionally narrow:

1. Crabwise blocks upstream provider calls that go through the forward proxy.
2. Crabwise enriches those proxy events with OpenClaw session identity when the Gateway is available and correlation is confident.
3. Changes under `adapters.openclaw.*` require a daemon restart in phase 1. `SIGHUP` does not restart or reconfigure the OpenClaw adapter.
4. Crabwise does not yet govern local OpenClaw tool execution after a model response has already reached the OpenClaw host.

**Writing commandments that actually block:**

The user's commandments file (`~/.config/crabwise/commandments.yaml`) already works -- the issue was never the rules, it was that traffic wasn't hitting the proxy. With the forward proxy, the same rules now enforce:

```yaml
version: "1"
commandments:
  - name: no-gpt3
    description: "Only allow approved models"
    match:
      action_type: ai_request
      model:
        not_in: [gpt-4o, gpt-4o-mini]
    enforcement: block
    message: "Model not in approved list"

  - name: no-destructive-tools
    description: "Block dangerous tool calls at the API level"
    match:
      action_type: ai_request
      tool_name:
        pattern: "Bash|shell|terminal"
      arguments:
        pattern: "rm -rf|DROP TABLE|format "
    enforcement: block
    message: "Blocked: destructive tool call in API request"
```

**What happens when a request is blocked:**

1. Codex sends a chat completion request to `api.openai.com`
2. The request goes through crabwise (via `HTTPS_PROXY`)
3. Crabwise decrypts it (MITM), reads the model/tools/content
4. Commandment engine matches a `block` rule
5. Crabwise returns a `403` to Codex -- **the request never reaches OpenAI**
6. An audit event is recorded with `outcome: blocked`
7. `crabwise watch` shows the block in real time, `crabwise audit --triggered` shows it in history

**What the user can verify:**

```bash
# Confirm proxy is seeing traffic
crabwise status
# Should show: Proxy reqs > 0 (this was 0 in the investigation)
# If OpenClaw attribution is enabled, status also shows:
#   OpenClaw: connected
#   OC sessions / matches / ambiguous

# See blocks happening live
crabwise watch

# Query blocked events
crabwise audit --triggered --outcome blocked

# Query only OpenClaw-attributed events
crabwise audit --agent openclaw
crabwise audit --agent openclaw --session agent:main:discord:channel:123
```

The critical difference from before: the user doesn't need to know that Codex uses `OPENAI_BASE_URL`, or that Claude Code uses a different var, or how each client configures its API endpoint. `crabwise wrap` handles all of it with one universal mechanism.

Want me to start executing the plan?
