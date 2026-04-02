# Guestbook Backend

Go API server for the [chenna.me](https://chenna.me) guestbook. Stores entries in SQLite and runs on [Fly.io](https://fly.io).

## Endpoints

### Public

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/api/entries?page=1&per_page=24` | Paginated approved entries (newest first) |
| `POST` | `/api/entries` | Submit a new entry (goes to pending) |
| `GET` | `/api/entries/{id}/image` | Approved entry drawing (PNG) |

### Admin (Bearer token required)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/admin/entries` | List pending entries |
| `GET` | `/api/admin/entries/{id}/image` | Pending entry drawing (PNG) |
| `POST` | `/api/admin/entries/{id}/approve` | Approve a pending entry |
| `POST` | `/api/admin/entries/{id}/reject` | Reject a pending entry |
| `DELETE` | `/api/admin/entries/{id}` | Delete any entry |
| `POST` | `/api/admin/purge-rejected` | Delete all rejected entries |

## Creating entries

**JSON** (messages only):

```json
POST /api/entries
Content-Type: application/json

{"name": "Alice", "website": "example.com", "entry_type": "message", "content": "Hello!"}
```

**Multipart** (drawings or messages):

```
POST /api/entries
Content-Type: multipart/form-data

name=Alice&entry_type=drawing&image=<PNG file, max 5MB>
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `DB_PATH` | `/data/guestbook.db` | SQLite database path |
| `ADMIN_TOKEN` | _(none)_ | Bearer token for admin endpoints; admin is disabled if unset |
| `ALLOWED_ORIGINS` | `https://chenna.me,http://localhost:4000` | Comma-separated CORS origins |

## Development

Requires Go 1.23+.

```sh
# Run locally
ALLOWED_ORIGINS="http://localhost:4001" ADMIN_TOKEN=secret DB_PATH=./guestbook.db go run .

# Run tests
go test -v ./...
```

## Deployment

Deployed to Fly.io with a persistent volume for the SQLite database.

```sh
fly deploy
fly secrets set ADMIN_TOKEN=<token>
fly secrets set ALLOWED_ORIGINS="https://chenna.me,http://localhost:4001"
```
