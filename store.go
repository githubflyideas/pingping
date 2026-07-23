package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	s.mu.RLock()
	dir := s.dirs[name]
	s.mu.RUnlock()
	return filepath.Join(s.dir, dir, day+".jsonl")
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

func (s *Store) ReadRange(name string, from, to int64) []Round {
	var rounds []Round
	// ring first (covers the recent tail cheaply)
	s.mu.RLock()
	ring := s.rings[name]
	ringFrom := int64(1<<62)
	if len(ring) > 0 {
		ringFrom = ring[0].T
	}
	s.mu.RUnlock()

	if from >= ringFrom {
		rounds = s.Recent(name, from)
	} else {
		for d := time.Unix(from, 0); !d.After(time.Unix(to, 0)); d = d.AddDate(0, 0, 1) {
			day, _ := s.readDay(name, d.Format("2006-01-02"))
			rounds = append(rounds, day...)
		}
	}
	// clip to [from,to]. Always allocate: reusing rounds[:0] returns nil when rounds
	// is nil, which serializes as JSON null and blanks the chart instead of drawing
	// an empty window.
	out := make([]Round, 0, len(rounds))
	for _, r := range rounds {
		if r.T >= from && r.T <= to {
			out = append(out, r)
		}
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
// downsampleRound collapses a round's samples to [min, median, P90, max].
// The round itself is preserved — same timestamp, same sent/recv counts, same burst
// flag and z-score — so cold data flows through the exact same code path as hot data.
// Only the within-round redundancy is dropped: on a month-wide axis those 20-30
// samples land in the same pixel column anyway.
func downsampleRound(r Round) Round {
	if len(r.MS) <= 4 {
		return r // already small enough
	}
	sorted := append([]float64(nil), r.MS...)
	sort.Float64s(sorted)
	n := len(sorted)
	r.MS = []float64{
		round2(sorted[0]),
		round2(sorted[n/2]),
		round2(sorted[int(float64(n)*0.9)]),
		round2(sorted[n-1]),
	}
	return r
}

// Downsample rewrites one day file in place with per-round downsampling. Files are
// rewritten atomically via a temp file so a crash cannot leave a half-written day.
func (s *Store) Downsample(name, day string) error {
	rounds, err := s.readDay(name, day)
	if err != nil || len(rounds) == 0 {
		return err
	}
	// already downsampled? cheap check on the first round
	if len(rounds[0].MS) <= 4 {
		return nil
	}
	path := s.dayFile(name, day)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	for _, r := range rounds {
		line, err := json.Marshal(downsampleRound(r))
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		if _, err := f.Write(append(line, byte(10))); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Tier runs nightly: day files older than hotDays get downsampled in place, files
// older than keepDays are deleted. One pass, no separate cold directory — the
// tiering is invisible above the storage layer.
func (s *Store) Tier(hotDays, keepDays int) {
	hotCut := time.Now().AddDate(0, 0, -hotDays).Format("2006-01-02")
	keepCut := time.Now().AddDate(0, 0, -keepDays).Format("2006-01-02")
	s.mu.RLock()
	names := make([]string, 0, len(s.dirs))
	for n := range s.dirs {
		names = append(names, n)
	}
	s.mu.RUnlock()

	for _, name := range names {
		s.mu.RLock()
		dir := s.dirs[name]
		s.mu.RUnlock()
		entries, err := os.ReadDir(filepath.Join(s.dir, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			fn := e.Name()
			if len(fn) < 10 || strings.HasSuffix(fn, ".tmp") {
				continue
			}
			day := fn[:10]
			switch {
			case day < keepCut:
				os.Remove(filepath.Join(s.dir, dir, fn))
				log.Printf("retention: removed %s/%s", dir, fn)
			case day < hotCut:
				if err := s.Downsample(name, day); err != nil {
					log.Printf("downsample %s/%s failed: %v", dir, day, err)
				}
			}
		}
	}
}

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
