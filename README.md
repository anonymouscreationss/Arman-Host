# ARMAN — ارمان

Aspire. Learn. Achieve.

ARMAN is a student ecosystem for learning, practice, progress, community,
and an integrated AI study assistant.

## Build status

The project is being built as a production-oriented vertical slice. The
repository currently contains:

- Vue 3 + Vite web shell
- Go API foundation
- Python FastAPI AI service boundary
- OpenAPI contract
- PostgreSQL foundation migration
- architecture and setup documentation

Flutter/Android and the remaining product domains are being added in later
phases. Features are not represented as complete until their API, persistence,
loading, empty, error, and permission behavior exists.

## Brand

Use the official ARMAN logo and the supplied visual references in
`attached_assets/`. The core visual identity is deep navy, warm gold,
white/cream, subtle green, and Poppins with Arabic-script support.

## Commands

```bash
pnpm install
pnpm --filter @arman/web dev
cd backend && go run ./cmd/api
python3 -m uvicorn ai.app.main:app --host 0.0.0.0 --port 8000
```

See `docs/SETUP.md` for the current setup and known toolchain blockers.
