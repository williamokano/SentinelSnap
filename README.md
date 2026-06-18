# SentinelSnap

A self-hosted security snap service. Snap photos with GPS coordinates from your phone and view them on an interactive map. Designed to work with iOS Shortcuts and Android automation apps so you can silently capture evidence — photos, location, timestamp — and POST it to your own server without the person holding the device knowing.

## How it works

```
Phone (iOS Shortcut / Android Tasker)
  └─ Takes 2 photos (base64-encoded)
  └─ Reads GPS coordinates
  └─ POST /snaps  ──────────────────►  SentinelSnap server
                                             └─ Stores photos on disk
                                             └─ Saves lat/lng + timestamp in DB
                                             └─ Pushes SSE event to all browsers
                                                   └─ Pin appears on map instantly
```

Open the map on any browser and see every snap pinned at its exact location — live, no refresh needed. The companion **feed** page (`/feed`) shows every photo — snaps and GPS-less "simple uploads" alike — newest first, with a tap-to-zoom viewer and a per-photo delete menu. Multiple browser tabs stay in sync via [Server-Sent Events](docs/realtime.md).

See [docs/](docs/) for detailed flow documentation.

---

## Authentication

Set `API_TOKEN` to require a static bearer token on all snap and photo endpoints (`POST/GET /snaps`, `PATCH/DELETE /snaps/{id}`, `POST/GET /photos`, `DELETE /photos/{token}`) and the SSE stream (`GET /events`):

```bash
API_TOKEN=$(openssl rand -hex 32)
```

Clients authenticate with an `Authorization: Bearer <token>` header. For SSE, where `EventSource` cannot set headers, pass the token as a query parameter instead: `GET /events?token=<token>`. Unauthenticated requests get `401 {"error":"unauthorized"}`.

When `API_TOKEN` is empty (the default) authentication is disabled and the server is fully open — a warning is logged at startup. `/healthz`, `/metrics`, `GET /photos/{token}` (unguessable capability URLs), and the static map and feed pages are never token-protected; the UI prompts for the token in the browser the first time the server rejects a request and remembers it in `localStorage`.

---

## Phone setup

### iOS Shortcuts

1. Open the **Shortcuts** app and create a new shortcut.
2. Add **Take Photo** — set it to take 1 photo, disable the camera preview (set _Show Camera Preview_ to off so it runs silently). Store the result in a variable named `Photo1`.
3. Add **Encode Media** (Base64) immediately after — set the input to `Photo1`. Store the result in a variable named `Photo1Base64`.
4. Repeat steps 2–3 for the second photo: take another photo into `Photo2`, then encode it into `Photo2Base64`.
5. Add **Get Current Location** — this gives you `Latitude` and `Longitude`.
6. Add a **Get Contents of URL** action:
   - URL: `http://<your-server-ip>:8080/snaps`
   - Method: `POST`
   - Headers: `Content-Type: application/json` — and, if your server has `API_TOKEN` set, add a second header `Authorization: Bearer <your-token>`
   - Body (JSON):
     ```json
     {
       "latitude": <Latitude>,
       "longitude": <Longitude>,
       "photos": [
         "<Photo1Base64>",
         "<Photo2Base64>"
       ]
     }
     ```
7. Add the shortcut to your **Home Screen** or trigger it via **Back Tap** (Settings → Accessibility → Touch → Back Tap) so it runs with a triple-tap on the back of the phone — discreet and fast.

### Android (Tasker + HTTP Request Plugin)

