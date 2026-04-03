#!/usr/bin/env bash
set -euo pipefail

APP_NAME="micropub-jekyll"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ENV_FILE:-$SCRIPT_DIR/.env}"
ENV_FILE="$(cd "$(dirname "$ENV_FILE")" && pwd)/$(basename "$ENV_FILE")"
BUILD_OUTPUT="${BUILD_OUTPUT:-$SCRIPT_DIR/$APP_NAME}"
INSTALL_PATH="${INSTALL_PATH:-/usr/local/bin/$APP_NAME}"
SERVICE_NAME="${SERVICE_NAME:-$APP_NAME}"
SERVICE_USER="${SERVICE_USER:-$(id -un)}"
SERVICE_GROUP="${SERVICE_GROUP:-$(id -gn)}"
WORKING_DIR="${WORKING_DIR:-$SCRIPT_DIR}"
UNIT_PATH="${UNIT_PATH:-/etc/systemd/system/$SERVICE_NAME.service}"

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is not installed or not on PATH" >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "error: systemd is required for this deploy script" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "error: missing env file at $ENV_FILE" >&2
  echo "copy $SCRIPT_DIR/.env.example to $ENV_FILE and fill in the values first" >&2
  exit 1
fi

SUDO=""
if [[ ${EUID} -ne 0 ]]; then
  SUDO="sudo"
fi

echo "==> Building $APP_NAME"
pushd "$SCRIPT_DIR" >/dev/null
go build -o "$BUILD_OUTPUT" .
popd >/dev/null

echo "==> Installing binary to $INSTALL_PATH"
$SUDO install -d "$(dirname "$INSTALL_PATH")"
$SUDO install -m 0755 "$BUILD_OUTPUT" "$INSTALL_PATH"

echo "==> Writing systemd unit to $UNIT_PATH"
$SUDO tee "$UNIT_PATH" >/dev/null <<EOF
[Unit]
Description=Micropub Jekyll service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
WorkingDirectory=$WORKING_DIR
EnvironmentFile=$ENV_FILE
ExecStart=$INSTALL_PATH
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

echo "==> Reloading and restarting $SERVICE_NAME"
$SUDO systemctl daemon-reload
$SUDO systemctl enable "$SERVICE_NAME" >/dev/null
$SUDO systemctl restart "$SERVICE_NAME"

echo
echo "==> Service status"
$SUDO systemctl --no-pager --full status "$SERVICE_NAME"

echo
echo "==> Recent logs"
$SUDO journalctl -u "$SERVICE_NAME" -n 25 --no-pager
