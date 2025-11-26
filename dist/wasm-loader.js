// Simple WASM loader for ShadSpace
class ShadSpaceWASMLoader {
    constructor() {
        this.isLoaded = false;
        this.isInitialized = false;
        this.loadPromise = null;
    }

    async loadWASM(wasmPath = 'shadspace.wasm') {
        // Return existing promise if already loading
        if (this.loadPromise) {
            return this.loadPromise;
        }

        this.loadPromise = (async () => {
            try {
                console.log('Loading ShadSpace WASM module...');
                
                // Check WebAssembly support
                if (typeof WebAssembly === 'undefined') {
                    throw new Error('WebAssembly is not supported in this browser');
                }

                // Check if Go is available
                if (typeof Go === 'undefined') {
                    throw new Error('Go runtime not loaded. Make sure wasm_exec.js is loaded first.');
                }

                // Fetch the WASM module
                console.log('Fetching WASM module...');
                const response = await fetch(wasmPath);
                if (!response.ok) {
                    throw new Error(`Failed to fetch WASM file: ${response.statusText}`);
                }

                const wasmBuffer = await response.arrayBuffer();
                
                // Create Go instance
                const go = new Go();
                
                console.log('Instantiating WASM module...');
                // Instantiate the WASM module
                const result = await WebAssembly.instantiate(wasmBuffer, go.importObject);
                
                console.log('Running Go program...');
                // Run the Go program - this will register our functions
                go.run(result.instance);
                
                this.isLoaded = true;
                console.log('ShadSpace WASM module loaded successfully');
                
                return true;
                
            } catch (error) {
                console.error('Failed to load WASM module:', error);
                this.loadPromise = null;
                throw error;
            }
        })();

        return this.loadPromise;
    }

    // Wait for WASM functions to be registered
    async waitForFunctions(timeout = 10000) {
        const startTime = Date.now();
        
        while (Date.now() - startTime < timeout) {
            if (this.checkFunctions()) {
                this.isInitialized = true;
                console.log('WASM functions registered successfully');
                return true;
            }
            await new Promise(resolve => setTimeout(resolve, 100));
        }
        
        throw new Error('WASM functions not registered within timeout');
    }

    // Check if all required functions are available
    checkFunctions() {
        const requiredFunctions = ['init', 'uploadFile', 'downloadFile', 'listFiles', 'getNetworkStats', 'getNodeInfo', 'close'];
        
        if (!window.ShadSpaceSDK) {
            return false;
        }

        for (const funcName of requiredFunctions) {
            if (typeof window.ShadSpaceSDK[funcName] !== 'function') {
                return false;
            }
        }

        return true;
    }

    getStatus() {
        return {
            isLoaded: this.isLoaded,
            isInitialized: this.isInitialized,
            wasmSupported: typeof WebAssembly !== 'undefined',
            goAvailable: typeof Go !== 'undefined',
            sdkAvailable: this.checkFunctions()
        };
    }
}

// Create global loader instance
window.ShadSpaceWASMLoader = new ShadSpaceWASMLoader();