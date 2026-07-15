package main

import (
	"strings"
	"testing"
)

func TestFeishuSign(t *testing.T) {
	// 与飞书官方文档算法一致性由集成测试的 python 复算保证,这里锁定不回归
	got := feishuSign("testsecret", 1784074595)
	if len(got) != 44 || !strings.HasSuffix(got, "=") {
		t.Fatalf("签名格式异常: %q", got)
	}
	if feishuSign("a", 1) == feishuSign("b", 1) || feishuSign("a", 1) == feishuSign("a", 2) {
		t.Fatal("签名对 secret/timestamp 不敏感")
	}
}

func TestSensOf(t *testing.T) {
	base := AlertCfg{DegradeRatio: 1.5, BurstAlertN: 3}
	if s := sensOf(TargetCfg{Sensitivity: "strict"}, base); s.ratio != 1.3 || s.burstN != 2 || s.streak != 2 {
		t.Fatalf("strict 档错误: %+v", s)
	}
	if s := sensOf(TargetCfg{Sensitivity: "relaxed"}, base); s.ratio != 2.0 || s.burstN != 5 || s.streak != 5 {
		t.Fatalf("relaxed 档错误: %+v", s)
	}
	if s := sensOf(TargetCfg{}, base); s.ratio != 1.5 || s.burstN != 3 || s.streak != 3 {
		t.Fatalf("默认档应取全局配置: %+v", s)
	}
}

func TestAlertMeta(t *testing.T) {
	n := &Notifier{instance: "gz-01"}
	m := n.meta(AlertEvent{
		Target: "广州电信", Host: "1.2.3.4", Since: 1784070000,
		Extra: map[string]string{"机房": "GZ1", "负责人": "张三"},
	}, false)
	for _, want := range []string{"来源:gz-01", "广州电信(1.2.3.4)", "开始:", "已持续", "机房:GZ1", "负责人:张三"} {
		if !strings.Contains(m, want) {
			t.Fatalf("元信息缺少 %q:\n%s", want, m)
		}
	}
}

func TestProbeParams(t *testing.T) {
	g := ProbeCfg{IntervalSec: 60, Packets: 20}
	if iv, pk := probeParams(TargetCfg{Pace: "fast"}, g); iv.Seconds() != 15 || pk != 30 {
		t.Fatalf("fast 档错误: %v %d", iv, pk)
	}
	if iv, _ := probeParams(TargetCfg{Pace: "slow"}, g); iv.Seconds() != 300 {
		t.Fatalf("slow 档错误: %v", iv)
	}
	if iv, _ := probeParams(TargetCfg{Pace: "fast", IntervalSec: 7}, g); iv.Seconds() != 7 {
		t.Fatalf("显式 interval_sec 应最优先: %v", iv)
	}
}

func TestParseListLine(t *testing.T) {
	tt, err := parseListLine("59.43.247.1 香港 CN2 pace=fast sens=strict 机房=GZ1", "icmp")
	if err != nil || tt.Name != "香港 CN2" || tt.Pace != "fast" || tt.Sensitivity != "strict" || tt.Extra["机房"] != "GZ1" {
		t.Fatalf("icmp 行解析错误: %+v %v", tt, err)
	}
	tt, err = parseListLine("10.0.0.5:443 网关 alerts=false interval=30", "tcp")
	if err != nil || tt.Host != "10.0.0.5" || tt.Port != 443 || tt.Alerts == nil || *tt.Alerts || tt.IntervalSec != 30 {
		t.Fatalf("tcp 行解析错误: %+v %v", tt, err)
	}
	if tt, _ := parseListLine("www.baidu.com", "icmp"); tt.Name != "www.baidu.com" {
		t.Fatalf("无名称时应回落到 host: %+v", tt)
	}
	if _, err := parseListLine("nohost-noport 名字", "tcp"); err == nil {
		t.Fatal("tcp 缺端口应报错")
	}
}
