package api

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// clashPrefix is where the kernel's control API is mounted for the panel.
//
// zashboard talks to a Clash API directly and stores whatever credential it is
// given, so the browser is never handed the kernel secret. It holds a panel
// session token instead, and this proxy swaps that for the per-boot secret on
// the way out. The prefix keeps those routes apart from the panel's own /api
// surface and from the SPA's assets.
const clashPrefix = "/clash"

// clashProxy forwards /clash/* to the mihomo control API.
func (s *Server) clashProxy() http.Handler {
	if s.KernelAPI == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kernel control API is not configured"})
		})
	}
	target := &url.URL{Scheme: "http", Host: s.KernelAPI}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		// Strip the mount point from the decoded and the raw form alike: the
		// transport re-encodes RawPath, so a name mihomo percent-encoded (a proxy
		// name containing a slash) only survives the trip if both agree.
		r.URL.Path = trimClashPrefix(r.URL.Path)
		if r.URL.RawPath != "" {
			r.URL.RawPath = trimClashPrefix(r.URL.RawPath)
		}
		r.Host = target.Host
		// The caller's token authenticated this hop; the kernel wants its own.
		r.Header.Set("Authorization", "Bearer "+s.KernelSecret)
		// zashboard appends ?token= to WebSocket URLs because a browser cannot set
		// headers on one. The header above already covers this hop, so drop it
		// rather than hand the kernel a credential it does not need.
		if q := r.URL.Query(); q.Has("token") {
			q.Del("token")
			r.URL.RawQuery = q.Encode()
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("clash proxy %s: %v", r.URL.Path, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return proxy
}

func trimClashPrefix(p string) string {
	p = strings.TrimPrefix(p, clashPrefix)
	if p == "" {
		return "/"
	}
	return p
}

// clashAuthorized accepts a panel session token from the Authorization header,
// or — for a WebSocket handshake only — from ?token=. A browser cannot set
// headers on a WebSocket, so that one case has no alternative; everywhere else
// credentials stay out of the URL, where they would land in logs and history.
// The panel password is never accepted here.
func (s *Server) clashAuthorized(r *http.Request) bool {
	if s.UIPassword == "" {
		return true
	}
	if s.sessions.valid(bearerToken(r)) {
		return true
	}
	if isWebSocketUpgrade(r) {
		return s.sessions.valid(r.URL.Query().Get("token"))
	}
	return false
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		headerHasToken(r.Header.Get("Connection"), "upgrade")
}

// headerHasToken reports whether a comma-separated header lists token. Written
// out rather than pulled from x/net/httpguts, which this module does not depend on.
func headerHasToken(header, token string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}
