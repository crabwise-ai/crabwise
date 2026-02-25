1. **[P0] Missing explicit policy for non-intercept/unknown CONNECT targets creates bypass and security ambiguity.**  
The plan says “selective MITM TLS” and auto-derives intercept domains, but it does not define behavior for `CONNECT` hosts outside that set (deny, blind tunnel, or allowlist+audit). Without this, users can get either silent bypass or an accidental open proxy pattern.  
Refs: [proxy_enforcement_architecture_c24009cc.plan.md:48](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:48), [proxy_enforcement_architecture_c24009cc.plan.md:112](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:112), [proxy_enforcement_architecture_c24009cc.plan.md:114](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:114)

2. **[P0] No ALPN/HTTP2 strategy; this can break core client compatibility.**  
Forward MITM proxying HTTPS reliably needs an explicit stance on client-side `h2` vs `http/1.1` negotiation and server behavior. The plan assumes existing `handleProxy` internals are reused as-is, but does not specify h2 termination/downgrade handling.  
Refs: [proxy_enforcement_architecture_c24009cc.plan.md:77](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:77), [proxy_enforcement_architecture_c24009cc.plan.md:83](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:83), [proxy_enforcement_architecture_c24009cc.plan.md:114](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:114)

3. **[P1] CA key lifecycle/security model is underspecified for production safety.**  
It defines one-time CA generation and storage path, but not permissions, rotation, regeneration behavior, revocation/uninstall, or failure behavior when key/cert is unreadable/corrupt. This is a major operational and security gap for MITM architecture.  
Refs: [proxy_enforcement_architecture_c24009cc.plan.md:6](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:6), [proxy_enforcement_architecture_c24009cc.plan.md:27](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:27)

4. **[P1] Routing model is underdefined after removing reverse-proxy assumptions.**  
The plan adds domain-based routing and removes reverse-proxy defaults/rewrite assumptions, but doesn’t define precedence when multiple providers share a domain or need path-level disambiguation. Host-only resolution is likely insufficient long-term.  
Refs: [proxy_enforcement_architecture_c24009cc.plan.md:15](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:15), [proxy_enforcement_architecture_c24009cc.plan.md:98](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:98), [proxy_enforcement_architecture_c24009cc.plan.md:30](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:30)

5. **[P1] “One env var covers all providers” is too optimistic and risks rollout false confidence.**  
The design relies on `HTTPS_PROXY`/`NODE_EXTRA_CA_CERTS`, but does not define compatibility validation per client/runtime or fallback guidance when env-based proxy discovery is not honored.  
Refs: [proxy_enforcement_architecture_c24009cc.plan.md:74](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:74), [proxy_enforcement_architecture_c24009cc.plan.md:75](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:75), [proxy_enforcement_architecture_c24009cc.plan.md:21](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:21)

6. **[P2] Test plan misses key failure-mode gates for this architecture change.**  
Current test list is good baseline, but it omits critical scenarios: unknown-domain behavior, CA/key corruption, TLS handshake mismatch/SNI mismatch, HTTP/2 negotiation, cert caching pressure, and keep-alive multiplexing across multiple requests on one tunnel.  
Refs: [proxy_enforcement_architecture_c24009cc.plan.md:33](/Users/luc/Documents/GitHub/crabwise/docs/plans/proxy_enforcement_architecture_c24009cc.plan.md:33)

Open questions to resolve in the plan:
1. What is exact policy for `CONNECT` to non-provider domains: `deny` vs `passthrough` vs configurable allowlist?  
2. What is the client-facing protocol contract: support `h2` MITM, or force `http/1.1`?  
3. What is CA lifecycle policy: rotate never/by command/by age, and how do users safely untrust old CAs?