import React from 'react'
import { createRoot } from 'react-dom/client'
import { providerBridge } from './bridge/client'
import { ProviderApp } from './provider/ProviderApp'
import './styles.css'
createRoot(document.getElementById('root')!).render(<React.StrictMode><ProviderApp bridge={providerBridge} /></React.StrictMode>)
