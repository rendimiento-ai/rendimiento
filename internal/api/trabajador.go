package api

// workerRetirado is the service worker served at /sw.js when it is turned
// off (SERVICE_WORKER=off): a browser that had the real one takes this one at
// its next check, which throws away what was kept and unregisters itself; the
// page, seeing the X-Rendimiento-Trabajador header, registers none.
const workerRetirado = `// rendimiento's service worker is turned off here (SERVICE_WORKER=off).
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (e) => e.waitUntil((async () => {
  for (const n of await caches.keys()) if (n.startsWith("rendimiento-")) await caches.delete(n);
  await self.registration.unregister();
})()));
`
