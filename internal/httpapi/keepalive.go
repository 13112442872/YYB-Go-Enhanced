package httpapi

import (
	"context"
	"fmt"
	"log"
	"time"

	"yyb_go/internal/protocol"
	"yyb_go/internal/qr"
	"yyb_go/internal/store"
)

func (a *App) startKeepAlive() {
	if a.cfg.KeepAliveInterval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.keepAliveCancel = cancel
	a.keepAliveDone = make(chan struct{})
	log.Printf("keepalive: enabled interval=%s refresh_ahead=%s", a.cfg.KeepAliveInterval, a.cfg.KeepAliveAhead)
	go func() {
		defer close(a.keepAliveDone)
		a.keepAliveLoop(ctx)
	}()
}

func (a *App) keepAliveLoop(ctx context.Context) {
	a.refreshDueAccounts(ctx)
	ticker := time.NewTicker(a.cfg.KeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refreshDueAccounts(ctx)
		}
	}
}

func (a *App) refreshDueAccounts(ctx context.Context) {
	accounts, err := a.db.ListAccounts(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("keepalive: list accounts: %v", err)
		}
		return
	}
	for _, acc := range accounts {
		if ctx.Err() != nil {
			return
		}
		_, refreshed, err := a.refreshAccount(ctx, acc, false)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("keepalive: account id=%d refresh failed: %v", acc.ID, err)
			}
			continue
		}
		if refreshed {
			log.Printf("keepalive: account id=%d credentials renewed", acc.ID)
		}
	}
}

func (a *App) refreshLiveness(ctx context.Context, acc *store.WechatAccount) string {
	status, _, _ := a.refreshAccount(ctx, acc, true)
	return a.finishLivenessRefresh(ctx, acc, status)
}

func (a *App) refreshLivenessWithProxy(ctx context.Context, acc *store.WechatAccount, proxyValue string, fallbackDirect bool) string {
	status, _, _ := a.refreshAccountWithPolicy(ctx, acc, true, proxyValue, fallbackDirect, true)
	return a.finishLivenessRefresh(ctx, acc, status)
}

func (a *App) finishLivenessRefresh(ctx context.Context, acc *store.WechatAccount, status string) string {
	if status == "alive" {
		if avatar := a.resolveAvatar(ctx, acc.OpenID, acc.UserInfo); avatar != "" {
			_ = a.db.SetAccountProfile(ctx, acc.ID, acc.Nickname, &avatar, acc.UserInfo)
		}
	}
	return status
}

func (a *App) refreshAccount(ctx context.Context, acc *store.WechatAccount, force bool) (string, bool, error) {
	return a.refreshAccountWithPolicy(ctx, acc, force, "", false, false)
}

func (a *App) refreshAccountWithPolicy(ctx context.Context, acc *store.WechatAccount, force bool, proxyValue string, fallbackDirect, proxyResolved bool) (string, bool, error) {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()

	latest, err := a.db.GetAccount(ctx, acc.ID)
	if err != nil {
		return "unknown", false, err
	}
	if latest.Credentials == nil {
		err = fmt.Errorf("credentials are missing")
		if setErr := a.db.SetAccountStatus(ctx, latest.ID, "unknown"); setErr != nil {
			err = fmt.Errorf("%v; update status: %w", err, setErr)
		}
		return "unknown", false, err
	}

	creds := protocol.CredentialsFromMap(latest.Credentials)
	if !force && !credentialsDueForRefresh(creds, time.Now(), a.cfg.KeepAliveAhead) {
		return accountStatus(latest), false, nil
	}

	if !proxyResolved {
		proxyValue, fallbackDirect, err = a.resolveAccountProxy(ctx, latest.ID)
		if err != nil {
			return accountStatus(latest), false, fmt.Errorf("resolve account proxy: %w", err)
		}
	}
	result, err := a.refreshLoginBufferWithProxy(ctx, creds, proxyValue, fallbackDirect)
	if err != nil {
		status := accountStatus(latest)
		if force || creds.ExpiresAt <= time.Now().Unix() {
			status = "expired"
		}
		if setErr := a.db.SetAccountStatus(ctx, latest.ID, status); setErr != nil {
			err = fmt.Errorf("%v; update status: %w", err, setErr)
		}
		return status, false, err
	}
	if err = a.db.SetAccountCredentialStatus(ctx, latest.ID, result.LoginBuffer, result.Credentials.ToMap(), "alive"); err != nil {
		return "expired", false, err
	}
	return "alive", true, nil
}

func (a *App) refreshLoginBufferWithProxy(ctx context.Context, creds protocol.LoginBufferCredentials, proxyValue string, fallbackDirect bool) (protocol.LoginBufferResult, error) {
	if proxyValue == "" {
		return a.refreshLoginBuffer(ctx, creds)
	}
	client, err := qr.NewClientWithProxy(a.cfg.RequestTimeout, proxyValue, fallbackDirect)
	if err != nil {
		return protocol.LoginBufferResult{}, err
	}
	return client.RefreshLoginBuffer(ctx, creds)
}

func (a *App) fetchUserInfoWithProxy(ctx context.Context, creds protocol.LoginBufferCredentials, proxyValue string, fallbackDirect bool) (map[string]any, error) {
	if proxyValue == "" {
		return a.fetchUserInfo(ctx, creds)
	}
	client, err := qr.NewClientWithProxy(a.cfg.RequestTimeout, proxyValue, fallbackDirect)
	if err != nil {
		return nil, err
	}
	return client.LoginBuffers().FetchUserInfo(ctx, creds)
}

func credentialsDueForRefresh(creds protocol.LoginBufferCredentials, now time.Time, ahead time.Duration) bool {
	return creds.ExpiresAt <= 0 || now.Add(ahead).Unix() >= creds.ExpiresAt
}

func accountStatus(acc *store.WechatAccount) string {
	if acc.Status == nil || *acc.Status == "" {
		return "unknown"
	}
	return *acc.Status
}
