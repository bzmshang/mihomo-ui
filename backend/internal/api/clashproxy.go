package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// patchPath is the kernel endpoint the panel's own switches go through. It is
// the one request this proxy has to understand rather than just forward: the
// panel applies a switch to the running kernel and forgets it, while a switch
// made here has to outlive the next config apply.
const patchPath = "/configs"

// patchBodyKey carries the patch body from the director, which is the only
// place with the body to read, to the response callback, which is the only place
// that knows the kernel accepted it.
type patchBodyKey struct{}

// captureLimit bounds how much of a patch body is kept. Real patches are a
// handful of keys; anything larger is not a settings switch and is left alone.
const captureLimit = 64 << 10

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
		if body, ok := capturePatch(r); ok {
			*r = *r.WithContext(context.WithValue(r.Context(), patchBodyKey{}, body))
		}
	}
	proxy.ModifyResponse = func(res *http.Response) error {
		s.recordConfigPatch(res)
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("clash proxy %s: %v", r.URL.Path, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return proxy
}

// capturePatch reads a config patch and rewinds the body so the proxied request
// is still whole — reading it would otherwise leave the transport with nothing
// to send. It reports false for every other request, which is all of them.
func capturePatch(r *http.Request) ([]byte, bool) {
	if r.Method != http.MethodPatch || r.URL.Path != patchPath || r.Body == nil {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, captureLimit))
	if err != nil {
		// Unreadable means the hop is about to fail anyway; let it.
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	return body, true
}

// recordConfigPatch mirrors an accepted panel switch into the panel's own
// preferences.
//
// zashboard applies a switch with PATCH /configs and stores nothing, so without
// this the value lives only in the running kernel: the next apply rebuilds
// config.yaml from override.yaml, the subscription and these prefs, and the
// switch reverts with no sign of why. Recording it here is what makes the TUN
// toggle in the panel behave like a setting.
//
// Only a kernel that accepted the patch (2xx) is recorded, and only for the
// keys this panel owns. Persisting happens before the config.yaml mirror, the
// same order the /api handlers use, so an apply racing this cannot rebuild from
// the stale value.
func (s *Server) recordConfigPatch(res *http.Response) {
	if res.Request == nil || res.StatusCode < 200 || res.StatusCode >= 300 {
		return
	}
	body, _ := res.Request.Context().Value(patchBodyKey{}).([]byte)
	if len(body) == 0 {
		return
	}
	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		return
	}

	mirror := map[string]any{}
	if mode, _ := patch["mode"].(string); validMode(mode) {
		if err := s.Store.SetMode(mode); err == nil {
			mirror["mode"] = mode
		}
	}
	if level, _ := patch["log-level"].(string); validLogLevel(level) {
		if err := s.Store.SetLogLevel(level); err == nil {
			mirror["log-level"] = level
		}
	}
	if tun, ok := patch["tun"].(map[string]any); ok {
		tunMirror := map[string]any{}
		if enable, ok := tun["enable"].(bool); ok {
			if err := s.Store.SetTunEnable(enable); err == nil {
				tunMirror["enable"] = enable
			}
		}
		// A stack the panel switched away from is reported as ""; recording it
		// would pin the stack to nothing, so only a real one is taken.
		if stack, _ := tun["stack"].(string); stack != "" {
			if err := s.Store.SetTunStack(stack); err == nil {
				tunMirror["stack"] = stack
			}
		}
		if len(tunMirror) > 0 {
			mirror["tun"] = tunMirror
		}
	}
	if allowLan, ok := patch["allow-lan"].(bool); ok {
		if err := s.Store.SetAllowLan(allowLan); err == nil {
			mirror["allow-lan"] = allowLan
		}
	}
	if len(mirror) == 0 {
		return
	}
	// Best-effort: the kernel already took the value, and a failure here only
	// means the next apply reverts it — worth a line, not a failed request.
	if err := s.Config.PatchRuntime(mirror); err != nil {
		log.Printf("clash proxy: mirror config patch into %s: %v", s.ConfigPath, err)
	}
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
