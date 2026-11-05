package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fetch 通过 handler 取一个资源，返回状态码、Content-Type 和正文。
func fetch(t *testing.T, method, path string) (int, string, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Header().Get("Content-Type"), rec.Body.String()
}

func TestHandlerServesIndex(t *testing.T) {
	code, contentType, body := fetch(t, http.MethodGet, "/")
	if code != http.StatusOK {
		t.Fatalf("GET / 状态码 = %d，期望 %d", code, http.StatusOK)
	}
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("GET / Content-Type = %q，期望 %q", contentType, "text/html; charset=utf-8")
	}
	for _, id := range []string{`id="input"`, `id="send"`, `id="messages"`} {
		if !strings.Contains(body, id) {
			t.Errorf("GET / 正文缺少 %s", id)
		}
	}
	if !strings.Contains(body, `<meta name="viewport" content="width=device-width, initial-scale=1">`) {
		t.Error("GET / 正文缺少 viewport meta")
	}
	if !strings.Contains(body, `<script src="app.js">`) {
		t.Error("GET / 正文缺少相对路径的 <script src=\"app.js\">")
	}
}

func TestHandlerServesAssets(t *testing.T) {
	cases := []struct {
		path        string
		contentType string
		needle      string
	}{
		{"/app.js", "text/javascript; charset=utf-8", "fetch("},
		{"/style.css", "text/css; charset=utf-8", "@media"},
		{"/index.html", "text/html; charset=utf-8", "id=\"messages\""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			code, contentType, body := fetch(t, http.MethodGet, tc.path)
			if code != http.StatusOK {
				t.Fatalf("GET %s 状态码 = %d，期望 %d", tc.path, code, http.StatusOK)
			}
			if contentType != tc.contentType {
				t.Errorf("GET %s Content-Type = %q，期望 %q", tc.path, contentType, tc.contentType)
			}
			if !strings.Contains(body, tc.needle) {
				t.Errorf("GET %s 正文缺少 %q", tc.path, tc.needle)
			}
		})
	}
}

func TestHandlerRejectsUnknownPathsAndMethods(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"未知路径", http.MethodGet, "/nope", http.StatusNotFound},
		{"嵌套路径", http.MethodGet, "/a/b", http.StatusNotFound},
		{"根路径不接受 POST", http.MethodPost, "/", http.StatusMethodNotAllowed},
		{"静态资源不接受 POST", http.MethodPost, "/app.js", http.StatusMethodNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := fetch(t, tc.method, tc.path)
			if code != tc.want {
				t.Errorf("%s %s 状态码 = %d，期望 %d", tc.method, tc.path, code, tc.want)
			}
		})
	}
}

// appJS 取内嵌的 app.js 内容。
func appJS(t *testing.T) string {
	t.Helper()
	code, _, body := fetch(t, http.MethodGet, "/app.js")
	if code != http.StatusOK {
		t.Fatalf("GET /app.js 状态码 = %d，期望 %d", code, http.StatusOK)
	}
	return body
}

func TestAppJSRendersWithTextContent(t *testing.T) {
	js := appJS(t)
	if !strings.Contains(js, "textContent") {
		t.Error("app.js 必须用 textContent 写入文本")
	}
}

func TestAppJSHasNoUnsafeAPIs(t *testing.T) {
	js := appJS(t)
	forbidden := []string{
		"innerHTML",
		"outerHTML",
		"insertAdjacentHTML",
		"document.write",
		"eval(",
		"new Function(",
	}
	for _, bad := range forbidden {
		if strings.Contains(js, bad) {
			t.Errorf("app.js 不应包含 %q", bad)
		}
	}
}

func TestAppJSStoresNothing(t *testing.T) {
	js := appJS(t)
	forbidden := []string{
		"localStorage",
		"sessionStorage",
		"document.cookie",
	}
	for _, bad := range forbidden {
		if strings.Contains(js, bad) {
			t.Errorf("app.js 不应包含 %q", bad)
		}
	}
}

func TestAppJSHandlesEveryResponseKind(t *testing.T) {
	js := appJS(t)
	required := []string{
		"'record'",
		"'query'",
		"'unrecognized'",
		"'error'",
		"status",
		"citations",
	}
	for _, want := range required {
		if !strings.Contains(js, want) {
			t.Errorf("app.js 必须处理 %q", want)
		}
	}
}

// API URL, method and payload are exercised by tests/browser against the real
// HTTPS server. Do not bind them to variable names or JSON.stringify spelling.

func TestStaticAssetsHaveNoExternalResources(t *testing.T) {
	for _, path := range []string{"/index.html", "/style.css"} {
		t.Run(path, func(t *testing.T) {
			code, _, body := fetch(t, http.MethodGet, path)
			if code != http.StatusOK {
				t.Fatalf("GET %s 状态码 = %d，期望 %d", path, code, http.StatusOK)
			}
			for _, bad := range []string{"http://", "https://", "//cdn."} {
				if strings.Contains(body, bad) {
					t.Errorf("%s 不应引用外部资源 %q", path, bad)
				}
			}
		})
	}
}
