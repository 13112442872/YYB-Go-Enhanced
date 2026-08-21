package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerServesGinRoutesAndSwaggerDocs(t *testing.T) {
	t.Setenv("GIN_MODE", "test")

	app, err := NewApp(Config{
		ResourceRoot:   t.TempDir(),
		RequestTimeout: time.Second,
		AvatarTimeout:  time.Second,
		SessionTTL:     time.Minute,
		QRSessionTTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	defer app.Close()

	handler := app.Handler()

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d", health.Code)
	}
	var healthBody struct {
		Code int            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(health.Body.Bytes(), &healthBody); err != nil {
		t.Fatalf("decode health JSON: %v", err)
	}
	if healthBody.Code != 0 || healthBody.Msg != "success" || healthBody.Data["ok"] != true {
		t.Fatalf("GET /health body = %#v", healthBody)
	}

	openapi := httptest.NewRecorder()
	handler.ServeHTTP(openapi, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if openapi.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json status = %d", openapi.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(openapi.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI JSON: %v", err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("openapi version = %v", spec["openapi"])
	}
	if _, ok := spec["code"]; ok {
		t.Fatalf("OpenAPI JSON should not be wrapped in API envelope")
	}
	components := spec["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	wxappResponse := schemas["WxappResponse"].(map[string]any)
	wxappProperties := wxappResponse["properties"].(map[string]any)
	accountRef := wxappProperties["account"].(map[string]any)["$ref"]
	if accountRef != "#/components/schemas/WxappAccountLabel" {
		t.Fatalf("OpenAPI WxappResponse account schema = %v", accountRef)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI paths missing or invalid")
	}
	for _, path := range []string{"/quick-login", "/quick-login/{session_id}/confirm", "/wx/code", "/wx/getuserinfo", "/wx/encryptkey", "/wx/getphonenumber", "/wx/cloud", "/wx/qrcodeauth", "/wx/mpgeta8key", "/wx/appmsgext", "/wx/appmsglike", "/wxapp/getCode", "/wxapp/getPhoneNumber", "/wxapp/operateWxData", "/accounts/avatar", "/accounts/remark", "/accounts/proxy", "/accounts/proxy/test", "/api/proxy-profiles", "/api/proxy-profiles/{id}", "/api/proxy-profiles/areas/provinces", "/api/proxy-profiles/areas/cities", "/api/qinglong/config", "/api/qinglong/sync", "/api/qinglong/jobs", "/api/qinglong/push"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI path %s missing", path)
		}
	}
	for _, path := range []string{"/wxapp/getCode", "/wxapp/getPhoneNumber", "/wxapp/operateWxData"} {
		pathItem := paths[path].(map[string]any)
		post := pathItem["post"].(map[string]any)
		tags := post["tags"].([]any)
		if len(tags) != 1 || tags[0] != "wxapp" {
			t.Fatalf("OpenAPI path %s tags = %#v, want [wxapp]", path, tags)
		}
	}
	for _, path := range []string{"/wx/code", "/wx/encryptkey", "/wx/getphonenumber", "/wx/cloud", "/wx/mpgeta8key", "/wx/appmsgext", "/wx/appmsglike"} {
		pathItem := paths[path].(map[string]any)
		post := pathItem["post"].(map[string]any)
		tags := post["tags"].([]any)
		if len(tags) != 1 || tags[0] != "wx" {
			t.Fatalf("OpenAPI path %s tags = %#v, want [wx]", path, tags)
		}
	}
	encryptPost := paths["/wx/encryptkey"].(map[string]any)["post"].(map[string]any)
	encryptSchema := encryptPost["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if encryptSchema["$ref"] != "#/components/schemas/OperateWXDataRequest" {
		t.Fatalf("OpenAPI /wx/encryptkey request schema = %#v", encryptSchema)
	}
	for _, path := range []string{"/accounts/{ref}", "/accounts/{ref}/getCode", "/accounts/{ref}/getPhoneNumber", "/accounts/{ref}/operateWxData", "/accounts/getCode", "/accounts/getPhoneNumber", "/accounts/operateWxData"} {
		if _, ok := paths[path]; ok {
			t.Fatalf("OpenAPI still exposes old account feature route %s", path)
		}
	}
	if _, ok := paths["/features"]; ok {
		t.Fatalf("OpenAPI still exposes /features")
	}

	docs := httptest.NewRecorder()
	handler.ServeHTTP(docs, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if docs.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /docs status = %d", docs.Code)
	}
	if got := docs.Header().Get("Location"); got != "/docs/index.html" {
		t.Fatalf("GET /docs Location = %q", got)
	}

	proxies := httptest.NewRecorder()
	handler.ServeHTTP(proxies, httptest.NewRequest(http.MethodGet, "/proxies", nil))
	if proxies.Code != http.StatusOK || !strings.Contains(proxies.Body.String(), "代理设置") {
		t.Fatalf("GET /proxies = %d %s", proxies.Code, proxies.Body.String())
	}
	proxiesPost := httptest.NewRecorder()
	handler.ServeHTTP(proxiesPost, httptest.NewRequest(http.MethodPost, "/proxies", nil))
	if proxiesPost.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /proxies status = %d", proxiesPost.Code)
	}

	features := httptest.NewRecorder()
	handler.ServeHTTP(features, httptest.NewRequest(http.MethodGet, "/features", nil))
	if features.Code != http.StatusNotFound {
		t.Fatalf("GET /features status = %d", features.Code)
	}
	var notFoundBody struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data any    `json:"data"`
	}
	if err := json.Unmarshal(features.Body.Bytes(), &notFoundBody); err != nil {
		t.Fatalf("decode /features error JSON: %v", err)
	}
	if notFoundBody.Code == 0 || notFoundBody.Msg == "" || notFoundBody.Data != nil {
		t.Fatalf("GET /features body = %#v", notFoundBody)
	}

	oldPath := httptest.NewRecorder()
	handler.ServeHTTP(oldPath, httptest.NewRequest(http.MethodPost, "/accounts/getCode", nil))
	if oldPath.Code != http.StatusNotFound {
		t.Fatalf("POST old account feature route status = %d", oldPath.Code)
	}
	for _, path := range []string{"/wx/code", "/wx/encryptkey", "/wx/getphonenumber", "/wx/cloud", "/wx/mpgeta8key", "/wx/appmsgext", "/wx/appmsglike", "/wx/qrcodeauth"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET %s status = %d, want %d", path, recorder.Code, http.StatusMethodNotAllowed)
		}
	}
	encryptKey := httptest.NewRecorder()
	encryptKeyRequest := httptest.NewRequest(http.MethodPost, "/wx/encryptkey", strings.NewReader(`{"ref":"1","app_id":"wx0000000000000000"}`))
	encryptKeyRequest.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(encryptKey, encryptKeyRequest)
	if encryptKey.Code != http.StatusBadRequest || !strings.Contains(encryptKey.Body.String(), "payload is required") {
		t.Fatalf("POST /wx/encryptkey without payload = %d %s", encryptKey.Code, encryptKey.Body.String())
	}
	userinfo := httptest.NewRecorder()
	handler.ServeHTTP(userinfo, httptest.NewRequest(http.MethodGet, "/wx/getuserinfo", nil))
	if userinfo.Code != http.StatusBadRequest {
		t.Fatalf("GET /wx/getuserinfo without ref status = %d, want %d", userinfo.Code, http.StatusBadRequest)
	}
}

