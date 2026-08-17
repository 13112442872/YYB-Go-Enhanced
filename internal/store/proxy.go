package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type AccountProxySetting struct {
	AccountID   int64  `json:"account_id"`
	Mode        string `json:"mode"`
	ProxyType   string `json:"proxy_type"`
	StaticProxy string `json:"static_proxy"`
	APIURL      string `json:"api_url"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

func (db *DB) GetAccountProxySetting(ctx context.Context, accountID int64) (*AccountProxySetting, error) {
	setting := &AccountProxySetting{}
	err := db.sql.QueryRowContext(ctx, `
SELECT account_id, mode, proxy_type, static_proxy, api_url, created_at, updated_at
FROM account_proxy_settings WHERE account_id=?`, accountID).Scan(
		&setting.AccountID, &setting.Mode, &setting.ProxyType, &setting.StaticProxy,
		&setting.APIURL, &setting.CreatedAt, &setting.UpdatedAt,
	)
	return setting, err
}

func (db *DB) AccountProxySettingOrDefault(ctx context.Context, accountID int64) (*AccountProxySetting, error) {
	setting, err := db.GetAccountProxySetting(ctx, accountID)
	if err == nil {
		return setting, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &AccountProxySetting{AccountID: accountID, Mode: "direct", ProxyType: "http"}, nil
}

func (db *DB) UpsertAccountProxySetting(ctx context.Context, accountID int64, mode, proxyType, staticProxy, apiURL string) (*AccountProxySetting, error) {
	now := time.Now().Unix()
	_, err := db.sql.ExecContext(ctx, `
INSERT INTO account_proxy_settings(account_id, mode, proxy_type, static_proxy, api_url, created_at, updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(account_id) DO UPDATE SET
mode=excluded.mode, proxy_type=excluded.proxy_type, static_proxy=excluded.static_proxy,
api_url=excluded.api_url, updated_at=excluded.updated_at`,
		accountID, mode, proxyType, staticProxy, apiURL, now, now,
	)
	if err != nil {
		return nil, err
	}
	return db.GetAccountProxySetting(ctx, accountID)
}
