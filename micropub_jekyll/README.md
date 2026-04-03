# micropub_jekyll

A Go-based [Micropub](https://micropub.spec.indieweb.org/) server that creates Jekyll micro-posts on [chenna.me](https://chenna.me). Compatible with [Sunlit](https://sunlit.io), [micro.blog](https://micro.blog) iOS app, and other Micropub clients.

## How it works

1. Micropub client authenticates via [IndieAuth](https://indieauth.spec.indieweb.org/)
2. Client sends a post (text, photos, or both) to the `/micropub` endpoint
3. Server creates a Jekyll post file in `_micros/` collection
4. For photos: images are resized to 4 responsive variants and uploaded to Google Cloud Storage
5. Server commits and pushes to the git repo → GitHub Pages rebuilds the site
6. Post appears at `chenna.me/micro/`

## Endpoints

| Endpoint | Method | Description |
|---|---|---|
| `/micropub` | `GET` | Query config, source, categories |
| `/micropub` | `POST` | Create, update, delete posts |
| `/media` | `POST` | Upload images |
| `/health` | `GET` | Health check |

## Environment Variables

| Variable | Required | Description | Default |
|---|---|---|---|
| `PORT` | No | HTTP listen port | `8080` |
| `REPO_PATH` | Yes | Path to local Jekyll repo clone | `/data/chenna.me` |
| `GCS_BUCKET` | Yes | Google Cloud Storage bucket name | — |
| `GCS_PREFIX` | No | Object prefix in bucket | `photos/prod/opt/micro` |
| `IMAGE_BASE_URL` | No | CDN base URL for images | `//i.chenna.me/photos/prod/opt/micro` |
| `SITE_URL` | No | Site URL for IndieAuth | `https://chenna.me` |
| `TOKEN_ENDPOINT` | No | IndieAuth token endpoint | `https://tokens.indieauth.com/token` |
| `ALLOWED_ORIGINS` | No | CORS origins (comma-separated) | `https://chenna.me,http://localhost:4000` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Yes | GCS service account key path | — |

## Built with

- [indielib](https://github.com/hacdias/indielib) — Micropub/IndieAuth protocol handling
- [imaging](https://github.com/disintegration/imaging) — Image resizing
- [cloud.google.com/go/storage](https://pkg.go.dev/cloud.google.com/go/storage) — GCS client

## Development

1. Copy `.env.example` to `.env` and fill in the values.
2. Build and run the service locally:

```sh
cp .env.example .env
go build -o micropub-jekyll .
./micropub-jekyll
```

The binary automatically loads `.env` from the current working directory. Existing shell environment variables still take precedence.

## Deployment

Deployments are now `.env`-driven and managed with `systemd` instead of Docker.

```sh
cp .env.example .env
$EDITOR .env
./deploy.sh
```

What `deploy.sh` does:

- builds the Go binary
- installs it to `/usr/local/bin/micropub-jekyll`
- creates or updates a `systemd` unit
- restarts the service and shows its status/logs

> The script is intended for the GCloud/Linux host where this service runs. The Jekyll site discovers the server via `<link rel="micropub">` in the HTML `<head>`.
