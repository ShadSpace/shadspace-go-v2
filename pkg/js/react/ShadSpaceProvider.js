import React, { createContext, useContext } from 'react';
import useShadSpace from './useShadSpace';

const ShadSpaceContext = createContext();

export const useShadSpaceContext = () => {
    const context = useContext(ShadSpaceContext);
    if (!context) {
        throw new Error('useShadSpaceContext must be used within a ShadSpaceProvider');
    }
    return context;
};

export const ShadSpaceProvider = ({ 
    children, 
    config = {},
    onInitialized,
    onError 
}) => {
    const sdk = useShadSpace(config);

    React.useEffect(() => {
        if (sdk.isInitialized && onInitialized) {
            onInitialized(sdk.nodeInfo);
        }
    }, [sdk.isInitialized, sdk.nodeInfo, onInitialized]);

    React.useEffect(() => {
        if (sdk.error && onError) {
            onError(sdk.error);
        }
    }, [sdk.error, onError]);

    return (
        <ShadSpaceContext.Provider value={sdk}>
            {children}
        </ShadSpaceContext.Provider>
    );
};

export default ShadSpaceProvider;