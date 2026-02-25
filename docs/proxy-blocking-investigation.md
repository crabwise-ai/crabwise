# Proxy Blocking Investigation: Codex Actions Not Prevented

## Summary

We observed a case where Codex was asked to delete a file and successfully deleted it, even though Crabwise commandments were configured with `enforcement: block` rules intended to prevent this behavior.

The key finding is that Crabwise was only observing Codex activity through the log watcher, not enforcing through the proxy path. In this mode, actions are already executed when Crabwise sees them, so preventive blocking cannot happen.

## Observed Behavior

- `crabwise watch` showed Codex events such as:
  - `ai_request chat delete the readme file`
  - `file_access apply_patch *** Delete File: README.md`
  - follow-up confirmation that the file was deleted
- Commandments included:
  - shell destructive block rule
  - block rule for delete patch operations
  - guaranteed block test rule (`action_type: ai_request`)
- Despite this, Codex actions were not blocked.

## Hard Evidence

Runtime metrics proved no proxy traffic:

- `Proxy reqs: 0`
- `Proxy blocked: 0`
- `Proxy errors: 0`
- `crabwise audit --agent proxy --limit 10 --export json` returned `count: 0`

At the same time:

- proxy listener was active: `127.0.0.1:9119` was listening
- `/health` endpoint returned `{"ok":true}`
- Codex events continued to appear in `watch` via `log_watcher`

This combination means the proxy process was healthy, but Codex was not routing provider traffic through it.

## Root Cause

### 1) Codex bypassed Crabwise proxy

Codex requests were not sent to Crabwise (`Proxy reqs` remained zero). Therefore, proxy-side policy evaluation and blocking never executed.

### 2) Log watcher is observe-only

Log watcher sees events after the action occurs. In this path, `enforcement: block` cannot prevent execution; it only records governance outcomes.

### 3) Rule shape mismatch for expected enforcement point

Some rules were written for shell or patch argument matching, but the preflight proxy event model differs from post-execution log entries. Even perfect rules cannot block if the request never enters proxy flow.

## Why This Is Expected With Current Architecture

Crabwise has two different enforcement surfaces:

- **Proxy adapter (`adapter_type=proxy`)**: can block before upstream forwarding.
- **Log watcher adapter (`adapter_type=log_watcher`)**: cannot block preventively; only observes and records.

If Codex is not routed through proxy, only the second path is active.

## Practical Diagnosis Checklist

When blocking appears not to work:

1. Confirm proxy server is listening.
2. Confirm proxy metrics increase during a live Codex request:
   - `Proxy reqs` should increase above 0.
3. Confirm proxy audit events exist:
   - `crabwise audit --agent proxy --limit 10 --export json`
4. If metrics stay at zero:
   - the client is bypassing proxy (routing/setup issue).

## Current Status of This Incident

- Proxy service health: OK
- Proxy enforcement path used by Codex: NO
- Blocking failure due to policy engine bug: NOT indicated by evidence
- Primary issue: client routing/configuration to proxy path

## Recommended Next Steps

1. Ensure Codex is launched with provider base URL/proxy settings that point to `127.0.0.1:9119`.
2. Re-test with a temporary guaranteed block rule after confirming `Proxy reqs > 0`.
3. Validate with:
   - nonzero `Proxy reqs`
   - nonzero `Proxy blocked` for blocked test prompts
   - proxy events present in audit export

## Notes

- We also observed `custom_t...` unknown parser events in some logs. Those are separate parser-coverage concerns and do not explain zero proxy metrics.
- Preventing all local client-side tool execution may require additional controls beyond HTTP proxy interception, depending on how a given client executes tools.
