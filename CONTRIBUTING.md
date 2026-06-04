# Contributing to SentinelSnap

Thank you for your interest in contributing. This document covers how to set up your environment, submit changes, and use the project's issue and label conventions.

---

## Development setup

```bash
# 1. Clone and enter the repo
git clone https://github.com/williamokano/SentinelSnap.git
cd SentinelSnap

# 2. Copy the env file and fill in your values
cp .env.example .env

# 3. Start the database
docker compose up -d

# 4. Run the server
go run ./cmd/server

# 5. Open the map
open http://localhost:8080
```

Requirements: Go 1.24+, Docker, PostgreSQL (via Docker Compose).

---

## Running tests

```bash
go test ./...
```

Integration tests (in `internal/repository/postgres/`) require a live database and are skipped when `DB_DSN` is not set.

---

## Submitting changes

1. Fork the repository and create a branch from `main`.
2. Keep commits focused — one logical change per commit.
3. Run `go build ./...` and `go test ./...` before opening a PR.
4. Reference the related issue in your PR description (e.g. `Closes #12`).

---

## Issue labels

These labels are used on all issues and pull requests. Use them when opening issues so they can be triaged quickly.

### Type

| Label | When to use |
|---|---|
| `bug` | Something is broken or behaves incorrectly |
| `enhancement` | A new feature or improvement to an existing one |
| `documentation` | Docs-only change — README, CONTRIBUTING, docs/ |
| `question` | A question or discussion, not a concrete task |

### Domain

| Label | When to use |
|---|---|
| `security` | Authentication, authorisation, encryption, or any security concern |
| `storage` | Storage backends — local disk, S3, encryption at rest |
| `ux` | Frontend behaviour, map interaction, toasts, popups |
| `performance` | Latency, throughput, database query efficiency |
| `infrastructure` | Docker, CI/CD, deployment, health checks |
| `realtime` | SSE hub, event propagation, live updates |

### Effort / flags

| Label | When to use |
|---|---|
| `good first issue` | Self-contained, well-scoped, suitable for a first contribution |
| `hard` | Requires significant research, cross-cutting changes, or unsolved design problems |
| `wontfix` | Out of scope or intentionally not addressed |

---

## Commit style

Use the [Conventional Commits](https://www.conventionalcommits.org/) format:

```
feat: add S3 storage backend
fix: preserve spiderfy state on SSE-driven marker updates
docs: document SSE event reference
chore: update Go to 1.24
```

---

## Code conventions

- No comments unless the *why* is non-obvious (hidden constraint, subtle invariant, workaround).
- No backwards-compatibility shims for removed code — just delete it.
- Validate only at system boundaries (HTTP handlers); trust internal contracts.
- New storage backends must implement `storage.StorageProvider`.
- New SSE event types must be defined as constants in `internal/hub/hub.go`.
