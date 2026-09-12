import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  cacheDir: ".vite-cache",
  server: {
    port: 3000,
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
