package objstore

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// BucketName is <prefix>-<first 8 hex of seed>-<ordinal>.
// The ordinal is zero-padded to 3 digits so a resume can find the same claim.
func BucketName(prefix string, runSeed [32]byte, ordinal int) string {
	return fmt.Sprintf("%s-%x-%03d", strings.ToLower(prefix), runSeed[:4], ordinal)
}

// Key builds the object key for index i.
// hashed: <prefix>/<2-hex-fanout>/<16-digit-index>
// flat:   <prefix>/<16-digit-index>
// deep:   <prefix>/<a>/<b>/<c>/<16-digit-index>
func Key(layout, prefix string, i uint64) (string, error) {
	idx := fmt.Sprintf("%016d", i)
	switch layout {
	case "flat":
		return prefix + "/" + idx, nil
	case "deep":
		a := fmt.Sprintf("%02x", byte(i>>16))
		b := fmt.Sprintf("%02x", byte(i>>8))
		c := fmt.Sprintf("%02x", byte(i))
		return fmt.Sprintf("%s/%s/%s/%s/%s", prefix, a, b, c, idx), nil
	case "hashed", "":
		sum := sha256.Sum256([]byte(idx))
		return fmt.Sprintf("%s/%02x/%s", prefix, sum[0], idx), nil
	default:
		return "", fmt.Errorf("unknown key layout %q", layout)
	}
}

// Shard is the half-open index range owned by worker of workers.
// Ranges are contiguous and cover [0, count) exactly once.
func Shard(worker, workers int, count uint64) (start, end uint64) {
	if workers <= 0 || worker < 0 || worker >= workers || count == 0 {
		return 0, 0
	}
	w := uint64(workers)
	i := uint64(worker)
	return i * count / w, (i + 1) * count / w
}

// Chunk splits keys into batches of at most n. The final batch may be short.
// n <= 0 uses the S3 DeleteObjects limit of 1000.
func Chunk(keys []string, n int) [][]string {
	if len(keys) == 0 {
		return nil
	}
	if n <= 0 {
		n = 1000
	}
	out := make([][]string, 0, (len(keys)+n-1)/n)
	for i := 0; i < len(keys); i += n {
		j := i + n
		if j > len(keys) {
			j = len(keys)
		}
		out = append(out, keys[i:j])
	}
	return out
}

// DeletePath selects the per-bucket delete behaviour.
// both: even ordinals are recursive, odd ordinals are obc-only.
// A single bucket always takes obc-only, which is the finalizer case.
func DeletePath(mode string, ordinal, buckets int) string {
	switch mode {
	case "recursive", "obc-only":
		return mode
	default:
		if buckets <= 1 {
			return "obc-only"
		}
		if ordinal%2 == 0 {
			return "recursive"
		}
		return "obc-only"
	}
}

// Versioned reports whether ordinal is one of the first n versioned buckets.
func Versioned(ordinal, versionedBuckets int) bool {
	return ordinal >= 0 && ordinal < versionedBuckets
}
