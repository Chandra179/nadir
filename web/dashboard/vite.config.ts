import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const dashboardPort = Number(process.env.DASHBOARD_PORT || "3002");

export default defineConfig({
  plugins: [react(), tailwindcss()],
  cacheDir: ".vite-cache",
  server: {
    port: dashboardPort,
    strictPort: true,
    proxy: {
      "/api": "http://localhost:8100",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: "src/test/setup.ts",
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
  },
});
