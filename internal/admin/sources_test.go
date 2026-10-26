package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gzzchh/asu-go/internal/db"
	"github.com/gzzchh/asu-go/internal/sources"
)

func testRouter(t *testing.T, token string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterSources(router.Group("/admin/api", RequireAuth(BearerAuthenticator(token))), sources.NewStore(database))
	return router
}

func TestAuth(t *testing.T) {
	for _, tc := range []struct {
		configured, header string
		status             int
	}{
		{"secret", "", 401}, {"secret", "Bearer wrong", 401}, {"secret", "Basic secret", 401},
		{"secret", "Bearer secret", 200}, {"", "", 503}, {"", "Bearer anything", 503}, {" ", "Bearer anything", 503},
	} {
		router := testRouter(t, tc.configured)
		request := httptest.NewRequest(http.MethodGet, "/admin/api/sources", nil)
		request.Header.Set("Authorization", tc.header)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, response.Code, response.Body)
		}
	}
	// 后续 cookie 鉴权可与未配置的 Bearer 并列组合。
	router := gin.New()
	router.GET("/", RequireAuth(BearerAuthenticator(""), func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "ok" }), func(c *gin.Context) { c.Status(204) })
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Session", "ok")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("鉴权组合失败")
	}
}

func TestSourcesAPI(t *testing.T) {
	router := testRouter(t, "secret")
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/admin/api/sources"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w
	}
	body := `{"distribution":"custom","branch":"test","version":"1.0.0","upstream_url":"https://example.com","image":"example/imagebuilder"}`
	request("GET", "", "", 200)
	w := request("POST", "", body, 201)
	var source sources.Source
	if err := json.Unmarshal(w.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if source.ID != 1 || !source.Enabled {
		t.Fatalf("默认值: %+v", source)
	}
	request("POST", "", body, 409)
	request("GET", "/1", "", 200)
	request("PATCH", "/1", `{"enabled":false}`, 200)
	request("PATCH", "/1", `{"unknown":true}`, 400)
	request("PATCH", "/1", `{"enabled":null}`, 400)
	request("PATCH", "/1", `null`, 400)
	request("POST", "", body+` {}`, 400)
	request("POST", "", `{"unknown":true}`, 400)
	request("GET", "/not-an-id", "", 400)
	request("GET", "/900", "", 404)
	request("DELETE", "/1", "", 204)
	request("DELETE", "/1", "", 404)
	request("GET", "/1", "", 404)
}
