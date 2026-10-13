package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The panel is a Clash API client now, so the kernel's own API is reachable
// through it. Everything below is about the boundary that creates: who may
// cross it, and what credential crosses with them.

// A browser session is the only ticket, and it is exchanged for the kernel
// secret rather than passed through.
func TestClashProxyRequiresSessionAndInjectsKernelSecret(t *testing.T) {
	e := newEnv(t)

	res := e.do("GET", "/clash/version", "", "")
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("unauthenticated /clash/version: status = %d, want 401", res.StatusCode)
	}

	// The panel password is not a ticket here either: it buys a token at
	// /api/login and nothing else, the same rule the panel API follows.
	res = e.do("GET", "/clash/version", testPassword, "")
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("panel password at /clash/version: status = %d, want 401", res.StatusCode)
	}

	token := e.login()
	code, body := e.json("GET", "/clash/echo", token, "")
	if code != 200 {
		t.Fatalf("/clash/echo with session: status = %d, want 200", code)
	}
	if got := body["auth"]; got != "Bearer test-secret" {
		t.Fatalf("kernel saw Authorization %v, want the injected kernel secret", got)
	}
	if got := body["path"]; got != "/echo" {
		t.Fatalf("kernel saw path %v, want the mount point stripped", got)
	}
}

// A WebSocket handshake cannot carry headers from a browser, so ?token= is
// accepted there — and only there, because a URL lands in logs and history.
func TestClashProxyAcceptsTokenInQueryOnlyForWebSockets(t *testing.T) {
	e := newEnv(t)
	token := e.login()

	res := e.do("GET", "/clash/traffic?token="+token, "", "")
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("plain GET with ?token=: status = %d, want 401", res.StatusCode)
	}

	req, err := http.NewRequest("GET", e.HTTP.URL+"/clash/traffic?token="+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	res, err = e.HTTP.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	// The fake kernel answers /traffic as a stream, so any non-401 status means
	// the handshake was let through. Nothing here speaks WebSocket.
	if res.StatusCode == 401 {
		t.Fatal("websocket handshake with ?token= was rejected")
	}
}

// The kernel has its own secret and does not want the panel's token. Forwarding
// it would put the token in the kernel's access log for nothing.
func TestClashProxyStripsTokenFromUpstreamQuery(t *testing.T) {
	e := newEnv(t)
	token := e.login()

	req, err := http.NewRequest("GET", e.HTTP.URL+"/clash/echo?token="+token+"&level=info", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	res, err := e.HTTP.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	query, _ := body["query"].(string)
	if strings.Contains(query, "token") {
		t.Fatalf("kernel received query %q, want the panel token removed", query)
	}
	if !strings.Contains(query, "level=info") {
		t.Fatalf("kernel received query %q, want other parameters kept", query)
	}
}

// A proxy name may contain a slash, and mihomo percent-encodes it. The mount
// point has to come off the encoded form too, or the request arrives at the
// kernel as /clash/proxies/... and 404s.
func TestClashProxyPreservesEncodedPath(t *testing.T) {
	e := newEnv(t)
	token := e.login()

	// The fake kernel echoes under /echo/, and an encoded slash keeps "a/b" a
	// single segment on the way in — so the echo path is the one that shows what
	// the kernel actually received.
	req, err := http.NewRequest("GET", e.HTTP.URL+"/clash/echo/a%2Fb", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := e.HTTP.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if got := body["path"]; got != "/echo/a/b" {
		t.Fatalf("kernel saw path %v, want the mount point stripped", got)
	}
}

// Static assets and the login page must stay public: they are how a visitor
// gets a token in the first place.
func TestPanelAssetsArePublicAndBootstrapIsInjected(t *testing.T) {
	e := newEnv(t)

	res := e.do("GET", "/", "", "")
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /: status = %d, want 200", res.StatusCode)
	}
	raw := make([]byte, 64*1024)
	n, _ := res.Body.Read(raw)
	page := string(raw[:n])
	if !strings.Contains(page, "/panel-bootstrap.js") {
		t.Fatal("served index.html does not load the panel bootstrap")
	}

	res = e.do("GET", "/panel-bootstrap.js", "", "")
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /panel-bootstrap.js: status = %d, want 200", res.StatusCode)
	}
}

// An unknown /api path must not fall through to the SPA: a client asking for a
// JSON endpoint should get a 404, not a page of HTML.
func TestUnknownApiPathIsNotServedAsSpa(t *testing.T) {
	e := newEnv(t)
	token := e.login()

	res := e.do("GET", "/api/nope", token, "")
	defer res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("GET /api/nope: status = %d, want 404", res.StatusCode)
	}
}

// The panel reaches the kernel through this proxy for everything zashboard
// needs, so a kernel that is down must read as a bad gateway rather than as a
// hung request.
func TestClashProxyReportsKernelFailure(t *testing.T) {
	e := newEnv(t)
	token := e.login()
	e.Kernel.Close()

	res := e.do("GET", "/clash/version", token, "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 when the kernel is gone", res.StatusCode)
	}
}
