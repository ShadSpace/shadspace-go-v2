import { useState, useEffect, useCallback, useRef } from 'react';

export const useShadSpace = (config = {}) => {
    const [sdk, setSdk] = useState(null);
    const [isInitialized, setIsInitialized] = useState(false);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
    const [nodeInfo, setNodeInfo] = useState(null);
    const [networkStats, setNetworkStats] = useState(null);
    const eventListeners = useRef(new Map());

    useEffect(() => {
        const initializeSDK = async () => {
            if (typeof window === 'undefined' || !window.ShadSpaceSDK) {
                setError('ShadSpace SDK not loaded. Make sure shadspace.wasm is loaded.');
                return;
            }

            try {
                setLoading(true);
                const sdkInstance = new window.ShadSpaceSDK();
                
                // Set up event listeners
                sdkInstance.on('initialized', (data) => {
                    setIsInitialized(true);
                    setNodeInfo(data);
                    setError(null);
                });

                sdkInstance.on('error', (err) => {
                    setError(err.message || err);
                });

                sdkInstance.on('upload:start', (data) => {
                    console.log('Upload started:', data);
                });

                sdkInstance.on('upload:complete', (data) => {
                    console.log('Upload completed:', data);
                });

                sdkInstance.on('upload:error', (err) => {
                    console.error('Upload error:', err);
                });

                sdkInstance.on('download:start', (data) => {
                    console.log('Download started:', data);
                });

                sdkInstance.on('download:complete', (data) => {
                    console.log('Download completed:', data);
                });

                sdkInstance.on('download:error', (err) => {
                    console.error('Download error:', err);
                });

                await sdkInstance.init(config);
                setSdk(sdkInstance);
                
            } catch (err) {
                setError(err.message);
                console.error('Failed to initialize ShadSpace SDK:', err);
            } finally {
                setLoading(false);
            }
        };

        initializeSDK();

        return () => {
            if (sdk) {
                sdk.close();
            }
        };
    }, []);

    const uploadFile = useCallback(async (file, shards = { total: 3, required: 2 }) => {
        if (!sdk || !isInitialized) {
            throw new Error('SDK not initialized');
        }

        try {
            setLoading(true);
            const result = await sdk.uploadFile(file, shards);
            setError(null);
            return result;
        } catch (err) {
            setError(err.message);
            throw err;
        } finally {
            setLoading(false);
        }
    }, [sdk, isInitialized]);

    const downloadFile = useCallback(async (fileHash, fileName = null) => {
        if (!sdk || !isInitialized) {
            throw new Error('SDK not initialized');
        }

        try {
            setLoading(true);
            const result = await sdk.downloadFile(fileHash, fileName);
            setError(null);
            return result;
        } catch (err) {
            setError(err.message);
            throw err;
        } finally {
            setLoading(false);
        }
    }, [sdk, isInitialized]);

    const listFiles = useCallback(async () => {
        if (!sdk || !isInitialized) {
            throw new Error('SDK not initialized');
        }

        try {
            setLoading(true);
            const result = await sdk.listFiles();
            setError(null);
            return result;
        } catch (err) {
            setError(err.message);
            throw err;
        } finally {
            setLoading(false);
        }
    }, [sdk, isInitialized]);

    const getNetworkStats = useCallback(async () => {
        if (!sdk || !isInitialized) {
            throw new Error('SDK not initialized');
        }

        try {
            const result = await sdk.getNetworkStats();
            setNetworkStats(result);
            return result;
        } catch (err) {
            setError(err.message);
            throw err;
        }
    }, [sdk, isInitialized]);

    const refreshStats = useCallback(async () => {
        return getNetworkStats();
    }, [getNetworkStats]);

    const on = useCallback((event, callback) => {
        if (!sdk) return;
        
        if (!eventListeners.current.has(event)) {
            eventListeners.current.set(event, new Set());
        }
        eventListeners.current.get(event).add(callback);
        sdk.on(event, callback);
    }, [sdk]);

    const off = useCallback((event, callback) => {
        if (!sdk) return;
        
        if (eventListeners.current.has(event)) {
            eventListeners.current.get(event).delete(callback);
        }
        sdk.off(event, callback);
    }, [sdk]);

    return {
        // State
        sdk,
        isInitialized,
        loading,
        error,
        nodeInfo,
        networkStats,
        
        // Methods
        uploadFile,
        downloadFile,
        listFiles,
        getNetworkStats,
        refreshStats,
        
        // Event handling
        on,
        off,
        
        // Utility
        getStatus: () => sdk?.getStatus() || { isInitialized: false, nodeId: null }
    };
};

// Hook for individual file operations
export const useFileOperations = () => {
    const [uploadProgress, setUploadProgress] = useState({});
    const [downloadProgress, setDownloadProgress] = useState({});

    const uploadWithProgress = useCallback(async (sdk, file, shards = { total: 3, required: 2 }) => {
        if (!sdk) throw new Error('SDK required');

        const fileId = file.name || file.fileName || `file-${Date.now()}`;
        
        setUploadProgress(prev => ({
            ...prev,
            [fileId]: { status: 'starting', progress: 0 }
        }));

        try {
            // Simulate progress (in real implementation, this would come from SDK events)
            const progressInterval = setInterval(() => {
                setUploadProgress(prev => ({
                    ...prev,
                    [fileId]: { 
                        status: 'uploading', 
                        progress: Math.min(prev[fileId]?.progress + 10, 90)
                    }
                }));
            }, 200);

            const result = await sdk.uploadFile(file, shards);
            
            clearInterval(progressInterval);
            
            setUploadProgress(prev => ({
                ...prev,
                [fileId]: { status: 'completed', progress: 100, result }
            }));

            return result;
        } catch (error) {
            setUploadProgress(prev => ({
                ...prev,
                [fileId]: { status: 'error', progress: 0, error: error.message }
            }));
            throw error;
        }
    }, []);

    const clearProgress = useCallback((fileId) => {
        setUploadProgress(prev => {
            const newProgress = { ...prev };
            delete newProgress[fileId];
            return newProgress;
        });
        setDownloadProgress(prev => {
            const newProgress = { ...prev };
            delete newProgress[fileId];
            return newProgress;
        });
    }, []);

    return {
        uploadProgress,
        downloadProgress,
        uploadWithProgress,
        clearProgress
    };
};

export default useShadSpace;