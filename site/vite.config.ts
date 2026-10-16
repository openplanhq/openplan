import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vitest/config";

// The stylesheet imports ../web/src/styles/theme.css; the dev server may serve
// files from the repository root for that one import.
export default defineConfig({
  plugins: [tailwindcss()],
  server: { fs: { allow: [".."] } },
  test: { environment: "node" }
});
