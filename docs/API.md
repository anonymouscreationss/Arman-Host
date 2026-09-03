# ARMAN API

The API is versioned under `/api/v1/`. The source-of-truth contract is
`contracts/openapi.yaml`.

Responses use:

```json
{
  "success": true,
  "data": {},
  "message": null,
  "error": null
}
```

Errors contain stable machine-readable codes and user-safe messages. Request
IDs are returned in `X-Request-ID` and should be included in server logs.

The initial contract covers health, readiness, service configuration, login,
registration, current user, profile, and paginated resources. New domains
must be added to the contract before client implementation.
