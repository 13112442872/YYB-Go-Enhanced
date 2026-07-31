package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"yyb_go/internal/store"
)

var validScriptKey = regexp.MustCompile(`^[\p{L}\p{N}_+./-]+\.(?:js|py)$`)

type scriptSource struct {
	Key      string
	Name     string
	Schedule string
	Cron     qingLongCron
}

type accountJobPublic struct {
	ScriptKey        string `json:"script_key"`
	Name             string `json:"name"`
	Schedule         string `json:"schedule"`
	Provisioned      bool   `json:"provisioned"`
	Enabled          bool   `json:"enabled"`
	Running          bool   `json:"running"`
	QLCronID         int64  `json:"ql_cron_id,omitempty"`
	LastExecutionAt  int64  `json:"last_execution_at"`
	LastRunningTime  int64  `json:"last_running_time"`
	GlobalTaskActive bool   `json:"global_task_active"`
}

type jobActionIn struct {
	Ref       string `json:"ref"`
	ScriptKey string `json:"script_key"`
	Enabled   bool   `json:"enabled"`
}

type pushSettingIn struct {
	Ref     string  `json:"ref"`
	Channel string  `json:"channel"`
	Token   string  `json:"token"`
	Topic   *string `json:"topic"`
}

type pushSettingPublic struct {
	Channel         string `json:"channel"`
	TokenConfigured bool   `json:"token_configured"`
	TopicConfigured bool   `json:"topic_configured"`
}

func (a *App) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	serveFileOrText(w, r, filepath.Join(a.resources.Templates, "runs.html"), fallbackRunsHTML)
}

func (a *App) handleQingLongStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.qinglong.configured() {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "connected": false})
		return
	}
	if err := a.qinglong.status(r.Context()); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": true, "connected": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "connected": true})
}

func (a *App) handleQingLongJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	acc, ok := a.resolveAccountFromQuery(w, r)
	if !ok {
		return
	}
	sources, cronsByID, err := a.scriptCatalog(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	storedJobs, err := a.db.ListAccountScriptJobs(r.Context(), acc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jobsByKey := make(map[string]store.AccountScriptJob, len(storedJobs))
	for _, job := range storedJobs {
		jobsByKey[job.ScriptKey] = job
	}
	out := make([]accountJobPublic, 0, len(sources))
	for _, source := range sources {
		item := accountJobPublic{
			ScriptKey:        source.Key,
			Name:             source.Name,
			Schedule:         source.Schedule,
			GlobalTaskActive: source.Cron.Status == 0,
		}
		if job, exists := jobsByKey[source.Key]; exists {
			if cron, found := cronsByID[job.QLCronID]; found {
				item.Provisioned = true
				item.Enabled = cron.Status == 0
				item.Running = cron.PID != nil
				item.QLCronID = cron.ID
				item.LastExecutionAt = cron.LastExecutionTime
				item.LastRunningTime = cron.LastRunningTime
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"account": acc.Public(),
		"jobs":    out,
		"count":   len(out),
	})
}

func (a *App) handleQingLongJobEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body jobActionIn
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	acc, ok := a.resolveAccountRef(w, r, body.Ref)
	if !ok {
		return
	}
	if !body.Enabled {
		job, err := a.db.GetAccountScriptJob(r.Context(), acc.ID, body.ScriptKey)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "provisioned": false})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := a.qinglong.setCronsEnabled(r.Context(), []int64{job.QLCronID}, false); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "provisioned": true, "ql_cron_id": job.QLCronID})
		return
	}
	job, _, err := a.ensureAccountJob(r.Context(), acc, body.ScriptKey)
	if err != nil {
		writeRunError(w, err)
		return
	}
	if err := a.qinglong.setCronsEnabled(r.Context(), []int64{job.QLCronID}, true); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "provisioned": true, "ql_cron_id": job.QLCronID})
}

