package identity

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func VerifyTOTP(secret, code string, now time.Time) bool {
	if len(code) != 6 || secret == "" {
		return false
	}
	key, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if e != nil || len(key) < 20 {
		return false
	}
	for d := int64(-1); d <= 1; d++ {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(now.Unix()/30+d))
		h := hmac.New(sha1.New, key)
		h.Write(b[:])
		v := h.Sum(nil)
		o := v[len(v)-1] & 15
		n := binary.BigEndian.Uint32(v[o:o+4]) & 0x7fffffff
		expected := fmt.Sprintf("%06d", n%1000000)
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}
