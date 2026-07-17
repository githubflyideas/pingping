// pingping — a network link quality oscilloscope, radically simple.
// Monitoring tells you if it's down. pingping shows you how well it lives.
// One binary. Text files. A smoke graph. Nothing else.
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

const demoConfig = `{
  // pingping — generated on first run. Targets live in targets/ping.list
  // and targets/tcp.list, one per line; restart to apply.
  "listen": "0.0.0.0:8517",
  "retention_days": 300   // raw JSONL kept this long, then deleted
}
`

const demoPingList = `# one ICMP target per line; # is a comment; restart to apply
# format: host  [name]  [pace=fast|slow]  [interval=seconds]
www.google.com Demo pace=fast
`

func main() {
	cfgPath := flag.String("c", "pingping.jsonc", "config file (JSONC)")
	localOnly := flag.Bool("localhost", false, "bind 127.0.0.1 only; put Caddy/Nginx in front for auth/TLS")
	showVer := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVer {
		fmt.Println("pingping", version)
		return
	}

	cfg, err := LoadConfig(*cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			if werr := os.WriteFile(*cfgPath, []byte(demoConfig), 0o644); werr == nil {
				os.MkdirAll("targets", 0o755)
				if _, serr := os.Stat("targets/ping.list"); os.IsNotExist(serr) {
					os.WriteFile("targets/ping.list", []byte(demoPingList), 0o644)
				}
				log.Printf("no config found — generated %s + targets/ping.list (probing www.google.com)", *cfgPath)
				log.Printf(`add a target with one line: echo "1.2.3.4 my-link" >> targets/ping.list`)
				cfg, err = LoadConfig(*cfgPath)
			}
		}
		if err != nil {
			log.Fatalf("config: %v", err)
		}
	}

	if *localOnly {
		cfg.Listen = "127.0.0.1" + portOf(cfg.Listen)
	}

	store, err := NewStore(cfg.DataDir, cfg.Targets)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	store.Replay()

	detector := NewDetector(store)

	stop := make(chan struct{})
	for _, t := range cfg.Targets {
		go probeLoop(t, cfg.Probe, store, detector, stop)
	}
	go serveWeb(cfg, store)
	go housekeeping(cfg, store, stop)

	log.Printf("pingping %s up · %d targets · listening on %s · data in %s",
		version, len(cfg.Targets), cfg.Listen, cfg.DataDir)
	log.Printf("➜  open http://localhost%s for the smoke graph", portOf(cfg.Listen))
	if !*localOnly {
		log.Printf("tip: for a safer setup run with --localhost and put a reverse proxy (Caddy/Nginx) in front for auth/TLS")
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	close(stop)
	log.Println("pingping down")
}

// housekeeping: once a day, delete raw files older than retention. That is all.
func housekeeping(cfg *Config, store *Store, stop chan struct{}) {
	last := ""
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		now := time.Now()
		day := now.Format("2006-01-02")
		if now.Hour() == 0 && now.Minute() == 5 && last != day {
			last = day
			store.Retention(cfg.RetentionDays)
		}
	}
}

func portOf(listen string) string {
	for i := len(listen) - 1; i >= 0; i-- {
		if listen[i] == ':' {
			return listen[i:]
		}
	}
	return ":8517"
}
