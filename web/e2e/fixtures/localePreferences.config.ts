import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Build/preview this fixture separately; the application build never includes it.
export default defineConfig({
  root: fileURLToPath(new URL("../..", import.meta.url)),
  plugins: [react()],
  build: {
    outDir: fileURLToPath(new URL("../../../.debug/locale-preferences-harness", import.meta.url)),
    emptyOutDir: true,
    rollupOptions: { input: fileURLToPath(new URL("./localePreferences.html", import.meta.url)) },
  },
});
