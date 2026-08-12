import { Component, StrictMode, type ErrorInfo, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("#root not found");

interface ErrorBoundaryState {
  error: Error | null;
}

class ErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("Renderer error:", error, info);
  }

  override render(): ReactNode {
    if (this.state.error) {
      return (
        <div className="p-8 font-mono text-sm">
          <div className="text-red-400 mb-2 font-semibold">Renderer error</div>
          <pre className="whitespace-pre-wrap text-[--color-fg-muted]">
            {String(this.state.error.stack ?? this.state.error.message)}
          </pre>
        </div>
      );
    }
    return this.props.children;
  }
}

window.addEventListener("error", (e) => {
  console.error("window error", e.error ?? e.message);
});
window.addEventListener("unhandledrejection", (e) => {
  console.error("unhandled rejection", e.reason);
});

createRoot(root).render(
  <StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </StrictMode>,
);
