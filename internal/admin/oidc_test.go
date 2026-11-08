package admin

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chisaato/asu-go/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func generateTestRSAKey(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	nStr := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
	eBytes := big.NewInt(int64(priv.E)).Bytes()
	eStr := base64.RawURLEncoding.EncodeToString(eBytes)
	return priv, nStr, eStr
}

func signRS256(priv *rsa.PrivateKey, kid, iss, sub, aud string, exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"alg":"RS256","typ":"JWT","kid":"%s"}`, kid)))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iss":"%s","sub":"%s","aud":"%s","exp":%d,"preferred_username":"alice_sso","name":"Alice SSO"}`, iss, sub, aud, exp.Unix())))
	data := header + "." + payload
	h := sha256.Sum256([]byte(data))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
	return data + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestOIDCFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	privKey, nStr, eStr := generateTestRSAKey(t)
	kid := "test-key-id"

	var mockIdP *httptest.Server
	mockIdP = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(OpenIDConfiguration{
				Issuer:                mockIdP.URL,
				AuthorizationEndpoint: mockIdP.URL + "/oauth/authorize",
				TokenEndpoint:         mockIdP.URL + "/oauth/token",
				JWKSUri:               mockIdP.URL + "/jwks.json",
				UserinfoEndpoint:      mockIdP.URL + "/userinfo",
			})
		case "/jwks.json":
			_ = json.NewEncoder(w).Encode(JWKSet{
				Keys: []JWK{
					{
						Kty: "RSA",
						Kid: kid,
						Use: "sig",
						Alg: "RS256",
						N:   nStr,
						E:   eStr,
					},
				},
			})
		case "/oauth/token":
			idToken := signRS256(privKey, kid, mockIdP.URL, "user-sub-12345", "test-client-id", time.Now().Add(time.Hour))
			_ = json.NewEncoder(w).Encode(OIDCTokenResponse{
				AccessToken: "mock-access-token",
				IDToken:     idToken,
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mockIdP.Close)

	database, dataDir := setupTestDB(t)
	cfg := config.Admin{
		AccessTokenTTL:     15 * time.Minute,
		RefreshTokenTTL:    30 * 24 * time.Hour,
		RefreshAbsoluteTTL: 90 * 24 * time.Hour,
		OIDC: config.OIDC{
			Enabled:      true,
			Issuer:       mockIdP.URL,
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
			ButtonText:   "企业统一认证",
		},
	}

	router := gin.New()
	_, err := RegisterAuth(router, cfg, zap.NewNop(), WithDataDir(dataDir), WithDB(database.DB))
	if err != nil {
		t.Fatal(err)
	}

	// 1. 测试配置发现接口 /admin/api/auth/config
	configReq := httptest.NewRequest("GET", "/admin/api/auth/config", nil)
	wConfig := httptest.NewRecorder()
	router.ServeHTTP(wConfig, configReq)
	if wConfig.Code != http.StatusOK {
		t.Fatalf("auth config: %d", wConfig.Code)
	}
	var configResp struct {
		OIDCEnabled    bool   `json:"oidc_enabled"`
		OIDCButtonText string `json:"oidc_button_text"`
		OIDCLoginURL   string `json:"oidc_login_url"`
	}
	if err := json.Unmarshal(wConfig.Body.Bytes(), &configResp); err != nil {
		t.Fatal(err)
	}
	if !configResp.OIDCEnabled || configResp.OIDCButtonText != "企业统一认证" {
		t.Fatalf("auth config 内容不符合预期: %+v", configResp)
	}

	// 2. 发起登录 /admin/api/auth/oidc/login
	loginReq := httptest.NewRequest("GET", "/admin/api/auth/oidc/login", nil)
	wLogin := httptest.NewRecorder()
	router.ServeHTTP(wLogin, loginReq)
	if wLogin.Code != http.StatusFound {
		t.Fatalf("oidc login 未返回 302: %d", wLogin.Code)
	}

	loc := wLogin.Header().Get("Location")
	if !strings.HasPrefix(loc, mockIdP.URL+"/oauth/authorize") {
		t.Fatalf("重定向目标错误: %s", loc)
	}

	var stateCookie *http.Cookie
	for _, c := range wLogin.Result().Cookies() {
		if c.Name == oidcStateCookie {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil || stateCookie.Value == "" {
		t.Fatal("未签发 state Cookie")
	}

	// 从重定向链接提取 state
	stateParam := ""
	for _, part := range strings.Split(loc, "&") {
		if strings.HasPrefix(part, "state=") || strings.Contains(part, "state=") {
			kv := strings.Split(part, "state=")
			if len(kv) == 2 {
				stateParam = kv[1]
			}
		}
	}
	if stateParam != stateCookie.Value {
		t.Fatalf("state 参数不匹配: url=%s, cookie=%s", stateParam, stateCookie.Value)
	}

	// 3. 测试错误 state 回调
	badCallbackReq := httptest.NewRequest("GET", "/admin/api/auth/oidc/callback?state=wrong&code=auth-code", nil)
	badCallbackReq.AddCookie(stateCookie)
	wBadCallback := httptest.NewRecorder()
	router.ServeHTTP(wBadCallback, badCallbackReq)
	if wBadCallback.Code != http.StatusBadRequest {
		t.Fatalf("篡改 state 应拒绝: %d", wBadCallback.Code)
	}

	// 4. 正确回调 /admin/api/auth/oidc/callback
	callbackReq := httptest.NewRequest("GET", fmt.Sprintf("/admin/api/auth/oidc/callback?state=%s&code=valid-code", stateCookie.Value), nil)
	callbackReq.AddCookie(stateCookie)
	wCallback := httptest.NewRecorder()
	router.ServeHTTP(wCallback, callbackReq)
	if wCallback.Code != http.StatusFound {
		t.Fatalf("oidc callback 失败: %d %s", wCallback.Code, wCallback.Body.String())
	}
	if wCallback.Header().Get("Location") != "/admin" {
		t.Fatalf("回调完成后未跳回 /admin: %s", wCallback.Header().Get("Location"))
	}

	var accessCookie, refreshCookie *http.Cookie
	for _, c := range wCallback.Result().Cookies() {
		switch c.Name {
		case accessTokenCookie:
			accessCookie = c
		case refreshTokenCookie:
			refreshCookie = c
		}
	}
	if accessCookie == nil || refreshCookie == nil {
		t.Fatal("OIDC 登录成功后未下发双 Token")
	}

	// 5. 验证登录后能够以新会话正常访问受保护接口
	meReq := httptest.NewRequest("GET", "/admin/api/auth/me", nil)
	meReq.AddCookie(accessCookie)
	wMe := httptest.NewRecorder()
	router.ServeHTTP(wMe, meReq)
	if wMe.Code != http.StatusOK || !strings.Contains(wMe.Body.String(), "alice_sso") {
		t.Fatalf("使用 OIDC 颁发的 Token 鉴权失败: %d %s", wMe.Code, wMe.Body.String())
	}
}
