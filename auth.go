package main

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func validToken(r *http.Request, secret string) bool {
	value := ""
	if cookie, err := r.Cookie("token"); err == nil {
		value = cookie.Value
	}
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		value = strings.TrimPrefix(header, "Bearer ")
	}
	if value == "" || defaultSecret(secret) {
		return false
	}
	token, err := jwt.Parse(value, func(*jwt.Token) (any, error) { return []byte(secret), nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	return ok && claims["authorized"] == true
}

func AuthStatus(w http.ResponseWriter, r *http.Request) {
	config := ConfigSnapshot()
	writeJSON(w, http.StatusOK, map[string]any{"code": 200, "data": map[string]any{
		"need_setup": defaultSecret(config.Server.JwtSecret), "auth_enabled": config.Server.AuthEnabled,
		"authenticated": validToken(r, config.Server.JwtSecret),
	}})
}

func Login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &input); err != nil {
		apiError(w, 400, err)
		return
	}
	secret := ConfigSnapshot().Server.JwtSecret
	if defaultSecret(secret) || subtle.ConstantTimeCompare([]byte(input.Token), []byte(secret)) != 1 {
		apiError(w, http.StatusUnauthorized, errors.New("invalid login password"))
		return
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"authorized": true, "exp": time.Now().Add(24 * time.Hour).Unix(),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		apiError(w, 500, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "token", Value: signed, MaxAge: 86400, Path: "/",
		Secure: r.TLS != nil, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"code": 200, "msg": "Login successful", "token": signed})
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		config := ConfigSnapshot()
		if config.Server.AuthEnabled && !validToken(r, config.Server.JwtSecret) {
			apiError(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
