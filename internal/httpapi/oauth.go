package httpapi

import (
	"net/http"
	"path/filepath"

	"yyb_go/internal/oauth"
)

const fallbackOAuthHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>公众号网页授权</title><h1>公众号网页授权页面缺失</h1><p>请检查 resource/templates/oauth.html 是否存在。</p><p><a href="/">返回控制台</a></p>`

func (a *App) handleOAuthPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/oauth" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	serveFileOrText(w, r, filepath.Join(a.resources.Templates, "oauth.html"), fallbackOAuthHTML)
}

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

	writeJSON(w, http.StatusOK, map[string]any{
		"account_id":             acc.ID,
		"openid":                 acc.OpenID,
		"appid":                  body.AppID,
		"scope":                  result.Scope,
		"state":                  result.State,
		"url":                    result.URL,
		"full_url":               result.FullURL,
		"code":                   nil,
		"authorization_required": true,
		"callback_code_source":   "redirect_uri",
		"mode":                   "authorization_url",
	})
}
