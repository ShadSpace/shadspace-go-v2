package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ShadSpace/shadspace-go-v2/internal/network"
	"github.com/ShadSpace/shadspace-go-v2/pkg/utils"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

// GatewayServer handles HTTP API requests and serves as an interface to the P2P network
type GatewayServer struct {
	node *network.DecentralizedNode
}

// NodeInfo represents information about a node in the network
type NodeInfo struct {
	ID              string   `json:"id"`
	Addresses       []string `json:"addresses"`
	StorageCapacity int64    `json:"storage_capacity"`
	UsedStorage     int64    `json:"used_storage"`
	Reliability     float64  `json:"reliability"`
	IsActive        bool     `json:"is_active"`
	LastSeen        string   `json:"last_seen,omitempty"`
	NodeType        string   `json:"node_type,omitempty"`
}

// FileInfo represents information about a stored file
type FileInfo struct {
	Hash           string   `json:"hash"`
	Name           string   `json:"name"`
	Size           int64    `json:"size"`
	TotalShards    int      `json:"total_shards"`
	RequiredShards int      `json:"required_shards"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	StoredPeers    []string `json:"stored_peers"`
	MimeType       string   `json:"mime_type,omitempty"`
}

// NetworkStats represents overall network statistics
type NetworkStats struct {
	TotalNodes        int     `json:"total_nodes"`
	TotalFiles        int     `json:"total_files"`
	TotalStorage      int64   `json:"total_storage"`
	UsedStorage       int64   `json:"used_storage"`
	AvailableStorage  int64   `json:"available_storage"`
	AverageReputation float64 `json:"average_reputation"`
	NetworkHealth     string  `json:"network_health"`
	Timestamp         string  `json:"timestamp"`
}

// UploadResponse represents the response after a successful file upload
type UploadResponse struct {
	Hash     string `json:"hash"`
	Status   string `json:"status"`
	FileSize int64  `json:"file_size"`
	Shards   int    `json:"shards"`
	Message  string `json:"message,omitempty"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}

// NewGatewayServer creates a new gateway server instance
func NewGatewayServer(node *network.DecentralizedNode) *GatewayServer {
	return &GatewayServer{node: node}
}

// Start begins the HTTP API server
func (gs *GatewayServer) Start(port int) {
	router := mux.NewRouter()

	// API routes
	api := router.PathPrefix("/api/v1").Subrouter()

	// Node endpoints
	api.HandleFunc("/nodes", gs.getNodes).Methods("GET")
	api.HandleFunc("/nodes/{id}", gs.getNode).Methods("GET")
	api.HandleFunc("/nodes/{id}/stats", gs.getNodeStats).Methods("GET")

	// File endpoints
	api.HandleFunc("/files", gs.getFiles).Methods("GET")
	api.HandleFunc("/files/{hash}", gs.getFile).Methods("GET")
	api.HandleFunc("/files/{hash}/download", gs.downloadFile).Methods("GET")
	api.HandleFunc("/files/{hash}/info", gs.getFileInfo).Methods("GET")
	api.HandleFunc("/files/upload", gs.uploadFile).Methods("POST")
	api.HandleFunc("/files/{hash}", gs.deleteFile).Methods("DELETE")

	// Network endpoints
	api.HandleFunc("/network/stats", gs.getNetworkStats).Methods("GET")
	api.HandleFunc("/network/health", gs.getNetworkHealth).Methods("GET")
	api.HandleFunc("/network/peers", gs.getNetworkPeers).Methods("GET")

	// Storage endpoints
	api.HandleFunc("/storage/capacity", gs.getStorageCapacity).Methods("GET")
	api.HandleFunc("/storage/usage", gs.getStorageUsage).Methods("GET")
	api.HandleFunc("/storage/stats", gs.getStorageStats).Methods("GET")

	// System endpoints
	api.HandleFunc("/system/status", gs.getSystemStatus).Methods("GET")
	api.HandleFunc("/system/info", gs.getSystemInfo).Methods("GET")

	// Root endpoint
	router.HandleFunc("/", gs.handleRoot).Methods("GET")
	router.HandleFunc("/health", gs.healthCheck).Methods("GET")

	// Add CORS support
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
		Debug:            false,
	})

	// Add logging middleware
	loggedRouter := gs.loggingMiddleware(router)

	// Start server
	addr := fmt.Sprintf(":%d", port)
	log.Printf("🚀 ShadSpace Gateway Server starting on http://localhost%s", addr)
	log.Printf("📊 API available at http://localhost%s/api/v1", addr)
	log.Printf("🔍 Health check at http://localhost%s/health", addr)

	server := &http.Server{
		Addr:         addr,
		Handler:      corsHandler.Handler(loggedRouter),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// handleRoot returns basic API information
func (gs *GatewayServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"name":        "ShadSpace Gateway API",
		"version":     "1.0.0",
		"description": "Decentralized Storage Network Gateway",
		"endpoints": map[string]string{
			"nodes":   "/api/v1/nodes",
			"files":   "/api/v1/files",
			"network": "/api/v1/network",
			"storage": "/api/v1/storage",
			"system":  "/api/v1/system",
			"health":  "/health",
		},
		"node_id": gs.node.GetHostID(),
	}
	gs.respondWithJSON(w, http.StatusOK, response)
}

