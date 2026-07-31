package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeQingLong struct {
	mu       sync.Mutex
	crons    []qingLongCron
	envs     []qingLongEnv
	nextCron int64
	nextEnv  int64
	runIDs   []int64
	commands []string
}

func intPointer(value int) *int { return &value }

func newFakeQingLong(t *testing.T) (*fakeQingLong, *httptest.Server) {
	t.Helper()
	fake := &fakeQingLong{
		nextCron: 100,
		nextEnv:  50,
		envs:     []qingLongEnv{},
		crons: []qingLongCron{
			{ID: 1, Name: "美的会员", Command: "task SuperNaiBA_YYB-GO-Script/MDHY.js", Schedule: "11 8 * * *", Status: 1, IsDisabled: intPointer(1)},
			{ID: 2, Name: "EOOS", Command: "task SuperNaiBA_YYB-GO-Script/eoos/eoos_checkin.py", Schedule: "30 8 * * *", Status: 1, IsDisabled: intPointer(1)},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	return fake, server
}

func (f *fakeQingLong) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data}) }
	if r.URL.Path == "/open/auth/token" {
		write(map[string]any{"token": "fake-token", "expiration": 3600})
		return
	}
	if r.Header.Get("Authorization") != "Bearer fake-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/open/crons":
		write(f.crons)
	case r.Method == http.MethodPost && r.URL.Path == "/open/crons":
		var in qingLongCron
		_ = json.NewDecoder(r.Body).Decode(&in)
		in.ID = f.nextCron
		f.nextCron++
		in.Status = 1
		in.IsDisabled = intPointer(0)
		f.crons = append(f.crons, in)
		f.commands = append(f.commands, in.Command)
		write(in)
	case r.Method == http.MethodPut && r.URL.Path == "/open/crons":
		var in qingLongCron
		_ = json.NewDecoder(r.Body).Decode(&in)
		for i := range f.crons {
			if f.crons[i].ID == in.ID {
				f.crons[i].Name, f.crons[i].Command, f.crons[i].Schedule = in.Name, in.Command, in.Schedule
				f.commands = append(f.commands, in.Command)
			}
		}
		write(nil)
	case r.Method == http.MethodPut && (r.URL.Path == "/open/crons/enable" || r.URL.Path == "/open/crons/disable"):
		var ids []int64
		_ = json.NewDecoder(r.Body).Decode(&ids)
		disabled := 1
		if strings.HasSuffix(r.URL.Path, "/enable") {
			disabled = 0
		}
		for i := range f.crons {
			for _, id := range ids {
				if f.crons[i].ID == id {
					f.crons[i].IsDisabled = intPointer(disabled)
				}
			}
		}
		write(nil)
	case r.Method == http.MethodPut && r.URL.Path == "/open/crons/run":
		var ids []int64
		_ = json.NewDecoder(r.Body).Decode(&ids)
		f.runIDs = append(f.runIDs, ids...)
		write(nil)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/log"):
		write("fake account log")
	case r.Method == http.MethodGet && r.URL.Path == "/open/envs":
		write(f.envs)
	case r.Method == http.MethodPost && r.URL.Path == "/open/envs":
		var in []qingLongEnv
		_ = json.NewDecoder(r.Body).Decode(&in)
		for i := range in {
			in[i].ID = f.nextEnv
			f.nextEnv++
			f.envs = append(f.envs, in[i])
		}
		write(in)
	case r.Method == http.MethodPut && r.URL.Path == "/open/envs":
		var in qingLongEnv
		_ = json.NewDecoder(r.Body).Decode(&in)
		for i := range f.envs {
			if f.envs[i].ID == in.ID {
				f.envs[i].Name, f.envs[i].Value, f.envs[i].Remarks = in.Name, in.Value, in.Remarks
			}
		}
		write(nil)
	case r.Method == http.MethodPut && (r.URL.Path == "/open/envs/enable" || r.URL.Path == "/open/envs/disable"):
		write(nil)
	default:
		w.WriteHeader(http.StatusNotFound)
		write(nil)
	}
}

func newRunsTestApp(t *testing.T, qlURL string) (*App, http.Handler, string) {
	t.Helper()
	app, err := NewApp(Config{
		ResourceRoot:     t.TempDir(),
		RequestTimeout:   time.Second,
		SessionTTL:       time.Minute,
		QRSessionTTL:     time.Minute,
		QingLongURL:      qlURL,
		QingLongClientID: "client-id",
		QingLongSecret:   "client-secret",
		QingLongServer:   "yyb-go:8000",
		QingLongRepo:     "SuperNaiBA_YYB-GO-Script",
	})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	status := "alive"
	acc, err := app.db.UpsertAccount(context.Background(), "test-openid", "buffer", nil, nil, nil, nil, nil, &status)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	return app, app.Handler(), fmt.Sprintf("%d", acc.ID)
}

func apiRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestAccountJobsAreIsolatedDisabledByDefaultAndRunExplicitly(t *testing.T) {
	fake, server := newFakeQingLong(t)
	_, handler, ref := newRunsTestApp(t, server.URL)

	list := apiRequest(t, handler, http.MethodGet, "/api/qinglong/jobs?ref="+url.QueryEscape(ref), nil)
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "eoos_checkin") {
		t.Fatalf("initial jobs response = %d %s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), "MDHY.js") {
		t.Fatalf("compatible script missing: %s", list.Body.String())
	}

	enable := apiRequest(t, handler, http.MethodPut, "/api/qinglong/jobs/enable", map[string]any{
		"ref": ref, "script_key": "MDHY.js", "enabled": true,
	})
	if enable.Code != http.StatusOK {
		t.Fatalf("enable response = %d %s", enable.Code, enable.Body.String())
	}
	fake.mu.Lock()
	if len(fake.runIDs) != 0 {
		t.Fatalf("enabling a task unexpectedly ran it: %v", fake.runIDs)
	}
	if len(fake.commands) == 0 {
		t.Fatal("managed task command was not created")
	}
	command := fake.commands[len(fake.commands)-1]
	fake.mu.Unlock()
	if !strings.Contains(command, "YYB_SERVER='yyb-go:8000@"+ref+"'") || !strings.HasSuffix(command, "task SuperNaiBA_YYB-GO-Script/MDHY.js") {
		t.Fatalf("managed command = %q", command)
	}

	run := apiRequest(t, handler, http.MethodPost, "/api/qinglong/jobs/run", map[string]any{
		"ref": ref, "script_key": "MDHY.js",
	})
	if run.Code != http.StatusAccepted {
		t.Fatalf("run response = %d %s", run.Code, run.Body.String())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.runIDs) != 1 {
		t.Fatalf("explicit run IDs = %v", fake.runIDs)
	}
}

func TestAccountJobUsesCurrentQingLongStateFields(t *testing.T) {
	fake, server := newFakeQingLong(t)
	_, handler, ref := newRunsTestApp(t, server.URL)
	_ = apiRequest(t, handler, http.MethodPut, "/api/qinglong/jobs/enable", map[string]any{
		"ref": ref, "script_key": "MDHY.js", "enabled": true,
	})

	fake.mu.Lock()
	fake.crons[0].IsDisabled = intPointer(0)
	for i := range fake.crons {
		if strings.HasPrefix(fake.crons[i].Name, "[YYB:") {
			fake.crons[i].Status = 1
			fake.crons[i].PID = 12345
			fake.crons[i].IsDisabled = intPointer(0)
		}
	}
	fake.mu.Unlock()

	idle := apiRequest(t, handler, http.MethodGet, "/api/qinglong/jobs?ref="+url.QueryEscape(ref), nil)
	if idle.Code != http.StatusOK || !strings.Contains(idle.Body.String(), `"enabled":true`) || !strings.Contains(idle.Body.String(), `"running":false`) {
		t.Fatalf("idle current QingLong job response = %d %s", idle.Code, idle.Body.String())
	}
	if !strings.Contains(idle.Body.String(), `"global_task_active":true`) {
		t.Fatalf("current QingLong enabled source was not detected: %s", idle.Body.String())
	}

	fake.mu.Lock()
	for i := range fake.crons {
		if strings.HasPrefix(fake.crons[i].Name, "[YYB:") {
			fake.crons[i].Status = 0.5
		}
	}
	fake.mu.Unlock()

	queued := apiRequest(t, handler, http.MethodGet, "/api/qinglong/jobs?ref="+url.QueryEscape(ref), nil)
	if queued.Code != http.StatusOK || !strings.Contains(queued.Body.String(), `"running":true`) {
		t.Fatalf("queued current QingLong job response = %d %s", queued.Code, queued.Body.String())
	}
}

func TestPushSecretStaysInQingLongEnvironment(t *testing.T) {
	fake, server := newFakeQingLong(t)
	_, handler, ref := newRunsTestApp(t, server.URL)
	_ = apiRequest(t, handler, http.MethodPut, "/api/qinglong/jobs/enable", map[string]any{
		"ref": ref, "script_key": "MDHY.js", "enabled": true,
	})
	secret := "SCT_FAKE_SECRET_VALUE"
	save := apiRequest(t, handler, http.MethodPut, "/api/qinglong/push", map[string]any{
		"ref": ref, "channel": "serverchan", "token": secret,
	})
	if save.Code != http.StatusOK {
		t.Fatalf("save push response = %d %s", save.Code, save.Body.String())
	}
	if strings.Contains(save.Body.String(), secret) || strings.Contains(save.Body.String(), "token_env_name") {
		t.Fatalf("push response leaked secret metadata: %s", save.Body.String())
	}
	if !strings.Contains(save.Body.String(), `"token_configured":true`) {
		t.Fatalf("push response does not report configured state: %s", save.Body.String())
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	foundSecret := false
	for _, env := range fake.envs {
		if env.Value == secret {
			foundSecret = true
		}
	}
	if !foundSecret {
		t.Fatal("secret was not stored in QingLong environment")
	}
	for _, command := range fake.commands {
		if strings.Contains(command, secret) {
			t.Fatalf("task command leaked secret: %q", command)
		}
	}
	if !strings.Contains(fake.commands[len(fake.commands)-1], "${YYB_RUN_ACCOUNT_"+ref+"_SERVERCHAN_KEY:-}") {
		t.Fatalf("task command does not reference account environment: %q", fake.commands[len(fake.commands)-1])
	}
}
