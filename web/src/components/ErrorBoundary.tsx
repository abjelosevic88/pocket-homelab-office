import React from 'react'

// A crash in one screen shows what went wrong instead of leaving the whole page
// blank, and is reported to the server's log.
export class ErrorBoundary extends React.Component<{ children: React.ReactNode; where?: string }, { err: Error | null }> {
  state = { err: null as Error | null }
  static getDerivedStateFromError(err: Error) { return { err } }
  componentDidCatch(err: Error) {
    fetch('/api/v1/client-error', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ message: String(err?.message || err), stack: String(err?.stack || ''), where: this.props.where || location.pathname }) }).catch(() => {})
  }
  render() {
    if (!this.state.err) return this.props.children
    const translated = /removeChild|insertBefore|not a child of this node/i.test(this.state.err.message)
    return (
      <div className="crash">
        <h2>Something went wrong</h2>
        {translated && <p>The browser translated this page, which pulls the text out from under it. Turn translation off for this site (the translate icon in the address bar → “Never translate this site”).</p>}
        <pre className="mono small">{String(this.state.err.message || this.state.err)}</pre>
        <div className="row"><button type="button" className="btn primary" onClick={() => location.reload()}>Reload</button><button type="button" className="btn" onClick={() => this.setState({ err: null })}>Try again</button></div>
      </div>
    )
  }
}
