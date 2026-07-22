package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionLifecycleAndCSRF(t *testing.T) {
	t.Parallel()

	manager := New("admin-token-with-enough-entropy", Options{
		CookieSecure: true,
		TTL:          time.Hour,
	})
	if _, err := manager.Login(httptest.NewRecorder(), "wrong-token"); err == nil {
		t.Fatal("expected invalid credentials")
	}

	recorder := httptest.NewRecorder()
	csrf, err := manager.Login(recorder, "admin-token-with-enough-entropy")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe session cookie: %#v", cookie)
	}

	request := httptest.NewRequest("GET", "/api/v1/system/health", nil)
	request.AddCookie(cookie)
	session, ok := manager.Authenticate(request)
	if !ok {
		t.Fatal("expected session to authenticate")
	}
	if !manager.ValidCSRF(session, csrf) {
		t.Fatal("expected issued CSRF token to validate")
	}
	if manager.ValidCSRF(session, "wrong-csrf") {
		t.Fatal("unexpected CSRF validation success")
	}
}

func TestNewLoginInvalidatesPreviousSession(t *testing.T) {
	t.Parallel()

	manager := New("admin-token-with-enough-entropy", Options{TTL: time.Hour})
	first := httptest.NewRecorder()
	if _, err := manager.Login(first, "admin-token-with-enough-entropy"); err != nil {
		t.Fatal(err)
	}
	second := httptest.NewRecorder()
	if _, err := manager.Login(second, "admin-token-with-enough-entropy"); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("GET", "/api/v1/system/health", nil)
	request.AddCookie(first.Result().Cookies()[0])
	if _, ok := manager.Authenticate(request); ok {
		t.Fatal("previous single-admin session should be invalidated")
	}
}
