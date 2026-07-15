package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Listen        string            `json:"listen"`
	DataDir       string            `json:"data_dir"`
	WebBaseURL    string            `json:"web_base_url"` // 飞书卡片按钮链接,留空则不带按钮
	Instance      string            `json:"instance"`     // 告警来源标识,不填默认取主机名
	TargetsDir    string            `json:"targets_dir"`  // 目标列表目录(ping.list / tcp.list),默认 ./targets
	WebUser       string            `json:"web_user"`     // 登录用户名,默认 admin
	WebPassword   string            `json:"web_password"` // 登录密码(明文);留空 = 不启用登录
	Extra         map[string]string `json:"extra"`        // 全局自定义字段,进所有告警/恢复卡片
	Targets       []TargetCfg  `json:"targets"`
	Probe         ProbeCfg     `json:"probe"`
	Webhooks      []WebhookCfg `json:"webhooks"`
	Alerts        AlertCfg     `json:"alerts"`
	Heartbeat     ScheduleCfg  `json:"heartbeat"`
	DailyReport   ScheduleCfg  `json:"daily_report"`
	RetentionDays int          `json:"retention_days"`
}

type TargetCfg struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"` // "icmp" | "tcp"
	Host        string            `json:"host"`
	Port        int               `json:"port"`         // 仅 tcp
	Pace        string            `json:"pace"`         // "fast"(15s) | "normal"(60s) | "slow"(300s),不填走全局
	IntervalSec int               `json:"interval_sec"` // 显式秒数,优先级最高
	Sensitivity string            `json:"sensitivity"`  // "strict" | "normal" | "relaxed"
	Alerts      *bool             `json:"alerts"`       // false = 只观测不告警
	Extra       map[string]string `json:"extra"`        // 目标级自定义字段,覆盖全局同名键
	dir         string            // 数据目录名(sanitized)
}

type ProbeCfg struct {
	IntervalSec int `json:"interval_sec"` // 每轮间隔
	Packets     int `json:"packets"`      // 每轮包数(烟雾图的"烟"就来自这里)
	GapMs       int `json:"gap_ms"`       // 包间隔
	TimeoutMs   int `json:"timeout_ms"`   // 单包超时
}

type WebhookCfg struct {
	Name   string   `json:"name"`
	URL    string   `json:"url"`
	Secret string   `json:"secret"` // 飞书"签名校验"密钥,机器人未开签名则留空
	Kinds  []string `json:"kinds"`  // 只接收哪些消息:alert recovery heartbeat daily manual;不填 = 全收
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

// loadTargetLists 读取 targets 目录下的 ping.list / tcp.list。
// 行格式:host[:port]  [名称...]  [k=v ...]
//   已知选项:pace= sensitivity=(或 sens=) interval= alerts=
//   未知的 k=v 全部进目标的自定义字段(extra),随告警卡片展示
// # 开头是注释,空行跳过。目录或文件不存在则静默跳过。
func loadTargetLists(dir string) ([]TargetCfg, error) {
	var out []TargetCfg
	for _, spec := range []struct{ file, typ string }{{"ping.list", "icmp"}, {"tcp.list", "tcp"}} {
		raw, err := os.ReadFile(filepath.Join(dir, spec.file))
		if err != nil {
			continue
		}
		for ln, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			t, err := parseListLine(line, spec.typ)
			if err != nil {
				return nil, fmt.Errorf("%s/%s 第 %d 行: %w", dir, spec.file, ln+1, err)
			}
			out = append(out, t)
		}
	}
	return out, nil
}

func parseListLine(line, typ string) (TargetCfg, error) {
	t := TargetCfg{Type: typ}
	fields := strings.Fields(line)
	addr := fields[0]
	if typ == "tcp" {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return t, fmt.Errorf("tcp 目标 %q 需要 host:port 格式", addr)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			return t, fmt.Errorf("tcp 目标 %q 端口无效", addr)
		}
		t.Host, t.Port = host, port
	} else {
		t.Host = addr
	}
	var nameParts []string
	for _, f := range fields[1:] {
		k, v, isKV := strings.Cut(f, "=")
		if !isKV {
			nameParts = append(nameParts, f)
			continue
		}
		switch k {
		case "pace":
			t.Pace = v
		case "sensitivity", "sens":
			t.Sensitivity = v
		case "interval":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return t, fmt.Errorf("interval=%q 无效", v)
			}
			t.IntervalSec = n
		case "alerts":
			b := v == "true"
			t.Alerts = &b
		default: // 未知键 = 自定义字段(机房、负责人、runbook……)
			if t.Extra == nil {
				t.Extra = map[string]string{}
			}
			t.Extra[k] = v
		}
	}
	if len(nameParts) > 0 {
		t.Name = strings.Join(nameParts, " ")
	} else {
		t.Name = addr
	}
	return t, nil
}

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
	if cfg.TargetsDir == "" {
		cfg.TargetsDir = "./targets"
	}
	// 列表目录:运维加目标的正道 —— echo "1.2.3.4 广州电信" >> targets/ping.list
	listTargets, err := loadTargetLists(cfg.TargetsDir)
	if err != nil {
		return nil, err
	}
	cfg.Targets = append(cfg.Targets, listTargets...)
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("没有任何探测目标:在配置的 targets 里或 %s/ping.list、tcp.list 中至少加一个", cfg.TargetsDir)
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
	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		switch t.Pace {
		case "", "fast", "normal", "slow":
		default:
			return nil, fmt.Errorf("目标 %q 的 pace %q 无效(fast|normal|slow)", t.Name, t.Pace)
		}
		switch t.Sensitivity {
		case "", "strict", "normal", "relaxed":
		default:
			return nil, fmt.Errorf("目标 %q 的 sensitivity %q 无效(strict|normal|relaxed)", t.Name, t.Sensitivity)
		}
	}
	validKinds := map[string]bool{"alert": true, "recovery": true, "heartbeat": true, "daily": true, "manual": true}
	for _, w := range cfg.Webhooks {
		for _, k := range w.Kinds {
			if !validKinds[k] {
				return nil, fmt.Errorf("webhook %q 的 kinds 含无效值 %q", w.Name, k)
			}
		}
	}
	if cfg.WebUser == "" {
		cfg.WebUser = "admin"
	}
	if cfg.Instance == "" {
		if hn, err := os.Hostname(); err == nil {
			cfg.Instance = hn
		} else {
			cfg.Instance = "pingping"
		}
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
