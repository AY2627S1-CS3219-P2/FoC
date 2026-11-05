// AI Assistance Disclosure:
// Tool: Claude Code (model: Opus 5), date: 2026-09-17
// Scope: React entry point.
// Author review: Nigeltzy - Simple main.tsx boilerpoint.

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { config } from "./lib/config";
import { createErrorReporter, createGatewayPost } from "./lib/errorReporter";
import { readLastView } from "./lib/lastView";
import { createTokenStore } from "./lib/tokens";
import "./styles.css";

/** Runs once per page load, so the store, the reporter and its listeners are made once. */
function bootstrap(container: HTMLElement): void {
  // Made here rather than in App so the reporter reads the token App uses. It
  // still lives only in the store's closure, never in component state.
  const tokens = createTokenStore();

  // A fixture build has no gateway to report to, so it gets no reporter.
  const reporter = config.useFixtures
    ? null
    : createErrorReporter({
        post: createGatewayPost(config.gatewayBaseUrl),
        getAccessToken: () => tokens.getAccessToken(),
        // App opens on Home when no view is remembered.
        getView: () => readLastView() ?? "home",
      });

  const root = createRoot(container, {
    // React's default handler is what put the error in the console, so this
    // one logs it too.
    onUncaughtError(error, errorInfo) {
      console.error(error);
      reporter?.reportRender(error, errorInfo.componentStack);
    },
    // Logged, never reported. React's default hands these to reportError,
    // which the "error" listener below would report.
    onRecoverableError(error) {
      console.error(error);
    },
  });

  if (reporter) {
    // Bubble phase: a failed image or script load fires an "error" that does
    // not bubble, so only a capture listener would see it.
    window.addEventListener("error", reporter.reportWindowError);
    window.addEventListener("unhandledrejection", reporter.reportRejection);
  }

  root.render(
    <StrictMode>
      <App tokens={tokens} />
    </StrictMode>,
  );
}

const container = document.getElementById("root");
if (!container) throw new Error("Root element #root not found in index.html");
bootstrap(container);
