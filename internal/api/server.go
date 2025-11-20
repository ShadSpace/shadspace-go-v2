package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
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
			Handler:      corsMiddleware(mux),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	// Register routes
	mux.HandleFunc("/health", api.healthHandler)
	mux.HandleFunc("/farmers", api.farmersHandler)
	mux.HandleFunc("/nodes", api.nodesHandler)
	mux.HandleFunc("/metrics", api.metricsHandler)

	return api
}

// corsMiddleware adds CORS headers to all responses
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}

// Start begins listening on API port
func (a *APIServer) Start() error {
	log.Printf("🚀 API Server starting on port %d", a.port)
	log.Printf("📡 Endpoints available:")
	log.Printf("   • Health:    http://localhost:%d/health", a.port)
	log.Printf("   • Nodes:     http://localhost:%d/nodes", a.port)
	log.Printf("   • Farmers:   http://localhost:%d/farmers", a.port)
	log.Printf("   • Metrics:   http://localhost:%d/metrics", a.port)

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
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
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

// metricsHandler provides network metrics
func (a *APIServer) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := a.masterNode.GetMetrics()

	response := map[string]interface{}{
		"total_farmers":    metrics.TotalFarmers,
		"active_farmers":   metrics.ActiveFarmers,
		"total_storage_gb": float64(metrics.TotalStorage) / (1024 * 1024 * 1024),
		"used_storage_gb":  float64(metrics.UsedStorage) / (1024 * 1024 * 1024),
		"files_stored":     metrics.FilesStored,
		"uploads_today":    metrics.UploadsToday,
		"downloads_today":  metrics.DownloadsToday,
	}

	writeJSONResponse(w, http.StatusOK, response)
}

// farmersHandler lists registered farmers
func (a *APIServer) farmersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	farmers := a.masterNode.GetFarmers()

	// Serialize farmers into JSON-friendly structures
	serialized := make([]map[string]interface{}, 0, len(farmers))
	for _, f := range farmers {
		if f == nil {
			continue
		}
		serialized = append(serialized, map[string]interface{}{
			"peer_id":          f.PeerID.String(),
			"node_name":        f.NodeName,
			"version":          f.Version,
			"storage_capacity": f.StorageCapacity,
			"used_storage":     f.UsedStorage,
			"reliability":      utils.RoundToTwoDecimals(f.Reliability),
			"location":         f.Location,
			"latency":          f.Latency,
			"uptime":           utils.RoundToTwoDecimals(f.Uptime),
			"last_seen":        f.LastSeen.Format(time.RFC3339),
			"last_sync":        f.LastSync.Format(time.RFC3339),
			"is_active":        f.IsActive,
			"is_primary":       f.IsPrimary,
			"addresses":        f.Addresses,
			"tags":             f.Tags,
		})
	}

	response := map[string]interface{}{
		"count":   len(serialized),
		"farmers": serialized,
	}

	writeJSONResponse(w, http.StatusOK, response)
}

// nodesHandler returns enhanced farmer data for frontend dashboard
func (a *APIServer) nodesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	farmers := a.masterNode.GetFarmers()

	// Transform farmers to match frontend StorageNode structure
	nodes := make([]map[string]interface{}, 0, len(farmers))
	for _, farmer := range farmers {
		if farmer == nil {
			continue
		}

		// Calculate storage in GB for frontend
		storageUsedGB := float64(farmer.UsedStorage) / (1024 * 1024 * 1024)
		storageTotalGB := float64(farmer.StorageCapacity) / (1024 * 1024 * 1024)
		storagePercentage := 0.0
		if storageTotalGB > 0 {
			storagePercentage = (storageUsedGB / storageTotalGB) * 100
		}

		// Determine status based on activity and tags
		status := "offline"
		if farmer.IsActive {
			if utils.Contains(farmer.Tags, "high-performance") {
				status = "online"
			} else if utils.Contains(farmer.Tags, "degraded") {
				status = "degraded"
			} else if utils.Contains(farmer.Tags, "syncing") {
				status = "syncing"
			} else {
				status = "online"
			}
		}

		// Calculate uptime percentage
		uptimePercentage := farmer.Uptime
		if farmer.Uptime > 100 {
			// If uptime is in seconds, convert to percentage
			days := farmer.Uptime / (24 * 60 * 60)
			if days >= 1 {
				uptimePercentage = 99.9
			} else {
				uptimePercentage = (farmer.Uptime / (24 * 60 * 60)) * 100
			}
			if uptimePercentage > 100 {
				uptimePercentage = 99.9
			}
		}

		// Extract IP address from farmer addresses if available
		ipAddress := "Unknown"
		if len(farmer.Addresses) > 0 {
			// Try to extract IP from the first address
			ipAddress = extractIPFromAddress(farmer.Addresses[0])
		}

		node := map[string]interface{}{
			"id":       farmer.NodeName + "-" + farmer.Location,
			"name":     farmer.NodeName,
			"status":   status,
			"location": farmer.Location,
			"storage": map[string]interface{}{
				"used":       utils.RoundToTwoDecimals(storageUsedGB),
				"total":      utils.RoundToTwoDecimals(storageTotalGB),
				"percentage": utils.RoundToTwoDecimals(storagePercentage),
			},
			"performance": utils.RoundToTwoDecimals(farmer.Reliability * 100),
			"latency":     farmer.Latency,
			"uptime":      utils.RoundToTwoDecimals(uptimePercentage),
			"version":     farmer.Version,
			"lastSync":    farmer.LastSync.Format(time.RFC3339),
			"ipAddress":   ipAddress,
			"isPrimary":   farmer.IsPrimary,
			"tags":        farmer.Tags,
		}

		nodes = append(nodes, node)
	}

	response := map[string]interface{}{
		"count": len(nodes),
		"nodes": nodes,
	}

	writeJSONResponse(w, http.StatusOK, response)
}

// extractIPFromAddress tries to extract IP address from multiaddress
func extractIPFromAddress(address string) string {
	// Simple extraction - you might want to use a proper multiaddress parser
	// This handles addresses like "/ip4/192.168.1.100/tcp/4001/p2p/..."
	if len(address) > 6 && address[:5] == "/ip4/" {
		// Find the next slash after IP
		start := 5
		end := start
		for end < len(address) && address[end] != '/' {
			end++
		}
		if end > start {
			return address[start:end]
		}
	}
	return "192.168.1.100" // fallback
}

// writeJSONResponse helper function to write JSON responses
func writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	wrapper := map[string]interface{}{
		"status": statusCode,
		"data":   data,
	}

	if err := json.NewEncoder(w).Encode(wrapper); err != nil {
		log.Printf("failed to write JSON response: %v", err)
	}
}
