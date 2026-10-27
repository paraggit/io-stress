package objstore

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"math/rand/v2"
)

// SizeWeight is a parsed size-mix entry. Bytes is the object size.
type SizeWeight struct {
	Bytes  int64
	Weight int
}

// ParseSeed decodes a 32-byte hex seed. A 0x prefix is accepted.
func ParseSeed(s string) ([32]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return [32]byte{}, fmt.Errorf("object seed must be 64 hex characters")
	}
	var out [32]byte
	copy(out[:], b)
	return out, nil
}

// PayloadReader is a deterministic incompressible byte stream.
// version is 0 for unversioned objects and 1..versions_per_key otherwise.
// bucketOrdinal, not the bucket name, is what the hash uses.
func PayloadReader(runSeed [32]byte, bucketOrdinal int, key string, version, size int64) io.Reader {
	h := sha256.New()
	_, _ = h.Write(runSeed[:])
	_ = binary.Write(h, binary.BigEndian, int64(bucketOrdinal))
	_, _ = h.Write([]byte(key))
	_ = binary.Write(h, binary.BigEndian, version)
	var seed [32]byte
	copy(seed[:], h.Sum(nil))
	c := rand.NewChaCha8(seed)
	if size < 0 {
		size = 0
	}
	return io.LimitReader(c, size)
}

// SizeFor picks a size from mix for object index i. The choice is a pure
// function of the seed and the index.
func SizeFor(runSeed [32]byte, i uint64, mix []SizeWeight) int64 {
	if len(mix) == 0 {
		return 0
	}
	var total uint64
	for _, m := range mix {
		if m.Weight > 0 {
			total += uint64(m.Weight)
		}
	}
	if total == 0 {
		return mix[0].Bytes
	}
	h := sha256.New()
	_, _ = h.Write(runSeed[:])
	_ = binary.Write(h, binary.BigEndian, i)
	sum := h.Sum(nil)
	pick := binary.BigEndian.Uint64(sum[:8]) % total
	var acc uint64
	for _, m := range mix {
		if m.Weight <= 0 {
			continue
		}
		acc += uint64(m.Weight)
		if pick < acc {
			return m.Bytes
		}
	}
	return mix[len(mix)-1].Bytes
}

// Hit is a deterministic sample. pct is a percentage in [0, 100].
// salt separates independent samples (read-after-write, range, copy, verify).
func Hit(runSeed [32]byte, bucketOrdinal int, key, salt string, pct float64) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	h := sha256.New()
	_, _ = h.Write(runSeed[:])
	_ = binary.Write(h, binary.BigEndian, int64(bucketOrdinal))
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte(salt))
	sum := h.Sum(nil)
	v := binary.BigEndian.Uint32(sum[:4])
	// 0.001% resolution.
	return float64(v%100000)/1000.0 < pct
}
