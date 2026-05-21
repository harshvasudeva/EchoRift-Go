# EchoRift

Self-hosted realtime team communication platform: Go backend, PostgreSQL, native WebSockets, React/Vite frontend, and LiveKit-ready media.

## Local database

A local development PostgreSQL database is expected:

- Database: `echorift`
- App role: `echorift_app`

The app reads config from `backend/.env`.

## Backend quick start

```text
cd backend
go mod tidy
go run ./cmd/migrate
go run ./cmd/echorift
```

If Windows Security blocks temporary `go run` executables, use the signed Windows flow instead:

```text
powershell -ExecutionPolicy Bypass -File .\deploy\scripts\build_sign_windows.ps1
powershell -ExecutionPolicy Bypass -File .\deploy\scripts\run_backend_windows.ps1
```

The signing script creates a local self-signed `EchoRift Local Dev` code-signing certificate, trusts it for the current user, builds `bin\echorift.exe`, signs it, and verifies the signature.

If your current terminal cannot find Go on Windows, restart the terminal/app that launched it, or set `GO` explicitly when using `make`.

## Web quick start

This repo uses pnpm through Corepack:

```text
corepack pnpm install
corepack pnpm --filter @echorift/web dev
```
