# ARMAN database

The development migration baseline is:

`backend/migrations/0001_foundation.sql`

It establishes identity, profiles/privacy, roles, refresh-token storage,
subjects, resources, and bookmarks. Resources are private by default and
require approval before appearing in the public feed.

All future schema changes must be additive migrations with:

- UUID identifiers where appropriate
- foreign keys
- explicit constraints
- `created_at` and `updated_at`
- soft deletion for user-generated content where appropriate
- indexes for feed, filter, ownership, and status queries
- audit records for sensitive administrative operations

Do not edit production schema manually.
