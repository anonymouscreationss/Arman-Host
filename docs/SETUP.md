# ARMAN local setup

This repository is being built in vertical slices. The first slice contains
the Vue web shell, Go API foundation, Python AI service boundary, API contract,
and the first PostgreSQL migration.

## Current prerequisites

- Node.js 24+
- pnpm 10+
- Go 1.25+
- Python 3.12+ with `pip`
- PostgreSQL 16+
- Flutter 3.32+ and Android SDK 35 for the mobile build

## Run the web shell

```bash
pnpm install
pnpm --filter @arman/web dev
```

The web development server uses port 5000.

## Run the Go API

```bash
cd backend
go mod tidy
go run ./cmd/api
```

The API uses port 8080 by default. It starts healthfully without a database,
but `/api/v1/ready` remains unavailable until `DATABASE_URL` is configured.

## Run the AI boundary

```bash
python3 -m pip install -r ai/requirements.txt
python3 -m uvicorn ai.app.main:app --host 0.0.0.0 --port 8000
```

AI generation intentionally reports a configuration state until a provider is
configured. No provider key is stored in source code.

## Run the mobile slice

The mobile client requires the API base URL at build time. The current Replit
API workflow listens on port `8008`:

```bash
cd mobile
flutter pub get
flutter run --dart-define=API_BASE_URL=https://YOUR_REPLIT_DEV_DOMAIN:8008
```

For the Android emulator, use `http://10.0.2.2:8008` when the API is running on
the host machine. For a physical device, use an HTTPS API URL reachable by that
device. A release build can be produced with the same define:

```bash
flutter build apk --release \
  --dart-define=API_BASE_URL=https://YOUR_REPLIT_DEV_DOMAIN:8008
```

The mobile client does not silently fall back to local or fake data when
`API_BASE_URL` is missing.

## Database

Apply `backend/migrations/0001_foundation.sql` to the development PostgreSQL
database after reviewing it. Production schema changes must use the approved
deployment migration process.

## Environment

Copy `.env.example` to a local environment configuration and set only the
values for services that have been authorized. Never commit `.env`.
