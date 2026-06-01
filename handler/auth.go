package handler

import (
	"log/slog"
	"net/http"
)

// HTTPInterceptor HTTP 拦截器，验证用户登录状态
func HTTPInterceptor(h http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			usernameCookie, err1 := r.Cookie("username")
			tokenCookie, err2 := r.Cookie("token")
			if err1 != nil || err2 != nil {
				writeJSON(w, http.StatusUnauthorized, 1, "请先登录", nil)
				return
			}

			username := usernameCookie.Value
			token := tokenCookie.Value

			if len(username) < 3 || !isTokenValid(username, token) {
				slog.Warn("auth failed", "username", username, "path", r.URL.Path)
				writeJSON(w, http.StatusUnauthorized, 1, "登录已过期，请重新登录", nil)
				return
			}
			h(w, r)
		})
}
