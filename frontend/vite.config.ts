/// <reference types="vitest/config" />
import { fileURLToPath, URL } from "node:url";
import { paraglideVitePlugin } from "@inlang/paraglide-js";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    paraglideVitePlugin({
      project: "./project.inlang",
      outdir: "./src/paraglide",
      strategy: ["localStorage", "preferredLanguage", "baseLocale"],
      emitTsDeclarations: true,
    }),
  ],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    // /auth/oauth lives on the backend (mirrors deploy/Caddyfile routing)
    proxy: {
      "/api": "http://localhost:8080",
      "/auth/oauth": "http://localhost:8080",
    },
  },
  test: {
    environment: "happy-dom",
    setupFiles: ["./src/vitest-setup.ts"],
  },
  build: {
    assetsInlineLimit: 0,
  },
});