// healthCheck performs a basic health check
func (gs *GatewayServer) healthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"node": map[string]interface{}{
			"id":      gs.node.GetHostID(),
			"running": true,
		},
		"services": map[string]string{
			"p2p_network":  "active",
			"file_storage": "active",
			"api":          "active",
		},
	}
	gs.respondWithJSON(w, http.StatusOK, health)
}

// getNodes returns a list of all nodes in the network
func (gs *GatewayServer) getNodes(w http.ResponseWriter, r *http.Request) {
	// Get network statistics to include basic node count
	networkStats := gs.node.GetNetworkStats()

	// For now, we'll return the gateway node info plus any known peers
	// In a full implementation, this would query the network view for all peers
	nodes := []NodeInfo{
		{
			ID:              gs.node.GetHostID(),
			Addresses:       []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", gs.getNodePort())},
			StorageCapacity: 10 * 1024 * 1024 * 1024, // 10GB
			UsedStorage:     gs.getUsedStorage(),
			Reliability:     1.0,
			IsActive:        true,
			LastSeen:        time.Now().Format(time.RFC3339),
			NodeType:        "gateway",
		},
	}

	response := map[string]interface{}{
		"nodes": nodes,
		"total": len(nodes),
		"stats": map[string]interface{}{
			"total_peers": networkStats["total_peers"],
			"known_files": networkStats["known_files"],
		},
	}

	gs.respondWithJSON(w, http.StatusOK, response)
}

// getNode returns information about a specific node
func (gs *GatewayServer) getNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["id"]

	// Check if the requested node is this gateway node
	if nodeID == gs.node.GetHostID() {
		nodeInfo := NodeInfo{
			ID:              gs.node.GetHostID(),
			Addresses:       []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", gs.getNodePort())},
			StorageCapacity: 10 * 1024 * 1024 * 1024, // 10GB
			UsedStorage:     gs.getUsedStorage(),
			Reliability:     1.0,
			IsActive:        true,
			LastSeen:        time.Now().Format(time.RFC3339),
			NodeType:        "gateway",
		}
		gs.respondWithJSON(w, http.StatusOK, nodeInfo)
		return
	}

	// For other nodes, we would query the network view
	// This is a simplified implementation
	gs.respondWithError(w, http.StatusNotFound, "Node not found", "The requested node is not currently known to this gateway")
}

