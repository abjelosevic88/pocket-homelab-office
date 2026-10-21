import React from 'react'

export class ErrorBoundary extends React.Component<{ children: React.ReactNode; where?: string }, { err: Error | null }> {
  state = { err: null as Error | null }
  static getDerivedStateFromError(err: Error) { return { err } }
  componentDidCatch(err: Error) {
    fetch('/api/v1/client-error', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message: String(err?.message || err), stack: String(err?.stack || ''), where: this.props.where || location.pathname }) }).catch(() => {})
  }
  render() {
    if (this.state.err) {
      return <div className="crash"><h1>Something went wrong</h1><pre>{String(this.state.err.message || this.state.err)}</pre><button className="btn" onClick={() => location.reload()}>Reload</button></div>
    }
    return this.props.children
  }
}
