import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "");
  const devPort = Number(env.WEB_PORT || 29283);
  const previewPort = Number(env.WEB_PORT || 29281);
  const devProxyTarget = env.VITE_DEV_PROXY_TARGET || "http://127.0.0.1:29282";
  const allowedHosts = env.WEB_ALLOWED_HOSTS
    ? env.WEB_ALLOWED_HOSTS.split(",").map((host) => host.trim()).filter(Boolean)
    : ["draw.devbin.de"];

  return {
    plugins: [react(), tailwindcss()],
    server: {
      port: devPort,
      strictPort: true,
      host: "0.0.0.0",
      allowedHosts,
      proxy: {
        "/api": {
          target: devProxyTarget,
          changeOrigin: true,
        },
      },
    },
    preview: {
      port: previewPort,
      strictPort: true,
      host: "0.0.0.0",
      allowedHosts,
    },
  };
});
