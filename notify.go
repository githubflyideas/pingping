package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Notifier 的消息模型:四类消息、推拉对称:
//   告警 alert / 恢复 recovery  推 · 随时   有事才响(守夜人)
//   心跳 heartbeat              推 · 周频   证明自己活着(dead man's switch)
//   报告 daily / manual         推 · 日频可选 / Web 手动拉取
// 每类消息都可以按 webhook 的 kinds 白名单分群路由:告警进运维群,日报进领导群。
type Notifier struct {
	mu       sync.Mutex
	hooks    []*hook
	webURL   string
	instance string // 告警来源标识(多实例部署时靠它区分是谁在叫)
	store    *Store
	det      *Detector
	alertCnt int
}

// note 是一条与投递格式无关的消息:每个 hook 按自己的 format 渲染。
type note struct {
	kind, color, title, body, meta string
	event                          *AlertEvent // 告警/恢复带结构化事件,json 格式的收端可编程消费
}

type hook struct {
	name, url, secret, format string
	kinds             map[string]bool // nil = 全收
	fails             int
	skipUntil         time.Time
}

// AlertEvent 是一次告警/恢复的完整证据包。
type AlertEvent struct {
	Target, Host, Kind string
	P99Base, P99Cur    float64
	Bursts             int
	WorstLoss          float64
	Since              int64             // 告警开始时间
	Extra              map[string]string // 全局+目标合并后的自定义字段
}

func NewNotifier(cfg *Config) *Notifier {
	n := &Notifier{webURL: cfg.WebBaseURL, instance: cfg.Instance}
	for _, w := range cfg.Webhooks {
		h := &hook{name: w.Name, url: w.URL, secret: w.Secret, format: w.Format}
		if len(w.Kinds) > 0 {
			h.kinds = map[string]bool{}
			for _, k := range w.Kinds {
				h.kinds[k] = true
			}
		}
		n.hooks = append(n.hooks, h)
	}
	return n
}

func (n *Notifier) BindSnapshot(s *Store, d *Detector) { n.store, n.det = s, d }

func (n *Notifier) SendAlert(ev AlertEvent) {
	n.mu.Lock()
	n.alertCnt++
	n.mu.Unlock()
	var lines []string
	if ev.P99Cur > 0 && ev.P99Base > 0 {
		lines = append(lines, fmt.Sprintf("P99 基线 **%.1f ms** → 当前 **%.1f ms**(+%.0f%%)",
			ev.P99Base, ev.P99Cur, (ev.P99Cur/ev.P99Base-1)*100))
	}
	if ev.Bursts > 0 {
		lines = append(lines, fmt.Sprintf("近 30 分钟丢包突发 **%d** 次,最差单轮丢包 **%.0f%%**", ev.Bursts, ev.WorstLoss))
	}
	n.push(note{kind: "alert", color: "red", title: "🔴 链路异常 · " + ev.Target,
		body: "**" + ev.Kind + "**\n" + join(lines), meta: n.meta(ev, false), event: &ev})
}

func (n *Notifier) SendRecovery(ev AlertEvent) {
	body := fmt.Sprintf("**%s** 已恢复", ev.Kind)
	n.push(note{kind: "recovery", color: "green", title: "🟢 链路恢复 · " + ev.Target,
		body: body, meta: n.meta(ev, true), event: &ev})
}

// meta 组装卡片的元信息区:来源 · 目标 · 时间 · 持续 · 自定义字段。
// 告警卡片的价值在于可直接行动 —— 是谁、在哪、从何时起、找谁,一段说完。
func (n *Notifier) meta(ev AlertEvent, ended bool) string {
	lines := []string{fmt.Sprintf("来源:%s · 目标:%s(%s)", n.instance, ev.Target, ev.Host)}
	if ev.Since > 0 {
		t := time.Unix(ev.Since, 0)
		dur := fmtDur(time.Since(t))
		if ended {
			lines = append(lines, fmt.Sprintf("开始:%s · 共持续 %s", t.Format("01-02 15:04"), dur))
		} else {
			lines = append(lines, fmt.Sprintf("开始:%s · 已持续 %s", t.Format("01-02 15:04"), dur))
		}
	} else {
		lines = append(lines, "时间:"+time.Now().Format("2006-01-02 15:04"))
	}
	if len(ev.Extra) > 0 { // 排序保证卡片字段顺序稳定
		keys := make([]string, 0, len(ev.Extra))
		for k := range ev.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		kv := ""
		for i, k := range keys {
			if i > 0 {
				kv += " · "
			}
			kv += k + ":" + ev.Extra[k]
		}
		lines = append(lines, kv)
	}
	return join(lines)
}

