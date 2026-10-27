package objstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// StreamMismatch is the byte-level detail of one bad object.
// Key, version, ETag, and request id are filled in by the caller.
type StreamMismatch struct {
	Size         int64
	ExpectedSHA  string
	ActualSHA    string
	FirstBadByte int64
	ExpectedHex  string
	ActualHex    string
}

// CompareStreams compares expected and actual incrementally.
// A match returns ok=true. Memory stays proportional to the 64-byte window,
// not to the object size.
func CompareStreams(expected, actual io.Reader) (StreamMismatch, bool, error) {
	expHash := sha256.New()
	actHash := sha256.New()
	const side = 64
	var expBefore, actBefore []byte
	var expWin, actWin []byte
	var offset int64
	bad := int64(-1)

	buf := make([]byte, 32*1024)
	expBuf := make([]byte, 32*1024)
	for {
		n, err := actual.Read(buf)
		if n > 0 {
			got, expErr := readFull(expected, expBuf[:n])
			limit := got
			if limit > n {
				limit = n
			}
			hashCopy(actHash, buf[:n])
			if got > 0 {
				hashCopy(expHash, expBuf[:got])
			}
			cmp := limit
			if got < n {
				cmp = got
			}
			for i := 0; i < cmp; i++ {
				noteByte(expBuf[i], buf[i], &offset, &bad, side, &expBefore, &actBefore, &expWin, &actWin)
			}
			if got < n && bad < 0 {
				// Actual continues, expected ended.
				bad = offset
				expWin = append([]byte{}, expBefore...)
				actWin = append(append([]byte{}, actBefore...), buf[got:n]...)
				if int64(n-got) > side {
					actWin = actWin[:len(actBefore)+int(side)]
				}
				offset += int64(n - got)
			} else if got < n {
				offset += int64(n - got)
			}
			if expErr != nil && !errors.Is(expErr, io.EOF) && !errors.Is(expErr, io.ErrUnexpectedEOF) {
				return StreamMismatch{}, false, expErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return StreamMismatch{}, false, err
		}
	}
	// Expected bytes the actual stream never produced.
	extra := make([]byte, 32*1024)
	for {
		n, err := expected.Read(extra)
		if n > 0 {
			hashCopy(expHash, extra[:n])
			if bad < 0 {
				bad = offset
				expWin = append(append([]byte{}, expBefore...), extra[:min(n, side)]...)
				actWin = append([]byte{}, actBefore...)
			} else if offset <= bad+side {
				take := int(bad + side - offset + 1)
				if take > n {
					take = n
				}
				if take > 0 {
					expWin = append(expWin, extra[:take]...)
				}
			}
			offset += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return StreamMismatch{}, false, err
		}
	}
	if bad < 0 {
		return StreamMismatch{}, true, nil
	}
	return StreamMismatch{
		Size:         offset,
		ExpectedSHA:  hex.EncodeToString(expHash.Sum(nil)),
		ActualSHA:    hex.EncodeToString(actHash.Sum(nil)),
		FirstBadByte: bad,
		ExpectedHex:  hex.EncodeToString(expWin),
		ActualHex:    hex.EncodeToString(actWin),
	}, false, nil
}

func noteByte(exp, act byte, offset, bad *int64, side int, expBefore, actBefore, expWin, actWin *[]byte) {
	if *bad < 0 {
		if exp == act {
			*expBefore = pushRing(*expBefore, exp, side)
			*actBefore = pushRing(*actBefore, act, side)
		} else {
			*bad = *offset
			*expWin = append(append([]byte{}, *expBefore...), exp)
			*actWin = append(append([]byte{}, *actBefore...), act)
		}
	} else if *offset <= *bad+int64(side) {
		*expWin = append(*expWin, exp)
		*actWin = append(*actWin, act)
	}
	*offset++
}

func readFull(r io.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		nn, err := r.Read(buf[n:])
		n += nn
		if err != nil {
			return n, err
		}
		if nn == 0 {
			return n, io.ErrUnexpectedEOF
		}
	}
	return n, nil
}

func pushRing(buf []byte, b byte, n int) []byte {
	if len(buf) < n {
		return append(buf, b)
	}
	copy(buf, buf[1:])
	buf[len(buf)-1] = b
	return buf
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
