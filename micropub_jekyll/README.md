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
| `BIND_ADDR` | No | HTTP listen address | `127.0.0.1` |
| `PORT` | No | HTTP listen port | `8080` |
| `REPO_PATH` | Yes | Path to a clean local Jekyll repo clone tracking an upstream branch | `/data/chenna.me` |
| `GCS_BUCKET` | Yes | Google Cloud Storage bucket name | — |
| `GCS_PREFIX` | No | Object prefix in bucket | `photos/prod/opt/micro` |
| `IMAGE_BASE_URL` | No | CDN base URL for images | `//i.chenna.me/photos/prod/opt/micro` |
| `SITE_URL` | No | Site URL for IndieAuth | `https://chenna.me` |
| `TOKEN_ENDPOINT` | No | IndieAuth token endpoint | `https://tokens.indieauth.com/token` |
| `ALLOWED_ORIGINS` | No | CORS origins (comma-separated) | `https://chenna.me,http://localhost:4000` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Yes | GCS service account key path | — |
| `ORIGIN_CERT_PATH` | No | Cloudflare origin cert path for nginx | — |
| `ORIGIN_KEY_PATH` | No | Cloudflare origin key path for nginx | — |

## Built with

- [indielib](https://github.com/hacdias/indielib) — Micropub/IndieAuth protocol handling
- [godotenv](https://github.com/joho/godotenv) — `.env` loading
- [frontmatter](https://github.com/adrg/frontmatter) + [yaml.v3](https://pkg.go.dev/gopkg.in/yaml.v3) — Jekyll front matter parsing and serialization
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

`REPO_PATH` should point at a dedicated checkout with a configured upstream branch. The service now fast-forwards that checkout and refuses local divergence instead of resetting it away.

## Deployment

Deployments are now `.env`-driven and managed with `systemd` instead of Docker.

```sh
cp .env.example .env
$EDITOR .env
./setup_proxy.sh
./deploy.sh
```

What `deploy.sh` does:

- builds the Go binary
- installs it to `/usr/local/bin/micropub-jekyll`
- creates or updates a `systemd` unit
- restarts the service and shows its status/logs

What `setup_proxy.sh` does:

- installs nginx if it is missing
- writes or updates an nginx reverse-proxy config
- proxies `SERVER_NAME` or `SITE_URL` to the Micropub app on `BIND_ADDR:PORT`
- optionally enables origin-side HTTPS when `ORIGIN_CERT_PATH` and `ORIGIN_KEY_PATH` are set

Both scripts are intended to be safe to rerun. They overwrite generated config only when content changes and validate the nginx config before reloading it.

### Google Cloud + Cloudflare

For a small GCE instance such as `e2-micro`, the simplest layout is:

- run the Go service on `127.0.0.1:8080`
- expose nginx on ports `80` and optionally `443`
- proxy DNS through Cloudflare

Recommended setup:

1. Leave `BIND_ADDR=127.0.0.1` in `.env` so the Go service is not directly exposed.
2. Run `./setup_proxy.sh` once on the VM.
3. Open ports `80` and `443` in the GCP firewall for the instance.
4. Point your Cloudflare proxied DNS record at the VM.
5. If you want Cloudflare `Full (strict)`, install a Cloudflare Origin Certificate on the VM, set `ORIGIN_CERT_PATH` and `ORIGIN_KEY_PATH`, and rerun `./setup_proxy.sh`.

If you prefer a single command on the host, `deploy.sh` can also invoke the proxy setup:

```sh
SETUP_PROXY=1 ./deploy.sh
```

> The script is intended for the GCloud/Linux host where this service runs. The Jekyll site discovers the server via `<link rel="micropub">` in the HTML `<head>`.
