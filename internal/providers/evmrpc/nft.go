package evmrpc

import (
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/nft"
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"
)

func tokenWord(id string) string {
	n, _ := new(big.Int).SetString(id, 10)
	return hex.EncodeToString(n.FillBytes(make([]byte, 32)))
}
func (p *Provider) nftCall(ctx context.Context, contract, block, input string) (string, error) {
	return p.rpc(ctx, "eth_call", []any{map[string]string{"to": contract, "data": input, "gas": "0x7a120"}, block})
}
func (p *Provider) supportsNFTInterface(ctx context.Context, contract, block, id string) (bool, error) {
	raw, err := p.nftCall(ctx, contract, block, "0x01ffc9a7"+id+strings.Repeat("0", 56))
	if err != nil {
		return false, err
	}
	n, err := abiWord(raw)
	if err != nil || n.Cmp(big.NewInt(1)) > 0 {
		return false, data.ErrUnavailable
	}
	return n.Sign() != 0, nil
}
func (p *Provider) nftStandard(ctx context.Context, r nft.TokenRequest) (string, string, error) {
	block, err := p.block(ctx, data.Request{Network: r.Network})
	if err != nil {
		return "", "", err
	}
	code, err := p.codeAt(ctx, r.Contract, block)
	if err != nil {
		return "", "", err
	}
	if len(code) == 0 {
		return "", "", nft.ErrUnsupported
	}
	for _, check := range []struct {
		id   string
		want bool
	}{{"01ffc9a7", true}, {"ffffffff", false}} {
		ok, err := p.supportsNFTInterface(ctx, r.Contract, block, check.id)
		if errors.Is(err, nft.ErrReverted) {
			return "", "", nft.ErrUnsupported
		}
		if err != nil {
			return "", "", err
		}
		if ok != check.want {
			return "", "", nft.ErrUnsupported
		}
	}
	seven, err := p.supportsNFTInterface(ctx, r.Contract, block, "80ac58cd")
	if err != nil {
		return "", "", err
	}
	eleven, err := p.supportsNFTInterface(ctx, r.Contract, block, "d9b67a26")
	if err != nil {
		return "", "", err
	}
	if seven == eleven {
		return "", "", nft.ErrUnsupported
	} // ambiguous dual-standard contracts are not guessed
	if seven {
		return "erc721", block, nil
	}
	return "erc1155", block, nil
}
func (p *Provider) nftOwnerAt(ctx context.Context, r nft.TokenRequest, block string) (string, error) {
	raw, err := p.nftCall(ctx, r.Contract, block, "0x6352211e"+tokenWord(r.TokenID))
	if err != nil {
		return "", err
	}
	n, err := abiWord(raw)
	if err != nil || n.BitLen() > 160 {
		return "", data.ErrUnavailable
	}
	if n.Sign() == 0 {
		return "", nft.ErrReverted
	}
	return "0x" + hex.EncodeToString(n.FillBytes(make([]byte, 20))), nil
}
func (p *Provider) NFTOwner(ctx context.Context, r nft.TokenRequest) (nft.Owner, error) {
	if err := r.Validate(); err != nil {
		return nft.Owner{}, err
	}
	standard, block, err := p.nftStandard(ctx, r)
	if err != nil {
		return nft.Owner{}, err
	}
	if standard != "erc721" {
		return nft.Owner{}, nft.ErrUnsupported
	}
	owner, err := p.nftOwnerAt(ctx, r, block)
	if err != nil {
		return nft.Owner{}, err
	}
	b, _ := quantity(block)
	return nft.Owner{Network: r.Network, Contract: r.Contract, TokenID: r.TokenID, Standard: standard, Owner: owner, BlockNumber: b.String(), ObservedAt: time.Now().UTC()}, nil
}
func (p *Provider) NFTBalance(ctx context.Context, r nft.BalanceRequest) (nft.Balance, error) {
	if err := r.Validate(); err != nil {
		return nft.Balance{}, err
	}
	standard, block, err := p.nftStandard(ctx, r.Token())
	if err != nil {
		return nft.Balance{}, err
	}
	amount := "0"
	if standard == "erc721" {
		owner, err := p.nftOwnerAt(ctx, r.Token(), block)
		if err != nil {
			return nft.Balance{}, err
		}
		if owner == r.Address {
			amount = "1"
		}
	} else {
		raw, err := p.nftCall(ctx, r.Contract, block, "0x00fdd58e"+strings.Repeat("0", 24)+r.Address[2:]+tokenWord(r.TokenID))
		if err != nil {
			return nft.Balance{}, err
		}
		n, err := abiWord(raw)
		if err != nil {
			return nft.Balance{}, err
		}
		amount = n.String()
	}
	b, _ := quantity(block)
	return nft.Balance{Network: r.Network, Contract: r.Contract, TokenID: r.TokenID, Standard: standard, Address: r.Address, Amount: amount, BlockNumber: b.String(), ObservedAt: time.Now().UTC()}, nil
}

