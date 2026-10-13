package api

import (
	"net/http"
	"strings"

	"github.com/bzmshang/mihomo-ui/internal/configsvc"
	"github.com/bzmshang/mihomo-ui/internal/mihomo"
	"github.com/bzmshang/mihomo-ui/internal/store"
	"github.com/bzmshang/mihomo-ui/internal/web"
)

type Server struct {
	Mihomo     *mihomo.Client
	Store      *store.Store
	UIPassword string
	ConfigPath string
	ConfigDir  string

	// Config owns mihomo/config.yaml; handlers must never write it themselves.
	Config *configsvc.Service

	// KernelAPI is the kernel's control-API address (host:port) and KernelSecret
	// the credential it expects. The panel injects the secret when it proxies a
	// browser request to the kernel, so the browser never holds it.
	KernelAPI    string
	KernelSecret string

	sessions *sessionStore
}

func (s *Server) Routes() http.Handler {
	s.sessions = newSessionStore()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/mode", s.handleMode)
	mux.HandleFunc("/api/tun", s.handleTun)
	mux.HandleFunc("/api/proxies", s.handleProxies)
	mux.HandleFunc("/api/proxies/select", s.handleSelect)
	mux.HandleFunc("/api/proxies/delay", s.handleDelay)
	mux.HandleFunc("/api/group/delay", s.handleGroupDelay)
	mux.HandleFunc("/api/config/list", s.handleConfigList)
	mux.HandleFunc("/api/config/apply", s.handleApply)
	mux.HandleFunc("/api/config/refresh", s.handleRefreshConfigs)
	mux.HandleFunc("/api/config/", s.handleConfigItem)
	mux.HandleFunc("/api/config", s.handleConfigCreate)
	mux.HandleFunc("/api/providers/update", s.handleUpdateProvider)
	mux.HandleFunc("/api/runtime", s.handleRuntime)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/log-level", s.handleLogLevel)
	mux.HandleFunc("/api/traffic", s.handleTraffic)
	mux.HandleFunc("/api/connections", s.handleConnections)

	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/check", s.handleAuthCheck)

	// The panel is zashboard, which speaks the kernel's own API. Both the
	// subtree and the bare prefix need registering: ServeMux does not let
	// "/clash/" match "/clash".
	mux.Handle(clashPrefix+"/", s.clashProxy())
	mux.Handle(clashPrefix, s.clashProxy())

	mux.Handle("/", web.Handler())
	// We serve the SPA ourselves, so everything is same-origin: no CORS headers.
	return s.withAuth(mux)
}

// withAuth gates the two API surfaces. /api/* wants a session token except for
// health and the login handshake; /clash/* wants one too, since it is the
// kernel's control API wearing this panel's origin. Static assets and the login
// page stay public — the page is how a visitor gets a token in the first place.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/"):
			switch r.URL.Path {
			case "/api/health", "/api/login", "/api/logout", "/api/auth/check":
				next.ServeHTTP(w, r)
				return
			}
			if !s.authorized(r) {
				writeUnauthorized(w)
				return
			}
		case r.URL.Path == clashPrefix || strings.HasPrefix(r.URL.Path, clashPrefix+"/"):
			if !s.clashAuthorized(r) {
				writeUnauthorized(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mihomo-ui"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}
