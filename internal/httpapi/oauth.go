package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"yyb_go/internal/oauth"
	"yyb_go/internal/store"
)

type publicOAuthRequest struct {
	Ref            string `json:"ref"`
	AppID          string `json:"appid"`
	LegacyAppID    string `json:"app_id"`
	RedirectURI    string `json:"redirect_uri"`
	Scope          string `json:"scope"`
	State          string `json:"state"`
	ComponentAppID string `json:"component_appid"`
}

func (a *App) handlePublicOAuth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/wx/oauth" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body publicOAuthRequest
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if body.Ref == "" {
		writeError(w, http.StatusBadRequest, "ref is required")
		return
	}
	if body.AppID == "" {
		body.AppID = body.LegacyAppID
	}
	acc, ok := a.resolveAccountRef(w, r, body.Ref)
	if !ok {
		return
	}
	result, err := oauth.Build(oauth.Request{
		AppID:          body.AppID,
		RedirectURI:    body.RedirectURI,
		Scope:          body.Scope,
		State:          body.State,
		ComponentAppID: body.ComponentAppID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	protocolResult, err := a.invokeWXApp(r.Context(), acc, body.AppID, map[string]any{
		"scope": result.Scope, "state": result.State, "request_url": result.FullURL,
	}, a.invokePublicOAuth)
	if err == nil {
		protocolResult["oauth_provider"] = "native"
	} else if a.oauthUpstream != nil {
		upstreamResult, upstreamErr := a.oauthUpstream.authorize(r.Context(), acc, oauthUpstreamRequest{
			AppID: body.AppID, RedirectURI: body.RedirectURI, Scope: result.Scope,
			State: result.State, ComponentAppID: body.ComponentAppID,
		})
		if upstreamErr == nil {
			protocolResult = upstreamResult
			err = nil
		} else {
			err = fmt.Errorf("native protocol failed: %v; upstream fallback failed: %w", err, upstreamErr)
		}
	}
	if err != nil {
		var expired accountExpiredError
		if errors.As(err, &expired) {
			writeError(w, http.StatusConflict, "account login_buffer expired (refresh failed); re-scan required")
			return
		}
		writeError(w, http.StatusBadGateway, "OAuth authorization failed: "+err.Error())
		return
	}
	protocolResult["account_id"] = acc.ID
	protocolResult["openid"] = acc.OpenID
	protocolResult["appid"] = body.AppID
	protocolResult["scope"] = result.Scope
	protocolResult["state"] = result.State
	protocolResult["request_url"] = result.FullURL
	writeJSON(w, http.StatusOK, protocolResult)
}

func (a *App) invokePublicOAuth(ctx context.Context, account *store.WechatAccount, appID string, payload map[string]any) (map[string]any, error) {
	return a.authorizeOAuth(ctx, account, appID, stringFromAny(payload["scope"]), stringFromAny(payload["state"]), stringFromAny(payload["request_url"]))
}
