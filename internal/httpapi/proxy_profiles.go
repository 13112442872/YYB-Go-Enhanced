package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"yyb_go/internal/proxysource"
	"yyb_go/internal/store"
)

const ipzanHost = "service.ipzan.com"

type proxyProfileIn struct {
	Name              string `json:"name"`
	Provider          string `json:"provider"`
	ProxyType         string `json:"proxy_type"`
	APIURL            string `json:"api_url"`
	AuthorizationMode string `json:"authorization_mode"`
}

type proxyArea struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (a *App) handleProxyProfiles(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/proxy-profiles"), "/")
	if path == "areas/provinces" {
		a.handleIPZanAreas(w, r, "https://service.ipzan.com/area-get-province")
		return
	}
	if path == "areas/cities" {
		province := strings.TrimSpace(r.URL.Query().Get("province"))
		if len(province) != 6 || !digitsOnly(province) {
			writeError(w, http.StatusBadRequest, "province must be a 6-digit area code")
			return
		}
		a.handleIPZanAreas(w, r, "https://service.ipzan.com/area-find-citys?province="+url.QueryEscape(province))
		return
	}
	if path == "" {
		a.handleProxyProfileCollection(w, r)
		return
	}
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "proxy profile not found")
		return
	}
	a.handleProxyProfileItem(w, r, id)
}