// getNodeStats returns statistics for a specific node
func (gs *GatewayServer) getNodeStats(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodeID := vars["id"]

	if nodeID != gs.node.GetHostID() {
		gs.respondWithError(w, http.StatusNotFound, "Node not found", "Only gateway node statistics are currently available")
		return
	}

	fileStats := gs.node.GetFileManager().GetFileStats()
	networkStats := gs.node.GetNetworkStats()

	stats := map[string]interface{}{
		"node_id":   gs.node.GetHostID(),
		"timestamp": time.Now().Format(time.RFC3339),
		"storage": map[string]interface{}{
			"total_capacity":    10 * 1024 * 1024 * 1024,
			"used_storage":      gs.getUsedStorage(),
			"available_storage": 10*1024*1024*1024 - gs.getUsedStorage(),
			"usage_percentage":  float64(gs.getUsedStorage()) / float64(10*1024*1024*1024) * 100,
		},
		"files": map[string]interface{}{
			"total_files": fileStats["total_files"],
			"total_size":  fileStats["total_file_size"],
			"avg_size":    fileStats["average_file_size"],
		},
		"network": map[string]interface{}{
			"known_peers":    networkStats["total_peers"],
			"known_files":    networkStats["known_files"],
			"dht_peers":      networkStats["dht_peers"],
			"avg_reputation": networkStats["avg_reputation"],
		},
	}

	gs.respondWithJSON(w, http.StatusOK, stats)
}

// getFiles returns a list of all files stored on this node
func (gs *GatewayServer) getFiles(w http.ResponseWriter, r *http.Request) {
	fileManager := gs.node.GetFileManager()
	files := fileManager.ListFiles()

	fileInfos := make([]FileInfo, len(files))
	for i, file := range files {
		peerIDs := make([]string, len(file.StoredPeers))
		for j, peer := range file.StoredPeers {
			peerIDs[j] = peer.String()
		}

		fileInfos[i] = FileInfo{
			Hash:           file.FileHash,
			Name:           file.FileName,
			Size:           file.FileSize,
			TotalShards:    file.TotalShards,
			RequiredShards: file.RequiredShards,
			CreatedAt:      file.CreatedAt.Format(time.RFC3339),
			UpdatedAt:      file.UpdatedAt.Format(time.RFC3339),
			StoredPeers:    peerIDs,
			MimeType:       file.MimeType,
		}
	}

	response := map[string]interface{}{
		"files": fileInfos,
		"total": len(fileInfos),
		"stats": map[string]interface{}{
			"total_size": gs.getUsedStorage(),
		},
	}

	gs.respondWithJSON(w, http.StatusOK, response)
}

// getFile returns information about a specific file
func (gs *GatewayServer) getFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileHash := vars["hash"]

	fileManager := gs.node.GetFileManager()
	file, err := fileManager.GetFileInfo(fileHash)
	if err != nil {
		gs.respondWithError(w, http.StatusNotFound, "File not found", "The requested file was not found on this node")
		return
	}

	peerIDs := make([]string, len(file.StoredPeers))
	for i, peer := range file.StoredPeers {
		peerIDs[i] = peer.String()
	}

	fileInfo := FileInfo{
		Hash:           file.FileHash,
		Name:           file.FileName,
		Size:           file.FileSize,
		TotalShards:    file.TotalShards,
		RequiredShards: file.RequiredShards,
		CreatedAt:      file.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      file.UpdatedAt.Format(time.RFC3339),
		StoredPeers:    peerIDs,
		MimeType:       file.MimeType,
	}

	gs.respondWithJSON(w, http.StatusOK, fileInfo)
}

// getFileInfo is an alias for getFile for compatibility
func (gs *GatewayServer) getFileInfo(w http.ResponseWriter, r *http.Request) {
	gs.getFile(w, r)
}

