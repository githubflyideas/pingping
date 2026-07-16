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

// demoConfig 是首次运行自动生成的演示配置。fast 档让第一屏的烟雾几分钟内成形。
const demoConfig = `{
  // pingping 演示配置 —— 首次运行自动生成
  // 探测目标写在 targets/ping.list 和 targets/tcp.list,一行一个,重启生效
  // 完整字段说明见仓库内 config.example.jsonc
  "listen": "0.0.0.0:8517"
  // 飞书推送:去掉下面的注释,填入你的群机器人 webhook
  //,"webhooks": [
  //  { "name": "运维群", "url": "https://open.feishu.cn/open-apis/bot/v2/hook/xxxx" }
  //]
}
`

const demoPingList = `# 一行一个 ICMP 探测目标,# 是注释,改完重启生效
# 格式:host  [名称]  [pace=fast|slow] [sensitivity=strict|relaxed] [interval=秒] [alerts=false] [自定义k=v]
# 例:59.43.247.1  香港CN2  pace=fast sensitivity=strict 机房=GZ1 负责人=张三
www.google.com Demo·Google pace=fast
`

// portOf 从监听地址提取给人看的端口后缀。
func portOf(listen string) string {
	for i := len(listen) - 1; i >= 0; i-- {
		if listen[i] == ':' {
			return listen[i:]
		}
	}
	return ":8517"
}

func main() {
	cfgPath := flag.String("c", "pingping.jsonc", "config file path (JSONC)")
	localOnly := flag.Bool("localhost", false, "bind 127.0.0.1 only — put Caddy/Nginx/Apache in front for auth/TLS")
	showVer := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVer {
		fmt.Println("pingping", version)
		return
	}

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		// 零配置开箱:找不到配置文件时自动生成演示配置(探测 www.google.com),
		// 下载解压、执行、开浏览器,三十秒看到第一缕烟。
		if os.IsNotExist(err) {
			if werr := os.WriteFile(*cfgPath, []byte(demoConfig), 0o644); werr == nil {
				os.MkdirAll("targets", 0o755)
				if _, serr := os.Stat("targets/ping.list"); os.IsNotExist(serr) {
					os.WriteFile("targets/ping.list", []byte(demoPingList), 0o644)
				}
				log.Printf("未找到配置文件,已生成演示配置 %s + targets/ping.list(探测 www.google.com)", *cfgPath)
				log.Printf("加监控就一行:echo \"1.2.3.4 广州电信\" >> targets/ping.list,重启生效")
				cfg, err = LoadConfig(*cfgPath)
			}
		}
		if err != nil {
			log.Fatalf("配置加载失败: %v", err)
		}
	}

	if *localOnly {
		cfg.Listen = "127.0.0.1" + portOf(cfg.Listen)
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
	log.Printf("➜  打开浏览器访问 http://localhost%s 查看烟雾图", portOf(cfg.Listen))

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
			n.SendReport("📊 链路健康日报", "daily")
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
