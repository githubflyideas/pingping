package main

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

// Detector 是 ai-sreagent brain 的确定性转世:
// robust z-score(中位数 + MAD,抗离群点,对长尾的 RTT/丢包序列是正确统计工具)
// 替代了 LLM 循环 —— 结论先行的哲学保留,推理引擎换成死规则。
//
// 两条规则:
//   burst   丢包突发 —— 单轮丢包数相对历史分布的 robust z 超阈,30 分钟内 N 次升级为告警
//   degrade P99 劣化 —— 近 15 分钟 P99 相对 1 小时前基线超倍数,连续 3 次确认
type Detector struct {
	mu      sync.Mutex
	cfg     AlertCfg
	store   *Store
	notify  *Notifier
	states  map[string]*tstate
	targets map[string]TargetCfg // 告警事件需要 host/extra/敏感度
	extra   map[string]string    // 全局自定义字段
}

// sensParams 敏感度三档:对基准阈值的整体缩放。
// strict 给核心链路(早叫),relaxed 给天生就抖的公网链路(少叫)。
type sensParams struct {
	ratio  float64 // P99 劣化倍数
	burstN int     // 30 分钟内突发次数升级线
	streak int     // 劣化连续确认次数
}

func sensOf(t TargetCfg, base AlertCfg) sensParams {
	switch t.Sensitivity {
	case "strict":
		return sensParams{1.3, 2, 2}
	case "relaxed":
		return sensParams{2.0, 5, 5}
	default:
		return sensParams{base.DegradeRatio, base.BurstAlertN, 3}
	}
}

type tstate struct {
	degradeStreak int   // 连续劣化确认次数
	alertActive   bool
	alertKind     string
	alertSince    int64 // 本次告警的开始时间
	lastNotify    int64
	clearSince    int64 // 条件消失的起点(恢复确认用)
}

func NewDetector(cfg *Config, store *Store, n *Notifier) *Detector {
	d := &Detector{cfg: cfg.Alerts, store: store, notify: n,
		states: map[string]*tstate{}, targets: map[string]TargetCfg{}, extra: cfg.Extra}
	for _, t := range cfg.Targets {
		d.states[t.Name] = &tstate{}
		d.targets[t.Name] = t
	}
	return d
}

// mergedExtra 全局字段 + 目标字段,目标覆盖同名键。
func (d *Detector) mergedExtra(t TargetCfg) map[string]string {
	out := map[string]string{}
	for k, v := range d.extra {
		out[k] = v
	}
	for k, v := range t.Extra {
		out[k] = v
	}
	return out
}

// CheckBurst 在落盘前判定当前轮是否丢包突发(基线不含当前轮)。
func (d *Detector) CheckBurst(name string, r Round) bool {
	loss := r.S - r.R
	if loss < 2 { // 丢 1 个包在互联网上是噪音,不值得叫突发
		return false
	}
	hist := d.store.Recent(name, time.Now().Add(-4*time.Hour).Unix())
	if len(hist) < 30 {
		// 冷启动:没有基线就用绝对阈值兜底
		return float64(loss)/float64(r.S) >= 0.25
	}
	series := make([]float64, len(hist))
	for i, h := range hist {
		series[i] = float64(h.S - h.R)
	}
	z := robustZ(float64(loss), series)
	if z < 0 { // MAD=0(基线全零丢包,最常见的健康态)
		return float64(loss)/float64(r.S) >= 0.10
	}
	return z >= d.cfg.BurstZ
}

// robustZ = 0.6745 * (x - median) / MAD;MAD 为 0 时返回 -1 让调用方走绝对阈值。
func robustZ(x float64, series []float64) float64 {
	med := median(series)
	dev := make([]float64, len(series))
	for i, v := range series {
		if v > med {
			dev[i] = v - med
		} else {
			dev[i] = med - v
		}
	}
	mad := median(dev)
	if mad == 0 {
		return -1
	}
	return 0.6745 * (x - med) / mad
}