// downloadFile allows downloading a file by its hash
func (gs *GatewayServer) downloadFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileHash := vars["hash"]

	// Retrieve the file from the network
	data, err := gs.node.GetStorageOperations().RetrieveFileDistributed(fileHash)
	if err != nil {
		gs.respondWithError(w, http.StatusInternalServerError, "Download failed", "Failed to retrieve file from the network")
		return
	}

	// Get file metadata for proper filename
	fileManager := gs.node.GetFileManager()
	file, err := fileManager.GetFileInfo(fileHash)
	if err != nil {
		// Use hash as filename if metadata not available
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s.bin", fileHash))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", file.FileName))
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.Header().Set("X-File-Hash", fileHash)

	// Write file data
	if _, err := w.Write(data); err != nil {
		log.Printf("Error writing file data: %v", err)
	}
}

// uploadFile handles file uploads to the network
func (gs *GatewayServer) uploadFile(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form with increased size limit for larger files
	err := r.ParseMultipartForm(100 << 20) // 100MB max
	if err != nil {
		gs.respondWithError(w, http.StatusBadRequest, "Invalid form data", "Failed to parse multipart form")
		return
	}

	// Get the file from the form
	file, header, err := r.FormFile("file")
	if err != nil {
		gs.respondWithError(w, http.StatusBadRequest, "No file provided", "Please provide a file to upload")
		return
	}
	defer file.Close()

	// Read file data
	data, err := io.ReadAll(file)
	if err != nil {
		gs.respondWithError(w, http.StatusInternalServerError, "Read failed", "Failed to read uploaded file")
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
		gs.respondWithError(w, http.StatusBadRequest, "Invalid parameters", "Required shards cannot exceed total shards")
		return
	}

	// Store file in the network
	fileHash, err := gs.node.GetStorageOperations().StoreFileDistributed(header.Filename, data, totalShards, requiredShards)
	if err != nil {
		gs.respondWithError(w, http.StatusInternalServerError, "Storage failed", "Failed to store file in the network")
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

	gs.respondWithJSON(w, http.StatusCreated, response)
}

// deleteFile removes a file from the network
func (gs *GatewayServer) deleteFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileHash := vars["hash"]

	// Delete the file
	err := gs.node.GetStorageOperations().DeleteFileDistributed(fileHash)
	if err != nil {
		gs.respondWithError(w, http.StatusInternalServerError, "Deletion failed", "Failed to delete file from the network")
		return
	}

	response := map[string]interface{}{
		"status":  "deleted",
		"hash":    fileHash,
		"message": "File successfully deleted from the network",
	}

	gs.respondWithJSON(w, http.StatusOK, response)
}

// getNetworkStats returns overall network statistics
func (gs *GatewayServer) getNetworkStats(w http.ResponseWriter, r *http.Request) {
	stats := gs.node.GetNetworkStats()
	fileStats := gs.node.GetFileManager().GetFileStats()

	networkStats := NetworkStats{
		TotalNodes:        stats["total_peers"].(int),
		TotalFiles:        stats["known_files"].(int),
		TotalStorage:      0, // Would need to aggregate from all nodes
		UsedStorage:       gs.getUsedStorage(),
		AvailableStorage:  10*1024*1024*1024 - gs.getUsedStorage(),
		AverageReputation: stats["avg_reputation"].(float64),
		NetworkHealth:     "healthy",
		Timestamp:         time.Now().Format(time.RFC3339),
	}

	// Enhance with file statistics
	enhancedStats := map[string]interface{}{
		"network": networkStats,
		"gateway": map[string]interface{}{
			"node_id":        gs.node.GetHostID(),
			"local_files":    fileStats["total_files"],
			"local_storage":  fileStats["total_file_size"],
			"storage_engine": gs.node.GetStorageEngine().GetStats(),
		},
	}

	gs.respondWithJSON(w, http.StatusOK, enhancedStats)
}

