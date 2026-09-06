systemd

# /etc/systemd/system/pingping.service
[Unit]
Description=pingping link quality oscilloscope
After=network-online.target

[Service]
ExecStart=/opt/pingping/pingping -c /opt/pingping/pingping.jsonc
WorkingDirectory=/opt/pingping
Restart=always
RestartSec=5
#2way #not root user
# 1) sysctl net.ipv4.ping_group_range="0 2147483647"
# 2) AmbientCapabilities=CAP_NET_RAW
User=pingping
AmbientCapabilities=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
useradd -r -s /sbin/nologin pingping
mkdir -p /opt/pingping && cp pingping /opt/pingping/ && chown -R pingping: /opt/pingping
systemctl enable --now pingping

------------------------ssl behind caddy------

Caddy( auto HTTPS + basic auth):

ping.example.com {
    basic_auth {
        yong $2a$14$...   # caddy hash-password 
    }
    reverse_proxy 127.0.0.1:8517
}
Nginx:

server {
    listen 443 ssl;
    server_name ping.example.com;
    auth_basic "pingping";
    auth_basic_user_file /etc/nginx/htpasswd;   # htpasswd -c generation
    
    location / { proxy_pass http://127.0.0.1:8517; }
}
