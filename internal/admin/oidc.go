package admin

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chisaato/asu-go/internal/config"
	"go.uber.org/zap"
)

type OpenIDConfiguration struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSUri               string `json:"jwks_uri"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

type JWKSet struct {
	Keys []JWK `json:"keys"`
}

type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type IDTokenClaims struct {
	Iss               string `json:"iss"`
	Sub               string `json:"sub"`
	Aud               any    `json:"aud"`
	Exp               int64  `json:"exp"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
}

type OIDCTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type OIDCClient struct {
	cfg        config.OIDC
	httpClient *http.Client
	logger     *zap.Logger

	mu     sync.RWMutex
	disc   *OpenIDConfiguration
	discAt time.Time
	keys   map[string]*rsa.PublicKey
	keysAt time.Time
}

func NewOIDCClient(cfg config.OIDC, logger *zap.Logger) *OIDCClient {
	if !cfg.Enabled {
		return nil
	}
	return &OIDCClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
		keys:   make(map[string]*rsa.PublicKey),
	}
}

// GetDiscovery 获取 OIDC 自动发现配置。
func (c *OIDCClient) GetDiscovery(ctx context.Context) (*OpenIDConfiguration, error) {
	c.mu.RLock()
	if c.disc != nil && time.Since(c.discAt) < time.Hour {
		disc := c.disc
		c.mu.RUnlock()
		return disc, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disc != nil && time.Since(c.discAt) < time.Hour {
		return c.disc, nil
	}

	discURL := strings.TrimSuffix(c.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 discovery 请求: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 OIDC discovery: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("OIDC discovery 状态码 %d: %s", resp.StatusCode, string(body))
	}

	var disc OpenIDConfiguration
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return nil, fmt.Errorf("解析 OIDC discovery: %w", err)
	}

	if disc.AuthorizationEndpoint == "" || disc.TokenEndpoint == "" {
		return nil, errors.New("OIDC discovery 响应缺少必要端点配置")
	}

	c.disc = &disc
	c.discAt = time.Now()
	return &disc, nil
}

// GetPublicKey 根据 kid 从 JWKS 获取公钥。
func (c *OIDCClient) GetPublicKey(ctx context.Context, jwksURI, kid string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	if key, ok := c.keys[kid]; ok && time.Since(c.keysAt) < time.Hour {
		c.mu.RUnlock()
		return key, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if key, ok := c.keys[kid]; ok && time.Since(c.keysAt) < time.Hour {
		return key, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 JWKS 请求: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS 状态码异常: %d", resp.StatusCode)
	}

	var set JWKSet
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("解析 JWKS 失败: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range set.Keys {
		if strings.ToUpper(k.Kty) == "RSA" && k.N != "" && k.E != "" {
			pub, err := parseRSAPublicKey(k.N, k.E)
			if err == nil {
				newKeys[k.Kid] = pub
			}
		}
	}

	c.keys = newKeys
	c.keysAt = time.Now()

	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("未在 JWKS 中找到对应公钥 (kid: %s)", kid)
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}
	var e int
	for _, b := range eBytes {
		e = (e << 8) | int(b)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: e,
	}, nil
}

// BuildAuthURL 构建授权登录 URL。
func (c *OIDCClient) BuildAuthURL(ctx context.Context, state, nonce, redirectURI string) (string, error) {
	disc, err := c.GetDiscovery(ctx)
	if err != nil {
		return "", err
	}

	scopes := c.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", c.cfg.ClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", strings.Join(scopes, " "))
	values.Set("state", state)
	if nonce != "" {
		values.Set("nonce", nonce)
	}

	sep := "?"
	if strings.Contains(disc.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return disc.AuthorizationEndpoint + sep + values.Encode(), nil
}

// ExchangeCode 用授权码换取令牌。
func (c *OIDCClient) ExchangeCode(ctx context.Context, code, redirectURI string) (*OIDCTokenResponse, error) {
	disc, err := c.GetDiscovery(ctx)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("构造 token 请求: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 token 失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("换取 Token 失败 (%d): %s", resp.StatusCode, string(body))
	}

	var tr OIDCTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("解析 Token 响应失败: %w", err)
	}
	return &tr, nil
}

// VerifyIDToken 验证 ID Token 并提取 Claims。
func (c *OIDCClient) VerifyIDToken(ctx context.Context, idTokenStr string, now time.Time) (*IDTokenClaims, error) {
	parts := strings.Split(idTokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("无效 ID Token 格式")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("解码 ID Token header 失败")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, errors.New("解析 ID Token header 失败")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("解码 ID Token payload 失败")
	}

	var claims IDTokenClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, errors.New("解析 ID Token claims 失败")
	}

	// 校验有效期与受众
	if claims.Exp <= now.Unix() {
		return nil, errors.New("ID Token 已过期")
	}

	// 校验 audience
	audValid := false
	switch v := claims.Aud.(type) {
	case string:
		audValid = subtle.ConstantTimeCompare([]byte(v), []byte(c.cfg.ClientID)) == 1
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && subtle.ConstantTimeCompare([]byte(s), []byte(c.cfg.ClientID)) == 1 {
				audValid = true
				break
			}
		}
	}
	if !audValid {
		return nil, errors.New("ID Token 受众 (aud) 不匹配")
	}

	// 校验签名（RS256）
	if strings.ToUpper(header.Alg) == "RS256" && header.Kid != "" {
		disc, err := c.GetDiscovery(ctx)
		if err == nil && disc.JWKSUri != "" {
			pubKey, err := c.GetPublicKey(ctx, disc.JWKSUri, header.Kid)
			if err != nil {
				return nil, fmt.Errorf("获取 JWKS 公钥失败: %w", err)
			}
			signedData := parts[0] + "." + parts[1]
			h := sha256.Sum256([]byte(signedData))
			sig, err := base64.RawURLEncoding.DecodeString(parts[2])
			if err != nil {
				return nil, errors.New("解码签名失败")
			}
			if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, h[:], sig); err != nil {
				return nil, fmt.Errorf("ID Token 签名校验失败: %w", err)
			}
		}
	}

	return &claims, nil
}
