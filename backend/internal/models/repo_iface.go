package models

import "context"

type StateRepo interface {
	LoadState(ctx context.Context) (*PersistedState, error)
	SaveState(ctx context.Context, state *PersistedState) error
	IsEmpty(ctx context.Context) (bool, error)
	FindAuthAccount(ctx context.Context, email string) (AuthAccount, error)
	ListAuthAccounts(ctx context.Context) ([]AuthAccount, error)
}

type TokenStore interface {
	SaveRefresh(ctx context.Context, tokenID, email string) error
	ConsumeRefresh(ctx context.Context, tokenID string) (string, error)
	Close() error
}

type TokenIssuer interface {
	Issue(sess Session) (AuthTokens, string, error)
	ParseRefresh(token string) (tokenID, email string, err error)
}
