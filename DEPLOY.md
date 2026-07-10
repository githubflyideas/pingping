# 部署指南

## 節點規劃

```
┌─ 運維節點 (1台, 2C4G 足夠) ──────────────────┐      ┌─ 目標主機 (N台) ─┐
│  VictoriaMetrics  :8428   歷史庫             │◄─push─│  sre-agent :9911 │
│  boss-board       :8080   管理層面板          │      │  (單二進制,只讀)  │
│  brain (Ruby)      -      按需/告警觸發診斷    │─查詢─►│                  │
│  reports/          -      診斷歸檔            │      └──────────────────┘
└──────────────────────────────────────────────┘
```

網路要求：運維節點 → 目標主機 :9911（取證）；目標主機 → 運維節點 :8428（上報）；
運維節點 → api.anthropic.com:443 與飛書 webhook（如走代理，Ruby 尊重 HTTPS_PROXY）。
老闆的瀏覽器 → 運維節點 :8080。

## 一、運維節點

```bash
# 1. 歷史庫
bash platform/install_vm.sh          # 裝 VictoriaMetrics + systemd，監聽 :8428

# 2. 編譯兩個二進制（任何有 Go 1.22+ 的機器編譯一次即可）
cd agent && CGO_ENABLED=0 go build -ldflags="-s -w" -o sre-agent . && cd ..
cd board && CGO_ENABLED=0 go build -ldflags="-s -w" -o boss-board . && cd ..

# 3. 面板
cp board/boss-board /usr/local/bin/
cp board/boss-board.service /etc/systemd/system/   # 改 -hosts 與 -vm 參數
systemctl daemon-reload && systemctl enable --now boss-board
# 瀏覽器打開 http://運維節點:8080

# 4. brain
apt install ruby   # ≥3.0，零 gem
cp config.example.yml config.yml     # 填飛書 webhook；API key 走環境變量
vim platform/topology.yml            # 維護主機清單與環境知識（重要）
export ANTHROPIC_API_KEY=sk-ant-xxx
```

## 二、每台目標主機

```bash
scp agent/sre-agent  host:/usr/local/bin/
scp agent/sre-agent.service host:/etc/systemd/system/
# 編輯 unit 中 ExecStart:
#   -vm http://運維節點IP:8428
#   -dns www.baidu.com,你的內部域名      # 可選，DNS 探測
ssh host 'systemctl daemon-reload && systemctl enable --now sre-agent'
ssh host 'curl -s localhost:9911/triage | head -c 200'   # 驗證
```

新主機記得同步加進 `platform/topology.yml` 的 agents 段和 boss-board 的 -hosts 參數。

## 三、日常使用

```bash
# 故障診斷（手動）
ruby brain/diagnose.rb "從國內訪問又卡了，卡了幾十秒，負載不高"
# → 終端輸出 + 飛書卡片 + 歸檔到 reports/（面板自動展示）

# 故障點前後對比（手動快查，不經 AI）
curl "http://host:9911/diff?t=$(date -d '10:44' +%s)&before=120&after=120" | jq .
```

## 四、驗證清單

- [ ] `curl 目標主機:9911/healthz` 返回 ok
- [ ] VM 界面 http://運維節點:8428/vmui 能查到 `cpu_util_pct`
- [ ] 面板 :8080 各主機顯示綠色而非「無數據」
- [ ] 跑一次 diagnose，飛書收到卡片、面板出現診斷記錄

## 常見問題

- 面板顯示「無數據」：agent 的 -vm 參數指錯 / 防火牆擋了 8428 / 主機名
  不一致（agent 上報用 hostname，boss-board -hosts 必須用相同名字）
- brain 連不上 Anthropic：確認出口策略放行 api.anthropic.com:443，
  或設置 HTTPS_PROXY
- 工程師視角的細粒度圖表：VictoriaMetrics 兼容 Prometheus 數據源，
  接一個 Grafana 即可，面板與其互不衝突
