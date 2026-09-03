# ARMAN architecture

ARMAN uses a vertical-slice architecture with a Flutter Android application,
a Go business API, a Python AI/document service, PostgreSQL, Redis, an
OpenSearch-compatible search layer, S3-compatible storage, Firebase
notifications, and Vue applications for the website and admin surface.

## Boundaries

```text
Flutter / Vue
      |
      v
Go API: auth, authorization, business rules, persistence orchestration
      |
      +--> PostgreSQL
      +--> Redis
      +--> object storage
      +--> OpenSearch
      +--> Python AI service
                    |
                    +--> provider abstraction
                    +--> OCR/PDF/workers
```

Handlers should call services, services should call repositories, and only
repositories should contain database access. Clients communicate with ARMAN,
not directly with AI providers or private storage.

## First vertical slice

The first slice establishes:

- versioned API contract
- health and readiness
- environment-driven feature configuration
- database foundation for users, profiles, roles, sessions, subjects,
  resources, and bookmarks
- Vue responsive shell
- API-backed resource loading
- authentication UI connected to the planned API endpoint
- explicit unavailable-service states

Later features must extend these boundaries rather than create parallel
authentication, resource, or profile systems.
