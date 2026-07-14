package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store 三层文件哲学:
//   内存环  — 最近 24h,出图和检测直接读,进程内飞行记录器
//   原始层  — data/<target>/YYYY-MM-DD.jsonl,append-only,到期整文件删除
//   汇总层  — data/summary/<target>.jsonl,每天一行,永久保留
// 没有数据库。文本文件可以 grep、可以 tar、永远不会"坏得打不开"。

const ringCap = 1440 // 24h @ 1min

type Store struct {
	mu    sync.RWMutex
	dir   string
	names []string          // 目标显示名(保序)
	dirs  map[string]string // name -> 目录名
	rings map[string][]Round
	total uint64 // 本进程累计轮数(心跳用)
	start time.Time
}

func NewStore(dir string, targets []TargetCfg) (*Store, error) {
	s := &Store{dir: dir, dirs: map[string]string{}, rings: map[string][]Round{}, start: time.Now()}
	for _, t := range targets {
		s.names = append(s.names, t.Name)
		s.dirs[t.Name] = t.dir
		s.rings[t.Name] = nil
		if err := os.MkdirAll(filepath.Join(dir, t.dir), 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "summary"), 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) dayFile(name, day string) string {
	return filepath.Join(s.dir, s.dirs[name], day+".jsonl")
}

// Append 先落盘再进环:进程崩溃时宁可环里少一条,不可文件里丢一条。
func (s *Store) Append(name string, r Round) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	day := time.Unix(r.T, 0).Format("2006-01-02")
	f, err := os.OpenFile(s.dayFile(name, day), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	f.Close()

	s.mu.Lock()
	ring := append(s.rings[name], r)
	if len(ring) > ringCap {
		ring = ring[len(ring)-ringCap:]
	}
	s.rings[name] = ring
	s.total++
	s.mu.Unlock()
	return nil
}

// Replay 开机回放昨天+今天的文件,重建飞行记录器。
func (s *Store) Replay() {
	days := []string{
		time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
		time.Now().Format("2006-01-02"),
	}
	cut := time.Now().Add(-24 * time.Hour).Unix()
	for _, name := range s.names {
		var ring []Round
		for _, day := range days {
			f, err := os.Open(s.dayFile(name, day))
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for sc.Scan() {
				var r Round
				if json.Unmarshal(sc.Bytes(), &r) == nil && r.T >= cut {
					ring = append(ring, r)
				}
			}
			f.Close()
		}
		if len(ring) > ringCap {
			ring = ring[len(ring)-ringCap:]
		}
		s.mu.Lock()
		s.rings[name] = ring
		s.mu.Unlock()
		if len(ring) > 0 {
			log.Printf("[%s] 回放 %d 轮", name, len(ring))
		}
	}
}

// Recent 返回 since 之后的轮(拷贝,调用方随便用)。
func (s *Store) Recent(name string, since int64) []Round {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ring := s.rings[name]
	i := sort.Search(len(ring), func(i int) bool { return ring[i].T >= since })
	out := make([]Round, len(ring)-i)
	copy(out, ring[i:])
	return out
}

func (s *Store) Names() []string { return s.names }

func (s *Store) Counters() (uint64, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.total, s.start
}

// Stats 对一组轮做分布统计。这里是"存分布不存均值"兑现的地方。
type Stats struct {
	Rounds  int     `json:"rounds"`
	P50     float64 `json:"p50"`
	P90     float64 `json:"p90"`
	P99     float64 `json:"p99"`
	LossPct float64 `json:"loss_pct"`
	Bursts  int     `json:"bursts"`
}

func calcStats(rounds []Round) Stats {
	st := Stats{Rounds: len(rounds)}
	var pool []float64
	sent, recv := 0, 0
	for _, r := range rounds {
		sent += r.S
		recv += r.R
		pool = append(pool, r.MS...)
		if r.B {
			st.Bursts++
		}
	}
	if sent > 0 {
		st.LossPct = 100 * float64(sent-recv) / float64(sent)
	}
	if len(pool) > 0 {
		sort.Float64s(pool)
		st.P50 = pct(pool, 50)
		st.P90 = pct(pool, 90)
		st.P99 = pct(pool, 99)
	}
	return st
}

// pct 要求已排序。
func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p / 100)
	return sorted[idx]
}

// RollupDay 把某天的原始文件压成汇总层的一行。
func (s *Store) RollupDay(day string) error {
	for _, name := range s.names {
		f, err := os.Open(s.dayFile(name, day))
		if err != nil {
			continue // 该目标当天无数据
		}
		var rounds []Round
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var r Round
			if json.Unmarshal(sc.Bytes(), &r) == nil {
				rounds = append(rounds, r)
			}
		}
		f.Close()
		if len(rounds) == 0 {
			continue
		}
		st := calcStats(rounds)
		rec := map[string]any{"d": day, "p50": round2(st.P50), "p90": round2(st.P90),
			"p99": round2(st.P99), "loss": round2(st.LossPct), "rounds": st.Rounds, "bursts": st.Bursts}
		line, _ := json.Marshal(rec)
		sf, err := os.OpenFile(filepath.Join(s.dir, "summary", s.dirs[name]+".jsonl"),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		sf.Write(append(line, '\n'))
		sf.Close()
	}
	return nil
}

// Retention:保留期就是 rm。按天分文件让清理不需要任何压缩整理逻辑。
func (s *Store) Retention(days int) {
	if days <= 0 {
		return
	}
	cut := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	for _, dir := range s.dirs {
		entries, err := os.ReadDir(filepath.Join(s.dir, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			day := e.Name()
			if len(day) >= 10 && day[:10] < cut {
				os.Remove(filepath.Join(s.dir, dir, e.Name()))
				log.Printf("保留期清理: %s/%s", dir, e.Name())
			}
		}
	}
}

func (s *Store) SummaryLines(name string, n int) []json.RawMessage {
	f, err := os.Open(filepath.Join(s.dir, "summary", s.dirs[name]+".jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []json.RawMessage
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, json.RawMessage(append([]byte(nil), sc.Bytes()...)))
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }

func fmtDur(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
}