func (a *App) handleQingLongJobRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body jobActionIn
	if err := decodeOptionalJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	acc, ok := a.resolveAccountRef(w, r, body.Ref)
	if !ok {
		return
	}
	job, source, err := a.ensureAccountJob(r.Context(), acc, body.ScriptKey)
	if err != nil {
		writeRunError(w, err)
		return
	}
	if err := a.qinglong.runCrons(r.Context(), []int64{job.QLCronID}); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "ql_cron_id": job.QLCronID, "name": source.Name})
}

func (a *App) handleQingLongJobLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	acc, ok := a.resolveAccountFromQuery(w, r)
	if !ok {
		return
	}
	scriptKey := strings.TrimSpace(r.URL.Query().Get("script_key"))
	job, err := a.db.GetAccountScriptJob(r.Context(), acc.ID, scriptKey)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "该账号尚未创建此脚本任务")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logText, err := a.qinglong.cronLog(r.Context(), job.QLCronID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"script_key": scriptKey, "ql_cron_id": job.QLCronID, "log": logText})
}

func (a *App) handleQingLongPush(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		acc, ok := a.resolveAccountFromQuery(w, r)
		if !ok {
			return
		}
		setting, err := a.db.AccountPushSettingOrDefault(r.Context(), acc.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		public, err := a.pushSettingPublic(r.Context(), setting)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, public)
	case http.MethodPut:
		var body pushSettingIn
		if err := decodeOptionalJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		acc, ok := a.resolveAccountRef(w, r, body.Ref)
		if !ok {
			return
		}
		setting, err := a.savePushSetting(r.Context(), acc, body)
		if err != nil {
			if errors.Is(err, errPushTokenRequired) {
				writeError(w, http.StatusBadRequest, err.Error())
			} else {
				writeError(w, http.StatusBadGateway, err.Error())
			}
			return
		}
		public, err := a.pushSettingPublic(r.Context(), setting)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, public)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *App) scriptCatalog(ctx context.Context) ([]scriptSource, map[int64]qingLongCron, error) {
	if !a.qinglong.configured() {
		return nil, nil, fmt.Errorf("青龙 OpenAPI 未配置")
	}
	crons, err := a.qinglong.listCrons(ctx, a.cfg.QingLongRepo)
	if err != nil {
		return nil, nil, err
	}
	prefix := "task " + a.cfg.QingLongRepo + "/"
	byKey := make(map[string]scriptSource)
	byID := make(map[int64]qingLongCron, len(crons))
	for _, cron := range crons {
		byID[cron.ID] = cron
		if !strings.HasPrefix(cron.Command, prefix) {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(cron.Command, prefix))
		if !validScriptKey.MatchString(key) || key == "eoos/eoos_checkin.py" || key == "SendNotify.py" {
			continue
		}
		byKey[key] = scriptSource{Key: key, Name: cron.Name, Schedule: cron.Schedule, Cron: cron}
	}
	out := make([]scriptSource, 0, len(byKey))
	for _, source := range byKey {
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, byID, nil
}

func (a *App) ensureAccountJob(ctx context.Context, acc *store.WechatAccount, scriptKey string) (*store.AccountScriptJob, scriptSource, error) {
	scriptKey = strings.TrimSpace(scriptKey)
	sources, cronsByID, err := a.scriptCatalog(ctx)
	if err != nil {
		return nil, scriptSource{}, err
	}
	var source scriptSource
	found := false
	for _, candidate := range sources {
		if candidate.Key == scriptKey {
			source, found = candidate, true
			break
		}
	}
	if !found {
		return nil, scriptSource{}, fmt.Errorf("不支持的脚本: %s", scriptKey)
	}
	setting, err := a.db.AccountPushSettingOrDefault(ctx, acc.ID)
	if err != nil {
		return nil, scriptSource{}, err
	}
	command, err := a.accountTaskCommand(acc.ID, scriptKey, setting)
	if err != nil {
		return nil, scriptSource{}, err
	}
	name := managedTaskName(acc.ID, source.Name)
	job, err := a.db.GetAccountScriptJob(ctx, acc.ID, scriptKey)
	if err == nil {
		if _, exists := cronsByID[job.QLCronID]; exists {
			if err := a.qinglong.updateCron(ctx, job.QLCronID, name, command, source.Schedule); err != nil {
				return nil, scriptSource{}, err
			}
			return job, source, nil
		}
		_ = a.db.DeleteAccountScriptJob(ctx, acc.ID, scriptKey)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, scriptSource{}, err
	}
	cron, err := a.qinglong.createCron(ctx, name, command, source.Schedule)
	if err != nil {
		return nil, scriptSource{}, err
	}
	if err := a.qinglong.setCronsEnabled(ctx, []int64{cron.ID}, false); err != nil {
		return nil, scriptSource{}, err
	}
	job, err = a.db.UpsertAccountScriptJob(ctx, acc.ID, scriptKey, cron.ID, source.Schedule)
	return job, source, err
}

func managedTaskName(accountID int64, sourceName string) string {
	return fmt.Sprintf("[YYB:%d] %s", accountID, sourceName)
}

func (a *App) accountTaskCommand(accountID int64, scriptKey string, setting *store.AccountPushSetting) (string, error) {
	if !validScriptKey.MatchString(scriptKey) {
		return "", fmt.Errorf("脚本路径不合法")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`).MatchString(a.cfg.QingLongServer) {
		return "", fmt.Errorf("YYB_QINGLONG_SERVER 格式不合法")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(a.cfg.QingLongRepo) {
		return "", fmt.Errorf("YYB_QINGLONG_REPO 格式不合法")
	}
	pushKey, pushPlusToken, pushPlusTopic, qywxKey := "''", "''", "''", "''"
	switch setting.Channel {
	case "serverchan":
		pushKey = envReference(setting.TokenEnvName)
	case "pushplus":
		pushPlusToken = envReference(setting.TokenEnvName)
		if setting.TopicEnvName != "" {
			pushPlusTopic = envReference(setting.TopicEnvName)
		}
	case "qywx":
		qywxKey = envReference(setting.TokenEnvName)
	}
	return fmt.Sprintf(
		"YYB_SERVER='%s@%d' PUSH_KEY=%s PUSH_PLUS_TOKEN=%s PUSH_PLUS_USER=%s QYWX_KEY=%s task %s/%s",
		a.cfg.QingLongServer, accountID, pushKey, pushPlusToken, pushPlusTopic, qywxKey, a.cfg.QingLongRepo, scriptKey,
	), nil
}

func envReference(name string) string {
	if !regexp.MustCompile(`^[A-Z0-9_]+$`).MatchString(name) {
		return "''"
	}
	return `"${` + name + `:-}"`
}

var errPushTokenRequired = errors.New("首次配置该推送渠道时必须填写 Token 或 Key")

func pushEnvNames(accountID int64) map[string][2]string {
	prefix := "YYB_RUN_ACCOUNT_" + strconv.FormatInt(accountID, 10) + "_"
	return map[string][2]string{
		"serverchan": {prefix + "SERVERCHAN_KEY", ""},
		"pushplus":   {prefix + "PUSHPLUS_TOKEN", prefix + "PUSHPLUS_TOPIC"},
		"qywx":       {prefix + "QYWX_KEY", ""},
	}
}

func (a *App) savePushSetting(ctx context.Context, acc *store.WechatAccount, body pushSettingIn) (*store.AccountPushSetting, error) {
	channel := strings.ToLower(strings.TrimSpace(body.Channel))
	if channel == "" {
		channel = "none"
	}
	names := pushEnvNames(acc.ID)
	if channel != "none" {
		if _, ok := names[channel]; !ok {
			return nil, fmt.Errorf("不支持的推送渠道")
		}
	}
	allNames := make([]string, 0, 4)
	for _, pair := range names {
		allNames = append(allNames, pair[0])
		if pair[1] != "" {
			allNames = append(allNames, pair[1])
		}
	}
	if channel == "none" {
		if err := a.qinglong.setNamedEnvsEnabled(ctx, allNames, false); err != nil {
			return nil, err
		}
		setting, err := a.db.UpsertAccountPushSetting(ctx, acc.ID, "none", "", "")
		if err != nil {
			return nil, err
		}
		if err := a.refreshAccountJobCommands(ctx, acc, setting); err != nil {
			return nil, err
		}
		return setting, nil
	}
	selected := names[channel]
	configured, err := a.namedEnvHasValue(ctx, selected[0])
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(body.Token)
	if token == "" && !configured {
		return nil, errPushTokenRequired
	}
	if token != "" {
		if err := a.qinglong.upsertEnv(ctx, selected[0], token, fmt.Sprintf("YYB Go 账号 %d %s 推送", acc.ID, channel)); err != nil {
			return nil, err
		}
	}
	if selected[1] != "" && body.Topic != nil {
		if err := a.qinglong.upsertEnv(ctx, selected[1], strings.TrimSpace(*body.Topic), fmt.Sprintf("YYB Go 账号 %d PushPlus 群组", acc.ID)); err != nil {
			return nil, err
		}
	}
	otherNames := make([]string, 0, len(allNames))
	for _, name := range allNames {
		if name != selected[0] && name != selected[1] {
			otherNames = append(otherNames, name)
		}
	}
	if err := a.qinglong.setNamedEnvsEnabled(ctx, otherNames, false); err != nil {
		return nil, err
	}
	selectedNames := []string{selected[0]}
	if selected[1] != "" {
		selectedNames = append(selectedNames, selected[1])
	}
	if err := a.qinglong.setNamedEnvsEnabled(ctx, selectedNames, true); err != nil {
		return nil, err
	}
	setting, err := a.db.UpsertAccountPushSetting(ctx, acc.ID, channel, selected[0], selected[1])
	if err != nil {
		return nil, err
	}
	if err := a.refreshAccountJobCommands(ctx, acc, setting); err != nil {
		return nil, err
	}
	return setting, nil
}

func (a *App) refreshAccountJobCommands(ctx context.Context, acc *store.WechatAccount, setting *store.AccountPushSetting) error {
	sources, cronsByID, err := a.scriptCatalog(ctx)
	if err != nil {
		return err
	}
	sourceByKey := make(map[string]scriptSource, len(sources))
	for _, source := range sources {
		sourceByKey[source.Key] = source
	}
	jobs, err := a.db.ListAccountScriptJobs(ctx, acc.ID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		source, sourceExists := sourceByKey[job.ScriptKey]
		if _, cronExists := cronsByID[job.QLCronID]; !sourceExists || !cronExists {
			continue
		}
		command, err := a.accountTaskCommand(acc.ID, job.ScriptKey, setting)
		if err != nil {
			return err
		}
		if err := a.qinglong.updateCron(ctx, job.QLCronID, managedTaskName(acc.ID, source.Name), command, source.Schedule); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) pushSettingPublic(ctx context.Context, setting *store.AccountPushSetting) (pushSettingPublic, error) {
	out := pushSettingPublic{Channel: setting.Channel}
	if setting.Channel == "none" || setting.TokenEnvName == "" {
		return out, nil
	}
	var err error
	out.TokenConfigured, err = a.namedEnvHasValue(ctx, setting.TokenEnvName)
	if err != nil {
		return out, err
	}
	if setting.TopicEnvName != "" {
		out.TopicConfigured, err = a.namedEnvHasValue(ctx, setting.TopicEnvName)
	}
	return out, err
}

func (a *App) namedEnvHasValue(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	envs, err := a.qinglong.listEnvs(ctx, name)
	if err != nil {
		return false, err
	}
	for _, env := range envs {
		if env.Name == name && strings.TrimSpace(env.Value) != "" {
			return true, nil
		}
	}
	return false, nil
}

func writeRunError(w http.ResponseWriter, err error) {
	if strings.HasPrefix(err.Error(), "不支持的脚本") || strings.Contains(err.Error(), "格式不合法") {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}
