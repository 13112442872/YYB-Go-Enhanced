package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"yyb_go/internal/store"
)

func TestMergeYYBServerValueIsIdempotent(t *testing.T) {
	acc := &store.WechatAccount{ID: 3, OpenID: "openid-3"}
	tests := []struct {
		name     string
		existing string
		want     string
		added    bool
	}{
		{name: "empty", want: "yyb-go:8000@3", added: true},
		{name: "preserves other accounts", existing: "yyb-go:8000@1\ncustom-host:8000@4", want: "yyb-go:8000@1\ncustom-host:8000@4\nyyb-go:8000@3", added: true},
		{name: "existing id", existing: "custom-host:8000@3", want: "custom-host:8000@3", added: false},
		{name: "existing openid", existing: "custom-host:8000@openid-3", want: "custom-host:8000@openid-3", added: false},
		{name: "preserves malformed line", existing: "manual-content", want: "manual-content\nyyb-go:8000@3", added: true},
		{name: "normalizes windows lines", existing: "yyb-go:8000@1\r\n", want: "yyb-go:8000@1\nyyb-go:8000@3", added: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, added := mergeYYBServerValue(test.existing, "yyb-go:8000", acc)
			if got != test.want || added != test.added {
				t.Fatalf("mergeYYBServerValue() = %q, %v; want %q, %v", got, added, test.want, test.added)
			}
		})
	}
}

func TestRemarkUpdatesManagedNameAndSyncPreservesExistingEnv(t *testing.T) {
	fake, server := newFakeQingLong(t)
	_, handler, ref := newRunsTestApp(t, server.URL)
	fake.mu.Lock()
	fake.envs = append(fake.envs, qingLongEnv{ID: 41, Name: "YYB_SERVER", Value: "manual-line\nyyb-go:8000@8", Remarks: "keep-this-remark"})
	fake.mu.Unlock()

	enable := apiRequest(t, handler, http.MethodPut, "/api/qinglong/jobs/enable", map[string]any{
		"ref": ref, "script_key": "MDHY.js", "enabled": true,
	})
	if enable.Code != http.StatusOK {
		t.Fatalf("enable response = %d %s", enable.Code, enable.Body.String())
	}
	remark := apiRequest(t, handler, http.MethodPut, "/accounts/remark", map[string]any{"ref": ref, "remark": " Boom "})
	if remark.Code != http.StatusOK || !strings.Contains(remark.Body.String(), `"remark":"Boom"`) {
		t.Fatalf("remark response = %d %s", remark.Code, remark.Body.String())
	}

	for i := 0; i < 2; i++ {
		sync := apiRequest(t, handler, http.MethodPost, "/api/qinglong/sync", map[string]any{"ref": ref})
		if sync.Code != http.StatusOK {
			t.Fatalf("sync %d response = %d %s", i, sync.Code, sync.Body.String())
		}
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	var managedName, value, remarks string
	for _, cron := range fake.crons {
		if strings.HasPrefix(cron.Name, "[YYB:") {
			managedName = cron.Name
		}
	}
	for _, env := range fake.envs {
		if env.Name == "YYB_SERVER" {
			value, remarks = env.Value, env.Remarks
		}
	}
	if !strings.Contains(managedName, "] Boom · ") {
		t.Fatalf("managed task name = %q", managedName)
	}
	wantLine := "yyb-go:8000@" + ref
	if strings.Count(value, wantLine) != 1 || !strings.Contains(value, "manual-line") || !strings.Contains(value, "yyb-go:8000@8") {
		t.Fatalf("YYB_SERVER value = %q", value)
	}
	if remarks != "keep-this-remark" {
		t.Fatalf("YYB_SERVER remarks = %q", remarks)
	}
}

func TestQingLongConfigNeverReturnsSecret(t *testing.T) {
	_, server := newFakeQingLong(t)
	app, handler, _ := newRunsTestApp(t, server.URL)
	put := apiRequest(t, handler, http.MethodPut, "/api/qinglong/config", map[string]any{
		"url": server.URL, "client_id": "new-client", "client_secret": "very-secret-value",
	})
	if put.Code != http.StatusOK {
		t.Fatalf("config PUT = %d %s", put.Code, put.Body.String())
	}
	get := apiRequest(t, handler, http.MethodGet, "/api/qinglong/config", nil)
	if get.Code != http.StatusOK || strings.Contains(get.Body.String(), "very-secret-value") || !strings.Contains(get.Body.String(), `"secret_configured":true`) {
		t.Fatalf("config GET = %d %s", get.Code, get.Body.String())
	}
	persisted, err := app.db.GetSetting(context.Background(), qingLongSecretSetting)
	if err != nil || persisted != "very-secret-value" {
		t.Fatalf("persisted secret = %q, %v", persisted, err)
	}

	badURL := apiRequest(t, handler, http.MethodPut, "/api/qinglong/config", map[string]any{
		"url": "file:///tmp/qinglong", "client_id": "x", "client_secret": "x",
	})
	if badURL.Code != http.StatusBadRequest {
		t.Fatalf("invalid URL response = %d %s", badURL.Code, badURL.Body.String())
	}
}

func TestAccountRemarkRejectsUnsafeTaskNameText(t *testing.T) {
	_, server := newFakeQingLong(t)
	_, handler, ref := newRunsTestApp(t, server.URL)
	for _, remark := range []string{"line one\nline two", strings.Repeat("字", 81)} {
		response := apiRequest(t, handler, http.MethodPut, "/accounts/remark", map[string]any{"ref": ref, "remark": remark})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("remark %q response = %d %s", remark, response.Code, response.Body.String())
		}
	}
}
