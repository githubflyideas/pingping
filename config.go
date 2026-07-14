package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Config struct {
	Listen        string       `json:"listen"`
	DataDir       string       `json:"data_dir"`
	WebBaseURL    string       `json:"web_base_url"` // 飞书卡片按钮链接,留空则不带按钮
	Targets       []TargetCfg  `json:"targets"`
	Probe         ProbeCfg     `json:"probe"`
	Webhooks      []WebhookCfg `json:"webhooks"`
	Alerts        AlertCfg     `json:"alerts"`
	Heartbeat     ScheduleCfg  `json:"heartbeat"`
	DailyReport   ScheduleCfg  `json:"daily_report"`
	RetentionDays int          `json:"retention_days"`
}

type TargetCfg struct {
	Name string `json:"name"`
	Type string `json:"type"` // "icmp" | "tcp"
	Host string `json:"host"`
	Port int    `json:"port"` // 仅 tcp
	dir  string // 数据目录名(sanitized)
}

type ProbeCfg struct {
	IntervalSec int `json:"interval_sec"` // 每轮间隔
	Packets     int `json:"packets"`      // 每轮包数(烟雾图的"烟"就来自这里)
	GapMs       int `json:"gap_ms"`       // 包间隔
	TimeoutMs   int `json:"timeout_ms"`   // 单包超时
}

type WebhookCfg struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type AlertCfg struct {
	CooldownMin  int     `json:"cooldown_min"`  // 同一告警再次推送的冷却
	RecoverMin   int     `json:"recover_min"`   // 恢复确认时长
	DegradeRatio float64 `json:"degrade_ratio"` // P99 相对基线的劣化倍数
	DegradeMinMs float64 `json:"degrade_min_ms"`// P99 绝对劣化下限,防低延迟链路误报
	BurstZ       float64 `json:"burst_z"`       // 丢包突发的 robust z-score 阈值
	BurstAlertN  int     `json:"burst_alert_n"` // 30 分钟内突发 N 次才升级为告警
}

type ScheduleCfg struct {
	Enabled bool `json:"enabled"`
	Weekday int  `json:"weekday"` // 0=周日 1=周一 …(仅心跳用)
	Hour    int  `json:"hour"`
}

var dirSan = regexp.MustCompile(`[^a-zA-Z0-9._\p{Han}-]`)

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{ // 默认值即最佳实践:不改任何配置也应该能得到正确的烟雾图
		Listen:        "0.0.0.0:8517",
		DataDir:       "./data",
		Probe:         ProbeCfg{IntervalSec: 60, Packets: 20, GapMs: 50, TimeoutMs: 1000},
		Alerts:        AlertCfg{CooldownMin: 30, RecoverMin: 15, DegradeRatio: 1.5, DegradeMinMs: 10, BurstZ: 3.5, BurstAlertN: 3},
		Heartbeat:     ScheduleCfg{Enabled: true, Weekday: 1, Hour: 10},
		DailyReport:   ScheduleCfg{Enabled: false, Hour: 10},
		RetentionDays: 14,
	}
	if err := json.Unmarshal(stripJSONC(raw), cfg); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("targets 为空:至少配置一个探测目标")
	}
	seen := map[string]bool{}
	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		if t.Name == "" {
			return nil, fmt.Errorf("targets[%d] 缺少 name", i)
		}
		switch t.Type {
		case "", "icmp":
			t.Type = "icmp"
		case "tcp":
			if t.Port <= 0 {
				return nil, fmt.Errorf("目标 %q 是 tcp 但未配置 port", t.Name)
			}
		default:
			return nil, fmt.Errorf("目标 %q 类型 %q 不支持(icmp|tcp)", t.Name, t.Type)
		}
		t.dir = dirSan.ReplaceAllString(t.Name, "_")
		if seen[t.dir] {
			return nil, fmt.Errorf("目标名 %q 与其他目标目录名冲突", t.Name)
		}
		seen[t.dir] = true
	}
	if cfg.Probe.Packets < 3 {
		cfg.Probe.Packets = 3 // 少于 3 个样本谈不上分布
	}
	return cfg, nil
}

// stripJSONC 剥离 // 与 /* */ 注释(字符串内除外),让 stdlib json 能吃 JSONC。
func stripJSONC(b []byte) []byte {
	var out strings.Builder
	out.Grow(len(b))
	inStr, esc := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case c == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				i++
			}
			i++
		default:
			out.WriteByte(c)
		}
	}
	return []byte(out.String())
}
