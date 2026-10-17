package server

import (
	"mime"
	"net"
	"net/http"
	"strings"

	"agentd/internal/api/httpx"
)

// guardMiddleware keeps web pages the user happens to open from driving the
// daemon (B-064). Three checks, all before any handler runs:
//
//   - Host must be a loopback name or an api.allowed_hosts entry, which stops
//     DNS rebinding from reaching the API under an attacker's hostname.
//   - A write that carries an Origin header must come from api.ui_origin.
//     Browsers always send Origin on cross-site writes and pages cannot forge
//     it. Clients that are not browsers send none and are unaffected. A
//     request from the UI origin is marked trusted, which stands in for the
//     materialize token (decision D1).
//   - A write with a body must be application/json. A text/plain POST is a CORS
//     "simple request" that needs no preflight, so the type is the gate.
//
// Bodiless writes such as workspace/ready skip the content-type rule.
// guardConfig holds the guard's settings from api.allowed_hosts and api.ui_origin.
type guardConfig struct {
	allowedHosts []string
	uiOrigin     string
}

func guardMiddleware(cfg guardConfig, next http.Handler) http.Handler {
	allowedHosts := cfg.allowedHosts
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, allowedHosts) {
			httpx.WriteError(w, http.StatusForbidden, CodeForbidden, "host not allowed; add it to api.allowed_hosts")
			return
		}
		if isWriteMethod(r.Method) {
			if origin := r.Header.Get("Origin"); origin != "" && !isUIOrigin(origin, cfg.uiOrigin) {
				httpx.WriteError(w, http.StatusForbidden, CodeForbidden, "cross-site write refused")
				return
			}
			if r.ContentLength != 0 && !isJSONContentType(r.Header.Get("Content-Type")) {
				httpx.WriteError(w, http.StatusUnsupportedMediaType, CodeBadRequest, "Content-Type must be application/json")
				return
			}
		}
		if isUIOrigin(r.Header.Get("Origin"), cfg.uiOrigin) {
			r = r.WithContext(httpx.WithTrustedOrigin(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}

// isUIOrigin reports whether origin is the configured UI origin. An empty
// configuration trusts no origin.
func isUIOrigin(origin, uiOrigin string) bool {
	want := strings.TrimRight(strings.TrimSpace(uiOrigin), "/")
	if want == "" {
		return false
	}
	return origin == want
}

func isWriteMethod(m string) bool {
	switch m {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func isJSONContentType(header string) bool {
	mt, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return mt == "application/json"
}

// hostAllowed accepts loopback hosts and configured entries. An entry without
// a port matches any port; an entry with one must match exactly.
func hostAllowed(hostport string, allowed []string) bool {
	host := strings.ToLower(hostport)
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = strings.ToLower(h)
	} else {
		host = strings.Trim(host, "[]")
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" && (a == host || a == strings.ToLower(hostport)) {
			return true
		}
	}
	return false
}
