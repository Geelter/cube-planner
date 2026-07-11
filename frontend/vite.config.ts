/// <reference types="vitest/config" />
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react()],
  server: {
    proxy: { "/api": "http://localhost:8080" },
  },
  test: {
    environment: "happy-dom",
    setupFiles: ["./src/vitest-setup.ts"],
  },
});