func TestAuthMeWithoutConfiguredAuthentication(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	app, err := NewApp(Config{
		ResourceRoot:   t.TempDir(),
		RequestTimeout: time.Second,
		AvatarTimeout:  time.Second,
		SessionTTL:     time.Minute,
		QRSessionTTL:   time.Minute,
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	defer app.Close()

	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Code int `json:"code"`
		Data struct {
			AuthEnabled bool `json:"auth_enabled"`
			User        struct {
				Username    string `json:"username"`
				DisplayName string `json:"display_name"`
				Role        string `json:"role"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode /api/auth/me JSON: %v", err)
	}
	if response.Code != 0 || response.Data.AuthEnabled || response.Data.User.Username != "local" || response.Data.User.DisplayName == "" || response.Data.User.Role != "admin" {
		t.Fatalf("GET /api/auth/me body = %#v", response)
	}
	for _, path := range []string{"/settings", "/users"} {
		page := httptest.NewRecorder()
		app.Handler().ServeHTTP(page, httptest.NewRequest(http.MethodGet, path, nil))
		if page.Code != http.StatusSeeOther || page.Header().Get("Location") != "/" {
			t.Fatalf("GET %s status = %d, Location = %q", path, page.Code, page.Header().Get("Location"))
		}
	}
}

func TestSQLiteAuthFirstRegistrationAndUnauthorizedAPI(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	app, err := NewApp(Config{
		ResourceRoot: t.TempDir(),
		AuthDriver:   "sqlite",
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	defer app.Close()
	handler := app.Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/auth/me status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	register := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(`{"username":"owner","displayName":"Owner","password":"owner-password"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(register, request)
	if register.Code != http.StatusCreated {
		t.Fatalf("POST /register status = %d body = %s", register.Code, register.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Next string `json:"next"`
			User struct {
				Role string `json:"role"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(register.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if body.Code != 0 || body.Data.User.Role != "admin" || body.Data.Next != "/" {
		t.Fatalf("POST /register body = %#v", body)
	}
	result := register.Result()
	if len(result.Cookies()) != 1 {
		t.Fatalf("POST /register cookies = %d, want 1", len(result.Cookies()))
	}

	index := httptest.NewRecorder()
	indexRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	indexRequest.AddCookie(result.Cookies()[0])
	handler.ServeHTTP(index, indexRequest)
	if index.Code != http.StatusOK {
		t.Fatalf("authenticated GET / status = %d", index.Code)
	}
}
