import { Component, type ErrorInfo, type ReactNode } from "react";

interface State { error: Error | null }

/** Last line of defence: a render crash shows a message and a reload button instead of a blank page. */
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  override state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("UI crash:", error, info.componentStack);
  }

  override render(): ReactNode {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert" style={{ maxWidth: "40rem", margin: "4rem auto", padding: "0 1rem" }}>
        <h1>Something went wrong</h1>
        <p>The interface hit an unexpected error. Your data on the server is not affected.</p>
        <pre style={{ whiteSpace: "pre-wrap", opacity: 0.8 }}>{this.state.error.message}</pre>
        <button type="button" onClick={() => window.location.reload()}>Reload</button>
      </div>
    );
  }
}
