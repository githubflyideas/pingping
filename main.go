// pingping — 网络链路质量示波器
// 监控告诉你挂没挂,pingping 告诉你活得好不好。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	cfgPath := flag.String("c", "pingping.jsonc", "配置文件路径 (JSONC)")
	showVer := flag.Bool("version", false, "打印版本")
	flag.Parse()

	if *showVer {
		fmt.Println("pingping", version)
		return
	}

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	store, err := NewStore(cfg.DataDir, cfg.Targets)
	if err != nil {
		log.Fatalf("存储初始化失败: %v", err)
	}
	store.Replay() // 回放今天+昨天的 JSONL 进内存环

	notifier := NewNotifier(cfg)
	detector := NewDetector(cfg, store, notifier)
	notifier.BindSnapshot(store, detector)

	// 探测循环:每个目标一个 goroutine
	stop := make(chan struct{})
	for _, t := range cfg.Targets {
		go probeLoop(t, cfg.Probe, store, detector, stop)
	}

	// Web
	go serveWeb(cfg, store, detector, notifier)

	// 分钟级调度器:日汇总、保留期清理、日报、周心跳
	go scheduler(cfg, store, notifier, stop)

	log.Printf("pingping %s 启动 · %d 个目标 · 监听 %s · 数据目录 %s",
		version, len(cfg.Targets), cfg.Listen, cfg.DataDir)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	close(stop)
	log.Println("pingping 退出")
}

// scheduler 每分钟醒一次,对表干活。刻意不用 cron 库:需求就四条。
func scheduler(cfg *Config, store *Store, n *Notifier, stop chan struct{}) {
	lastRollup, lastDaily, lastHB := "", "", ""
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		now := time.Now()
		day := now.Format("2006-01-02")

		// 00:05 汇总昨天 + 清理过期文件
		if now.Hour() == 0 && now.Minute() == 5 && lastRollup != day {
			lastRollup = day
			yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
			if err := store.RollupDay(yesterday); err != nil {
				log.Printf("日汇总失败: %v", err)
			}
			store.Retention(cfg.RetentionDays)
		}

		// 日报(可选)
		if cfg.DailyReport.Enabled && now.Hour() == cfg.DailyReport.Hour &&
			now.Minute() == 0 && lastDaily != day {
			lastDaily = day
			n.SendReport("📊 链路健康日报")
		}

		// 周心跳:证明自己活着,给沉默赋予语义
		if cfg.Heartbeat.Enabled && int(now.Weekday()) == cfg.Heartbeat.Weekday &&
			now.Hour() == cfg.Heartbeat.Hour && now.Minute() == 0 {
			wk := fmt.Sprintf("%s-w", day)
			if lastHB != wk {
				lastHB = wk
				n.SendHeartbeat()
			}
		}
	}
}
