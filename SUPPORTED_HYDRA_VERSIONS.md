# Supported Hydra versions

| Runtime `/version` | Pinned OpenAPI SHA-256 | Adapter |
| --- | --- | --- |
| `v26.2.0` | `aa95a73bb14a90b8f2ce1f852cc41418cdbef3ac3bdedc09a48df0bc97a40540` | Fixed v26.2.0 adapter |

The adapter requires an exact version match, a valid embedded specification digest, `GET /health/ready`, and a successful read-only Client list probe. Any different runtime version closes all Client and Token business operations; login, status, diagnostics, and local audit reading remain available. `check` sends no write requests to Hydra, but checks local log and audit path writability using temporary files. It cannot establish business write acceptance.

The adapter exposes Client list/detail/create/JSON Patch/delete for this version when checks pass. Replacement PUT and Secret rotation are disabled. Token introspection is gated by the pinned Admin API contract. Public revoke additionally requires explicit Public URL configuration, activation, and this request's Client credentials. The status page reports effective capabilities and reasons. The Client list uses Hydra cursor pagination with 10 Clients per page; Scope and Audience still aggregate only the first 100 readable Clients.

Client creation can optionally override the per-grant Access/ID/Refresh Token lifespans exposed by the pinned schema, consent skip flags, and front/back-channel logout settings. Omitted values inherit Hydra defaults. Consent and logout fields can later be changed through allowlisted JSON Patch. The twelve UI-supported lifespans can be changed or reset through a separate, guarded `PUT /admin/clients/{id}/lifespans`; the adapter reads and resends every existing lifespan, including the non-editable JWT bearer field, because Hydra clears omitted lifespans. Full Client replacement PUT remains disabled. Deployments with Token janitor cleanup must include any longer Client-specific lifespans in the janitor safety review. Opt-in live tests passed against local v26.2.0 on 2026-11-02: all twelve lifespan fields and consent/logout settings round-tripped, one lifespan was updated and reset without changing the others, consent/logout JSON Patch worked, and a Client Credentials Token received the configured 2h30m lifespan.

The pinned source URL, download date, license and digest are in [SOURCE.md](https://github.com/FutureHai/hydra-admin/blob/master/specs/v26.2.0/SOURCE.md) in this repository. They are not inferred from a live `/openapi.json` endpoint.