func (a *App) handleProxyProfileCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		profiles, err := a.db.ListProxyProviderProfiles(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, profiles)
	case http.MethodPost:
		body, ok := decodeProxyProfile(w, r)
		if !ok {
			return
		}
		profile, err := a.db.CreateProxyProviderProfile(r.Context(), body.Name, body.Provider, body.ProxyType, body.APIURL)
		if err != nil {
			writeProxyProfileStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, profile)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) handleProxyProfileItem(w http.ResponseWriter, r *http.Request, id int64) {
	switch r.Method {
	case http.MethodPut:
		body, ok := decodeProxyProfile(w, r)
		if !ok {
			return
		}
		profile, err := a.db.UpdateProxyProviderProfile(r.Context(), id, body.Name, body.Provider, body.ProxyType, body.APIURL)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "proxy profile not found")
			return
		}
		if err != nil {
			writeProxyProfileStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, profile)
	case http.MethodDelete:
		count, err := a.db.CountAccountsUsingProxyProfile(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if count > 0 {
			writeError(w, http.StatusConflict, fmt.Sprintf("该配置仍被 %d 个账号使用，请先切换这些账号", count))
			return
		}
		if _, err := a.db.GetProxyProviderProfile(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "proxy profile not found")
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.db.DeleteProxyProviderProfile(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func decodeProxyProfile(w http.ResponseWriter, r *http.Request) (proxyProfileIn, bool) {
	var body proxyProfileIn
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return proxyProfileIn{}, false
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Provider = strings.ToLower(strings.TrimSpace(body.Provider))
	body.ProxyType = strings.ToLower(strings.TrimSpace(body.ProxyType))
	body.AuthorizationMode = strings.ToLower(strings.TrimSpace(body.AuthorizationMode))
	if body.Provider == "" {
		body.Provider = "ipzan"
	}
	if body.ProxyType == "" {
		body.ProxyType = "http"
	}
	if body.Name == "" || len([]rune(body.Name)) > 50 {
		writeError(w, http.StatusBadRequest, "配置名称不能为空且不能超过 50 个字符")
		return proxyProfileIn{}, false
	}
	if body.Provider != "ipzan" {
		writeError(w, http.StatusBadRequest, "目前配置库仅支持品赞代理")
		return proxyProfileIn{}, false
	}
	apiURL, err := normalizeIPZanURL(body.APIURL, body.ProxyType, body.AuthorizationMode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return proxyProfileIn{}, false
	}
	body.APIURL = apiURL
	return body, true
}

func normalizeIPZanURL(raw, proxyType, authorizationMode string) (string, error) {
	if proxyType != "http" && proxyType != "socks5" {
		return "", fmt.Errorf("代理类型必须为 http 或 socks5")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), ipzanHost) || u.Port() != "" || u.User != nil || u.Path != "/core-extract" {
		return "", fmt.Errorf("请填写 service.ipzan.com/core-extract 的 HTTPS 提取链接")
	}
	query := u.Query()
	if strings.TrimSpace(query.Get("no")) == "" || strings.TrimSpace(query.Get("secret")) == "" {
		return "", fmt.Errorf("品赞提取链接必须包含 no 和 secret")
	}
	query.Del("area")
	query.Set("num", "1")
	if authorizationMode == "" {
		if strings.EqualFold(strings.TrimSpace(query.Get("mode")), "auth") {
			authorizationMode = "auth"
		} else {
			authorizationMode = "whitelist"
		}
	}
	switch authorizationMode {
	case "auth":
		query.Set("mode", "auth")
		query.Set("format", "json")
	case "whitelist":
		query.Del("mode")
	default:
		return "", fmt.Errorf("品赞授权方式必须为 whitelist 或 auth")
	}
	if strings.TrimSpace(query.Get("format")) == "" {
		query.Set("format", "json")
	}
	if proxyType == "socks5" {
		query.Set("protocol", "3")
	} else {
		query.Set("protocol", "1")
	}
	u.RawQuery = query.Encode()
	u.Fragment = ""
	return u.String(), nil
}

func ipzanURLForRegion(profile *store.ProxyProviderProfile, regionCode string) (string, error) {
	base, err := normalizeIPZanURL(profile.APIURL, profile.ProxyType, "")
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(base)
	query := u.Query()
	regionCode = strings.TrimSpace(regionCode)
	if regionCode != "" && regionCode != "all" {
		if len(regionCode) != 6 || !digitsOnly(regionCode) {
			return "", fmt.Errorf("品赞地区编码必须为 6 位数字")
		}
		query.Set("area", regionCode)
	} else {
		query.Del("area")
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (a *App) normalizeAccountProxyInput(ctx context.Context, body accountProxyIn) (accountProxyIn, proxysource.Spec, error) {
	body.RegionCode = strings.TrimSpace(body.RegionCode)
	body.RegionProvince = strings.TrimSpace(body.RegionProvince)
	body.RegionCity = strings.TrimSpace(body.RegionCity)
	if body.RefreshAheadMinutes == 0 {
		body.RefreshAheadMinutes = store.DefaultProxyRefreshAheadSeconds / 60
	}
	if body.RefreshAheadMinutes < 5 || body.RefreshAheadMinutes > 90 {
		return accountProxyIn{}, proxysource.Spec{}, fmt.Errorf("代理账号提前刷新时间必须在 5 到 90 分钟之间")
	}
	if body.ProviderProfileID != nil {
		profile, err := a.db.GetProxyProviderProfile(ctx, *body.ProviderProfileID)
		if errors.Is(err, sql.ErrNoRows) {
			return accountProxyIn{}, proxysource.Spec{}, fmt.Errorf("品赞代理配置不存在")
		}
		if err != nil {
			return accountProxyIn{}, proxysource.Spec{}, err
		}
		apiURL, err := ipzanURLForRegion(profile, body.RegionCode)
		if err != nil {
			return accountProxyIn{}, proxysource.Spec{}, err
		}
		body.Mode = "api"
		body.ProxyType = profile.ProxyType
		body.StaticProxy = ""
		body.APIURL = ""
		normalized, err := proxysource.NormalizeSpec(proxysource.Spec{Mode: "api", ProxyType: profile.ProxyType, APIURL: apiURL})
		return body, normalized, err
	}
	normalized, err := proxysource.NormalizeSpec(body.spec())
	return body, normalized, err
}

func (a *App) proxySpecForSetting(ctx context.Context, setting *store.AccountProxySetting) (proxysource.Spec, error) {
	if setting == nil || setting.ProviderProfileID == nil {
		return proxysource.NormalizeSpec(proxySpecFromSetting(setting))
	}
	profile, err := a.db.GetProxyProviderProfile(ctx, *setting.ProviderProfileID)
	if err != nil {
		return proxysource.Spec{}, err
	}
	apiURL, err := ipzanURLForRegion(profile, setting.RegionCode)
	if err != nil {
		return proxysource.Spec{}, err
	}
	return proxysource.NormalizeSpec(proxysource.Spec{Mode: "api", ProxyType: profile.ProxyType, APIURL: apiURL})
}

func (a *App) handleIPZanAreas(w http.ResponseWriter, r *http.Request, endpoint string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	client := &http.Client{Timeout: a.cfg.RequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取品赞地区失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("品赞地区接口返回 HTTP %d", resp.StatusCode))
		return
	}
	var payload struct {
		Data []proxyArea `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadGateway, "无法解析品赞地区响应")
		return
	}
	writeJSON(w, http.StatusOK, payload.Data)
}

func writeProxyProfileStoreError(w http.ResponseWriter, err error) {
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		writeError(w, http.StatusConflict, "代理配置名称已存在")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func digitsOnly(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != ""
}
