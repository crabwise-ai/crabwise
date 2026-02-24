# Transport Validation Log (M2)

## Goal

Validate that the M2 transport abstraction supports multi-provider onboarding without changing policy/audit core logic.

## Covered by current abstraction

- Per-provider auth header preparation (`PrepareAuth`)
- Forwarding with provider-specific upstream base URL and client pool settings (`Forward`)
- Provider-specific stream event decoding (`ParseStreamEvent`)
- Router-owned provider resolution via header > route pattern > default
- Declarative mapping specs for request/response normalization

## Deferred/provider-specific follow-ups

- Non-SSE streaming protocols (if added by future providers)
- Multi-part upload/download endpoints
- Provider-specific retry semantics and backoff policies
- Rich error taxonomies beyond normalized M2 set

## Result

OpenAI is implemented as v1 transport + mapping. Anthropic/Google onboarding is expected to require:

1. A transport registration implementing the same interface.
2. A provider YAML mapping spec in `configs/proxy_mappings/`.

No changes to commandment evaluation or audit writer are required.
