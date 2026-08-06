package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"yyb_go/internal/store"
)

func TestPublicOAuthReturnsProtocolCode(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	app, err := NewApp(Config{ResourceRoot: t.TempDir(), RequestTimeout: time.Second, QRSessionTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	app.authorizeOAuth = func(_ context.Context, _ *store.WechatAccount, appID, scope, state, requestURL string) (map[string]any, error) {
		if appID != "wx1234567890abcdef" || scope != "snsapi_base" || state == "" || !strings.Contains(requestURL, "#wechat_redirect") {
			t.Fatalf("unexpected protocol request: appid=%q scope=%q state=%q url=%q", appID, scope, state, requestURL)
		}
		return map[string]any{
			"code": "oauth-code", "url": "https://example.com/callback?code=oauth-code", "full_url": "https://example.com/callback?code=oauth-code",
		}, nil
	}
	defer app.Close()
	status := "alive"
	account, err := app.db.UpsertAccount(context.Background(), "openid-oauth-test", "login-buffer", nil, nil, nil, nil, nil, &status)
	if err != nil {
		t.Fatal(err)
	}
	uin := int64(123)
	if err := app.db.PutSession(context.Background(), account.ID, &uin, map[string]any{"test": true}, time.Now().Add(time.Minute).Unix(), ""); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"ref":"` + strconv.FormatInt(account.ID, 10) + `","appid":"wx1234567890abcdef","redirect_uri":"https://example.com/callback","scope":"snsapi_base"}`)
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/wx/oauth", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Code int `json:"code"`
		Data struct {
			FullURL string  `json:"full_url"`
			State   string  `json:"state"`
			Code    *string `json:"code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 0 || response.Data.Code == nil || *response.Data.Code != "oauth-code" || response.Data.State == "" || !strings.Contains(response.Data.FullURL, "code=oauth-code") {
		t.Fatalf("unexpected OAuth response: %#v", response)
	}
}
