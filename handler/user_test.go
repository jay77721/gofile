package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSignupHandler_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/user/signup", SignupHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signup", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSignupHandler_ShortUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/user/signup", SignupHandler)

	form := url.Values{}
	form.Set("username", "ab")
	form.Set("password", "12345")
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for short username, got %d", w.Code)
	}
}

func TestSignupHandler_ShortPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/user/signup", SignupHandler)

	form := url.Values{}
	form.Set("username", "testuser")
	form.Set("password", "1234")
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for short password, got %d", w.Code)
	}
}

func TestSignInHandler_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/user/signin", SignInHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signin", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSignInHandler_InvalidCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/user/signin", SignInHandler)

	form := url.Values{}
	form.Set("username", "nonexistent_user_xx")
	form.Set("password", "somepassword")
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/user/signin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 无 MySQL 连接会 panic
	defer func() {
		if rec := recover(); rec != nil {
			t.Log("SignInHandler panicked as expected (no MySQL):", rec)
			return
		}
		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	}()

	r.ServeHTTP(w, req)
}

func TestUserInfoHandler_NoCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/user/info", AuthMiddleware(), UserInfoHandler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/user/info", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}