func median(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	c := append([]float64(nil), s...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// AfterAppend 是每轮落盘后的完整评估:劣化检查 + 突发计数 + 状态机。
func (d *Detector) AfterAppend(name string) {
	tc := d.targets[name]
	if tc.Alerts != nil && !*tc.Alerts {
		return // 纯观测目标:烟雾图和突发标记照常,但永远不叫
	}
	sp := sensOf(tc, d.cfg)
	now := time.Now()
	ring24 := d.store.Recent(name, now.Add(-24*time.Hour).Unix())

	// --- P99 劣化 ---
	var curPool, basePool []float64
	cut15 := now.Add(-15 * time.Minute).Unix()
	cut1h := now.Add(-time.Hour).Unix()
	baseRounds := 0
	for _, r := range ring24 {
		if r.T >= cut15 {
			curPool = append(curPool, r.MS...)
		}
		if r.T < cut1h {
			basePool = append(basePool, r.MS...)
			baseRounds++
		}
	}
	degrade := false
	var p99c, p99b float64
	if baseRounds >= 45 && len(curPool) >= 20 { // 基线至少 45 分钟,当前窗至少一轮的样本量
		sort.Float64s(curPool)
		sort.Float64s(basePool)
		p99c, p99b = pct(curPool, 99), pct(basePool, 99)
		degrade = p99c > p99b*sp.ratio && p99c-p99b > d.cfg.DegradeMinMs
	}

	// --- 突发计数(30 分钟窗) ---
	bursts30, worstLoss := 0, 0.0
	cut30 := now.Add(-30 * time.Minute).Unix()
	for _, r := range ring24 {
		if r.T >= cut30 && r.B {
			bursts30++
			if lp := 100 * float64(r.S-r.R) / float64(r.S); lp > worstLoss {
				worstLoss = lp
			}
		}
	}

	d.mu.Lock()
	st := d.states[name]
	if degrade {
		st.degradeStreak++
	} else {
		st.degradeStreak = 0
	}
	degradeAlert := st.degradeStreak >= sp.streak
	burstAlert := bursts30 >= sp.burstN
	condition := degradeAlert || burstAlert

	var fire, recovered bool
	var kind string
	var since int64
	if condition {
		st.clearSince = 0
		switch {
		case degradeAlert && burstAlert:
			kind = "劣化+丢包突发"
		case degradeAlert:
			kind = "P99 劣化"
		default:
			kind = "丢包突发"
		}
		cooldown := int64(d.cfg.CooldownMin) * 60
		if !st.alertActive || now.Unix()-st.lastNotify >= cooldown {
			if !st.alertActive {
				st.alertSince = now.Unix()
			}
			fire = true
			st.alertActive = true
			st.alertKind = kind
			st.lastNotify = now.Unix()
		}
	} else if st.alertActive {
		if st.clearSince == 0 {
			st.clearSince = now.Unix()
		}
		if now.Unix()-st.clearSince >= int64(d.cfg.RecoverMin)*60 {
			st.alertActive = false
			recovered = true
			kind = st.alertKind
		}
	}
	since = st.alertSince
	d.mu.Unlock()

	if fire {
		log.Printf("[%s] 告警: %s", name, kind)
		d.notify.SendAlert(AlertEvent{
			Target: name, Host: targetAddr(tc), Kind: kind,
			P99Base: p99b, P99Cur: p99c, Bursts: bursts30, WorstLoss: worstLoss,
			Since: since, Extra: d.mergedExtra(tc),
		})
	}
	if recovered {
		log.Printf("[%s] 恢复: %s", name, kind)
		d.notify.SendRecovery(AlertEvent{
			Target: name, Host: targetAddr(tc), Kind: kind,
			Since: since, Extra: d.mergedExtra(tc),
		})
	}
}

func targetAddr(t TargetCfg) string {
	if t.Type == "tcp" {
		return fmt.Sprintf("%s:%d", t.Host, t.Port)
	}
	return t.Host
}

// Status 给 Web 和报告用的目标状态快照。
type Status struct {
	Active bool   `json:"alert"`
	Kind   string `json:"kind,omitempty"`
}

func (d *Detector) Snapshot() map[string]Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string]Status{}
	for name, st := range d.states {
		s := Status{Active: st.alertActive}
		if st.alertActive {
			s.Kind = st.alertKind
		}
		out[name] = s
	}
	return out
}

// Conclusion 一句话结论 —— deltascope 哲学:结论在前,数据在后。
func Conclusion(name string, st Stats, status Status) string {
	if status.Active {
		return fmt.Sprintf("🔴 %s:%s · P50 %.1f / P99 %.1f ms · 丢包 %.1f%% · 24h 突发 %d 次",
			name, status.Kind, st.P50, st.P99, st.LossPct, st.Bursts)
	}
	if st.Rounds == 0 {
		return fmt.Sprintf("⚪ %s:暂无数据", name)
	}
	return fmt.Sprintf("🟢 %s:P50 %.1f / P99 %.1f ms · 丢包 %.1f%% · 24h 突发 %d 次",
		name, st.P50, st.P99, st.LossPct, st.Bursts)
}
