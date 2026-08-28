import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { RootErrorBoundary } from './RootErrorBoundary'
import { installVisibilityRestore } from './visibilityRestore'
import './styles.css'
installVisibilityRestore()
createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <RootErrorBoundary>
      <App />
    </RootErrorBoundary>
  </React.StrictMode>,
)