// getNetworkHealth returns the health status of the network
func (gs *GatewayServer) getNetworkHealth(w http.ResponseWriter, r *http.Request) {
	stats := gs.node.GetNetworkStats()

	health := map[string]interface{}{
		"status":     "healthy",
		"node_count": stats["total_peers"],
		"file_count": stats["known_files"],
		"dht_peers":  stats["dht_peers"],
		"timestamp":  stats["last_updated"],
		"indicators": map[string]string{
			"p2p_connectivity": "good",
			"storage_health":   "good",
			"api_responsive":   "good",
		},
	}

	gs.respondWithJSON(w, http.StatusOK, health)
}

// getNetworkPeers returns information about connected peers
func (gs *GatewayServer) getNetworkPeers(w http.ResponseWriter, r *http.Request) {
	stats := gs.node.GetNetworkStats()

	// This is a simplified implementation
	// In a real scenario, you'd query the network view for detailed peer info
	response := map[string]interface{}{
		"total_peers": stats["total_peers"],
		"dht_peers":   stats["dht_peers"],
		"gateway_id":  gs.node.GetHostID(),
		"peers":       []string{}, // Would be populated with actual peer info
	}

	gs.respondWithJSON(w, http.StatusOK, response)
}

// getStorageCapacity returns storage capacity information
func (gs *GatewayServer) getStorageCapacity(w http.ResponseWriter, r *http.Request) {
	capacity := map[string]interface{}{
		"total_capacity":    10 * 1024 * 1024 * 1024, // 10GB
		"used_storage":      gs.getUsedStorage(),
		"available_storage": 10*1024*1024*1024 - gs.getUsedStorage(),
		"usage_percentage":  float64(gs.getUsedStorage()) / float64(10*1024*1024*1024) * 100,
		"node_id":           gs.node.GetHostID(),
		"timestamp":         time.Now().Format(time.RFC3339),
	}

	gs.respondWithJSON(w, http.StatusOK, capacity)
}

// getStorageUsage returns detailed storage usage statistics
func (gs *GatewayServer) getStorageUsage(w http.ResponseWriter, r *http.Request) {
	fileManager := gs.node.GetFileManager()
	stats := fileManager.GetFileStats()

	gs.respondWithJSON(w, http.StatusOK, stats)
}

// getStorageStats returns comprehensive storage statistics
func (gs *GatewayServer) getStorageStats(w http.ResponseWriter, r *http.Request) {
	fileStats := gs.node.GetFileManager().GetFileStats()
	storageStats := gs.node.GetStorageEngine().GetStats()
	opsStats := gs.node.GetStorageOperations().GetStorageStats()

	combinedStats := map[string]interface{}{
		"files":      fileStats,
		"storage":    storageStats,
		"operations": opsStats,
		"capacity": map[string]interface{}{
			"total":     10 * 1024 * 1024 * 1024,
			"used":      gs.getUsedStorage(),
			"available": 10*1024*1024*1024 - gs.getUsedStorage(),
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	gs.respondWithJSON(w, http.StatusOK, combinedStats)
}

// getSystemStatus returns overall system status
func (gs *GatewayServer) getSystemStatus(w http.ResponseWriter, r *http.Request) {
	networkStats := gs.node.GetNetworkStats()
	fileStats := gs.node.GetFileManager().GetFileStats()

	status := map[string]interface{}{
		"status":    "operational",
		"timestamp": time.Now().Format(time.RFC3339),
		"components": map[string]interface{}{
			"p2p_network": map[string]interface{}{
				"status":    "connected",
				"peers":     networkStats["total_peers"],
				"dht_nodes": networkStats["dht_peers"],
			},
			"file_storage": map[string]interface{}{
				"status":     "active",
				"files":      fileStats["total_files"],
				"total_size": fileStats["total_file_size"],
				"used_space": gs.getUsedStorage(),
			},
			"api_gateway": map[string]interface{}{
				"status": "running",
				"uptime": time.Since(gs.getStartTime()).String(),
			},
		},
		"performance": map[string]interface{}{
			"reputation": networkStats["avg_reputation"],
			"health":     "good",
		},
	}

	gs.respondWithJSON(w, http.StatusOK, status)
}

// getSystemInfo returns system information
func (gs *GatewayServer) getSystemInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"name":       "ShadSpace Gateway",
		"version":    "1.0.0",
		"node_id":    gs.node.GetHostID(),
		"start_time": gs.getStartTime().Format(time.RFC3339),
		"uptime":     time.Since(gs.getStartTime()).String(),
		"network":    "shadspace-gateway-network",
		"protocol":   "/shadspace/1.0.0",
		"capabilities": []string{
			"file_storage",
			"distributed_retrieval",
			"erasure_coding",
			"p2p_networking",
			"http_api",
		},
	}

	gs.respondWithJSON(w, http.StatusOK, info)
}

