# Supported Hydra versions

| Runtime `/version` | Pinned OpenAPI SHA-256 | Adapter |
| --- | --- | --- |
| `v26.2.0` | `aa95a73bb14a90b8f2ce1f852cc41418cdbef3ac3bdedc09a48df0bc97a40540` | Fixed v26.2.0 adapter |

The adapter requires an exact version match, a valid embedded specification digest, `GET /health/ready`, and a successful read-only Client list probe. Any different runtime version closes all Client and Token business operations; login, status, diagnostics, and local audit reading remain available. `check` sends no write requests to Hydra, but checks local log and audit path writability using temporary files. It cannot establish business write acceptance.

The adapter exposes Client list/detail/create/JSON Patch/delete for this version when checks pass. Replacement PUT and Secret rotation are disabled. Token introspection is gated by the pinned Admin API contract. Public revoke additionally requires explicit Public URL configuration, activation, and this request's Client credentials. The status page reports effective capabilities and reasons. The Client list uses Hydra cursor pagination with 10 Clients per page; Scope and Audience still aggregate only the first 100 readable Clients.

The pinned source URL, download date, license and digest are in `specs/v26.2.0/SOURCE.md` in this repository. The [reviewed endpoint details](https://github.com/FutureHai/hydra-admin-doc/blob/master/specs/v26.2.0/ADAPTER_REVIEW.md) are in the separate hydra-admin-doc repository, which is currently private and available only to authorized readers. They are not inferred from a live `/openapi.json` endpoint.
