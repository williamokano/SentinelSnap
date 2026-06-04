# SentinelSnap

A self-hosted security snap service. Snap photos with GPS coordinates from your phone and view them on an interactive map. Designed to work with iOS Shortcuts and Android automation apps so you can silently capture evidence — photos, location, timestamp — and POST it to your own server without the person holding the device knowing.

## How it works

```
Phone (iOS Shortcut / Android Tasker)
  └─ Takes 2 photos
  └─ Reads GPS coordinates
  └─ POST /snaps  ──────────────────►  SentinelSnap server
                                             └─ Stores photos
                                             └─ Saves lat/lng + timestamp
                                             └─ Shows pins on map UI
```

You open the map on any browser and see every snap pinned at its exact location.

---

## Phone setup

### iOS Shortcuts

1. Open the **Shortcuts** app and create a new shortcut.
2. Add **Take Photo** — set it to take 2 photos, disable the camera preview (set _Show Camera Preview_ to off so it runs silently).
3. Add **Get Current Location** — this gives you `Latitude` and `Longitude`.
4. Add a **Base64 Encode** action for each photo (encode the image file).
5. Add a **Get Contents of URL** action:
   - URL: `http://<your-server-ip>:8080/snaps`
   - Method: `POST`
   - Headers: `Content-Type: application/json`
   - Body (JSON):
     ```json
     {
       "latitude": <Latitude>,
       "longitude": <Longitude>,
       "photos": [
         {
           "filename": "photo1.jpg",
           "content_type": "image/jpeg",
           "data": "<Base64 of photo 1>"
         },
         {
           "filename": "photo2.jpg",
           "content_type": "image/jpeg",
           "data": "<Base64 of photo 2>"
         }
       ]
     }
     ```
6. Add the shortcut to your **Home Screen** or trigger it via **Back Tap** (Settings → Accessibility → Touch → Back Tap) so it runs with a triple-tap on the back of the phone — discreet and fast.

### Android (Tasker + HTTP Request Plugin)

1. Install **Tasker** and the **HTTP Request Shortcuts** app (or use Tasker's built-in HTTP POST action).
2. Create a Tasker **Task**:
   - **Take Photo** action (Camera → Take Photo) — save to a variable, disable flash and shutter sound.
   - **Get Location** action — store latitude and longitude in variables `%LOC_LAT` and `%LOC_LONG`.
   - **Base64 Encode** the photo file (Variable → Base64 Encode).
   - **HTTP POST** to `http://<your-server-ip>:8080/snaps` with the same JSON body as above, substituting Tasker variables.
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

## Environment variables

All variables can be set in a `.env` file (loaded automatically at startup) or passed as regular environment variables. Use `ENV_FILE=path/to/file` to load a different file.

| Variable | Default | Description |
|---|---|---|
| `HTTP_PORT` | `8080` | Port the server listens on |
| `DB_DRIVER` | `postgres` | Database driver |
| `DB_DSN` | — | **Required.** Full Postgres DSN |
| `POSTGRES_DB` | — | Database name (used by docker-compose) |
| `POSTGRES_USER` | — | Database user (used by docker-compose) |
| `POSTGRES_PASSWORD` | — | Database password (used by docker-compose) |
| `STORAGE_BACKEND` | `local` | Storage backend (`local`) |
| `LOCAL_UPLOAD_DIR` | `./uploads` | Directory where photos are saved |
| `LOCAL_BASE_URL` | `http://localhost:8080/uploads` | Public base URL for serving photos |
| `ENV_FILE` | `.env` | Path to the env file to load |

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
    {
      "filename": "photo.jpg",
      "content_type": "image/jpeg",
      "data": "<base64-encoded image>"
    }
  ]
}
```

**Response `201`:**

```json
{
  "id": 1,
  "latitude": 37.7749,
  "longitude": -122.4194,
  "created_at": "2024-01-01T12:00:00Z",
  "photos": [
    {
      "id": 1,
      "snap_id": 1,
      "url": "http://localhost:8080/uploads/snaps/1/photo.jpg",
      "stored_key": "snaps/1/photo.jpg",
      "created_at": "2024-01-01T12:00:00Z"
    }
  ]
}
```

### `GET /snaps`

List all snaps with their photos.

### `GET /uploads/*`

Serves uploaded photos (only when `STORAGE_BACKEND=local`).

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
  -e LOCAL_BASE_URL=http://localhost:8080/uploads \
  -v ./uploads:/app/uploads \
  <your-dockerhub-username>/sentinelsnap:latest
```
