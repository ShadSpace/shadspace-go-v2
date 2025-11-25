// api/server.go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/storage"
	"github.com/ShadSpace/shadspace-go-v2/pkg/types"
)

// GatewayInterface defines the interface that the API server expects
type GatewayInterface interface {
	GetGatewayInfo() *types.GatewayInfo
	GetNetworkStats() map[string]interface{}
	GetStorageStats() map[string]interface{}
	GetSystemStatus() map[string]interface{}
	GetNodeList() []types.NodeInfo
	GetFileList() []*storage.FileMetadata
	GetFileInfo(fileHash string) (*storage.FileMetadata, error)
	StoreFile(filename string, data []byte, totalShards, requiredShards int) (string, error)
	RetrieveFile(fileHash string) ([]byte, error)
	DeleteFile(fileHash string) error
}

// APIServer handles HTTP API endpoints for the gateway node
type APIServer struct {
	server  *http.Server
	gateway GatewayInterface
	port    int
}

// Response types
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
	Uptime    string    `json:"uptime,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}

type UploadResponse struct {
	Hash     string `json:"hash"`
	Status   string `json:"status"`
	FileSize int64  `json:"file_size"`
	Shards   int    `json:"shards"`
	Message  string `json:"message,omitempty"`
}

// NewAPIServer creates a new API server instance
func NewAPIServer(gateway GatewayInterface, port int) *APIServer {
	mux := http.NewServeMux()
	api := &APIServer{
		gateway: gateway,
		port:    port,
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      corsMiddleware(mux),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	// Register routes
	api.registerRoutes(mux)
	return api
}

func (a *APIServer) registerRoutes(mux *http.ServeMux) {
	// Health and info endpoints
	mux.HandleFunc("/health", a.healthHandler)
	mux.HandleFunc("/info", a.infoHandler)

	// Node endpoints
	mux.HandleFunc("/nodes", a.nodesHandler)
	mux.HandleFunc("/nodes/stats", a.nodesStatsHandler)

	// File endpoints
	mux.HandleFunc("/files", a.filesHandler)
	mux.HandleFunc("/files/upload", a.uploadHandler)
	mux.HandleFunc("/files/", a.fileHandler) // This will match /files/{hash} and /files/{hash}/download

	// Network endpoints
	mux.HandleFunc("/network/stats", a.networkStatsHandler)
	mux.HandleFunc("/network/health", a.networkHealthHandler)

	// Storage endpoints
	mux.HandleFunc("/storage/stats", a.storageStatsHandler)

	// System endpoints
	mux.HandleFunc("/system/status", a.systemStatusHandler)
}

// Start begins listening on API port
func (a *APIServer) Start() error {
	log.Printf("🚀 API Server starting on port %d", a.port)
	log.Printf("📡 Endpoints available:")
	log.Printf("   • Health:    http://localhost:%d/health", a.port)
	log.Printf("   • Info:      http://localhost:%d/info", a.port)
	log.Printf("   • Nodes:     http://localhost:%d/nodes", a.port)
	log.Printf("   • Files:     http://localhost:%d/files", a.port)
	log.Printf("   • Network:   http://localhost:%d/network/stats", a.port)
	log.Printf("   • Storage:   http://localhost:%d/storage/stats", a.port)
	log.Printf("   • System:    http://localhost:%d/system/status", a.port)

	if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("API server failed: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the API server
func (a *APIServer) Shutdown(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}

// Handler Methods

// healthHandler provides basic health check endpoint
func (a *APIServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	gatewayInfo := a.gateway.GetGatewayInfo()
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC(),
		Version:   gatewayInfo.Version,
		Uptime:    gatewayInfo.Uptime,
	}

	a.respondWithJSON(w, http.StatusOK, response)
}

// infoHandler returns gateway information
func (a *APIServer) infoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	gatewayInfo := a.gateway.GetGatewayInfo()
	response := map[string]interface{}{
		"gateway": gatewayInfo,
		"system": map[string]interface{}{
			"name":        "ShadSpace Gateway",
			"description": "Decentralized Storage Network Gateway",
			"version":     gatewayInfo.Version,
			"start_time":  gatewayInfo.StartTime,
		},
	}

	a.respondWithJSON(w, http.StatusOK, response)
}

// nodesHandler returns a list of all nodes in the network
func (a *APIServer) nodesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	nodes := a.gateway.GetNodeList()
	networkStats := a.gateway.GetNetworkStats()

	response := map[string]interface{}{
		"nodes": nodes,
		"total": len(nodes),
		"stats": map[string]interface{}{
			"total_peers": networkStats["total_peers"],
			"known_files": networkStats["known_files"],
			"dht_peers":   networkStats["dht_peers"],
		},
	}

	a.respondWithJSON(w, http.StatusOK, response)
}

// nodesStatsHandler returns statistics for nodes
func (a *APIServer) nodesStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	nodes := a.gateway.GetNodeList()
	networkStats := a.gateway.GetNetworkStats()

	// Calculate node statistics
	var totalStorage uint64
	var totalUsedStorage uint64
	var activeNodes int

	for _, node := range nodes {
		if node.IsActive {
			activeNodes++
			totalStorage += node.StorageCapacity
			totalUsedStorage += node.UsedStorage
		}
	}

	stats := map[string]interface{}{
		"total_nodes":       len(nodes),
		"active_nodes":      activeNodes,
		"total_storage":     totalStorage,
		"used_storage":      totalUsedStorage,
		"available_storage": totalStorage - totalUsedStorage,
		"utilization_rate":  float64(totalUsedStorage) / float64(totalStorage) * 100,
		"network_stats":     networkStats,
		"timestamp":         time.Now().UTC(),
	}

	a.respondWithJSON(w, http.StatusOK, stats)
}

// filesHandler returns a list of files stored through this gateway
func (a *APIServer) filesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	files := a.gateway.GetFileList()
	fileInfos := make([]map[string]interface{}, len(files))

	for i, file := range files {
		peerIDs := make([]string, len(file.StoredPeers))
		for j, peer := range file.StoredPeers {
			peerIDs[j] = peer.String()
		}

		fileInfos[i] = map[string]interface{}{
			"hash":            file.FileHash,
			"name":            file.FileName,
			"size":            file.FileSize,
			"total_shards":    file.TotalShards,
			"required_shards": file.RequiredShards,
			"created_at":      file.CreatedAt.Format(time.RFC3339),
			"updated_at":      file.UpdatedAt.Format(time.RFC3339),
			"stored_peers":    peerIDs,
			"mime_type":       file.MimeType,
		}
	}

	response := map[string]interface{}{
		"files": fileInfos,
		"total": len(fileInfos),
		"stats": map[string]interface{}{
			"total_size": calculateTotalSize(files),
		},
	}

	a.respondWithJSON(w, http.StatusOK, response)
}

// uploadHandler handles file uploads to the network
func (a *APIServer) uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only POST and OPTIONS methods are allowed")
		return
	}

	// Parse multipart form with increased size limit for larger files
	err := r.ParseMultipartForm(100 << 20) // 100MB max
	if err != nil {
		a.respondWithError(w, http.StatusBadRequest, "Invalid form data", "Failed to parse multipart form")
		return
	}

	// Get the file from the form
	file, header, err := r.FormFile("file")
	if err != nil {
		a.respondWithError(w, http.StatusBadRequest, "No file provided", "Please provide a file to upload")
		return
	}
	defer file.Close()

	// Read file data
	data, err := io.ReadAll(file)
	if err != nil {
		a.respondWithError(w, http.StatusInternalServerError, "Read failed", "Failed to read uploaded file")
		return
	}

	// Get sharding parameters from form with defaults
	totalShards, _ := strconv.Atoi(r.FormValue("total_shards"))
	requiredShards, _ := strconv.Atoi(r.FormValue("required_shards"))

	if totalShards == 0 {
		totalShards = 3 // Default
	}
	if requiredShards == 0 {
		requiredShards = 2 // Default
	}

	// Validate sharding parameters
	if requiredShards > totalShards {
		a.respondWithError(w, http.StatusBadRequest, "Invalid parameters", "Required shards cannot exceed total shards")
		return
	}

	// Store file in the network
	fileHash, err := a.gateway.StoreFile(header.Filename, data, totalShards, requiredShards)
	if err != nil {
		a.respondWithError(w, http.StatusInternalServerError, "Storage failed", "Failed to store file in the network")
		return
	}

	// Return success response
	response := UploadResponse{
		Hash:     fileHash,
		Status:   "uploaded",
		FileSize: int64(len(data)),
		Shards:   totalShards,
		Message:  fmt.Sprintf("File successfully stored with %d shards (%d required for reconstruction)", totalShards, requiredShards),
	}

	a.respondWithJSON(w, http.StatusCreated, response)
}

// fileHandler handles file operations (GET info, DELETE, download)
func (a *APIServer) fileHandler(w http.ResponseWriter, r *http.Request) {
	// Extract file hash from URL path
	path := strings.TrimPrefix(r.URL.Path, "/files/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		a.respondWithError(w, http.StatusBadRequest, "Invalid path", "File hash is required")
		return
	}

	fileHash := parts[0]

	switch r.Method {
	case http.MethodGet:
		if len(parts) > 1 && parts[1] == "download" {
			a.downloadFileHandler(w, r, fileHash)
		} else {
			a.getFileHandler(w, r, fileHash)
		}
	case http.MethodDelete:
		a.deleteFileHandler(w, r, fileHash)
	case http.MethodOptions:
		// Handle preflight for CORS
		w.WriteHeader(http.StatusOK)
	default:
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET, DELETE, and OPTIONS methods are allowed")
	}
}

// getFileHandler returns information about a specific file
func (a *APIServer) getFileHandler(w http.ResponseWriter, r *http.Request, fileHash string) {
	file, err := a.gateway.GetFileInfo(fileHash)
	if err != nil {
		a.respondWithError(w, http.StatusNotFound, "File not found", "The requested file was not found")
		return
	}

	peerIDs := make([]string, len(file.StoredPeers))
	for i, peer := range file.StoredPeers {
		peerIDs[i] = peer.String()
	}

	fileInfo := map[string]interface{}{
		"hash":            file.FileHash,
		"name":            file.FileName,
		"size":            file.FileSize,
		"total_shards":    file.TotalShards,
		"required_shards": file.RequiredShards,
		"created_at":      file.CreatedAt.Format(time.RFC3339),
		"updated_at":      file.UpdatedAt.Format(time.RFC3339),
		"stored_peers":    peerIDs,
		"mime_type":       file.MimeType,
	}

	a.respondWithJSON(w, http.StatusOK, fileInfo)
}

// downloadFileHandler allows downloading a file by its hash
func (a *APIServer) downloadFileHandler(w http.ResponseWriter, r *http.Request, fileHash string) {
	// Retrieve the file from the network
	data, err := a.gateway.RetrieveFile(fileHash)
	if err != nil {
		a.respondWithError(w, http.StatusInternalServerError, "Download failed", "Failed to retrieve file from the network")
		return
	}

	// Get file metadata for proper filename
	file, err := a.gateway.GetFileInfo(fileHash)
	var filename string
	if err != nil {
		// Use hash as filename if metadata not available
		filename = fileHash + ".bin"
	} else {
		filename = file.FileName
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("X-File-Hash", fileHash)

	// Write file data
	if _, err := w.Write(data); err != nil {
		log.Printf("Error writing file data: %v", err)
	}
}

// deleteFileHandler removes a file from the network
func (a *APIServer) deleteFileHandler(w http.ResponseWriter, r *http.Request, fileHash string) {
	err := a.gateway.DeleteFile(fileHash)
	if err != nil {
		a.respondWithError(w, http.StatusInternalServerError, "Deletion failed", "Failed to delete file from the network")
		return
	}

	response := map[string]interface{}{
		"status":  "deleted",
		"hash":    fileHash,
		"message": "File successfully deleted from the network",
	}

	a.respondWithJSON(w, http.StatusOK, response)
}

// networkStatsHandler returns overall network statistics
func (a *APIServer) networkStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	networkStats := a.gateway.GetNetworkStats()
	storageStats := a.gateway.GetStorageStats()

	response := map[string]interface{}{
		"network":   networkStats,
		"storage":   storageStats,
		"gateway":   a.gateway.GetGatewayInfo(),
		"timestamp": time.Now().UTC(),
	}

	a.respondWithJSON(w, http.StatusOK, response)
}

// networkHealthHandler returns the health status of the network
func (a *APIServer) networkHealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	networkStats := a.gateway.GetNetworkStats()

	// Determine network health based on available peers
	totalPeers := networkStats["total_peers"].(int)
	healthStatus := "healthy"
	if totalPeers == 0 {
		healthStatus = "disconnected"
	} else if totalPeers < 3 {
		healthStatus = "degraded"
	}

	health := map[string]interface{}{
		"status":     healthStatus,
		"node_count": totalPeers,
		"file_count": networkStats["known_files"],
		"dht_peers":  networkStats["dht_peers"],
		"timestamp":  time.Now().UTC(),
		"indicators": map[string]string{
			"p2p_connectivity": healthStatus,
			"storage_health":   "good",
			"api_responsive":   "good",
		},
	}

	a.respondWithJSON(w, http.StatusOK, health)
}

// storageStatsHandler returns storage statistics
func (a *APIServer) storageStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	stats := a.gateway.GetStorageStats()
	a.respondWithJSON(w, http.StatusOK, stats)
}

// systemStatusHandler returns overall system status
func (a *APIServer) systemStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodOptions {
		a.respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and OPTIONS methods are allowed")
		return
	}

	status := a.gateway.GetSystemStatus()
	a.respondWithJSON(w, http.StatusOK, status)
}

// Helper functions

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

// respondWithJSON sends a JSON response
func (a *APIServer) respondWithJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// respondWithError sends an error response
func (a *APIServer) respondWithError(w http.ResponseWriter, statusCode int, errorType string, message string) {
	errorResponse := ErrorResponse{
		Error:   errorType,
		Message: message,
		Code:    statusCode,
	}
	a.respondWithJSON(w, statusCode, errorResponse)
}

// calculateTotalSize calculates total size of files
func calculateTotalSize(files []*storage.FileMetadata) int64 {
	var total int64
	for _, file := range files {
		total += file.FileSize
	}
	return total
}