// Helper methods

func (gs *GatewayServer) getUsedStorage() int64 {
	fileManager := gs.node.GetFileManager()
	stats := fileManager.GetFileStats()
	if used, ok := stats["total_file_size"]; ok {
		return used.(int64)
	}
	return 0
}

func (gs *GatewayServer) getNodePort() int {
	// This would need to be stored in the node configuration
	// For now, return a default
	return 4001
}

func (gs *GatewayServer) getStartTime() time.Time {
	// In a real implementation, this would track when the server started
	return time.Now().Add(-5 * time.Minute) // Example: started 5 minutes ago
}

// respondWithJSON sends a JSON response
func (gs *GatewayServer) respondWithJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

// respondWithError sends an error response
func (gs *GatewayServer) respondWithError(w http.ResponseWriter, statusCode int, errorType string, message string) {
	errorResponse := ErrorResponse{
		Error:   errorType,
		Message: message,
		Code:    statusCode,
	}
	gs.respondWithJSON(w, statusCode, errorResponse)
}

// loggingMiddleware adds logging to all requests
func (gs *GatewayServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a custom ResponseWriter to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		log.Printf("%s %s %d %v", r.Method, r.URL.Path, rw.statusCode, duration)
	})
}

// Custom response writer to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func main() {
	// Parse command-line flags
	keyFile := flag.String("key", "", "Path to private key file")
	port := flag.Int("port", 8080, "Port for HTTP API")
	nodePort := flag.Int("node-port", 4015, "Port for P2P node")
	flag.Parse()

	// Validate inputs
	if *keyFile == "" {
		defaultKeyFile := filepath.Join("keys", "gateway_node.key")
		if err := os.MkdirAll(filepath.Dir(defaultKeyFile), 0700); err != nil {
			log.Fatalf("Failed to create keys directory: %v", err)
		}
		*keyFile = defaultKeyFile
	}

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load or generate private key
	privKey, err := utils.GetOrCreateKey(*keyFile)
	if err != nil {
		log.Fatalf("Failed to load or generate private key: %v", err)
	}

	// Create node configuration
	config := network.NodeConfig{
		Host:       "0.0.0.0",
		Port:       *nodePort,
		ProtocolID: "/shadspace/1.0.0",
		Rendezvous: "shadspace-network",
		NodeType:   "gateway",
		BootstrapPeers: []string{
			"/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWFgQMSugNGS6mW4drX2N5oQsAF7Jxa9CWgy8bAQaSHJaU",
		},
		PrivKey: privKey,
		KeyFile: *keyFile,
	}

	// Create decentralized node
	node, err := network.NewDecentralizedNode(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}
	defer node.Close()

	log.Printf("🚀 ShadSpace Gateway Node started successfully")
	log.Printf("📡 P2P Node ID: %s", node.GetHostID())
	log.Printf("🔌 P2P Port: %d", *nodePort)
	log.Printf("🌐 API Port: %d", *port)

	// Wait a bit for node to initialize and connect to network
	log.Printf("⏳ Initializing network connections...")
	time.Sleep(3 * time.Second)

	// Start the API server
	gateway := NewGatewayServer(node)
	gateway.Start(*port)
}
