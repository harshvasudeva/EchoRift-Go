# Local LiveKit binary

This directory is for a local development copy of `livekit-server.exe`.

Expected file:

- `tools/livekit/livekit-server.exe`

The binary is intentionally ignored by git via `*.exe`.

Current tested local version:

```text
livekit-server version 1.12.0
```

Run it from the repo root with:

```text
powershell -ExecutionPolicy Bypass -File .\deploy\scripts\run_livekit_windows.ps1
```

EchoRift local config must match `deploy/livekit/livekit.local.yaml`:

```text
LIVEKIT_URL=ws://127.0.0.1:7880
LIVEKIT_API_KEY=replace-with-a-local-livekit-key
LIVEKIT_API_SECRET=replace-with-a-local-livekit-secret
```
