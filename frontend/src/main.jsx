import { Component, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App.jsx'
import './styles.css'
// 优先在模块加载阶段初始化主题，彻底杜绝白屏/黑屏闪烁
try {
  const match = document.cookie.match(/(?:^|; )pb_theme=([^;]*)/)
  const saved = match ? decodeURIComponent(match[1]) : 'system'
  let resolved = saved
  if (saved === 'system') {
    const prefersDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
    resolved = prefersDark ? 'dark' : 'light'
  }
  document.documentElement.setAttribute('data-theme', resolved)
  document.documentElement.setAttribute('data-theme-setting', saved)
  const metaTheme = document.querySelector('meta[name="theme-color"]')
  if (metaTheme) {
    metaTheme.setAttribute('content', resolved === 'light' ? '#f5f6f8' : '#08090a')
  }
} catch {}

class ErrorBoundary extends Component {
  state = { failed: false, category: '渲染异常' }
  static getDerivedStateFromError(error) { return { failed: true, category: error?.name === 'TypeError' ? '数据格式异常' : '渲染异常' } }
  componentDidCatch() {}
  render() { if (!this.state.failed) return this.props.children; return <main className="error-boundary" role="alert"><div className="error-boundary-card"><span className="eyebrow">ProbeWatch</span><h1>控制台加载失败</h1><p>页面遇到{this.state.category}，未显示详细错误信息。</p><button className="button button-primary" onClick={() => window.location.reload()}>刷新页面</button></div></main> }
}

createRoot(document.getElementById('root')).render(<StrictMode><ErrorBoundary><App /></ErrorBoundary></StrictMode>)
