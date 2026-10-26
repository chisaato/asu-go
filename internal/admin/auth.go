// Package admin 提供独立于兼容公开 API 的管理接口。
package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Authenticator 表示一种管理凭据；nil 表示该路径未配置。
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
		for _, authenticate := range authenticators {
			if authenticate == nil {
				continue
			}
			if authenticate(c.Request) {
				c.Next()
				return
			}
		}
		c.Header("WWW-Authenticate", "Bearer")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "需要有效的管理凭据"})
	}
}
