package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Notifier 的消息模型是整场产品讨论的结晶,四类消息、推拉对称:
//   告警   推 · 随时   有事才响(守夜人)
//   恢复   推 · 随时   有始有终
//   心跳   推 · 周频   证明自己活着,给沉默赋予语义(dead man's switch)
//   报告   推 · 日频可选 / 手动拉取(Web 按钮 → 立即推送)
type Notifier struct {
	mu       sync.Mutex
	hooks    []*hook
	webURL   string
	store    *Store
	det      *Detector
	alertCnt int // 本周期告警计数(心跳汇报用)
}

type hook struct {
	name, url string
	fails     int
	skipUntil time.Time
}

func NewNotifier(cfg *Config) *Notifier {
	n := &Notifier{webURL: cfg.WebBaseURL}
	for _, w := range cfg.Webhooks {
		n.hooks = append(n.hooks, &hook{name: w.Name, url: w.URL})
	}
	return n
}

// BindSnapshot 后注入,避免构造顺序循环。
func (n *Notifier) BindSnapshot(s *Store, d *Detector) { n.store, n.det = s, d }

func (n *Notifier) SendAlert(name, kind string, p99b, p99c float64, bursts int, worst float64) {
	n.mu.Lock()
	n.alertCnt++
	n.mu.Unlock()
	var lines []string
	if p99c > 0 && p99b > 0 {
		lines = append(lines, fmt.Sprintf("P99 基线 **%.1f ms** → 当前 **%.1f ms**(+%.0f%%)",
			p99b, p99c, (p99c/p99b-1)*100))
	}
	if bursts > 0 {
		lines = append(lines, fmt.Sprintf("近 30 分钟丢包突发 **%d** 次,最差单轮丢包 **%.0f%%**", bursts, worst))
	}
	lines = append(lines, time.Now().Format("2006-01-02 15:04"))
	n.push(card("red", "🔴 链路异常 · "+name, "**"+kind+"**\n"+join(lines), n.webURL))
}

func (n *Notifier) SendRecovery(name, kind string) {
	body := fmt.Sprintf("**%s** 已恢复,持续正常 %d 分钟\n%s",
		kind, 15, time.Now().Format("2006-01-02 15:04"))
	n.push(card("green", "🟢 链路恢复 · "+name, body, n.webURL))
}

func (n *Notifier) SendHeartbeat() {
	total, start := n.store.Counters()
	n.mu.Lock()
	alerts := n.alertCnt
	n.alertCnt = 0
	n.mu.Unlock()
	body := fmt.Sprintf("本周期探测 **%d** 轮 · **%d** 条链路 · 告警 **%d** 次 · 已运行 %s\n收到这条消息 = 过去一周的安静确实是平安。",
		total, len(n.store.Names()), alerts, fmtDur(time.Since(start)))
	n.push(card("grey", "⚪ pingping 值守正常", body, n.webURL))
}

// SendReport 日报和手动拉取共用:结论先行,一目标一行。
func (n *Notifier) SendReport(title string) {
	statuses := n.det.Snapshot()
	since := time.Now().Add(-time.Hour).Unix()
	var lines []string
	bad := 0
	for _, name := range n.store.Names() {
		st := calcStats(n.store.Recent(name, time.Now().Add(-24*time.Hour).Unix()))
		st1h := calcStats(n.store.Recent(name, since))
		_ = st1h
		if statuses[name].Active {
			bad++
		}
		lines = append(lines, Conclusion(name, st, statuses[name]))
	}
	head := fmt.Sprintf("**%d** 条链路,**%d** 条正常", len(lines), len(lines)-bad)
	if bad > 0 {
		head = fmt.Sprintf("**%d** 条链路,**%d** 条异常 ⚠️", len(lines), bad)
	}
	n.push(card("blue", title, head+"\n"+join(lines)+"\n"+time.Now().Format("2006-01-02 15:04"), n.webURL))
}

// ---- 飞书 interactive card ----

func card(color, title, mdBody, webURL string) []byte {
	elements := []any{
		map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": mdBody}},
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
	msg := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header":   map[string]any{"template": color, "title": map[string]any{"tag": "plain_text", "content": title}},
			"elements": elements,
		},
	}
	b, _ := json.Marshal(msg)
	return b
}

// push 逐个 webhook 发送:3 次重试,连续失败 10 次退避 1 小时(死链保护)。
func (n *Notifier) push(payload []byte) {
	for _, h := range n.hooks {
		go func(h *hook) {
			if time.Now().Before(h.skipUntil) {
				return
			}
			var lastErr error
			for i, backoff := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
				if i > 0 {
					time.Sleep(backoff)
				}
				if err := postJSON(h.url, payload); err != nil {
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
