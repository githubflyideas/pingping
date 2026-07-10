package main

// dns.go — DNS 主動探測。每個週期對配置的域名做一次解析，
// 產出 dns_lookup_ms{target=} 與 dns_fail{target=}，合併進連續指標流。
// 覆蓋「用戶感知卡頓其實是解析慢/解析失敗」這一常見盲區。

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
)

type DNSProber struct {
	mu      sync.RWMutex
	results map[string]float64
	targets []string
}

func NewDNSProber(targets string, interval time.Duration) *DNSProber {
	p := &DNSProber{results: map[string]float64{}}
	for _, t := range strings.Split(targets, ",") {
		if t = strings.TrimSpace(t); t != "" {
			p.targets = append(p.targets, t)
		}
	}
	if len(p.targets) > 0 {
		go p.loop(interval)
	}
	return p
}

func (p *DNSProber) loop(interval time.Duration) {
	r := &net.Resolver{} // 走 /etc/resolv.conf，量的就是本機真實解析路徑
	tick := time.NewTicker(interval)
	for range tick.C {
		for _, target := range p.targets {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			start := time.Now()
			_, err := r.LookupHost(ctx, target)
			ms := float64(time.Since(start).Microseconds()) / 1000
			cancel()
			p.mu.Lock()
			if err != nil {
				p.results["dns_fail{target="+target+"}"] = 1
				p.results["dns_lookup_ms{target="+target+"}"] = 2000 // 超時按上限記
			} else {
				p.results["dns_fail{target="+target+"}"] = 0
				p.results["dns_lookup_ms{target="+target+"}"] = ms
			}
			p.mu.Unlock()
		}
	}
}

// 合併最近一次探測結果到當前樣本
func (p *DNSProber) Merge(v map[string]float64) {
	p.mu.RLock()
	for k, x := range p.results {
		v[k] = x
	}
	p.mu.RUnlock()
}
