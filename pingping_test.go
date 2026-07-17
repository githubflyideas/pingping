package main

import "testing"

func TestParseListLine(t *testing.T) {
	tt, err := parseListLine("59.43.247.1 HK CN2 pace=fast", "icmp")
	if err != nil || tt.Name != "HK CN2" || tt.Pace != "fast" {
		t.Fatalf("icmp line: %+v %v", tt, err)
	}
	tt, err = parseListLine("10.0.0.5:443 gw interval=30", "tcp")
	if err != nil || tt.Host != "10.0.0.5" || tt.Port != 443 || tt.IntervalSec != 30 {
		t.Fatalf("tcp line: %+v %v", tt, err)
	}
	if _, err := parseListLine("noport name", "tcp"); err == nil {
		t.Fatal("tcp without port should fail")
	}
}

func TestProbeParams(t *testing.T) {
	g := ProbeCfg{IntervalSec: 60, Packets: 20}
	if iv, pk := probeParams(TargetCfg{Pace: "fast"}, g); iv.Seconds() != 15 || pk != 30 {
		t.Fatalf("fast: %v %d", iv, pk)
	}
	if iv, _ := probeParams(TargetCfg{Pace: "fast", IntervalSec: 7}, g); iv.Seconds() != 7 {
		t.Fatalf("explicit interval wins: %v", iv)
	}
}

func TestRobustZ(t *testing.T) {
	base := make([]float64, 60)
	for i := range base {
		base[i] = float64(i % 3) // 0,1,2 loss pattern
	}
	if z := robustZ(9, base); z < 3 {
		t.Fatalf("9 losses vs 0-2 baseline should be anomalous, z=%v", z)
	}
}