// Bounded single-string ABI decoding; reject offsets, truncation and invalid UTF-8.
func nftString(raw string) (string, error) {
	if !strings.HasPrefix(raw, "0x") || len(raw) > 32770 {
		return "", data.ErrUnavailable
	}
	b, err := hex.DecodeString(raw[2:])
	if err != nil || len(b) < 64 || len(b)%32 != 0 {
		return "", data.ErrUnavailable
	}
	offset := new(big.Int).SetBytes(b[:32])
	length := new(big.Int).SetBytes(b[32:64])
	if offset.Cmp(big.NewInt(32)) != 0 || !length.IsUint64() || length.Uint64() > 8192 {
		return "", data.ErrUnavailable
	}
	n := int(length.Uint64())
	if len(b) != 64+(n+31)/32*32 || !utf8.Valid(b[64:64+n]) {
		return "", data.ErrUnavailable
	}
	for _, v := range b[64+n:] {
		if v != 0 {
			return "", data.ErrUnavailable
		}
	}
	return string(b[64 : 64+n]), nil
}
func (p *Provider) NFTDetail(ctx context.Context, r nft.TokenRequest) (nft.Detail, error) {
	if err := r.Validate(); err != nil {
		return nft.Detail{}, err
	}
	standard, block, err := p.nftStandard(ctx, r)
	if err != nil {
		return nft.Detail{}, err
	}
	b, _ := quantity(block)
	out := nft.Detail{Network: r.Network, Contract: r.Contract, TokenID: r.TokenID, Standard: standard, BlockNumber: b.String(), ObservedAt: time.Now().UTC(), Metadata: nft.Metadata{Status: "not_supported"}}
	iface, selector := "0e89341c", "0x0e89341c" // ERC-1155 metadata URI interface and uri(uint256)
	if standard == "erc721" {
		if _, err = p.nftOwnerAt(ctx, r, block); err != nil {
			return nft.Detail{}, err
		}
		out.ExistenceVerified = true
		iface, selector = "5b5e139f", "0xc87b56dd"
	}
	metadata, err := p.supportsNFTInterface(ctx, r.Contract, block, iface)
	if err != nil {
		return nft.Detail{}, err
	}
	if !metadata {
		return out, nil
	}
	readString := func(input string) (string, error) {
		raw, err := p.nftCall(ctx, r.Contract, block, input)
		if err != nil {
			return "", err
		}
		return nftString(raw)
	}
	if standard == "erc721" {
		out.CollectionName, err = readString("0x06fdde03")
		if err != nil {
			return nft.Detail{}, err
		}
		out.CollectionSymbol, err = readString("0x95d89b41")
		if err != nil {
			return nft.Detail{}, err
		}
	}
	out.TokenURI, err = readString(selector + tokenWord(r.TokenID))
	if err != nil {
		return nft.Detail{}, err
	}
	if standard == "erc1155" {
		out.TokenURI = strings.ReplaceAll(out.TokenURI, "{id}", tokenWord(r.TokenID))
	}
	out.Metadata = nft.Metadata{Status: "unavailable", SourceURI: out.TokenURI}
	if out.TokenURI != "" && p.metadata != nil {
		out.Metadata = p.metadata.Read(ctx, out.TokenURI)
	}
	if ctx.Err() != nil {
		return nft.Detail{}, ctx.Err()
	}
	return out, nil
}

var _ nft.Provider = (*Provider)(nil)