func (n *Notifier) SendHeartbeat() {
	total, start := n.store.Counters()
	n.mu.Lock()
	alerts := n.alertCnt
	n.alertCnt = 0
	n.mu.Unlock()
	body := fmt.Sprintf("本周期探测 **%d** 轮 · **%d** 条链路 · 告警 **%d** 次 · 已运行 %s\n收到这条消息 = 过去一周的安静确实是平安。",
		total, len(n.store.Names()), alerts, fmtDur(time.Since(start)))
	meta := fmt.Sprintf("来源:%s · %s", n.instance, time.Now().Format("2006-01-02 15:04"))
	n.push(note{kind: "heartbeat", color: "grey", title: "⚪ pingping 值守正常", body: body, meta: meta})
}

// SendReport 日报(kind=daily)和手动拉取(kind=manual)共用:结论先行,一目标一行。
func (n *Notifier) SendReport(title, kind string) {
	statuses := n.det.Snapshot()
	var lines []string
	bad := 0
	for _, name := range n.store.Names() {
		st := calcStats(n.store.Recent(name, time.Now().Add(-24*time.Hour).Unix()))
		if statuses[name].Active {
			bad++
		}
		lines = append(lines, Conclusion(name, st, statuses[name]))
	}
	head := fmt.Sprintf("**%d** 条链路,**%d** 条正常", len(lines), len(lines)-bad)
	if bad > 0 {
		head = fmt.Sprintf("**%d** 条链路,**%d** 条异常 ⚠️", len(lines), bad)
	}
	meta := fmt.Sprintf("来源:%s · %s", n.instance, time.Now().Format("2006-01-02 15:04"))
	n.push(note{kind: kind, color: "blue", title: title, body: head + "\n" + join(lines), meta: meta})
}

// ---- 飞书 interactive card ----

func (nt note) feishuMsg(webURL string) map[string]any {
	return card(nt.color, nt.title, nt.body, nt.meta, webURL)
}

// rawMsg 是通用 JSON 事件:钉钉/Slack/自研平台自行转格式接入。
func (nt note) rawMsg(instance string) map[string]any {
	m := map[string]any{
		"source": "pingping", "instance": instance, "kind": nt.kind,
		"title": nt.title, "body": nt.body, "meta": nt.meta,
		"time": time.Now().Unix(),
	}
	if nt.event != nil {
		m["event"] = map[string]any{
			"target": nt.event.Target, "host": nt.event.Host, "type": nt.event.Kind,
			"p99_base_ms": nt.event.P99Base, "p99_cur_ms": nt.event.P99Cur,
			"bursts_30m": nt.event.Bursts, "worst_loss_pct": nt.event.WorstLoss,
			"since": nt.event.Since, "extra": nt.event.Extra,
		}
	}
	return m
}

func card(color, title, mdBody, mdMeta, webURL string) map[string]any {
	elements := []any{
		map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": mdBody}},
	}
	if mdMeta != "" {
		elements = append(elements,
			map[string]any{"tag": "hr"},
			map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": mdMeta}})
	}
	if webURL != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "查看实时烟雾图"},
				"type": "default", "url": webURL,
			}},
		})
	}
	return map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header":   map[string]any{"template": color, "title": map[string]any{"tag": "plain_text", "content": title}},
			"elements": elements,
		},
	}
}

// feishuSign 飞书"签名校验"算法:以 timestamp+"\n"+secret 为密钥,对空串做 HmacSHA256 再 base64。
func feishuSign(secret string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(strconv.FormatInt(ts, 10)+"\n"+secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// push 按 kinds 路由到各 webhook:3 次重试,连续失败 10 次退避 1 小时(死链保护)。
func (n *Notifier) push(nt note) {
	for _, h := range n.hooks {
		if h.kinds != nil && !h.kinds[nt.kind] {
			continue
		}
		go func(h *hook) {
			if time.Now().Before(h.skipUntil) {
				return
			}
			var payload map[string]any
			if h.format == "json" {
				payload = nt.rawMsg(n.instance)
			} else {
				payload = nt.feishuMsg(n.webURL)
				if h.secret != "" {
					ts := time.Now().Unix()
					payload["timestamp"] = strconv.FormatInt(ts, 10)
					payload["sign"] = feishuSign(h.secret, ts)
				}
			}
			body, _ := json.Marshal(payload)
			var lastErr error
			for i, backoff := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
				if i > 0 {
					time.Sleep(backoff)
				}
				if err := postJSON(h.url, body); err != nil {
					lastErr = err
					continue
				}
				h.fails = 0
				return
			}
			h.fails++
			log.Printf("webhook [%s] 推送失败(连续 %d 次): %v", h.name, h.fails, lastErr)
			if h.fails >= 10 {
				h.skipUntil = time.Now().Add(time.Hour)
				log.Printf("webhook [%s] 疑似死链,退避 1 小时", h.name)
			}
		}(h)
	}
}

func postJSON(url string, body []byte) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func join(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
