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
SETUP_PROXY="${SETUP_PROXY:-0}"
PROXY_SETUP_SCRIPT="${PROXY_SETUP_SCRIPT:-$SCRIPT_DIR/setup_proxy.sh}"

run_as_root() {
  if [[ ${EUID} -ne 0 ]]; then
    sudo "$@"
  else
    "$@"
  fi
}

install_if_changed() {
  local source_path="$1"
  local target_path="$2"
  local mode="$3"

  if [[ -f "$target_path" ]] && cmp -s "$source_path" "$target_path"; then
    return 1
  fi

  run_as_root install -D -m "$mode" "$source_path" "$target_path"
  return 0
}

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

if [[ "$SETUP_PROXY" == "1" && ! -x "$PROXY_SETUP_SCRIPT" ]]; then
  echo "error: proxy setup script is not executable: $PROXY_SETUP_SCRIPT" >&2
  exit 1
fi

echo "==> Building $APP_NAME"
pushd "$SCRIPT_DIR" >/dev/null
go build -o "$BUILD_OUTPUT" .
popd >/dev/null

echo "==> Installing binary to $INSTALL_PATH"
run_as_root install -d "$(dirname "$INSTALL_PATH")"
run_as_root install -m 0755 "$BUILD_OUTPUT" "$INSTALL_PATH"

echo "==> Writing systemd unit to $UNIT_PATH"
unit_tmp="$(mktemp)"
cat >"$unit_tmp" <<EOF
[Unit]
Description=Micropub Jekyll service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
WorkingDirectory=$WORKING_DIR
Environment=ENV_FILE=$ENV_FILE
EnvironmentFile=$ENV_FILE
ExecStart=$INSTALL_PATH
Restart=on-failure
RestartSec=5
KillSignal=SIGTERM
TimeoutStopSec=45
UMask=0027

[Install]
WantedBy=multi-user.target
EOF

unit_changed=0
if install_if_changed "$unit_tmp" "$UNIT_PATH" 0644; then
  unit_changed=1
fi
rm -f "$unit_tmp"

echo "==> Reloading and restarting $SERVICE_NAME"
if [[ $unit_changed -eq 1 ]]; then
  run_as_root systemctl daemon-reload
fi
run_as_root systemctl enable "$SERVICE_NAME" >/dev/null
run_as_root systemctl restart "$SERVICE_NAME"

if [[ "$SETUP_PROXY" == "1" ]]; then
  echo "==> Running proxy setup via $PROXY_SETUP_SCRIPT"
  ENV_FILE="$ENV_FILE" "$PROXY_SETUP_SCRIPT"
fi

echo
echo "==> Service status"
run_as_root systemctl --no-pager --full status "$SERVICE_NAME"

echo
echo "==> Recent logs"
run_as_root journalctl -u "$SERVICE_NAME" -n 25 --no-pager