1. Install **Tasker** and the **HTTP Request Shortcuts** app (or use Tasker's built-in HTTP POST action).
2. Create a Tasker **Task**:
   - **Take Photo** action (Camera → Take Photo) — save to a variable, disable flash and shutter sound.
   - **Get Location** action — store latitude and longitude in variables `%LOC_LAT` and `%LOC_LONG`.
   - **Base64 Encode** the photo file (Variable → Base64 Encode).
   - **HTTP POST** to `http://<your-server-ip>:8080/snaps` with the same JSON body as above, substituting Tasker variables. If your server has `API_TOKEN` set, add a custom header `Authorization: Bearer <your-token>` to the request.
3. Assign the task to a **widget**, a **gesture**, or a **Quick Settings tile** for fast, discreet triggering.

---

## Running with Docker Compose

### 1. Copy the example env file

```bash
cp .env.example .env
```

Edit `.env` and set your values (see [Environment variables](#environment-variables) below).

### 2. Start the dependencies

```bash
docker compose up -d
```

This starts only the PostgreSQL database. The app is excluded from the default profile so you can run it locally during development.

### 3. Run the app

**Locally (development):**

```bash
go run ./cmd/server
```

**As a container (all services):**

```bash
docker compose --profile app up
```

### 4. Open the map

```
http://localhost:8080
```

---

## Observability

SentinelSnap ships with a full observability stack (structured logs, metrics, distributed traces) built on **OpenTelemetry**.

### Quick start

Ensure you have a `.env` file with at minimum `DB_DSN`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, and `POSTGRES_DB` set (see step 1 of [Running with Docker Compose](#running-with-docker-compose)).

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml --profile app up -d
```

Open Grafana at **http://localhost:3000** — the SentinelSnap dashboard is pre-provisioned.

The observability compose file enables `OTEL_ENABLED=true`, metrics in pull mode (Prometheus scrapes `/metrics`), traces pushed to Tempo, and logs pushed to Loki.

### Signal modes

| Signal | Default mode | Options |
|---|---|---|
| Metrics | `pull` — app exposes `/metrics`, Prometheus scrapes it | `pull` \| `push` (OTLP to collector) \| `off` |
| Traces | `push` — sent via OTLP to the collector → Tempo | `push` \| `off` |
| Logs | `stdout` — structured JSON to stdout | `stdout` \| `push` (OTLP to collector → Loki) |

All signals are gated behind `OTEL_ENABLED=true`. Setting it to `false` (the default) runs the app with zero OTel overhead while still emitting structured JSON logs.

### Stack components

| Service | Image | Port | Purpose |
|---|---|---|---|
| OTel Collector | `otel/opentelemetry-collector-contrib:0.119.0` | 4317/4318 | Receives OTLP; fans out to Tempo, Prometheus, Loki |
| Prometheus | `prom/prometheus:v3.1.0` | 9090 | Metrics store; scrapes `/metrics` in pull mode |
| Tempo | `grafana/tempo:2.7.0` | 3200 | Trace backend |
| Loki | `grafana/loki:3.3.0` | 3100 | Log aggregation |
| Grafana | `grafana/grafana:11.4.0` | 3000 | Dashboards, datasource correlations |

Config files live in `observability/`. The compose file mounts them read-only.

### Switching between pull and push metrics

**Pull (default):** Prometheus scrapes `/metrics` directly from the app. The OTel Collector is not involved for metrics.

**Push:** Set `OTEL_METRICS_MODE=push` and `OTEL_ENABLED=true`. The app sends metrics to the collector via OTLP, which forwards to Prometheus using the OTLP write receiver (already enabled in `docker-compose.observability.yml`).

### Trace–log correlation

When `OTEL_LOGS_MODE=push`, log records carry `trace_id` and `span_id` fields. Grafana's Loki datasource is pre-configured with a derived field that links `trace_id` values to Tempo, so you can jump from a log line to the full trace in one click.

### OTel stability note

The log-push path (`sdk/log`, `otlplog*`, `otelslog` bridge) is **pre-1.0 / beta** (`go.opentelemetry.io/otel/sdk/log v0.20.0`). It is isolated in `internal/observability/logs_push.go`. The default (`OTEL_LOGS_MODE=stdout`) uses only stable packages.

---

## Environment variables

All variables can be set in a `.env` file (loaded automatically at startup) or passed as regular environment variables. Use `ENV_FILE=path/to/file` to load a different file.

| Variable | Default | Description |
|---|---|---|
| `HTTP_PORT` | `8080` | Port the server listens on |
| `API_TOKEN` | — | Bearer token required on `/snaps` and `/events` (see [Authentication](#authentication)). Empty disables auth |
| `DB_DRIVER` | `postgres` | Database driver |
| `DB_DSN` | — | **Required.** Full Postgres DSN |
| `POSTGRES_DB` | — | Database name (used by docker-compose) |
| `POSTGRES_USER` | — | Database user (used by docker-compose) |
| `POSTGRES_PASSWORD` | — | Database password (used by docker-compose) |
| `STORAGE_BACKEND` | `local` | Storage backend (`local`) |
| `LOCAL_UPLOAD_DIR` | `./uploads` | Directory where photos are saved |
| `ENV_FILE` | `.env` | Path to the env file to load |
| `DEBUG` | `false` | Log full request bodies |
| `LOG_LEVEL` | `info` | Log verbosity: `debug` \| `info` \| `warn` \| `error` |
| `LOG_FORMAT` | `json` | Log format: `json` \| `text` |
| `OTEL_ENABLED` | `false` | Master switch for metrics/traces/log-push |
| `OTEL_SERVICE_NAME` | `sentinelsnap` | OTel `service.name` resource attribute |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4317` | Collector endpoint (used when any signal is in push mode) |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` | OTLP protocol: `grpc` \| `http/protobuf` |
| `OTEL_METRICS_MODE` | `pull` | Metrics mode: `pull` \| `push` \| `off` |
| `OTEL_TRACES_MODE` | `push` | Traces mode: `push` \| `off` |
| `OTEL_LOGS_MODE` | `stdout` | Logs mode: `stdout` \| `push` |
| `OTEL_TRACES_SAMPLER_ARG` | `1.0` | Trace sample ratio `[0.0–1.0]` |

S3 support (`S3_BUCKET`, `S3_REGION`, `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`) is wired in the config but not yet implemented in the storage layer.

---

## API

### `POST /snaps`

Create a new snap with one or more photos.

**Request body:**

```json
{
  "latitude": 37.7749,
  "longitude": -122.4194,
  "photos": [
    "<base64-encoded image>",
    "<base64-encoded image>"
  ]
}
```

`photos` is a plain array of base64-encoded image bytes. The server detects the content type automatically (JPEG, PNG, GIF, WebP) and generates a random filename — no metadata needed from the client. `latitude` and `longitude` accept both numbers and strings (iOS Shortcuts sends strings).

Limits: request bodies larger than 50 MiB are rejected with `413 Request Entity Too Large`, and at most 10 photos are accepted per snap (`400` otherwise).

**Response `201`:**

```json
{
  "id": 1,
  "name": "",
  "latitude": 37.7749,
  "longitude": -122.4194,
  "created_at": "2024-01-01T12:00:00Z",
  "photos": [
    {
      "id": 1,
      "snap_id": 1,
      "url": "/photos/<random-token>",
      "created_at": "2024-01-01T12:00:00Z"
    }
  ]
}
```

Also broadcasts a `snap` SSE event to all connected browsers.

---

### `GET /snaps`

List all snaps with their photos, newest first.

---

### `PATCH /snaps/{id}`

Rename a snap (max 100 characters). Empty string clears the name back to the default "Snap #N" display. Request bodies larger than 1 MiB are rejected with `413`.

**Request body:**

```json
{ "name": "Front door" }
```

**Response `200`:**

```json
{ "id": 1, "name": "Front door" }
```

Also broadcasts a `snap_updated` SSE event to all connected browsers.

---

### `DELETE /snaps/{id}`

Delete a snap and all its photos from storage and the database.

**Response `204` No Content.**

Also broadcasts a `snap_deleted` SSE event to all connected browsers.

---

### `POST /photos`

Upload one or more photos **without GPS** ("simple uploads"). Unlike a snap, these photos are not tied to a location and never appear on the map — only in the feed. The body, content-type detection, photo limit (10), and 50 MiB cap match `POST /snaps`, minus the coordinates:

```json
{ "photos": [ "<base64-encoded image>" ] }
```

**Response `201`:** an array of the created photos (each with `id`, `url`, `created_at`; no `snap_id`). Also broadcasts a `photo` SSE event to all connected browsers.

---

### `GET /photos`

List **all** photos — both snap-linked and standalone uploads — newest first. Used by the feed page. Each entry carries `id`, `url`, `created_at`, and `snap_id` (omitted for standalone uploads).

---

### `DELETE /photos/{token}`

Delete a single photo, addressed by the same capability token used to view it. The photo's file and row are removed; if it was the **last** photo of a snap, that now-empty snap is deleted too (and a `snap_deleted` event fires so it disappears from the map). Always broadcasts a `photo_deleted` SSE event.

**Response `204` No Content.**

---

### `GET /photos/{token}`

Stream a photo by its random token. Tokens are generated at upload time and stored in the database — they are not derivable from any other ID or path, preventing enumeration. Each successful fetch increments the photo's view counter (stored on the row; not currently surfaced in any response).

---

### `GET /healthz`

Health check endpoint. Returns `200` when the server is up and the database is reachable, or `503` when the database ping fails.

**Response `200`:**

```json
{ "status": "ok" }
```

**Response `503`:**

```json
{ "status": "error", "error": "dial tcp ...: connection refused" }
```

Used by Docker and other orchestrators to determine container health.

---

### `GET /events`

Server-Sent Events stream. The browser connects once on page load and receives push notifications for all snap activity. When `API_TOKEN` is set, authenticate with `?token=<token>` (EventSource cannot send an `Authorization` header). See [docs/realtime.md](docs/realtime.md) for the full event reference.

---

## Building the Docker image

The image is built and pushed to Docker Hub automatically on every `v*` tag via GitHub Actions.

**Required repository secrets:**

| Secret | Description |
|---|---|
| `DOCKER_USERNAME` | Docker Hub username |
| `DOCKER_PASSWORD` | Docker Hub access token |

**To trigger a release:**

```bash
git tag v1.0.0
git push origin v1.0.0
```

You can also trigger it manually from the **Actions** tab in GitHub using the `workflow_dispatch` input.

The published image is:

```
<your-dockerhub-username>/sentinelsnap:1.0.0
<your-dockerhub-username>/sentinelsnap:latest
```

**To run the published image directly:**

```bash
docker run -d \
  -p 8080:8080 \
  -e DB_DSN="postgres://user:pass@host:5432/sentinelsnap?sslmode=disable" \
  -e STORAGE_BACKEND=local \
  -e LOCAL_UPLOAD_DIR=/app/uploads \
  -v ./uploads:/app/uploads \
  <your-dockerhub-username>/sentinelsnap:latest
```
