package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"yyb_go/internal/store"
)

const maxOAuthUpstreamResponse = 1 << 20

type oauthUpstreamClient struct {
	endpoint      string
	apiKey        string
	defaultOpenID string
	openIDMap     map[string]string
	client        *http.Client
}

type oauthUpstreamRequest struct {
	AppID          string
	RedirectURI    string
	Scope          string
	State          string
	ComponentAppID string
}

func newOAuthUpstreamClient(cfg Config) (*oauthUpstreamClient, error) {
	endpoint, apiKey := strings.TrimSpace(cfg.OAuthUpstreamURL), strings.TrimSpace(cfg.OAuthUpstreamAPIKey)
	if endpoint == "" && apiKey == "" {
		return nil, nil
	}
	if endpoint == "" || apiKey == "" {
		return nil, fmt.Errorf("YYB_OAUTH_UPSTREAM_URL and YYB_OAUTH_UPSTREAM_API_KEY must be configured together")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("YYB_OAUTH_UPSTREAM_URL must be an http(s) URL without credentials")
	}
	if !strings.HasSuffix(parsed.Path, "/wx/oauth") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/wx/oauth"
	}
	openIDMap := map[string]string{}
	if raw := strings.TrimSpace(cfg.OAuthUpstreamOpenIDMap); raw != "" {
		if err := json.Unmarshal([]byte(raw), &openIDMap); err != nil {
			return nil, fmt.Errorf("invalid YYB_OAUTH_UPSTREAM_OPENID_MAP: %w", err)
		}
	}
	return &oauthUpstreamClient{
		endpoint: parsed.String(), apiKey: apiKey, defaultOpenID: strings.TrimSpace(cfg.OAuthUpstreamOpenID),
		openIDMap: openIDMap, client: &http.Client{Timeout: cfg.RequestTimeout},
	}, nil
}

func (c *oauthUpstreamClient) authorize(ctx context.Context, account *store.WechatAccount, input oauthUpstreamRequest) (map[string]any, error) {
	openid := c.openIDFor(account)
	body := map[string]any{
		"openid": openid, "appid": input.AppID, "redirect_uri": input.RedirectURI,
		"scope": input.Scope, "state": input.State,
	}
	if input.ComponentAppID != "" {
		body["component_appid"] = input.ComponentAppID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxOAuthUpstreamResponse))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return decodeOAuthUpstreamResponse(resp.StatusCode, responseBody)
}

func (c *oauthUpstreamClient) openIDFor(account *store.WechatAccount) string {
	for _, key := range []string{strconv.FormatInt(account.ID, 10), account.OpenID} {
		if value := strings.TrimSpace(c.openIDMap[key]); value != "" {
			return value
		}
	}
	if c.defaultOpenID != "" {
		return c.defaultOpenID
	}
	return account.OpenID
}

func decodeOAuthUpstreamResponse(statusCode int, raw []byte) (map[string]any, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("HTTP %d returned invalid JSON", statusCode)
	}
	message := stringFromAny(envelope["message"])
	data, _ := envelope["data"].(map[string]any)
	if statusCode < 200 || statusCode >= 300 || envelope["status"] == false {
		if detail := upstreamErrorDetail(data); detail != "" {
			message += " (" + detail + ")"
		}
		if message == "" {
			message = http.StatusText(statusCode)
		}
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, message)
	}
	if data == nil {
		data = envelope
	}
	code := stringFromAny(data["code"])
	if code == "" {
		return nil, fmt.Errorf("HTTP %d returned no OAuth code", statusCode)
	}
	result := map[string]any{"code": code, "oauth_provider": "upstream"}
	for _, key := range []string{"url", "full_url", "action_code"} {
		if value, ok := data[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func upstreamErrorDetail(data map[string]any) string {
	if data == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, key := range []string{"error", "credits", "freeCallsRemaining"} {
		if value, ok := data[key]; ok && fmt.Sprint(value) != "" {
			parts = append(parts, key+"="+fmt.Sprint(value))
		}
	}
	return strings.Join(parts, " ")
}
