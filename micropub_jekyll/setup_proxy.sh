#!/usr/bin/env bash
set -euo pipefail

APP_NAME="micropub-jekyll"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ENV_FILE:-$SCRIPT_DIR/.env}"
ENV_FILE="$(cd "$(dirname "$ENV_FILE")" && pwd)/$(basename "$ENV_FILE")"
NGINX_CONF_PATH="${NGINX_CONF_PATH:-/etc/nginx/conf.d/$APP_NAME.conf}"
CLIENT_MAX_BODY_SIZE="${CLIENT_MAX_BODY_SIZE:-25m}"
PROXY_READ_TIMEOUT="${PROXY_READ_TIMEOUT:-180s}"
PROXY_SEND_TIMEOUT="${PROXY_SEND_TIMEOUT:-180s}"
INSTALL_NGINX="${INSTALL_NGINX:-1}"

run_as_root() {
  if [[ ${EUID} -ne 0 ]]; then
    sudo "$@"
  else
    "$@"
  fi
}

load_env_file() {
  if [[ ! -f "$ENV_FILE" ]]; then
    return
  fi

  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line##[[:space:]]}"
    line="${line%%[[:space:]]}"
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ "$line" == export\ * ]] && line="${line#export }"
    line="${line##[[:space:]]}"
    local key="${line%%=*}"
    local val="${line#*=}"
    key="${key%%[[:space:]]}"
    val="${val##[[:space:]]}"
    [[ -z "$key" ]] && continue
    # Strip matching quotes
    if [[ ${#val} -ge 2 && ( ("${val:0:1}" == "'" && "${val: -1}" == "'") || ("${val:0:1}" == '"' && "${val: -1}" == '"') ) ]]; then
      val="${val:1:${#val}-2}"
    fi
    # Only set if not already in the environment
    if [[ -z "${!key+x}" ]]; then
      export "$key=$val"
    fi
  done <"$ENV_FILE"
}

install_nginx() {
  if command -v nginx >/dev/null 2>&1; then
    return
  fi

  if [[ "$INSTALL_NGINX" != "1" ]]; then
    echo "error: nginx is not installed and INSTALL_NGINX is not set to 1" >&2
    exit 1
  fi

  echo "==> Installing nginx"
  if command -v apt-get >/dev/null 2>&1; then
    run_as_root apt-get update
    run_as_root apt-get install -y nginx
  elif command -v dnf >/dev/null 2>&1; then
    run_as_root dnf install -y nginx
  elif command -v yum >/dev/null 2>&1; then
    run_as_root yum install -y nginx
  else
    echo "error: could not determine how to install nginx on this host" >&2
    exit 1
  fi
}

install_nginx_config() {
  local source_path="$1"
  local target_path="$2"
  local backup_path=""

  if [[ -f "$target_path" ]] && cmp -s "$source_path" "$target_path"; then
    return 1
  fi

  if [[ -f "$target_path" ]]; then
    backup_path="$(mktemp)"
    run_as_root cp "$target_path" "$backup_path"
  fi

  run_as_root install -D -m 0644 "$source_path" "$target_path"
  if ! run_as_root nginx -t; then
    if [[ -n "$backup_path" ]]; then
      run_as_root install -D -m 0644 "$backup_path" "$target_path"
    else
      run_as_root rm -f "$target_path"
    fi
    run_as_root nginx -t >/dev/null 2>&1 || true
    rm -f "$backup_path"
    echo "error: nginx configuration test failed; restored the previous config" >&2
    exit 1
  fi

  rm -f "$backup_path"
  return 0
}

reload_or_start_nginx() {
  run_as_root systemctl enable nginx >/dev/null
  if run_as_root systemctl is-active --quiet nginx; then
    run_as_root systemctl reload nginx
  else
    run_as_root systemctl start nginx
  fi
}

if ! command -v systemctl >/dev/null 2>&1; then
  echo "error: systemd is required for this setup script" >&2
  exit 1
fi

load_env_file
install_nginx

if [[ -n "${BIND_ADDR:-}" && "$BIND_ADDR" != "0.0.0.0" && "$BIND_ADDR" != "::" ]]; then
  default_upstream_host="$BIND_ADDR"
else
  default_upstream_host="127.0.0.1"
fi

UPSTREAM_HOST="${UPSTREAM_HOST:-$default_upstream_host}"
UPSTREAM_PORT="${UPSTREAM_PORT:-${PORT:-8080}}"
SERVER_NAME="${SERVER_NAME:-}"
if [[ -z "$SERVER_NAME" && -n "${ENDPOINT_URL:-}" ]]; then
  SERVER_NAME="${ENDPOINT_URL#http://}"
  SERVER_NAME="${SERVER_NAME#https://}"
  SERVER_NAME="${SERVER_NAME%%/*}"
fi
if [[ -z "$SERVER_NAME" && -n "${SITE_URL:-}" ]]; then
  SERVER_NAME="${SITE_URL#http://}"
  SERVER_NAME="${SERVER_NAME#https://}"
  SERVER_NAME="${SERVER_NAME%%/*}"
fi

if [[ -z "$SERVER_NAME" ]]; then
  echo "error: set SERVER_NAME, ENDPOINT_URL, or SITE_URL before running setup_proxy.sh" >&2
  exit 1
fi

ORIGIN_CERT_PATH="${ORIGIN_CERT_PATH:-}"
ORIGIN_KEY_PATH="${ORIGIN_KEY_PATH:-}"
tls_enabled=0
if [[ -n "$ORIGIN_CERT_PATH" || -n "$ORIGIN_KEY_PATH" ]]; then
  if [[ -z "$ORIGIN_CERT_PATH" || -z "$ORIGIN_KEY_PATH" ]]; then
    echo "error: set both ORIGIN_CERT_PATH and ORIGIN_KEY_PATH to enable HTTPS at the proxy" >&2
    exit 1
  fi
  if [[ ! -f "$ORIGIN_CERT_PATH" ]]; then
    echo "error: origin certificate not found: $ORIGIN_CERT_PATH" >&2
    exit 1
  fi
  if [[ ! -f "$ORIGIN_KEY_PATH" ]]; then
    echo "error: origin key not found: $ORIGIN_KEY_PATH" >&2
    exit 1
  fi
  tls_enabled=1
fi

echo "==> Writing nginx config to $NGINX_CONF_PATH"
nginx_tmp="$(mktemp)"
trap 'rm -f "$nginx_tmp"' EXIT
cat >"$nginx_tmp" <<EOF
# Managed by setup_proxy.sh for $APP_NAME.
map \$http_x_forwarded_proto \$micropub_x_forwarded_proto {
  default \$http_x_forwarded_proto;
  ""      \$scheme;
}

EOF

if [[ $tls_enabled -eq 1 ]]; then
  cat >>"$nginx_tmp" <<EOF
server {
  listen 80;
  listen [::]:80;
  server_name $SERVER_NAME;
  return 301 https://\$host\$request_uri;
}

server {
  listen 443 ssl http2;
  listen [::]:443 ssl http2;
  server_name $SERVER_NAME;

  ssl_certificate $ORIGIN_CERT_PATH;
  ssl_certificate_key $ORIGIN_KEY_PATH;
  ssl_protocols TLSv1.2 TLSv1.3;

  client_max_body_size $CLIENT_MAX_BODY_SIZE;
  proxy_read_timeout $PROXY_READ_TIMEOUT;
  proxy_send_timeout $PROXY_SEND_TIMEOUT;

  location / {
    proxy_pass http://$UPSTREAM_HOST:$UPSTREAM_PORT;
    proxy_http_version 1.1;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Host \$host;
    proxy_set_header X-Forwarded-Proto \$micropub_x_forwarded_proto;
    proxy_redirect off;
  }
}
EOF
else
  cat >>"$nginx_tmp" <<EOF
server {
  listen 80;
  listen [::]:80;
  server_name $SERVER_NAME;

  client_max_body_size $CLIENT_MAX_BODY_SIZE;
  proxy_read_timeout $PROXY_READ_TIMEOUT;
  proxy_send_timeout $PROXY_SEND_TIMEOUT;

  location / {
    proxy_pass http://$UPSTREAM_HOST:$UPSTREAM_PORT;
    proxy_http_version 1.1;
    proxy_set_header Host \$host;
    proxy_set_header X-Real-IP \$remote_addr;
    proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Host \$host;
    proxy_set_header X-Forwarded-Proto \$micropub_x_forwarded_proto;
    proxy_redirect off;
  }
}
EOF
fi

config_changed=0
if install_nginx_config "$nginx_tmp" "$NGINX_CONF_PATH"; then
  config_changed=1
fi

if [[ $config_changed -eq 1 ]]; then
  echo "==> Applying nginx changes"
else
  echo "==> Nginx config unchanged; ensuring service is running"
fi
reload_or_start_nginx

echo
echo "Proxy is configured for $SERVER_NAME -> http://$UPSTREAM_HOST:$UPSTREAM_PORT"
if [[ $tls_enabled -eq 1 ]]; then
  echo "HTTPS is enabled at the origin proxy. Use Cloudflare SSL mode: Full (strict)."
else
  echo "Origin TLS is not configured. If Cloudflare connects over HTTPS, add ORIGIN_CERT_PATH and ORIGIN_KEY_PATH and rerun this script."
fi
echo "Remember to allow ports 80 and 443 in the GCP firewall if Cloudflare will reach this host directly."