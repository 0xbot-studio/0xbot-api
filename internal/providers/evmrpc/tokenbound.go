package evmrpc

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/big"
	"strings"
	"time"

	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/tokenbound"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const proxyHeader = "363d3d373d3d3d363d73"
const proxyFooter = "5af43d82803e903d91602b57fd5bf3"
const creationHeader = "3d60ad80600a3d3981f3"

// ERC-6551 canonical registry CREATE2 preimage. Inputs must be validated.
func accountCode(r tokenbound.ResolveRequest) []byte {
	chain, _ := new(big.Int).SetString(strings.TrimPrefix(r.Network, "eip155:"), 10)
	id, _ := new(big.Int).SetString(r.TokenID, 10)
	code, _ := hex.DecodeString(proxyHeader + r.Implementation[2:] + proxyFooter + r.Salt[2:])
	code = append(code, chain.FillBytes(make([]byte, 32))...)
	code = append(code, common.LeftPadBytes(common.HexToAddress(r.TokenContract).Bytes(), 32)...)
	return append(code, id.FillBytes(make([]byte, 32))...)
}
func accountAddress(r tokenbound.ResolveRequest) string {
	header, _ := hex.DecodeString(creationHeader)
	initCode := append(header, accountCode(r)...)
	salt := common.HexToHash(r.Salt)
	return strings.ToLower(crypto.CreateAddress2(common.HexToAddress(tokenbound.Registry), salt, crypto.Keccak256(initCode)).Hex())
}
func decodeAccount(code []byte, network string) (tokenbound.ResolveRequest, error) {
	if len(code) != 173 || hex.EncodeToString(code[:10]) != proxyHeader || hex.EncodeToString(code[30:45]) != proxyFooter || !bytes.Equal(code[109:121], make([]byte, 12)) {
		return tokenbound.ResolveRequest{}, tokenbound.ErrNotAccount
	}
	chain := new(big.Int).SetBytes(code[77:109]).String()
	r := tokenbound.ResolveRequest{Network: "eip155:" + chain, Implementation: "0x" + hex.EncodeToString(code[10:30]), Salt: "0x" + hex.EncodeToString(code[45:77]), TokenContract: "0x" + hex.EncodeToString(code[121:141]), TokenID: new(big.Int).SetBytes(code[141:173]).String()}
	if r.Network != network || r.Validate() != nil {
		return tokenbound.ResolveRequest{}, tokenbound.ErrNotAccount
	}
	return r, nil
}
func (p *Provider) codeAt(ctx context.Context, address, block string) ([]byte, error) {
	raw, err := p.call(ctx, "eth_getCode", []any{address, block})
	if err != nil {
		return nil, err
	}
	code, err := hex.DecodeString(raw[2:])
	if err != nil {
		return nil, data.ErrUnavailable
	}
	return code, nil
}
func (p *Provider) tokenBoundSnapshot(ctx context.Context, r tokenbound.ResolveRequest, address, block string, deployed bool) (tokenbound.Account, error) {
	implementation, err := p.codeAt(ctx, r.Implementation, block)
	if err != nil {
		return tokenbound.Account{}, err
	}
	b, err := quantity(block)
	if err != nil {
		return tokenbound.Account{}, err
	}
	verification := "counterfactual"
	if deployed {
		verification = "canonical_proxy"
	}
	return tokenbound.Account{Address: address, Network: r.Network, Registry: tokenbound.Registry, TokenContract: r.TokenContract, TokenID: r.TokenID, Implementation: r.Implementation, Salt: r.Salt, Deployed: deployed, ImplementationDeployed: len(implementation) > 0, Verification: verification, BlockNumber: b.String(), ObservedAt: time.Now().UTC()}, nil
}
func (p *Provider) ResolveTokenBoundAccount(ctx context.Context, r tokenbound.ResolveRequest) (tokenbound.Account, error) {
	if err := r.Validate(); err != nil {
		return tokenbound.Account{}, err
	}
	block, err := p.block(ctx, data.Request{Network: r.Network})
	if err != nil {
		return tokenbound.Account{}, err
	}
	address := accountAddress(r)
	code, err := p.codeAt(ctx, address, block)
	if err != nil {
		return tokenbound.Account{}, err
	}
	if len(code) > 0 && !bytes.Equal(code, accountCode(r)) {
		return tokenbound.Account{}, tokenbound.ErrNotAccount
	}
	return p.tokenBoundSnapshot(ctx, r, address, block, len(code) > 0)
}
func (p *Provider) GetTokenBoundAccount(ctx context.Context, r tokenbound.GetRequest) (tokenbound.Account, error) {
	if err := r.Validate(); err != nil {
		return tokenbound.Account{}, err
	}
	block, err := p.block(ctx, data.Request{Network: r.Network})
	if err != nil {
		return tokenbound.Account{}, err
	}
	code, err := p.codeAt(ctx, r.Address, block)
	if err != nil {
		return tokenbound.Account{}, err
	}
	binding, err := decodeAccount(code, r.Network)
	if err != nil {
		return tokenbound.Account{}, err
	}
	if accountAddress(binding) != r.Address {
		return tokenbound.Account{}, tokenbound.ErrNotAccount
	}
	return p.tokenBoundSnapshot(ctx, binding, r.Address, block, true)
}

var _ tokenbound.Provider = (*Provider)(nil)
