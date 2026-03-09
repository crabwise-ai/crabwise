# Example Commandment Sets

Copy-paste-ready policy files for common scenarios.

## Usage

```bash
cp examples/commandments/<file>.yaml ~/.config/crabwise/commandments.yaml
crabwise commandments reload   # if daemon is running
```

## Files

| File | Scenario | Summary |
|------|----------|---------|
| `solo-dev.yaml` | Solo/startup dev | Light guardrails — block destructive cmds, warn on creds & main push |
| `enterprise.yaml` | Enterprise/compliance | Strict lockdown — block unapproved models, creds, destructive cmds; redact everything |
| `open-source.yaml` | OSS maintainer | Protect secrets & CI — block secret commits, force push; warn on CI changes |
| `security-research.yaml` | AI red team / security | Prevent exfiltration — block network tools piping secrets, system writes, pkg installs |
| `cost-conscious.yaml` | Budget-aware teams | Token & model controls — block expensive models, enforce token limits |

## Schema Reference

See `internal/commandments/README.md` for full matcher and field documentation.
