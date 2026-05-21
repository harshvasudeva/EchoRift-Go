package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"echorift/backend/internal/auth"
	"echorift/backend/internal/channels"
	"echorift/backend/internal/config"
	"echorift/backend/internal/media"
	"echorift/backend/internal/messages"
	"echorift/backend/internal/organizations"
	"echorift/backend/internal/web"
	realtime "echorift/backend/internal/websocket"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	cfg    config.Config
	log    *slog.Logger
	db     *pgxpool.Pool
	server *http.Server
}

func New(cfg config.Config, log *slog.Logger, db *pgxpool.Pool) *Server {
	s := &Server{cfg: cfg, log: log, db: db}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.server = &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      recoverer(requestLogger(log)(secureHeaders(mux))),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
	return s
}

func (s *Server) Start() error {
	s.log.Info("http server starting", "addr", s.cfg.HTTPAddr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.HandleFunc("GET /version", s.version)
	mux.HandleFunc("GET /api/v1/ping", s.apiPing)

	authService := auth.NewService(s.db, s.cfg)
	realtimeHub := realtime.NewHub()

	authHandler := auth.NewHandler(authService, s.cfg.CookieSecure)
	authHandler.Register(mux)

	realtimeHandler := realtime.NewHandler(s.db, authService, realtimeHub, s.log)
	realtimeHandler.Register(mux)

	organizationHandler := organizations.NewHandler(s.db, authService)
	organizationHandler.Register(mux)

	channelHandler := channels.NewHandler(s.db, authService)
	channelHandler.Register(mux)

	messageHandler := messages.NewHandler(s.db, authService, realtimeHub)
	messageHandler.Register(mux)

	mediaHandler := media.NewHandler(s.db, authService, s.cfg)
	mediaHandler.Register(mux)

	mux.Handle("GET /", web.Handler())
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "error": "database_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"name": "echorift", "version": "dev"})
}

func (s *Server) apiPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"message": "pong"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
