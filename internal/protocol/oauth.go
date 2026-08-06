package protocol

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func (p *Pool) AuthorizePublicAccount(ctx context.Context, loginBuffer, appID, scope, state, requestURL string, accountID int64, tcpProxy string) (map[string]any, error) {
	return p.run(ctx, loginBuffer, accountID, tcpProxy, func(ctx context.Context, session WmpfSession) (map[string]any, error) {
		hostAppID := session.Session.HostAppID
		if len(hostAppID) == 0 {
			hostAppID = hostAppIDDefault
		}
		hostAppIDString := string(hostAppID)
		variants := []struct {
			appID, scope, state string
			scene               uint64
		}{
			{hostAppIDString, scope, state, 4},
			{hostAppIDString, "", "", 0},
			{"", "", "", 0},
		}
		var lastErr error
		for _, variant := range variants {
			request := buildMPGetA8KeyRequest(variant.appID, variant.scope, variant.state, requestURL, variant.scene)
			envelope, err := buildSessionPacket(session.Session, mpGetA8KeyURL, mpGetA8KeyID, request)
			if err != nil {
				return nil, err
			}
			_, response, err := p.sendEnvelope(ctx, session, envelope)
			if err != nil {
				lastErr = err
				continue
			}
			result, err := parseMPGetA8KeyResponse(response)
			if err == nil {
				return result, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("GetA8Key compatibility attempts failed: %w", lastErr)
	})
}

func parseMPGetA8KeyResponse(response []byte) (map[string]any, error) {
	fields := pbParse(response)
	fullURL := string(safeBytes(fields[2]))
	actionCode := int64FromAny(fields[4])
	baseCode, baseMessage := parseBaseResponse(safeBytes(fields[1]))
	if baseCode != 0 {
		return nil, fmt.Errorf("GetA8Key failed: code=%d msg=%s", baseCode, baseMessage)
	}
	if fullURL == "" {
		return nil, fmt.Errorf("GetA8Key returned no redirect URL: action_code=%d msg=%s", actionCode, baseMessage)
	}
	code := findOAuthCode(fullURL, 0)
	if code == "" {
		return nil, fmt.Errorf("GetA8Key redirect did not contain an OAuth code: action_code=%d", actionCode)
	}
	return map[string]any{
		"code":        code,
		"url":         fullURL,
		"full_url":    fullURL,
		"action_code": actionCode,
	}, nil
}

func parseBaseResponse(raw []byte) (int64, string) {
	fields := pbParse(raw)
	code := int64FromAny(fields[1])
	messageFields := pbParse(safeBytes(fields[2]))
	return code, string(safeBytes(messageFields[1]))
}

func findOAuthCode(raw string, depth int) string {
	if depth > 3 {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if code := parsed.Query().Get("code"); code != "" {
		return code
	}
	if fragment, err := url.ParseQuery(parsed.Fragment); err == nil {
		if code := fragment.Get("code"); code != "" {
			return code
		}
	}
	for _, values := range parsed.Query() {
		for _, value := range values {
			if code := findOAuthCode(value, depth+1); code != "" {
				return code
			}
		}
	}
	return ""
}
