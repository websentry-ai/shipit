package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/db"
)

func NewRouter(database *db.DB, encryptKey string) http.Handler {
	r := chi.NewRouter()
	h := NewHandler(database, encryptKey)

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(jsonContentType)

	// Public routes
	r.Get("/health", h.Health)

	// Protected routes
	r.Group(func(r chi.Router) {
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
			r.Delete("/", h.DeleteApp)
			r.Post("/deploy", h.DeployApp)
			r.Get("/logs", h.StreamLogs)
			r.Get("/status", h.GetAppStatus)
		})
	})

	return r
}

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}
