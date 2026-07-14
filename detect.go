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
	mu     sync.Mutex
	cfg    AlertCfg
	store  *Store
	notify *Notifier
	states map[string]*tstate
}

type tstate struct {
	degradeStreak int   // 连续劣化确认次数
	alertActive   bool
	alertKind     string
	lastNotify    int64
	clearSince    int64 // 条件消失的起点(恢复确认用)
	// 告警证据(推送和 UI 共用)
	evP99Base, evP99Cur float64
	evBursts            int
	evWorstLoss         float64
}

func NewDetector(cfg *Config, store *Store, n *Notifier) *Detector {
	d := &Detector{cfg: cfg.Alerts, store: store, notify: n, states: map[string]*tstate{}}
	for _, t := range cfg.Targets {
		d.states[t.Name] = &tstate{}
	}
	return d
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
		degrade = p99c > p99b*d.cfg.DegradeRatio && p99c-p99b > d.cfg.DegradeMinMs
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
	degradeAlert := st.degradeStreak >= 3
	burstAlert := bursts30 >= d.cfg.BurstAlertN
	condition := degradeAlert || burstAlert

	var fire, recovered bool
	var kind string
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
			fire = true
			st.alertActive = true
			st.alertKind = kind
			st.lastNotify = now.Unix()
			st.evP99Base, st.evP99Cur, st.evBursts, st.evWorstLoss = p99b, p99c, bursts30, worstLoss
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
	ev := *st
	d.mu.Unlock()

	if fire {
		log.Printf("[%s] 告警: %s", name, kind)
		d.notify.SendAlert(name, kind, ev.evP99Base, ev.evP99Cur, ev.evBursts, ev.evWorstLoss)
	}
	if recovered {
		log.Printf("[%s] 恢复: %s", name, kind)
		d.notify.SendRecovery(name, kind)
	}
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
