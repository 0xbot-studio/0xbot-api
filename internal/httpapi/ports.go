package httpapi

import (
	"0xbot.org/api/internal/apikeys"
	"0xbot.org/api/internal/data"
	"0xbot.org/api/internal/identity"
	"0xbot.org/api/internal/mcpoauth"
	"0xbot.org/api/internal/nft"
	"0xbot.org/api/internal/operations"
	"0xbot.org/api/internal/prices"
	"0xbot.org/api/internal/routing"
	"0xbot.org/api/internal/tokenbound"
	"context"
	"time"
)

// Consumer-owned ports keep transport code independent of database and provider implementations.
type readiness interface{ Ping(context.Context) error }
type oauthPort interface {
	Register(context.Context, mcpoauth.Registration) (mcpoauth.Client, error)
	Begin(context.Context, mcpoauth.Authorization, string) (string, error)
	Consent(context.Context, string, string) (mcpoauth.Consent, error)
	Decide(context.Context, string, string, string, bool) (string, error)
	ExchangeCode(context.Context, mcpoauth.Exchange) (mcpoauth.Tokens, error)
	Refresh(context.Context, mcpoauth.Exchange) (mcpoauth.Tokens, error)
	Authenticate(context.Context, string) (string, error)
	List(context.Context, string) ([]mcpoauth.Grant, error)
	RevokeGrant(context.Context, string, string) error
	RevokeToken(context.Context, string, string) error
}
type identityPort interface {
	SaveChallenge(context.Context, identity.Challenge) error
	Challenge(context.Context, string) (identity.Challenge, error)
	ConsumeChallenge(context.Context, string) error
	FindOrCreate(context.Context, string, string, string, *string) (string, error)
	CreateSession(context.Context, string, string) (string, error)
	Session(context.Context, string) (identity.Session, error)
	Revoke(context.Context, string) error
	MFA(context.Context, string) error
	SaveOAuth(context.Context, string, identity.OAuthState) error
	ConsumeOAuth(context.Context, string, string) (identity.OAuthState, error)
	Allow(context.Context, string, int, time.Duration) (bool, error)
}
type keyPort interface {
	Create(context.Context, string, string) (apikeys.Created, error)
	List(context.Context, string) ([]apikeys.Key, error)
	Revoke(context.Context, string, string) error
	Authenticate(context.Context, string) (string, error)
}
type operationReader interface {
	List(context.Context, string, int, int) ([]operations.Record, error)
	Usage(context.Context, string) (operations.Usage, error)
}
type balanceUseCase interface {
	AssetPrice(context.Context, string, prices.AssetRequest) (prices.Result, error)
	TokenPrice(context.Context, string, prices.TokenRequest) (prices.Result, error)
	EquityPrice(context.Context, string, prices.EquityRequest) (prices.Result, error)
	NFTDetail(context.Context, string, nft.TokenRequest) (nft.DetailResult, error)
	NFTOwner(context.Context, string, nft.TokenRequest) (nft.OwnerResult, error)
	NFTBalance(context.Context, string, nft.BalanceRequest) (nft.BalanceResult, error)
	ResolveTokenBoundAccount(context.Context, string, tokenbound.ResolveRequest) (tokenbound.Result, error)
	GetTokenBoundAccount(context.Context, string, tokenbound.GetRequest) (tokenbound.Result, error)
	Balance(context.Context, string, data.Request) (data.Result, error)
	TokenBalance(context.Context, string, data.TokenRequest) (data.Result, error)
	Balances(context.Context, string, data.Request) (data.BalancesResult, error)
	RWATokenBalance(context.Context, string, data.EquityTokenRequest) (data.Result, error)
}
type settingsPort interface {
	List(context.Context) ([]routing.Setting, error)
	Update(context.Context, string, routing.Setting) error
	Audit(context.Context, int, int) ([]map[string]any, error)
}
type routerObserver interface {
	Health(string) string
	Describe(string) routing.Metadata
	Knows(string) bool
}
