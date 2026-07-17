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
			log.Printf("[%s] replayed %d rounds", name, len(ring))
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

func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.names...)
}

// EnsureTarget / RemoveTarget: storage side of hot reload. Removal only drops
// the in-memory ring; data files stay on disk.
func (s *Store) EnsureTarget(t TargetCfg) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rings[t.Name]; ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(s.dir, t.dir), 0o755); err != nil {
		return err
	}
	s.dirs[t.Name] = t.dir
	s.rings[t.Name] = nil
	s.names = append(s.names, t.Name)
	return nil
}

func (s *Store) RemoveTarget(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rings, name)
	for i, n := range s.names {
		if n == name {
			s.names = append(s.names[:i], s.names[i+1:]...)
			break
		}
	}
}

// BandRow: one aggregated bucket for windows beyond the 24h smoke range.
type BandRow struct {
	T      int64   `json:"t"`
	P50    float64 `json:"p50"`
	P90    float64 `json:"p90"`
	P99    float64 `json:"p99"`
	Loss   float64 `json:"loss"`
	Bursts int     `json:"b,omitempty"`
	N      int     `json:"n"`
}

// BandSeries aggregates raw day files into fixed-width buckets. Bucket width is
// hardcoded per window so long ranges stay cheap: 7d→10min, 30d→30min, 90d→2h,
// 180d/all→4h. Reading a season of JSONL takes a moment; it is a manual click.
func (s *Store) BandSeries(name string, days int) []BandRow {
	bucket := int64(600)
	switch {
	case days > 90:
		bucket = 14400
	case days > 30:
		bucket = 7200
	case days > 7:
		bucket = 1800
	}
	var rows []BandRow
	for i := days; i >= 0; i-- {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		rounds, _ := s.readDay(name, day)
		if len(rounds) == 0 {
			continue
		}
		rows = append(rows, bucketRounds(rounds, bucket)...)
	}
	return rows
}

func bucketRounds(rounds []Round, size int64) []BandRow {
	buckets := map[int64][]Round{}
	for _, r := range rounds {
		k := r.T / size * size
		buckets[k] = append(buckets[k], r)
	}
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]BandRow, 0, len(keys))
	for _, k := range keys {
		st := calcStats(buckets[k])
		out = append(out, BandRow{T: k, P50: round2(st.P50), P90: round2(st.P90),
			P99: round2(st.P99), Loss: round2(st.LossPct), Bursts: st.Bursts, N: st.Rounds})
	}
	return out
}

func (s *Store) readDay(name, day string) ([]Round, error) {
	f, err := os.Open(s.dayFile(name, day))
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	var out []Round
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var r Round
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

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
				log.Printf("retention: removed %s/%s", dir, e.Name())
			}
		}
	}
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
