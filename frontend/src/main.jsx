import { Component, StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App.jsx'
import './styles.css'

class ErrorBoundary extends Component {
  state = { failed: false, category: '渲染异常' }
  static getDerivedStateFromError(error) { return { failed: true, category: error?.name === 'TypeError' ? '数据格式异常' : '渲染异常' } }
  componentDidCatch() {}
  render() { if (!this.state.failed) return this.props.children; return <main className="error-boundary" role="alert"><div className="error-boundary-card"><span className="eyebrow">ProbeWatch</span><h1>控制台加载失败</h1><p>页面遇到{this.state.category}，未显示详细错误信息。</p><button className="button button-primary" onClick={() => window.location.reload()}>刷新页面</button></div></main> }
}

createRoot(document.getElementById('root')).render(<StrictMode><ErrorBoundary><App /></ErrorBoundary></StrictMode>)
