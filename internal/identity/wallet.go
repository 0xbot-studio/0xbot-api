package identity

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/platform/token"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spruceid/siwe-go"
	"strings"
	"time"
)

func NewChallenge(address, origin, host string, binding *string) (Challenge, error) {
	if !data.ValidAddress(address) {
		return Challenge{}, data.ErrInput
	}
	nonce := token.New()
	now := time.Now().UTC()
	expires := now.Add(5 * time.Minute)
	statement := "Sign in to 0xbot. This does not authorize transactions or payments."
	if binding != nil {
		statement = "Link this wallet to your 0xbot account. This does not authorize transactions or payments."
	}
	message := fmt.Sprintf("%s wants you to sign in with your Ethereum account:\n%s\n\n%s\n\nURI: %s\nVersion: 1\nChain ID: 1\nNonce: %s\nIssued At: %s\nExpiration Time: %s", host, common.HexToAddress(address).Hex(), statement, origin, nonce, now.Format(time.RFC3339), expires.Format(time.RFC3339))
	return Challenge{ID: nonce, Address: strings.ToLower(address), Message: message, Origin: origin, BindingUser: binding, Expires: expires}, nil
}
func VerifyChallenge(c Challenge, signature string) error {
	if time.Now().After(c.Expires) {
		return ErrAuth
	}
	m, e := siwe.ParseMessage(c.Message)
	if e != nil {
		return ErrAuth
	}
	if _, e = m.Verify(signature, nil, &c.ID, nil); e != nil {
		return ErrAuth
	}
	return nil
}
