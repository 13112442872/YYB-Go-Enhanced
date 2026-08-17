package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAccountProxyAPIAndDirectOverride(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	app, err := NewApp(Config{
		ResourceRoot:   t.TempDir(),
		RequestTimeout: time.Second,
		TCPProxy:       "http-connect://global.example:8080",
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	defer app.Close()
	status := "alive"
	account, err := app.db.UpsertAccount(context.Background(), "proxy-openid", "buffer", nil, nil, nil, nil, nil, &status)
	if err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}

	proxyValue, fallbackDirect, err := app.resolveAccountProxy(context.Background(), account.ID)
	if err != nil || proxyValue != "http-connect://global.example:8080" || !fallbackDirect {
		t.Fatalf("global proxy = %q, fallback=%v, err=%v", proxyValue, fallbackDirect, err)
	}

	handler := app.Handler()
	response := apiRequest(t, handler, http.MethodPut, "/accounts/proxy", map[string]any{
		"ref": fmt.Sprint(account.ID), "mode": "direct", "proxy_type": "http",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("PUT /accounts/proxy status = %d body=%s", response.Code, response.Body.String())
	}
	proxyValue, fallbackDirect, err = app.resolveAccountProxy(context.Background(), account.ID)
	if err != nil || proxyValue != "" || fallbackDirect {
		t.Fatalf("direct override = %q, fallback=%v, err=%v", proxyValue, fallbackDirect, err)
	}
}

func TestAccountProxyAPIParsesJSON2AndCascades(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	proxyAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"proxy_list":[{"server_ip":"203.0.113.30","proxy_port":"9010","account":"city-user","pwd":"city-pass"}]}}`))
	}))
	defer proxyAPI.Close()

	app, err := NewApp(Config{ResourceRoot: t.TempDir(), RequestTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	defer app.Close()
	status := "alive"
	account, err := app.db.UpsertAccount(context.Background(), "json2-openid", "buffer", nil, nil, nil, nil, nil, &status)
	if err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	handler := app.Handler()
	payload := map[string]any{
		"ref": fmt.Sprint(account.ID), "mode": "api", "proxy_type": "socks5", "api_url": proxyAPI.URL + "?province=山东&city=济南",
	}
	if response := apiRequest(t, handler, http.MethodPut, "/accounts/proxy", payload); response.Code != http.StatusOK {
		t.Fatalf("PUT /accounts/proxy status = %d body=%s", response.Code, response.Body.String())
	}
	tested := apiRequest(t, handler, http.MethodPost, "/accounts/proxy/test", payload)
	if tested.Code != http.StatusOK {
		t.Fatalf("POST /accounts/proxy/test status = %d body=%s", tested.Code, tested.Body.String())
	}
	var result struct {
		Data struct {
			Resolved bool   `json:"resolved"`
			Proxy    string `json:"proxy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(tested.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode proxy test response: %v", err)
	}
	if !result.Data.Resolved || result.Data.Proxy != "socks5://203.0.113.30:9010" {
		t.Fatalf("proxy test response = %#v", result.Data)
	}

	if err := app.db.DeleteAccount(context.Background(), account.ID); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if _, err := app.db.GetAccountProxySetting(context.Background(), account.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("proxy setting after account deletion error = %v", err)
	}
}
