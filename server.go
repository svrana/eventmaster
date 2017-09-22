package eventmaster

import (
	"net/http"
	"path/filepath"

	assetfs "github.com/elazarl/go-bindata-assetfs"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	tmpl "github.com/ContextLogic/eventmaster/templates"
	"github.com/ContextLogic/eventmaster/ui"
)

// Server implements http.Handler for the eventmaster http server.
type Server struct {
	store *EventStore

	handler http.Handler

	ui        http.FileSystem
	templates TemplateGetter
}

// ServeHTTP dispatches to the underlying router.
func (srv *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	srv.handler.ServeHTTP(w, req)
}

// NewServer returns a ready-to-use Server that uses store, and the appropriate
// static and templates facilities.
//
// If static or templates are non-empty then files are served from those
// locations (useful for development). Otherwise the server uses embedded
// static assets.
func NewServer(store *EventStore, static, templates string) *Server {
	// Handle static files either embedded (empty static) or off the filesystem (during dev work)
	var fs http.FileSystem
	switch static {
	case "":
		fs = &assetfs.AssetFS{
			Asset:     ui.Asset,
			AssetDir:  ui.AssetDir,
			AssetInfo: ui.AssetInfo,
		}
	default:
		if p, d := filepath.Split(static); d == "ui" {
			static = p
		}
		fs = http.Dir(static)
	}

	var t TemplateGetter
	switch templates {
	case "":
		t = NewAssetTemplate(tmpl.Asset)
	default:
		t = Disk{Root: templates}
	}

	srv := &Server{
		store:     store,
		ui:        fs,
		templates: t,
	}

	srv.handler = registerRoutes(srv)

	return srv
}

func registerRoutes(srv *Server) http.Handler {
	r := httprouter.New()

	// API endpoints
	r.POST("/v1/event", wrapHandler(srv.handleAddEvent))
	r.GET("/v1/event", wrapHandler(srv.handleGetEvent))
	r.GET("/v1/event/:id", wrapHandler(srv.handleGetEventByID))
	r.POST("/v1/topic", wrapHandler(srv.handleAddTopic))
	r.PUT("/v1/topic/:name", wrapHandler(srv.handleUpdateTopic))
	r.GET("/v1/topic", wrapHandler(srv.handleGetTopic))
	r.DELETE("/v1/topic/:name", wrapHandler(srv.handleDeleteTopic))
	r.POST("/v1/dc", wrapHandler(srv.handleAddDc))
	r.PUT("/v1/dc/:name", wrapHandler(srv.handleUpdateDc))
	r.GET("/v1/dc", wrapHandler(srv.handleGetDc))

	r.GET("/v1/health", wrapHandler(srv.handleHealthCheck))

	// GitHub webhook endpoint
	r.POST("/v1/github_event", wrapHandler(srv.handleGitHubEvent))

	// UI endpoints
	r.GET("/", srv.HandleMainPage)
	r.GET("/add_event", srv.HandleCreatePage)
	r.GET("/topic", srv.HandleTopicPage)
	r.GET("/dc", srv.HandleDcPage)
	r.GET("/event", srv.HandleGetEventPage)

	// grafana datasource endpoints
	r.GET("/grafana", cors(srv.grafanaOK))
	r.GET("/grafana/", cors(srv.grafanaOK))
	r.OPTIONS("/grafana/:route", cors(srv.grafanaOK))
	r.POST("/grafana/annotations", cors(srv.grafanaAnnotations))
	r.POST("/grafana/search", cors(srv.grafanaSearch))

	r.Handler("GET", "/metrics", promhttp.Handler())

	r.Handler("GET", "/ui/*filepath", http.FileServer(srv.ui))

	return r
}
