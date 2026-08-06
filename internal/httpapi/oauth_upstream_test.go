package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yyb_go/internal/store"
)

func TestOAuthUpstreamAuthorizeUsesMappedOpenID(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-API-Key") != "secret-key" {
			t.Fatalf("unexpected API key header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["openid"] != "upstream-openid" || body["appid"] != "wx1234567890abcdef" || body["component_appid"] != "wxabcdef1234567890" {
			t.Fatalf("unexpected upstream body: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true,"data":{"code":"upstream-code","full_url":"https://example.com/callback?code=upstream-code"}}`))
	}))
	defer server.Close()

	client, err := newOAuthUpstreamClient(Config{
		RequestTimeout: time.Second, OAuthUpstreamURL: server.URL, OAuthUpstreamAPIKey: "secret-key",
		OAuthUpstreamOpenIDMap: `{"7":"upstream-openid"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.authorize(context.Background(), &store.WechatAccount{ID: 7, OpenID: "local-openid"}, oauthUpstreamRequest{
		AppID: "wx1234567890abcdef", RedirectURI: "https://example.com/callback", Scope: "snsapi_base",
		State: "state-1", ComponentAppID: "wxabcdef1234567890",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result["code"] != "upstream-code" || result["oauth_provider"] != "upstream" {
		t.Fatalf("calls=%d result=%#v", calls, result)
	}
}

func TestDecodeOAuthUpstreamResponseReportsBillingErrors(t *testing.T) {
	_, err := decodeOAuthUpstreamResponse(http.StatusPaymentRequired, []byte(`{"status":false,"message":"insufficient credits","data":{"credits":0,"freeCallsRemaining":0}}`))
	if err == nil || !strings.Contains(err.Error(), "HTTP 402") || !strings.Contains(err.Error(), "credits=0") || !strings.Contains(err.Error(), "freeCallsRemaining=0") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOAuthUpstreamRequiresURLAndAPIKeyTogether(t *testing.T) {
	if _, err := newOAuthUpstreamClient(Config{OAuthUpstreamURL: "https://example.com"}); err == nil {
		t.Fatal("expected incomplete configuration error")
	}
}
