// The build's step that writes dist/sw.js: the worker of sw.ts, given the
// files this build made (vite.config.ts uses it). The worker's version is the
// page's entry file, which the page also knows (registro.ts): a new build is
// a new worker.
import type { Plugin } from "vite";
import { trabajador, type DatosTrabajador } from "./sw.ts";

/** The page's files at the root, besides index.html (web/public). */
const FIJOS = ["/manifest.webmanifest", "/icono.svg", "/icono-mascara.svg", "/icono-180.png", "/icono-512.png"];

export function construirTrabajador(): Plugin {
  return {
    name: "rendimiento-trabajador",
    apply: "build",
    enforce: "post",
    generateBundle(_opciones, bundle) {
      const archivos = Object.values(bundle);
      const entrada = archivos.find((a) => a.type === "chunk" && a.isEntry);
      if (!entrada || entrada.type !== "chunk") return this.error("the build has no entry file for the service worker");
      const todos = archivos.map((a) => "/" + a.fileName).filter((f) => f.startsWith("/assets/") && !f.endsWith(".map"));
      // The first screen: the entry, what it imports at once (React, the router), and the styles.
      const inicio = ["/" + entrada.fileName, ...entrada.imports.map((f) => "/" + f), ...todos.filter((f) => f.endsWith(".css"))];
      const datos: DatosTrabajador = { version: "/" + entrada.fileName, fijos: FIJOS, inicio: [...new Set(inicio)], todos };
      this.emitFile({
        type: "asset",
        fileName: "sw.js",
        source: `// rendimiento's service worker, written by the build from web/src/trabajador/sw.ts.\n(${trabajador.toString()})(${JSON.stringify(datos)});\n`,
      });
    },
  };
}
