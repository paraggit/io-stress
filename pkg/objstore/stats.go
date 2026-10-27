package objstore

import (
	"math"
	"sort"
	"sync"
	"time"
)

// OpStat is the latency summary for one S3 operation.
type OpStat struct {
	Count uint64
	P50Ms float64
	P95Ms float64
	P99Ms float64
	MaxMs float64
}

// Stats records latencies and S3 error codes with flat memory.
// The first 8192 samples of an op are exact; after that a log histogram is used.
type Stats struct {
	mu      sync.Mutex
	ops     map[string]*latency
	errors  map[string]uint64
	samples map[string]string
	retries uint64
}

func NewStats() *Stats {
	return &Stats{
		ops:     map[string]*latency{},
		errors:  map[string]uint64{},
		samples: map[string]string{},
	}
}

func (s *Stats) Observe(op string, d time.Duration) {
	if s == nil {
		return
	}
	ms := float64(d) / float64(time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.ops[op]
	if l == nil {
		l = &latency{}
		s.ops[op] = l
	}
	l.add(ms)
}

func (s *Stats) NoteRetry(code, reqID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retries++
	s.note(code, reqID)
}

func (s *Stats) NoteError(code, reqID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.note(code, reqID)
}

func (s *Stats) note(code, reqID string) {
	if code == "" {
		code = "Unknown"
	}
	s.errors[code]++
	if reqID != "" && s.samples[code] == "" {
		s.samples[code] = reqID
	}
}

func (s *Stats) Retries() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retries
}

func (s *Stats) Errors() map[string]uint64 {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]uint64, len(s.errors))
	for k, v := range s.errors {
		out[k] = v
	}
	return out
}

func (s *Stats) ErrorSamples() map[string]string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.samples))
	for k, v := range s.samples {
		out[k] = v
	}
	return out
}

func (s *Stats) Ops() map[string]OpStat {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]OpStat, len(s.ops))
	for name, l := range s.ops {
		out[name] = l.stat()
	}
	return out
}

type latency struct {
	samples  []float64
	overflow bool
	hist     [160]uint64
	count    uint64
	max      float64
}

func (l *latency) add(ms float64) {
	l.count++
	if ms > l.max {
		l.max = ms
	}
	if !l.overflow && len(l.samples) < 8192 {
		l.samples = append(l.samples, ms)
		return
	}
	if !l.overflow {
		for _, v := range l.samples {
			l.hist[histBucket(v)]++
		}
		l.samples = nil
		l.overflow = true
	}
	l.hist[histBucket(ms)]++
}

func (l *latency) stat() OpStat {
	if l.count == 0 {
		return OpStat{}
	}
	if !l.overflow {
		cp := append([]float64(nil), l.samples...)
		sort.Float64s(cp)
		return OpStat{
			Count: l.count,
			P50Ms: percentile(cp, 0.50),
			P95Ms: percentile(cp, 0.95),
			P99Ms: percentile(cp, 0.99),
			MaxMs: l.max,
		}
	}
	return OpStat{
		Count: l.count,
		P50Ms: histPercentile(l.hist, l.count, 0.50),
		P95Ms: histPercentile(l.hist, l.count, 0.95),
		P99Ms: histPercentile(l.hist, l.count, 0.99),
		MaxMs: l.max,
	}
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(q*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// hist spans 0.05ms .. ~1h on a log scale.
func histBucket(ms float64) int {
	if ms <= 0.05 {
		return 0
	}
	const (
		min = 0.05
		max = 3600_000.0
	)
	if ms >= max {
		return len(histBounds) - 1
	}
	pos := math.Log(ms/min) / math.Log(max/min)
	i := int(pos * float64(len(histBounds)-1))
	if i < 0 {
		return 0
	}
	if i >= len(histBounds) {
		return len(histBounds) - 1
	}
	return i
}

var histBounds = func() [160]float64 {
	var b [160]float64
	const min = 0.05
	const max = 3600_000.0
	for i := range b {
		frac := float64(i) / float64(len(b)-1)
		b[i] = min * math.Pow(max/min, frac)
	}
	return b
}()

func histPercentile(hist [160]uint64, count uint64, q float64) float64 {
	if count == 0 {
		return 0
	}
	target := uint64(math.Ceil(q * float64(count)))
	if target < 1 {
		target = 1
	}
	var acc uint64
	for i, n := range hist {
		acc += n
		if acc >= target {
			return histBounds[i]
		}
	}
	return histBounds[len(histBounds)-1]
}
