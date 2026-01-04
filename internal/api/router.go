package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/db"
	"github.com/vigneshsubbiah/shipit/internal/web"
)

func NewRouter(database *db.DB, encryptKey string) http.Handler {
	r := chi.NewRouter()
	h := NewHandler(database, encryptKey)

	// Global middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	// Public routes
	r.Get("/health", h.Health)

	// API routes with JSON content type
	r.Group(func(r chi.Router) {
		r.Use(jsonContentType)
		r.Use(auth.Middleware(database))

		// Projects
		r.Route("/api/projects", func(r chi.Router) {
			r.Get("/", h.ListProjects)
			r.Post("/", h.CreateProject)

			r.Route("/{projectID}", func(r chi.Router) {
				r.Get("/", h.GetProject)
				r.Delete("/", h.DeleteProject)

				// Clusters under project
				r.Route("/clusters", func(r chi.Router) {
					r.Get("/", h.ListClusters)
					r.Post("/", h.ConnectCluster)
				})
			})
		})

		// Clusters (direct access)
		r.Route("/api/clusters/{clusterID}", func(r chi.Router) {
			r.Get("/", h.GetCluster)
			r.Delete("/", h.DeleteCluster)

			// Apps under cluster
			r.Route("/apps", func(r chi.Router) {
				r.Get("/", h.ListApps)
				r.Post("/", h.CreateApp)
			})
		})

		// Apps (direct access)
		r.Route("/api/apps/{appID}", func(r chi.Router) {
			r.Get("/", h.GetApp)
			r.Put("/", h.UpdateApp)
			r.Patch("/", h.UpdateApp)
			r.Delete("/", h.DeleteApp)
			r.Post("/deploy", h.DeployApp)
			r.Get("/logs", h.StreamLogs)
			r.Get("/status", h.GetAppStatus)
			r.Post("/rollback", h.RollbackApp)

			// Secrets under app
			r.Route("/secrets", func(r chi.Router) {
				r.Get("/", h.ListSecrets)
				r.Post("/", h.SetSecret)
				r.Delete("/{key}", h.DeleteSecret)
			})

			// Revisions under app
			r.Route("/revisions", func(r chi.Router) {
				r.Get("/", h.ListRevisions)
				r.Get("/{revision}", h.GetRevision)
			})

			// Autoscaling (HPA)
			r.Get("/autoscaling", h.GetAutoscaling)
			r.Put("/autoscaling", h.SetAutoscaling)

			// Custom domains
			r.Get("/domain", h.GetDomain)
			r.Put("/domain", h.SetDomain)
		})
	})

	// Serve the web dashboard for non-API routes
	r.NotFound(web.Handler().ServeHTTP)

	return r
}

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only set JSON content type for API routes
		if strings.HasPrefix(r.URL.Path, "/api") {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}
