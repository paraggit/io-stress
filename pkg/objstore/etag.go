package objstore

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
)

// MultipartETag is the S3 multipart ETag: hex(md5(concat(md5(part))))-N.
// partMD5s are raw 16-byte digests, not hex.
func MultipartETag(partMD5s [][]byte) string {
	h := md5.New()
	for _, p := range partMD5s {
		_, _ = h.Write(p)
	}
	return fmt.Sprintf("%x-%d", h.Sum(nil), len(partMD5s))
}

// NormalizeETag strips the quotes S3 puts around an ETag.
func NormalizeETag(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// ExpectedETag streams the payload once and returns the ETag a matching
// upload would have produced. skip is true when a multipart ETag cannot be
// computed (the caller should record the check as skipped).
func ExpectedETag(runSeed [32]byte, bucketOrdinal int, key string, version, size, partSize int64, multipart bool) (string, bool, error) {
	if size < 0 {
		size = 0
	}
	body := PayloadReader(runSeed, bucketOrdinal, key, version, size)
	if !multipart {
		sum := md5.New()
		if _, err := io.Copy(sum, body); err != nil {
			return "", false, err
		}
		return hex.EncodeToString(sum.Sum(nil)), false, nil
	}
	if partSize <= 0 {
		return "", true, nil
	}
	if size == 0 {
		sum := md5.New()
		return hex.EncodeToString(sum.Sum(nil)), false, nil
	}
	var parts [][]byte
	left := size
	for left > 0 {
		n := partSize
		if n > left {
			n = left
		}
		part := md5.New()
		if _, err := io.CopyN(part, body, n); err != nil {
			return "", false, err
		}
		parts = append(parts, append([]byte(nil), part.Sum(nil)...))
		left -= n
	}
	return MultipartETag(parts), false, nil
}

// hashCopy is a small helper so verify can hash while comparing.
func hashCopy(h hash.Hash, p []byte) {
	_, _ = h.Write(p)
}
