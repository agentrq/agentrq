// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package oauthconsenttest walks an authorize request through the consent
// page the way a person's browser does, for the handlers' own tests.
package oauthconsenttest

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
)

var (
	consentField  = regexp.MustCompile(`name="consent" value="([^"]*)"`)
	checkedAccess = regexp.MustCompile(`name="access" value="([^"]*)" checked`)
)

// Allow serves the GET authorize request and, when it shows the consent
// page, presses Allow on it. Any other response is returned as it is.
func Allow(h http.Handler, get *http.Request) *httptest.ResponseRecorder {
	return Decide(h, get, "allow")
}

// Deny is Allow with the other button.
func Deny(h http.Handler, get *http.Request) *httptest.ResponseRecorder {
	return Decide(h, get, "deny")
}

// AllowAs is Allow with access chosen on a page that offers a choice.
func AllowAs(h http.Handler, get *http.Request, access string) *httptest.ResponseRecorder {
	return decide(h, get, "allow", access)
}

// Decide serves the GET authorize request and, when it shows the consent
// page, posts decision back with the page's own consent token, and the access
// the page has chosen when it offers a choice.
func Decide(h http.Handler, get *http.Request, decision string) *httptest.ResponseRecorder {
	return decide(h, get, decision, "")
}

func decide(h http.Handler, get *http.Request, decision, access string) *httptest.ResponseRecorder {
	page := httptest.NewRecorder()
	h.ServeHTTP(page, get)
	if page.Code != http.StatusOK {
		return page
	}
	form := url.Values{"consent": {Token(page.Body.String())}, "decision": {decision}}
	if access == "" {
		access = Checked(page.Body.String())
	}
	if access != "" {
		form.Set("access", access)
	}
	return Post(h, get, form)
}

// Checked is the access a rendered page has chosen, or "" if it offers none.
func Checked(page string) string {
	if m := checkedAccess.FindStringSubmatch(page); m != nil {
		return html.UnescapeString(m[1])
	}
	return ""
}

// Token is the consent token a rendered page carries, or "" if it has none.
func Token(page string) string {
	m := consentField.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	return html.UnescapeString(m[1])
}

// Post sends form to the URL get was for, as the person's browser would when
// the page's form is submitted: same host, same cookies.
func Post(h http.Handler, get *http.Request, form url.Values) *httptest.ResponseRecorder {
	post := httptest.NewRequest(http.MethodPost, get.URL.String(), strings.NewReader(form.Encode()))
	post.Host = get.Host
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range get.Cookies() {
		post.AddCookie(c)
	}
	if v := get.PathValue("workspaceID"); v != "" {
		post.SetPathValue("workspaceID", v)
	}
	if v := get.Header.Get("X-Forwarded-Proto"); v != "" {
		post.Header.Set("X-Forwarded-Proto", v)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, post)
	return w
}
