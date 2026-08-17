package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"yyb_go/internal/proxysource"
	"yyb_go/internal/qr"
	"yyb_go/internal/store"
)

type accountProxyIn struct {
	Ref         string `json:"ref"`
	Mode        string `json:"mode"`
	ProxyType   string `json:"proxy_type"`
	StaticProxy string `json:"static_proxy"`
	APIURL      string `json:"api_url"`
}

type qrLoginSession struct {
	Session   *qr.Session
	Client    *qr.Client
	ProxySpec proxysource.Spec
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

func proxySettingPublic(setting *store.AccountProxySetting) map[string]any {
	return map[string]any{
		"account_id": setting.AccountID, "mode": setting.Mode, "proxy_type": setting.ProxyType,
		"static_proxy": setting.StaticProxy, "api_url": setting.APIURL,
		"configured": setting.Mode != "direct", "updated_at": setting.UpdatedAt,
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
		writeJSON(w, http.StatusOK, proxySettingPublic(setting))
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
		normalized, err := proxysource.NormalizeSpec(body.spec())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		setting, err := a.db.UpsertAccountProxySetting(r.Context(), acc.ID, normalized.Mode, normalized.ProxyType, normalized.StaticProxy, normalized.APIURL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.db.InvalidateAccountSessions(r.Context(), acc.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, proxySettingPublic(setting))
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
	spec := body.spec()
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
		spec = proxySpecFromSetting(setting)
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
	proxyValue, err := a.resolveProxySpec(ctx, proxySpecFromSetting(setting))
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

func (a *App) saveAccountProxySpec(ctx context.Context, accountID int64, spec proxysource.Spec) error {
	normalized, err := proxysource.NormalizeSpec(spec)
	if err != nil {
		return err
	}
	_, err = a.db.UpsertAccountProxySetting(ctx, accountID, normalized.Mode, normalized.ProxyType, normalized.StaticProxy, normalized.APIURL)
	return err
}
