# ADR-036: Authentication and Authorization Roadmap

## Status

Proposed (scale trigger: Fortune 500 security review, SSO mandates, multi-user deployments)

## Context

Authentication today:

- Static API keys in `hermem.ini` (`auth.Key{Value, Scope, Label}`),
  compared against the `X-API-Key` header (`server/middleware.go`).
- Three global scopes: `read`, `write`, `admin`, mapped from path
  prefixes (`auth.ScopeForPath`). No resource-level rules.
- Keys are plaintext in the INI file; rotation = edit + SIGHUP.
- No identity concept beyond the key `Label`; the audit ADR (034) needs
  a real actor; multi-tenancy (ADR-030) needs claims.
- No TLS story in the server (plain HTTP; deployment docs assume a
  reverse proxy, unenforced).

## Why the current design breaks

1. **Enterprise SSO is non-negotiable.** Fortune 500 security teams
   require OIDC/SAML integration, group-based access, and centralized
   offboarding. "We have 3 static keys in a file" ends procurement.
2. **Static keys leak.** INI files get committed, backed up to
   unencrypted stores, and pasted into tickets. No expiry, no rotation
   API, no hashing at rest.
3. **Scopes are path-prefix based.** `ScopeForPath` couples
   authorization to URL layout — every new endpoint must remember its
   mapping (an admin endpoint under a new prefix defaults to *no
   requirement* — fail-open by omission).
4. **No per-actor anything:** no per-user rate limits, quotas, audit
   attribution, or per-resource ACLs (e.g. "this agent may read the
   `world` category but not `experience`").
5. **Machine identity.** 5000 deployments need service accounts with
   short-lived credentials, not a shared long-lived key copied to every
   agent.

## Decision

1. **Pluggable `Authenticator` chain** (the interface exists — extend
   it): static keys (legacy, hashed at rest with argon2id) →
   OIDC JWT (RS256, JWKS refresh, issuer/audience validation) →
   mTLS service identities (SPIFFE IDs) for machine-to-machine.
   First match wins; all yield a unified `Claims{Subject, Tenant,
   Scopes, Expiry}` placed in context.
2. **Policy engine (authorization):** replace `ScopeForPath` with an
   explicit policy table evaluated at the route level:
   `allow(subject, action, resource)` where resource =
   `(tenant, category, endpoint-class)`. Ship with 3 built-in roles
   (reader/writer/admin) mapping to today's scopes for compatibility;
   allow per-deployment custom policies via config (start with a static
   YAML/CEL ruleset, not a full OPA dependency).
3. **Token lifecycle:** `hermem auth token create/revoke/list` CLI +
   API; expiry enforced; refresh via OIDC for humans, via mTLS
   rotation for machines.
4. **Transport security:** optional built-in TLS (cert/key paths or
   ACME behind a flag); refuse to start with `admin` scope keys and no
   TLS when `HERMEM_REQUIRE_TLS=1`.
5. **Every authenticated request** populates the audit log (ADR-034)
   with `Claims.Subject`.

## Alternatives considered

1. **Defer to the reverse proxy (nginx/Envoy auth).** Valid for
   sophisticated operators; rejected as the *only* answer — the
   single-binary developer story and small-enterprise installs need
   built-in OIDC; proxy auth composes with, not replaces, in-app authz.
2. **Full OPA/Cedar engine.** Powerful; deferred: a static CEL/YAML
   ruleset covers role+resource policies without a policy-language
   learning curve; OPA can be added behind the same `Authorizer`
   interface.
3. **API-gateway products (Kong etc.).** Deployment-specific; not a
   substitute for in-process authz of MCP (stdio) and CLI (same-machine)
   transports that never touch HTTP.
4. **Keep static keys only.** Rejected: fails procurement, fails audit
   attribution, fails rotation hygiene.

## Tradeoffs

- **Complexity budget:** OIDC discovery, JWKS caching, and clock-skew
  handling are ~2k lines + a new dependency (`go-oidc`); accepted as
  table stakes for enterprise.
- **Latency:** JWT verify ≈ tens of µs with cached keys — negligible;
  mTLS adds handshake cost amortized by keep-alive.
- **Migration:** existing INI keys keep working (hashed on first load,
  marked `legacy` in audit); a release note sets the deprecation clock.
- **Policy expressiveness vs simplicity:** the built-in ruleset will
  not satisfy every org; the `Authorizer` interface + OPA escape hatch
  bounds the support surface.

## Consequences

- SSO, group claims, and centralized offboarding become possible.
- Audit attribution has a real subject (feeds ADR-034).
- Tenant claims land in context (feeds ADR-030).
- Fail-open-by-omission class in `ScopeForPath` is removed by explicit
  route policies.
