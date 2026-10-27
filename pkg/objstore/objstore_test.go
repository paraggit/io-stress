package objstore

import (
	"bytes"
	"crypto/md5"
	"io"
	"testing"
)

func TestBucketNameAndKeyStable(t *testing.T) {
	var seed [32]byte
	seed[0], seed[1], seed[2], seed[3] = 0x4f, 0x2a, 0x9c, 0x1d
	if got := BucketName("objstress", seed, 7); got != "objstress-4f2a9c1d-007" {
		t.Fatalf("BucketName = %q", got)
	}
	if BucketName("objstress", seed, 0) == BucketName("objstress", seed, 1) {
		t.Fatal("ordinals must produce distinct bucket names")
	}
	for _, layout := range []string{"hashed", "flat", "deep"} {
		a, err := Key(layout, "objstress", 1)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Key(layout, "objstress", 1)
		if err != nil {
			t.Fatal(err)
		}
		c, err := Key(layout, "objstress", 2)
		if err != nil {
			t.Fatal(err)
		}
		if a != b {
			t.Fatalf("%s key not stable: %q vs %q", layout, a, b)
		}
		if a == c {
			t.Fatalf("%s keys for 1 and 2 are identical: %q", layout, a)
		}
		if layout == "flat" && a != "objstress/0000000000000001" {
			t.Fatalf("flat = %q", a)
		}
	}
	if _, err := Key("nope", "objstress", 1); err == nil {
		t.Fatal("expected unknown layout error")
	}
}

func TestShardCoversExactly(t *testing.T) {
	for _, tc := range []struct {
		count   uint64
		workers int
	}{{1000, 6}, {1, 4}, {7, 7}, {0, 3}} {
		seen := map[uint64]int{}
		for w := 0; w < tc.workers; w++ {
			start, end := Shard(w, tc.workers, tc.count)
			if end < start {
				t.Fatalf("worker %d range %d:%d", w, start, end)
			}
			for i := start; i < end; i++ {
				seen[i]++
			}
		}
		if uint64(len(seen)) != tc.count {
			t.Fatalf("count %d workers %d covered %d", tc.count, tc.workers, len(seen))
		}
		for i, n := range seen {
			if n != 1 {
				t.Fatalf("index %d seen %d", i, n)
			}
		}
	}
}

func TestChunkDeleteBatches(t *testing.T) {
	keys := make([]string, 2500)
	for i := range keys {
		keys[i] = "k"
	}
	parts := Chunk(keys, 1000)
	if len(parts) != 3 || len(parts[0]) != 1000 || len(parts[1]) != 1000 || len(parts[2]) != 500 {
		t.Fatalf("chunks = %d lengths %d %d %d", len(parts), len(parts[0]), len(parts[1]), len(parts[2]))
	}
	if Chunk(nil, 1000) != nil {
		t.Fatal("empty input should chunk to nil")
	}
}

func TestDeletePathSplit(t *testing.T) {
	if DeletePath("both", 0, 1) != "obc-only" {
		t.Fatal("single bucket must take obc-only")
	}
	if DeletePath("both", 0, 4) != "recursive" || DeletePath("both", 1, 4) != "obc-only" {
		t.Fatal("even ordinals are recursive, odd are obc-only")
	}
	if DeletePath("both", 2, 4) != "recursive" || DeletePath("both", 3, 4) != "obc-only" {
		t.Fatal("split did not follow ordinal parity")
	}
	if DeletePath("recursive", 1, 4) != "recursive" || DeletePath("obc-only", 0, 4) != "obc-only" {
		t.Fatal("explicit modes must not be split")
	}
}

func TestPayloadChangesWithEveryInput(t *testing.T) {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	base := readAll(t, PayloadReader(seed, 1, "k", 0, 64))
	same := readAll(t, PayloadReader(seed, 1, "k", 0, 64))
	if !bytes.Equal(base, same) {
		t.Fatal("payload is not stable")
	}
	seed2 := seed
	seed2[0]++
	cases := []io.Reader{
		PayloadReader(seed2, 1, "k", 0, 64),
		PayloadReader(seed, 2, "k", 0, 64),
		PayloadReader(seed, 1, "other", 0, 64),
		PayloadReader(seed, 1, "k", 1, 64),
	}
	for i, r := range cases {
		got := readAll(t, r)
		if bytes.Equal(base, got) {
			t.Fatalf("case %d matched the base payload", i)
		}
	}
}

func TestSizeForWeightedMix(t *testing.T) {
	var seed [32]byte
	seed[0] = 9
	mix := []SizeWeight{{Bytes: 4096, Weight: 60}, {Bytes: 1 << 20, Weight: 30}, {Bytes: 16 << 20, Weight: 10}}
	const n = 100000
	counts := map[int64]int{}
	for i := uint64(0); i < n; i++ {
		counts[SizeFor(seed, i, mix)]++
	}
	want := map[int64]float64{4096: 0.60, 1 << 20: 0.30, 16 << 20: 0.10}
	for size, frac := range want {
		got := float64(counts[size]) / n
		if got < frac-0.02 || got > frac+0.02 {
			t.Fatalf("size %d fraction %f, want %f ± 0.02", size, got, frac)
		}
	}
	if SizeFor(seed, 1, mix) != SizeFor(seed, 1, mix) {
		t.Fatal("SizeFor is not stable")
	}
}

func TestMultipartETagKnownInput(t *testing.T) {
	a := md5.Sum([]byte("aaaa"))
	b := md5.Sum([]byte("bbbb"))
	got := MultipartETag([][]byte{a[:], b[:]})
	h := md5.New()
	h.Write(a[:])
	h.Write(b[:])
	want := ""
	sum := h.Sum(nil)
	want = sprintfHex(sum) + "-2"
	if got != want {
		t.Fatalf("etag = %s, want %s", got, want)
	}
}

func TestCompareStreamsFirstBadByte(t *testing.T) {
	const n = 300
	const badAt = 100
	exp := bytes.Repeat([]byte{'a'}, n)
	act := bytes.Repeat([]byte{'a'}, n)
	act[badAt] = 'b'
	got, ok, err := CompareStreams(bytes.NewReader(exp), bytes.NewReader(act))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected a mismatch")
	}
	if got.FirstBadByte != badAt {
		t.Fatalf("FirstBadByte = %d, want %d", got.FirstBadByte, badAt)
	}
	if got.ExpectedSHA == got.ActualSHA {
		t.Fatal("hashes should differ")
	}
	if got.ExpectedHex == "" || got.ActualHex == "" || got.ExpectedHex == got.ActualHex {
		t.Fatalf("windows exp %q act %q", got.ExpectedHex, got.ActualHex)
	}
	match, ok, err := CompareStreams(bytes.NewReader(exp), bytes.NewReader(exp))
	if err != nil || !ok || match.FirstBadByte != 0 {
		t.Fatalf("identical streams: ok=%v err=%v mismatch=%+v", ok, err, match)
	}
}

func TestRetryableMismatchIsNotRetried(t *testing.T) {
	if retryable(ErrMismatch) {
		t.Fatal("verification mismatch must not be retried")
	}
	if !retryable(&apiErr{code: "SlowDown", msg: "slow", status: 503}) {
		t.Fatal("SlowDown should be retried")
	}
	if retryable(&apiErr{code: "NotImplemented", msg: "no", status: 501}) {
		t.Fatal("NotImplemented must not be retried")
	}
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sprintfHex(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
