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
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	// Register routes
	mux.HandleFunc("/health", api.healthHandler)
	mux.HandleFunc("/farmers", api.farmersHandler)
	mux.HandleFunc("/nodes", api.nodesHandler)

	return api
}

// Start begins listening on API port
func (a *APIServer) Start() error {
	log.Printf("🚀 API Server starting on port %d", a.port)
	log.Printf("📡 Endpoints available:")
	log.Printf("   • Health:    http://localhost:%d/health", a.port)
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

// farmersHandler lists registerd farmers
func (a *APIServer) farmersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
			"storage_capacity": f.StorageCapacity,
			"used_storage":     f.UsedStorage,
			"reliability":      f.Reliability,
			"last_seen":        f.LastSeen.Format(time.RFC3339),
			"is_active":        f.IsActive,
			"addresses":        f.Addresses,
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	enhancedFarmers := a.masterNode.GetEnhancedFarmers()

	// Transform enhanced farmers to match frontend StorageNode structure
	nodes := make([]map[string]interface{}, 0, len(enhancedFarmers))
	for _, farmer := range enhancedFarmers {
		if farmer == nil {
			continue
		}

		// Get basic farmer info for storage calculations
		var storageCapacity uint64
		var usedStorage uint64

		basicFarmers := a.masterNode.GetFarmers()
		for _, basicFarmer := range basicFarmers {
			// Find matching farmer by some identifier (you might need to adjust this)
			if basicFarmer != nil {
				storageCapacity = basicFarmer.StorageCapacity
				usedStorage = basicFarmer.UsedStorage
				break
			}
		}

		// Calculate storage in GB for frontend
		storageUsedGB := float64(usedStorage) / (1024 * 1024 * 1024)
		storageTotalGB := float64(storageCapacity) / (1024 * 1024 * 1024)
		storagePercentage := 0.0
		if storageTotalGB > 0 {
			storagePercentage = (storageUsedGB / storageTotalGB) * 100
		}

		// Determine status based on tags and last sync
		status := "offline"
		if utils.Contains(farmer.Tags, "online") {
			if utils.Contains(farmer.Tags, "high-performance") {
				status = "online"
			} else if utils.Contains(farmer.Tags, "degraded") {
				status = "degraded"
			} else {
				status = "online"
			}
		}

		// Calculate uptime percentage (assuming uptime is in seconds, convert to percentage)
		uptimePercentage := farmer.Uptime
		if farmer.Uptime > 100 {
			// If uptime is in seconds, convert to percentage (assuming 100% for anything over 24h)
			days := farmer.Uptime / (24 * 60 * 60)
			if days > 1 {
				uptimePercentage = 99.9
			} else {
				uptimePercentage = (farmer.Uptime / (24 * 60 * 60)) * 100
			}
			if uptimePercentage > 100 {
				uptimePercentage = 99.9
			}
		}

		node := map[string]interface{}{
			"id":       farmer.NodeName + "-" + farmer.Location, // Generate unique ID
			"name":     farmer.NodeName,
			"status":   status,
			"location": farmer.Location,
			"storage": map[string]interface{}{
				"used":       utils.RoundToTwoDecimals(storageUsedGB),
				"total":      utils.RoundToTwoDecimals(storageTotalGB),
				"percentage": utils.RoundToTwoDecimals(storagePercentage),
			},
			"performance": utils.RoundToTwoDecimals(farmer.Uptime), // Using uptime as performance proxy
			"latency":     farmer.Latency,
			"uptime":      utils.RoundToTwoDecimals(uptimePercentage),
			"version":     farmer.Version,
			"lastSync":    farmer.LastSync.Format(time.RFC3339),
			"ipAddress":   "192.168.1.100", // You'll need to extract this from addresses
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
