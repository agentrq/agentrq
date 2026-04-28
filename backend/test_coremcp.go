package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/handler/coremcp"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

func main() {
	mux := http.NewServeMux()
	tokenSvc := auth.NewTokenService(auth.TokenConfig{JWTSecret: "test-secret"})
	coremcp.New(coremcp.Params{
		TokenSvc: tokenSvc,
		BaseURL:  "http://localhost",
		Domain:   "agentrq.com",
		Mux:      mux,
	})

	// Make a POST request with NO body!
	req := httptest.NewRequest("POST", "https://mcp.agentrq.com/oauth2/token", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	fmt.Println("Empty POST:")
	fmt.Println(w.Code)
	fmt.Println(w.Body.String())

	// Make a POST request with grant_type=authorization_code
	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	req2 := httptest.NewRequest("POST", "https://mcp.agentrq.com/oauth2/token", strings.NewReader(formData.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	fmt.Println("POST with grant_type=authorization_code:")
	fmt.Println(w2.Code)
	fmt.Println(w2.Body.String())
}
