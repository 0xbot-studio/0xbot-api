package identity

import (
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"testing"
	"time"
)

func TestWalletSignatureAndExpiry(t *testing.T) {
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	c, e := NewChallenge(address, "https://app.0xbot.org", "app.0xbot.org", nil)
	if e != nil {
		t.Fatal(e)
	}
	hash := crypto.Keccak256([]byte(fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(c.Message), c.Message)))
	sig, e := crypto.Sign(hash, key)
	if e != nil {
		t.Fatal(e)
	}
	sig[64] += 27
	signature := "0x" + hex.EncodeToString(sig)
	if e = VerifyChallenge(c, signature); e != nil {
		t.Fatal(e)
	}
	bad := c
	bad.Message += " malicious"
	if VerifyChallenge(bad, signature) == nil {
		t.Fatal("modified challenge accepted")
	}
	c.Expires = time.Now().Add(-time.Minute)
	if VerifyChallenge(c, signature) == nil {
		t.Fatal("expired challenge accepted")
	}
}
func TestTOTPVector(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if !VerifyTOTP(secret, "287082", time.Unix(59, 0)) {
		t.Fatal("RFC vector failed")
	}
	if VerifyTOTP(secret, "123456", time.Unix(59, 0)) {
		t.Fatal("bad code accepted")
	}
	if VerifyTOTP("", "287082", time.Unix(59, 0)) {
		t.Fatal("missing secret accepted")
	}
}
