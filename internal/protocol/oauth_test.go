package protocol

import (
	"bytes"
	"net/url"
	"testing"
)

func TestBuildMPGetA8KeyRequest(t *testing.T) {
	requestURL := "https://open.weixin.qq.com/connect/oauth2/authorize?appid=wx1234567890abcdef#wechat_redirect"
	raw := buildMPGetA8KeyRequest("wx1234567890abcdef", "snsapi_base", "state-1", requestURL, 4)
	fields := pbParse(raw)
	if _, ok := fields[1]; ok {
		t.Fatal("BaseRequest must be omitted for direct TDI requests")
	}
	if got := int64FromAny(fields[2]); got != 2 {
		t.Fatalf("opcode = %d, want 2", got)
	}
	if got := string(safeBytes(pbParse(safeBytes(fields[4]))[1])); got != "wx1234567890abcdef" {
		t.Fatalf("appid = %q", got)
	}
	if got := string(safeBytes(pbParse(safeBytes(fields[7]))[1])); got != requestURL {
		t.Fatalf("request URL = %q", got)
	}
	if got := int64FromAny(fields[14]); got != 2 {
		t.Fatalf("reason = %d, want 2", got)
	}
}

func TestBuildMPGetA8KeyRequestWebViewDefaults(t *testing.T) {
	raw := buildMPGetA8KeyRequest("", "", "", "https://example.com", 0)
	fields := pbParse(raw)
	for _, field := range []int{1, 4, 5, 6} {
		if _, ok := fields[field]; ok {
			t.Fatalf("field %d should be omitted", field)
		}
	}
}

func TestParseMPGetA8KeyResponseFindsNestedOAuthCode(t *testing.T) {
	callback := "http://example.com/callback?code=oauth-code&state=state-1"
	finalURL := "https://sso.example.com/login?service=" + url.QueryEscape(callback)
	base := append(pbVar(1, 0), pbLen(2, pbLen(1, []byte("OK")))...)
	response := append(pbLen(1, base), pbLen(2, []byte(finalURL))...)
	response = append(response, pbVar(4, 1)...)

	result, err := parseMPGetA8KeyResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if result["code"] != "oauth-code" || result["full_url"] != finalURL {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseMPGetA8KeyResponseRejectsMissingCode(t *testing.T) {
	base := append(pbVar(1, 0), pbLen(2, pbLen(1, []byte("OK")))...)
	response := append(pbLen(1, base), pbLen(2, []byte("https://example.com/callback"))...)
	if _, err := parseMPGetA8KeyResponse(response); err == nil {
		t.Fatal("expected missing-code error")
	}
}

func TestSessionDecryptFindsPayloadAfterLongHeader(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 24)
	plaintext := pbLen(2, []byte("response"))
	encrypted, err := aesGCMEncryptLayout(key, nil, lz4AllLiteral(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	body := append(bytes.Repeat([]byte{0xff}, 300), encrypted...)
	got, err := sessionDecrypt(body, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("plaintext = %x, want %x", got, plaintext)
	}
}
