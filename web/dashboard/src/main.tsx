import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./app/App";
import { probeGpu, resolveRuntime } from "./runtime/config";
import RuntimeGate from "./runtime/RuntimeGate";
import "./styles/index.css";

// The server runtime is the default; the in-browser WASM runtime is opt-in.
// The query that selected the runtime is remembered for the tab: in-app
// navigation drops it, and a reload must not silently fall back to the server.
const RUNTIME_QUERY_KEY = "nadir.runtime.query";
function runtimeQuery(): string {
  try {
    if (new URLSearchParams(location.search).has("runtime")) sessionStorage.setItem(RUNTIME_QUERY_KEY, location.search);
    return new URLSearchParams(location.search).has("runtime") ? location.search : (sessionStorage.getItem(RUNTIME_QUERY_KEY) ?? "");
  } catch {
    return location.search;
  }
}

async function main() {
  const resolved = resolveRuntime(
    runtimeQuery(),
    {
      VITE_RUNTIME: import.meta.env.VITE_RUNTIME,
      BASE_URL: import.meta.env.BASE_URL,
      VITE_ORT_BASE_URL: import.meta.env.VITE_ORT_BASE_URL,
    },
    await probeGpu(),
  );

  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      {resolved ? (
        <RuntimeGate resolved={resolved}>
          <App />
        </RuntimeGate>
      ) : (
        <App />
      )}
    </StrictMode>,
  );
}

void main();
