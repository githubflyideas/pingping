#!/usr/bin/env bash
# 安裝 VictoriaMetrics 單機版（單一二進制）作為歷史庫。在數據平台節點執行。
set -euo pipefail

VER="${VM_VERSION:-v1.106.1}"
ARCH=$(uname -m); [[ "$ARCH" == "x86_64" ]] && ARCH=amd64
URL="https://github.com/VictoriaMetrics/VictoriaMetrics/releases/download/${VER}/victoria-metrics-linux-${ARCH}-${VER}.tar.gz"

curl -fL "$URL" | tar xz -C /usr/local/bin victoria-metrics-prod
mkdir -p /var/lib/victoria-metrics

cat > /etc/systemd/system/victoria-metrics.service <<'EOF'
[Unit]
Description=VictoriaMetrics TSDB
After=network.target

[Service]
ExecStart=/usr/local/bin/victoria-metrics-prod \
  -storageDataPath=/var/lib/victoria-metrics \
  -retentionPeriod=90d \
  -httpListenAddr=:8428
Restart=always
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now victoria-metrics
echo "VictoriaMetrics up: http://$(hostname -I | awk '{print $1}'):8428"
