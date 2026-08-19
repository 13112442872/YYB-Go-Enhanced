package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"yyb_go/internal/protocol"
	"yyb_go/internal/proxysource"
	"yyb_go/internal/qr"
	"yyb_go/internal/store"
)

type accountProxyIn struct {
	Ref                 string `json:"ref"`
	Mode                string `json:"mode"`
	ProxyType           string `json:"proxy_type"`
	StaticProxy         string `json:"static_proxy"`
	APIURL              string `json:"api_url"`
	ProviderProfileID   *int64 `json:"provider_profile_id"`
	RegionCode          string `json:"region_code"`
	RegionProvince      string `json:"region_province"`
	RegionCity          string `json:"region_city"`
	RefreshAheadMinutes int64  `json:"refresh_ahead_minutes"`
}

type qrLoginSession struct {
	Session   *qr.Session
	Client    *qr.Client
	ProxySpec proxysource.Spec
	ProxyIn   accountProxyIn
}

func (body accountProxyIn) spec() proxysource.Spec {
	return proxysource.Spec{Mode: body.Mode, ProxyType: body.ProxyType, StaticProxy: body.StaticProxy, APIURL: body.APIURL}
}

func proxySpecFromSetting(setting *store.AccountProxySetting) proxysource.Spec {
	if setting == nil {
		return proxysource.Spec{Mode: "direct", ProxyType: "http"}
	}
	return proxysource.Spec{
		Mode: setting.Mode, ProxyType: setting.ProxyType,
		StaticProxy: setting.StaticProxy, APIURL: setting.APIURL,
	}
}

func proxySettingPublic(setting *store.AccountProxySetting, account *store.WechatAccount) map[string]any {
	expiresIn := protocol.CredentialsFromMap(account.Credentials).ExpiresIn
	tokenTTLMinutes := (expiresIn + 59) / 60
	return map[string]any{
		"account_id": setting.AccountID, "mode": setting.Mode, "proxy_type": setting.ProxyType,
		"static_proxy": setting.StaticProxy, "api_url": setting.APIURL,
		"provider_profile_id": setting.ProviderProfileID,
		"region_code":         setting.RegionCode, "region_province": setting.RegionProvince, "region_city": setting.RegionCity,
		"refresh_ahead_minutes": setting.RefreshAheadSeconds / 60,
		"token_ttl_minutes":     tokenTTLMinutes,
		"configured":            setting.Mode != "direct", "updated_at": setting.UpdatedAt,
	}
}

func (a *App) handleAccountProxy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		acc, ok := a.resolveAccountFromQuery(w, r)
		if !ok {
			return
		}
		setting, err := a.db.AccountProxySettingOrDefault(r.Context(), acc.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, proxySettingPublic(setting, acc))
	case http.MethodPut:
		var body accountProxyIn
		if err := decodeOptionalJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		acc, ok := a.resolveAccountRef(w, r, body.Ref)
		if !ok {
			return
		}
		normalizedBody, normalized, err := a.normalizeAccountProxyInput(r.Context(), body)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		setting, err := a.saveAccountProxyInput(r.Context(), acc.ID, normalizedBody, normalized)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.db.InvalidateAccountSessions(r.Context(), acc.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, proxySettingPublic(setting, acc))
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleAccountProxyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body accountProxyIn
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	_, spec, err := a.normalizeAccountProxyInput(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Ref) != "" && strings.TrimSpace(body.Mode) == "" {
		acc, ok := a.resolveAccountRef(w, r, body.Ref)
		if !ok {
			return
		}
		setting, err := a.db.AccountProxySettingOrDefault(r.Context(), acc.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		spec, err = a.proxySpecForSetting(r.Context(), setting)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	resolved, err := a.resolveProxySpec(r.Context(), spec)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolved": resolved != "", "proxy": proxysource.Mask(resolved)})
}

func (a *App) resolveProxySpec(ctx context.Context, spec proxysource.Spec) (string, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Timeout: a.cfg.RequestTimeout, Transport: transport}
	return proxysource.Resolve(ctx, client, spec)
}

func (a *App) resolveAccountProxy(ctx context.Context, accountID int64) (string, bool, error) {
	setting, err := a.db.GetAccountProxySetting(ctx, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		if strings.TrimSpace(a.cfg.TCPProxy) == "" {
			return "", false, nil
		}
		normalized, normalizeErr := proxysource.NormalizeSpec(proxysource.Spec{Mode: "static", StaticProxy: a.cfg.TCPProxy})
		if normalizeErr != nil {
			return "", false, normalizeErr
		}
		return normalized.StaticProxy, true, nil
	}
	if err != nil {
		return "", false, err
	}
	spec, err := a.proxySpecForSetting(ctx, setting)
	if err != nil {
		return "", false, err
	}
	proxyValue, err := a.resolveProxySpec(ctx, spec)
	return proxyValue, false, err
}

func (a *App) qrClientForSpec(ctx context.Context, spec proxysource.Spec) (*qr.Client, string, error) {
	resolved, err := a.resolveProxySpec(ctx, spec)
	if err != nil {
		return nil, "", err
	}
	if resolved == "" {
		return a.qr, "", nil
	}
	client, err := qr.NewClientWithProxy(a.cfg.RequestTimeout, resolved, false)
	if err != nil {
		return nil, "", fmt.Errorf("创建代理客户端失败: %w", err)
	}
	return client, resolved, nil
}

func (a *App) saveAccountProxyInput(ctx context.Context, accountID int64, body accountProxyIn, normalized proxysource.Spec) (*store.AccountProxySetting, error) {
	refreshAheadSeconds := body.RefreshAheadMinutes * 60
	apiURL := normalized.APIURL
	if body.ProviderProfileID != nil {
		apiURL = ""
	}
	return a.db.UpsertAccountProxySetting(ctx, accountID, normalized.Mode, normalized.ProxyType,
		normalized.StaticProxy, apiURL, body.ProviderProfileID,
		strings.TrimSpace(body.RegionCode), strings.TrimSpace(body.RegionProvince), strings.TrimSpace(body.RegionCity),
		refreshAheadSeconds)
}

func (a *App) accountExistsBeforeScan(ctx context.Context, openID string) (bool, error) {
	_, err := a.db.GetAccountByOpenID(ctx, openID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (a *App) saveNewAccountProxy(ctx context.Context, accountID int64, existed bool, body accountProxyIn, normalized proxysource.Spec) error {
	if existed {
		return nil
	}
	_, err := a.saveAccountProxyInput(ctx, accountID, body, normalized)
	return err
}
