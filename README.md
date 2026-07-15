# pingping

> **pingping 探测链路质量画图小工具，像是一个单二进制的smokeping**。
> 当用户说"卡",但监控又是正常时 —— 有可能是故障藏在 RTT 分布和丢包突发里,用它说不定可以帮上你。
>
> 下载地址
> https://github.com/githubflyideas/pingping/releases

pingping 是一个网络链路质量示波器:每轮发 20 个探测包,把**全部样本**画成烟雾图(Smokeping 的可视化语义),用 robust z-score 自动判定丢包突发和 P99 劣化,还可以推到飞书。

A single-binary network link quality oscilloscope — modern Smokeping semantics, zero dependencies, air-gapped friendly, conclusions-first Feishu alerting.

![广州电信 · 6h 窗口:一场 40 分钟的拥塞事件,烟雾散开、突发标为 ◆](docs/hero.png)

| 24h 视图:昨晚的劣化窗口 + 晚高峰抖动 | 健康链路就是一条细亮线 |
|---|---|
| ![24h](docs/day.png) | ![clean](docs/clean.png) |

*(截图为内置回放机制生成的演示数据)*

## 特性

- **单二进制,零依赖** — 纯 Go 标准库,`go.mod` 里没有一行 require。Web UI(ECharts)内嵌,存储是 JSONL 文本文件。没有 Docker、没有数据库、没有 Prometheus,scp 一个文件就能在隔离网跑
- **存分布,不存均值** — 每轮 20 包的原始 RTT 全部落盘。P50/P90/P99、烟雾图、突发标注都从真实分布算出,均值抹平的故障在这里现形
- **免 root** — unprivileged ICMP(`SOCK_DGRAM`)优先,自动回退 raw socket(cap_net_raw)
- **结论先行** — 告警不甩图表,直接说话:"P99 基线 45.2ms → 当前 131.0ms(+190%),近 30 分钟丢包突发 3 次"
- **四类消息,推拉对称** — 告警(有事才响)· 恢复 · 周心跳(证明自己活着,给沉默赋予语义)· 报告(日报可选 + Web 一键手动拉取)

## 快速开始

从 [Releases](https://github.com/githubflyideas/pingping/releases) 下载对应平台的包(Linux 静态编译,所有发行版通用;另有 macOS 双架构):

```bash
tar xzf pingping-v*-linux-amd64.tar.gz && cd pingping-*/
./pingping
```

首次运行自动生成演示配置(探测 www.baidu.com),按提示打开 http://localhost:8517,几分钟后第一缕烟雾成形。

加监控目标就一行,重启生效:

```bash
echo "59.43.247.1 香港CN2 pace=fast sensitivity=strict 负责人=张三" >> targets/ping.list
echo "10.0.0.5:443 API网关" >> targets/tcp.list
```

行格式:`host[:port] [名称] [pace=fast|slow] [sensitivity=strict|relaxed] [interval=秒] [alerts=false]`,其余 `k=v` 自动成为自定义字段进告警卡片。飞书 webhook 在 `pingping.jsonc` 里配置。Web 界面可按 PING/TCP 筛选目标。

也可以源码构建:`go build -o pingping .`(无任何第三方依赖)。

无 root 环境需要放开 unprivileged ICMP(多数发行版默认已放开):

```bash
sysctl -w net.ipv4.ping_group_range="0 2147483647"
# 或者给二进制授权 raw socket:
setcap cap_net_raw+ep ./pingping
```

## 读图

- **亮线** = 每轮中位数;**烟雾** = 该轮全部样本 —— 烟越宽,抖动越大
- **红柱** = 该轮丢包率;**◆** = 被 z-score 判定的丢包突发
- 健康链路是一条细亮线;劣化链路是一团散开的烟


## 按目标分档

不同链路值得不同的对待,每个目标可选三个旋钮:

| 旋钮 | 档位 | 含义 |
|---|---|---|
| `pace` | `fast` / `normal` / `slow` | 15s(每轮 30 包)/ 60s / 300s;显式 `interval_sec` 优先级最高 |
| `sensitivity` | `strict` / `normal` / `relaxed` | 早叫(核心链路)/ 默认 / 少叫(天生就抖的公网链路) |
| `alerts` | `false` | 纯观测:烟雾图和突发标记照常,永远不推告警 |


## 数据即文本

```
data/
├── 广州电信/2026-07-13.jsonl   # 原始层:每轮一行,14 天后整文件删除
│   {"t":1752390000,"s":20,"r":19,"ms":[12.1,12.3,...],"b":false}
└── summary/广州电信.jsonl       # 汇总层:每天一行,永久保留
    {"d":"2026-07-12","p50":12.3,"p99":45.1,"loss":0.3,"rounds":1440,"bursts":2}
```

可以 `grep`,可以 `jq`,可以 `tar`,永远不会"数据库坏了打不开"。

## 赞助

pingping 的目标是"不赔钱地存在"。如果它帮你抓到过一次"监控全绿但就是卡"的元凶:

- ⭐ Star 是最好的支持
- [GitHub Sponsors](https://github.com/sponsors/githubflyideas) · 爱发电(筹备中)
- **赞助位虚位以待** —— README 此处与推送卡片底部各有一个固定展示位(内容将明确标注"推广"),适合 VPS / IDC / 网络服务商,联系方式见 GitHub 主页

## License

MIT
