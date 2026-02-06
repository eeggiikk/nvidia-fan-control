#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ ! -f "$SCRIPT_DIR/nvidia_fan_control" ]; then
    echo "==> Building nvidia_fan_control binary..."
    cd "$SCRIPT_DIR"
    go build -o nvidia_fan_control .
fi

if [ "$(id -u)" -ne 0 ]; then
    echo "==> Re-running as root..."
    exec sudo "$0" "$@"
fi

echo "==> Installing binary to /usr/local/bin/nvidia_fan_control..."
cp "$SCRIPT_DIR/nvidia_fan_control" /usr/local/bin/nvidia_fan_control
chmod 755 /usr/local/bin/nvidia_fan_control

echo "==> Creating config directory /etc/nvidia-fan-control/..."
mkdir -p /etc/nvidia-fan-control

echo "==> Installing config file..."
if [ ! -f /etc/nvidia-fan-control/config.json ]; then
    cp "$SCRIPT_DIR/config.json" /etc/nvidia-fan-control/config.json
    chmod 644 /etc/nvidia-fan-control/config.json
    echo "    config.json installed."
else
    echo "    config.json already exists, skipping to avoid overwriting."
fi

echo "==> Installing systemd service..."
cp "$SCRIPT_DIR/nvidia-fan-control.service" /etc/systemd/system/nvidia-fan-control.service
chmod 644 /etc/systemd/system/nvidia-fan-control.service

echo "==> Reloading systemd daemon..."
systemctl daemon-reload

echo "==> Enabling and starting nvidia-fan-control service..."
systemctl enable nvidia-fan-control
systemctl restart nvidia-fan-control

echo ""
echo "==> Service status:"
systemctl status nvidia-fan-control --no-pager

echo ""
echo "Done. View logs with: journalctl -u nvidia-fan-control -f"
