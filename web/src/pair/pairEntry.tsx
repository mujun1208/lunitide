// 配对页入口：pair.html 的 module 入口，仅做挂载（逻辑全在 pairApp.tsx）。
import React from 'react'
import { createRoot } from 'react-dom/client'
import { PairApp } from './pairApp'

createRoot(document.getElementById('root')!).render(<PairApp />)
