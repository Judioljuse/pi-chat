import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { LocaleProvider } from '@/lib/i18n'
import { beuiZh } from '@/lib/beui-zh'
import App from './App'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <LocaleProvider locale="zh-CN" messages={beuiZh}>
      <App />
    </LocaleProvider>
  </StrictMode>,
)
