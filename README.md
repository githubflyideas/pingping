# pingping

> A modern, lightweight, and cross-platform SmokePing alternative.

pingping provides long-term network performance monitoring, tracking latency, jitter, and packet loss with clear, user-friendly charts. 

It runs seamlessly across **Windows**, **Linux**, and **macOS**.

![pingping latency graph with an example corporate logo (Acme Networks)](docs/images/pingping-graph.png)
![pingping settings: accounts, console port and custom logo](docs/images/pingping-settings.png)
![pingping target list](docs/images/pingping-targets.png)

## Download & Quick Start

### Linux (amd64)

```bash
cd /home/
wget https://github.com/githubflyideas/pingping/releases/download/v0.5.9/pingping-linux-amd64
mv pingping-linux-amd64 pingping && chmod +x pingping
./pingping --port 8518
# Open http://localhost:8518
```

### Windows

[pingping-0.5.9-setup.exe](https://github.com/githubflyideas/pingping/releases/download/v0.5.9/pingping-0.5.9-setup.exe)

### macOS (Apple silicon)

[pingping-darwin-arm64](https://github.com/githubflyideas/pingping/releases/download/v0.5.9/pingping-darwin-arm64)

### ✨ Key Features

* 📊 **Latency & Packet Loss Visualization**: Displays historical ping trends and packet loss density using clear, SmokePing-style chart visualizations.
* 🌐 **Cross-Platform Support**: Native binary builds for Windows, Linux, and macOS without complex runtime dependencies.
* 🛠️ **Simple Configuration & Quick Setup**: Straightforward configuration allowing you to start monitoring target hosts within minutes.
* 📈 **Multi-Target Tracking**: Continuously monitors multiple IP addresses, domains, or gateways in parallel.
* ⚡ **Resource Efficient**: Built for continuous background operation with minimal CPU and memory usage.

---

### 💡 Common Use Cases

* **Server & Node Monitoring**: Track network quality across distributed servers and cloud instances.
* **Troubleshooting**: Diagnose home or office network instability, VPN drops, or ISP packet loss over custom time ranges.
* **Route & Line Comparison**: Compare route stability across different ISPs or nodes over 24-hour periods.

---

## License Terms

pingping is free for any individual or organization, including commercial use, provided that the original logo, project name, and author attribution are retained.

The only paid option: replacing the original logo with a custom or corporate logo requires a one-time payment of $12 for a perpetual license.

Selling, reselling, renting, or offering pingping as a paid service to others is prohibited.

To purchase a license, contact the author: route66traffic66@gmail.com
