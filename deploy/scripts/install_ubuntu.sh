#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Run as root: sudo ./install_ubuntu.sh /path/to/echorift-linux-amd64.tar.gz" >&2
  exit 1
fi

ARCHIVE="${1:-}"
if [[ -z "${ARCHIVE}" || ! -f "${ARCHIVE}" ]]; then
  echo "Usage: sudo $0 /path/to/echorift-linux-amd64.tar.gz" >&2
  exit 1
fi

install -d -m 0755 /opt/echorift
install -d -m 0750 -o root -g root /etc/echorift
install -d -m 0750 -o root -g root /var/lib/echorift/uploads
install -d -m 0750 -o root -g root /var/log/echorift

if ! id echorift >/dev/null 2>&1; then
  useradd --system --home /var/lib/echorift --shell /usr/sbin/nologin echorift
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

tar -xzf "${ARCHIVE}" -C "${TMP_DIR}"
cp "${TMP_DIR}/linux-amd64/echorift" /opt/echorift/echorift
cp "${TMP_DIR}/linux-amd64/echorift-migrate" /opt/echorift/echorift-migrate
chmod 0755 /opt/echorift/echorift /opt/echorift/echorift-migrate
cp -R "${TMP_DIR}/linux-amd64/migrations" /opt/echorift/migrations
cp "${TMP_DIR}/linux-amd64/echorift.service" /etc/systemd/system/echorift.service

if [[ ! -f /etc/echorift/echorift.env ]]; then
  cp "${TMP_DIR}/linux-amd64/echorift.env.example" /etc/echorift/echorift.env
  chmod 0640 /etc/echorift/echorift.env
  echo "Created /etc/echorift/echorift.env. Edit it before starting the service."
fi

chown -R echorift:echorift /opt/echorift /var/lib/echorift /var/log/echorift

systemctl daemon-reload
systemctl enable echorift

echo "Installed EchoRift. Edit /etc/echorift/echorift.env, run migrations, then start:"
echo "  sudo -u echorift /opt/echorift/echorift-migrate"
echo "  sudo systemctl start echorift"
