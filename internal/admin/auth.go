// Package admin 提供独立于兼容公开 API 的管理接口。
package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Authenticator 可由 Bearer 或后续 session cookie 实现；nil 表示未配置。
type Authenticator func(*http.Request) bool

func BearerAuthenticator(token string) Authenticator {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	expected := sha256.Sum256([]byte(token))
	return func(r *http.Request) bool {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return false
		}
		actual := sha256.Sum256([]byte(parts[1]))
		return subtle.ConstantTimeCompare(actual[:], expected[:]) == 1
	}
}

// RequireAuth 支持多个可组合凭据，全部未配置时 fail closed。
func RequireAuth(authenticators ...Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		configured := false
		for _, authenticate := range authenticators {
			if authenticate == nil {
				continue
			}
			configured = true
			if authenticate(c.Request) {
				c.Next()
				return
			}
		}
		if !configured {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "管理 API 已禁用：未配置 admin.api_token"})
			return
		}
		c.Header("WWW-Authenticate", "Bearer")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "需要有效的管理凭据"})
	}
}
