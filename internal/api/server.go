package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
)

// APIServer handles HTTP API endpoints for the master node
type APIServer struct {
	server     *http.Server
	masterNode types.MasterNodeInterface
	port       int
}

// NewAPIServer creates a new API server instance
func NewAPIServer(masterNode types.MasterNodeInterface, port int) *APIServer {
	mux := http.NewServeMux()

	api := &APIServer{
		masterNode: masterNode,
		port:       port,
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	// Register routes 
	mux.HandleFunc("/health", api.healthHandler)

	return api
}

// Start begins listening on API port
func (a *APIServer) Start() error {
	log.Printf("API server starting on port %d", a.port)

	if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("API server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the API server
func (a *APIServer) Shutdown(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}

// healthHandler provides basic health check endpoint
func (a *APIServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
	}

	writeJSONResponse(w, http.StatusOK, response)
}

// writeJSONResponse helper function to write JSON responses
func writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	// Simple JSON response
	jsonStr := fmt.Sprintf(`{"status":%d,"data":%v}`, statusCode, data)
	w.Write([]byte(jsonStr))
}