# 部署

## systemd

```ini
# /etc/systemd/system/pingping.service
[Unit]
Description=pingping link quality oscilloscope
After=network-online.target

[Service]
ExecStart=/opt/pingping/pingping -c /opt/pingping/pingping.jsonc
WorkingDirectory=/opt/pingping
Restart=always
RestartSec=5
# 非 root 运行时二选一:
# 1) sysctl net.ipv4.ping_group_range="0 2147483647"
# 2) AmbientCapabilities=CAP_NET_RAW
User=pingping
AmbientCapabilities=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
```

```bash
useradd -r -s /sbin/nologin pingping
mkdir -p /opt/pingping && cp pingping pingping.jsonc /opt/pingping/ && chown -R pingping: /opt/pingping
systemctl enable --now pingping
```

## 隔离网(air-gapped)

在有网机器上 `CGO_ENABLED=0 go build`,把二进制和配置 scp 进去即可。
无外部依赖,数据目录整个 tar 走就是完整备份。

## 让 pingping 自己也被看住(可选)

监控工具也要被监控,但不必自己监控自己。向任意 dead man's switch 服务发心跳:

```bash
# crontab,配合 healthchecks.io 或内网等价物
*/5 * * * * curl -fsS http://127.0.0.1:8517/api/targets >/dev/null && curl -fsS https://hc-ping.com/<uuid> >/dev/null
```

## 反向代理认证(--localhost)

不想让 pingping 自己管密码?加 `--localhost` 参数只绑定 127.0.0.1,认证和 TLS 交给前端反代:

```bash
./pingping --localhost -c pingping.jsonc
```

Caddy(自动 HTTPS + basic auth):

```
ping.example.com {
    basic_auth {
        yong $2a$14$...   # caddy hash-password 生成
    }
    reverse_proxy 127.0.0.1:8517
}
```

Nginx:

```nginx
server {
    listen 443 ssl;
    server_name ping.example.com;
    auth_basic "pingping";
    auth_basic_user_file /etc/nginx/htpasswd;   # htpasswd -c 生成
    location / { proxy_pass http://127.0.0.1:8517; }
}
```

此时可不设 `web_password`(反代已挡在前面),或两层都开当双保险。
