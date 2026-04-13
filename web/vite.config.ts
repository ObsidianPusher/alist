import path from "path"
import { defineConfig } from "vite"
import solidPlugin from "vite-plugin-solid"
import legacy from "@vitejs/plugin-legacy"
import { dynamicBase } from "vite-plugin-dynamic-base"

export default defineConfig({
  resolve: {
    alias: {
      "~": path.resolve(__dirname, "src"),
      "@solidjs/router": path.resolve(__dirname, "solid-router/src"),
    },
  },
  plugins: [
    solidPlugin(),
    legacy({
      targets: ["defaults"],
    }),
    dynamicBase({
      // dynamic public path var string, default window.__dynamic_base__
      publicPath: " window.__dynamic_base__",
      // dynamic load resources on index.html, default false. maybe change default true
      transformIndexHtml: true,
    }),
  ],
  base: process.env.NODE_ENV === "production" ? "/__dynamic_base__/" : "/",
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return
          if (
            id.includes("artplayer") ||
            id.includes("hls.js") ||
            id.includes("flv.js") ||
            id.includes("libass-wasm")
          ) {
            return "player"
          }
          if (
            id.includes("@hope-ui") ||
            id.includes("@stitches") ||
            id.includes("solid-js") ||
            id.includes("solid-icons") ||
            id.includes("@motionone/solid")
          ) {
            return "ui"
          }
          if (
            id.includes("asciinema-player") ||
            id.includes("lightgallery")
          ) {
            return "preview-extra"
          }
          if (
            id.includes("solid-markdown") ||
            id.includes("remark-") ||
            id.includes("rehype-") ||
            id.includes("katex") ||
            id.includes("mark.js")
          ) {
            return "markdown"
          }
          if (id.includes("@egjs/view360") || id.includes("aplayer")) {
            return "preview-media"
          }
          if (id.includes("@github/webauthn-json")) {
            return "authn"
          }
          if (id.includes("axios")) {
            return "network"
          }
        },
      },
    },
  },
  // experimental: {
  //   renderBuiltUrl: (filename, { type, hostId, hostType }) => {
  //     if (type === "asset") {
  //       return { runtime: `window.ALIST.cdn/${filename}` };
  //     }
  //     return { relative: true };
  //   },
  // },
  server: {
    host: "0.0.0.0",
    proxy: {
      "/api": {
        target: "http://localhost:5244",
        changeOrigin: true,
        // rewrite: (path) => path.replace(/^\/api/, ""),
      },
    },
  },
})
