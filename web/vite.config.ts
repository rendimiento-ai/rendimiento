import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/api": "http://localhost:8080" } },
  // three.js (the Universe in 3D) is its own file of about 600 kB, loaded only when 3D is chosen.
  // Each page is its own file too (main.tsx); React and the router, which change seldom, one of their own,
  // so an update of rendimiento's pages does not make the browser fetch them again.
  build: {
    outDir: "dist", emptyOutDir: true, chunkSizeWarningLimit: 700,
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [{ name: "react", test: /[\\/]node_modules[\\/](react|react-dom|react-router|react-router-dom|scheduler)[\\/]/ }],
        },
      },
    },
  },
});
