package fio

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type FIOResult struct {
	Jobs []FIOJobResult `json:"jobs"`
}

type FIOJobResult struct {
	Jobname string       `json:"jobname"`
	Error   int          `json:"error"`
	Read    FIODirection `json:"read"`
	Write   FIODirection `json:"write"`
	Trim    FIODirection `json:"trim"`
}

type FIODirection struct {
	IOPS    float64 `json:"iops"`
	BWBytes int64   `json:"bw_bytes"`
	IoBytes int64   `json:"io_bytes"`
	LatNS   FIOLat  `json:"lat_ns"`
}

type FIOLat struct {
	Mean float64 `json:"mean"`
	Max  float64 `json:"max"`
}

func ParseResult(data []byte) (*FIOResult, error) {
	var r FIOResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// WriteIOBytes sums write.io_bytes across jobs. ok is false if stdout is not fio JSON.
func WriteIOBytes(data []byte) (int64, bool) {
	r, err := ParseResult(data)
	if err != nil || len(r.Jobs) == 0 {
		return 0, false
	}
	var n int64
	for _, j := range r.Jobs {
		n += j.Write.IoBytes
	}
	return n, true
}

// SizeBytes parses fio-style sizes (512m, 512Mi, 1G, 256k) as binary units.
func SizeBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	lower := strings.ToLower(s)
	mul := int64(1)
	n := s
	switch {
	case strings.HasSuffix(lower, "gi"):
		mul = 1 << 30
		n = s[:len(s)-2]
	case strings.HasSuffix(lower, "mi"):
		mul = 1 << 20
		n = s[:len(s)-2]
	case strings.HasSuffix(lower, "ki"):
		mul = 1 << 10
		n = s[:len(s)-2]
	default:
		r := []rune(s)
		last := r[len(r)-1]
		if unicode.IsLetter(last) {
			switch unicode.ToLower(last) {
			case 't':
				mul = 1 << 40
			case 'g':
				mul = 1 << 30
			case 'm':
				mul = 1 << 20
			case 'k':
				mul = 1 << 10
			default:
				return 0, fmt.Errorf("unknown size suffix in %q", s)
			}
			n = string(r[:len(r)-1])
		}
	}
	v, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("size must be > 0, got %q", s)
	}
	return v * mul, nil
}
