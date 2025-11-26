class ShadSpaceSDK {
    constructor() {
        this.isInitialized = false;
        this.nodeId = null;
        this._eventListeners = new Map();
    }

    async init(config = {}) {
        const defaultConfig = {
            port: 4001,
            bootstrapPeers: [
                "/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWFgQMSugNGS6mW4drX2N5oQsAF7Jxa9CWgy8bAQaSHJaU"
            ],
            nodeType: "web"
        };

        const mergedConfig = { ...defaultConfig, ...config };
        
        try {
            const result = await this._callWasmFunction('init', JSON.stringify(mergedConfig));
            
            if (!result.success) {
                throw new Error(result.error);
            }
            
            const data = JSON.parse(result.result);
            
            this.isInitialized = true;
            this.nodeId = data.nodeId;
            
            this._emit('initialized', data);
            return data;
        } catch (error) {
            this._emit('error', error);
            throw new Error(`Failed to initialize SDK: ${error}`);
        }
    }

    async uploadFile(file, shards = { total: 3, required: 2 }) {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized. Call init() first.');
        }

        let fileData;
        let fileName;

        if (file instanceof File) {
            fileData = await this._fileToUint8Array(file);
            fileName = file.name;
        } else if (file instanceof Uint8Array) {
            fileData = file;
            fileName = `file-${Date.now()}`;
        } else if (typeof file === 'object' && file.data && file.name) {
            fileData = file.data;
            fileName = file.name;
        } else {
            throw new Error('Unsupported file type. Provide File object, Uint8Array, or {data: Uint8Array, name: string}');
        }

        this._emit('upload:start', { fileName, size: fileData.length });
        
        try {
            const result = await this._callWasmFunction(
                'uploadFile', 
                fileData,
                fileName,
                shards.total,
                shards.required
            );
            
            if (!result.success) {
                throw new Error(result.error);
            }
            
            const data = JSON.parse(result.result);
            this._emit('upload:complete', data);
            return data;
        } catch (error) {
            this._emit('upload:error', error);
            throw new Error(`Upload failed: ${error}`);
        }
    }

    async downloadFile(fileHash, fileName = null) {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized. Call init() first.');
        }

        this._emit('download:start', { fileHash });
        
        try {
            const result = await this._callWasmFunction('downloadFile', fileHash);
            
            if (!result.success) {
                throw new Error(result.error);
            }
            
            const data = JSON.parse(result.result);
            
            // Convert base64 data back to Uint8Array if needed
            if (data.data && typeof data.data === 'string') {
                const binaryString = atob(data.data);
                const bytes = new Uint8Array(binaryString.length);
                for (let i = 0; i < binaryString.length; i++) {
                    bytes[i] = binaryString.charCodeAt(i);
                }
                data.data = bytes;
            }

            this._emit('download:complete', data);
            
            // Auto-download if fileName provided
            if (fileName && data.data) {
                this._downloadBlob(data.data, fileName);
            }
            
            return data;
        } catch (error) {
            this._emit('download:error', error);
            throw new Error(`Download failed: ${error}`);
        }
    }

    async listFiles() {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized. Call init() first.');
        }

        try {
            const result = await this._callWasmFunction('listFiles');
            
            if (!result.success) {
                throw new Error(result.error);
            }
            
            return JSON.parse(result.result);
        } catch (error) {
            throw new Error(`Failed to list files: ${error}`);
        }
    }

    async getNetworkStats() {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized. Call init() first.');
        }

        try {
            const result = await this._callWasmFunction('getNetworkStats');
            
            if (!result.success) {
                throw new Error(result.error);
            }
            
            return JSON.parse(result.result);
        } catch (error) {
            throw new Error(`Failed to get network stats: ${error}`);
        }
    }

    async getNodeInfo() {
        if (!this.isInitialized) {
            throw new Error('SDK not initialized. Call init() first.');
        }

        try {
            const result = await this._callWasmFunction('getNodeInfo');
            
            if (!result.success) {
                throw new Error(result.error);
            }
            
            return JSON.parse(result.result);
        } catch (error) {
            throw new Error(`Failed to get node info: ${error}`);
        }
    }

    async close() {
        try {
            const result = await this._callWasmFunction('close');
            
            if (!result.success) {
                throw new Error(result.error);
            }
            
            this.isInitialized = false;
            this.nodeId = null;
            this._emit('closed');
        } catch (error) {
            throw new Error(`Failed to close SDK: ${error}`);
        }
    }

    // Event handling
    on(event, callback) {
        if (!this._eventListeners.has(event)) {
            this._eventListeners.set(event, new Set());
        }
        this._eventListeners.get(event).add(callback);
    }

    off(event, callback) {
        if (this._eventListeners.has(event)) {
            this._eventListeners.get(event).delete(callback);
        }
    }

    _emit(event, data) {
        if (this._eventListeners.has(event)) {
            this._eventListeners.get(event).forEach(callback => {
                try {
                    callback(data);
                } catch (error) {
                    console.error(`Error in event listener for ${event}:`, error);
                }
            });
        }
    }

    // Helper methods
    async _fileToUint8Array(file) {
        return new Promise((resolve, reject) => {
            const reader = new FileReader();
            reader.onload = () => {
                resolve(new Uint8Array(reader.result));
            };
            reader.onerror = reject;
            reader.readAsArrayBuffer(file);
        });
    }

    _downloadBlob(data, fileName) {
        const blob = new Blob([data], { type: 'application/octet-stream' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = fileName;
        a.style.display = 'none';
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }

    _callWasmFunction(functionName, ...args) {
        return new Promise((resolve, reject) => {
            if (!window.ShadSpaceSDK || !window.ShadSpaceSDK[functionName]) {
                reject(new Error(`WASM function ${functionName} not available`));
                return;
            }

            try {
                const result = window.ShadSpaceSDK[functionName](...args);
                
                // Handle both synchronous and promise-based returns
                if (result && typeof result.then === 'function') {
                    result.then(resolve).catch(reject);
                } else {
                    resolve(result);
                }
            } catch (error) {
                reject(error);
            }
        });
    }

    // Utility methods
    static async isWasmSupported() {
        try {
            if (typeof WebAssembly === 'undefined') return false;
            if (!WebAssembly.instantiate) return false;
            
            const module = new WebAssembly.Module(new Uint8Array([
                0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00
            ]));
            return module instanceof WebAssembly.Module;
        } catch (error) {
            return false;
        }
    }

    getStatus() {
        return {
            isInitialized: this.isInitialized,
            nodeId: this.nodeId,
            wasmSupported: typeof WebAssembly !== 'undefined'
        };
    }
}

// Make SDK available globally
if (typeof window !== 'undefined') {
    window.ShadSpaceSDK = ShadSpaceSDK;
}