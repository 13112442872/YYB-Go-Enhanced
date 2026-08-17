package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAccountProxySettingLifecycle(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "yyb.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	status := "alive"
	account, err := db.UpsertAccount(context.Background(), "openid-proxy", "buffer", nil, nil, nil, nil, nil, &status)
	if err != nil {
		t.Fatalf("UpsertAccount() error = %v", err)
	}
	setting, err := db.AccountProxySettingOrDefault(context.Background(), account.ID)
	if err != nil || setting.Mode != "direct" {
		t.Fatalf("default setting = %#v, %v", setting, err)
	}
	setting, err = db.UpsertAccountProxySetting(context.Background(), account.ID, "api", "http", "", "https://proxy.example/get?city=jinan")
	if err != nil || setting.Mode != "api" || setting.APIURL == "" {
		t.Fatalf("saved setting = %#v, %v", setting, err)
	}
}
