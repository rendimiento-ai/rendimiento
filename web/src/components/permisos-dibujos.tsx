// The drawings of the Permissions section (Ruta 184/185), in the same one-line stroke as the top bar's
// pages (navegacion.tsx): each platform agent has its own thing from the town of Cotija, and each
// permission too. They take the text's color, so day and night (and every color family) hold.

/** Each agent, by its stable id. */
const agentes: Record<string, string> = {
  // A backstrap loom with its diamond: the one who weaves the code.
  coder: "M3 5h18M3 19h18M6 5v14M18 5v14M12 8l3 4-3 4-3-4zM9 5v3M15 5v3M9 16v3M15 16v3",
  // A magnifier with a flower in it: the one who looks for why it failed.
  diagnostician: "M4 10a6 6 0 1 0 12 0a6 6 0 1 0-12 0M14.3 14.3 20 20M10 8.2v3.6M8.2 10h3.6",
  // The market's scale: the one who weighs what an encargo will cost.
  estimator: "M12 3v17M8 20.5h8M5 7h14M5 7l-2.5 6h5zM19 7l-2.5 6h5z",
  // The tecolote that keeps watch at night.
  vigia: "M6 9.5c0-3 2.6-5.5 6-5.5s6 2.5 6 5.5v5c0 3-2.6 5.5-6 5.5s-6-2.5-6-5.5zM6.4 7.2 5 4.2l3.2 1.4M17.6 7.2 19 4.2l-3.2 1.4M7.8 10.5a1.7 1.7 0 1 0 3.4 0a1.7 1.7 0 1 0-3.4 0M12.8 10.5a1.7 1.7 0 1 0 3.4 0a1.7 1.7 0 1 0-3.4 0M11 14.2l1 1.1 1-1.1",
  // A basket with its rows: the one who sorts the comments.
  clasificador: "M4 10h16l-2 9H6zM8 10a4 4 0 0 1 8 0M6.6 13.5h10.8M7.3 16.5h9.4M10 10v9M14 10v9",
  // Two speech scrolls of the codices: a conversation.
  platica: "M3.5 13c0-4.5 2.5-7.5 6-7.5a3 3 0 1 1 0 6 2 2 0 1 1 0-4M20.5 19c0-4.5-2.5-7.5-6-7.5a3 3 0 1 0 0 6 2 2 0 1 0 0-4",
  // An amate scroll: the day, in short.
  resumen: "M7 5h11v13a2 2 0 0 1-2 2H6a2 2 0 0 0 2-2V5zM7 5a2 2 0 0 0-3 1.7V8h3M10 9h5M10 12h5M10 15h3",
  // Two papel picado flags, one each way: the translator.
  traductor: "M3 5h8v7l-2-1.5-2 1.5-2-1.5L3 12zM13 12h8v7l-2-1.5-2 1.5-2-1.5-2 1.5zM7 14v3h4M17 10V7h-4",
  // A maize sprout: the one who learns from what was delivered.
  aprendiz: "M12 21V10M12 14c-3 0-5-2-5-5 3 0 5 2 5 5zM12 11c0-3 2-5 5-5 0 3-2 5-5 5zM8 21h8",
  // A key, for an agent of a person's (MCP).
  llave: "M11 9.5a4 4 0 1 0 8 0a4 4 0 1 0-8 0M12.2 12.3 4 20.5M6 18.5l2 2M8.6 15.9l2 2",
};

/** Each permission. */
const permisos: Record<string, string> = {
  // A rolled codex: the code and the Ruta.
  code_read: "M8 6v12a2 2 0 0 1-2 2h10a2 2 0 0 0 2-2V4H8zM8 6a2 2 0 0 0-4 0v2h4M11 8.5h4v3h-2.5M11 15.5h4",
  // Corn of every height on the furrow: the runs and their numbers.
  platform_read: "M3 20.5h18M6 20.5V13M6 15.5l-2-1.5M10 20.5V8M10 11l2.2-1.6M14 20.5v-5.5M14 17l-2-1.4M18 20.5V5M18 8.5l2.2-1.6",
  // A house with its roof tiles: where the app lives.
  infra_read: "M2.5 10.5 12 4l9.5 6.5M4 11.6q1.35 1.7 2.67 0q1.35 1.7 2.67 0q1.35 1.7 2.66 0q1.35 1.7 2.67 0q1.35 1.7 2.67 0q1.35 1.7 2.66 0M5.5 13.2v7.3h13v-7.3M10 20.5v-4a2 2 0 0 1 4 0v4",
  // A clay pot with its lid: what is cooked apart, closed.
  sandbox: "M7 8h10M8.2 8C5 9.6 4 12 4 14.5 4 18 7.6 20.5 12 20.5s8-2.5 8-6c0-2.5-1-4.9-4.2-6.5M9 8V6.3h6V8M11 4.3h2",
  // Two houses side by side: the copy beside the app.
  copies_read: "M2.5 12 7 8l4.5 4M4 11v8.5h6V11M12.5 12 17 8l4.5 4M14 11v8.5h6V11",
  // A candle: something that burns, with care.
  effects: "M10 10.5h4V20h-4zM12 10.5V8.8M12 8.8c-1.6-1.4-1.6-3.6 0-5.3 1.6 1.7 1.6 3.9 0 5.3zM7.5 20.5h9",
};

function Trazo({ d, className }: { d?: string; className: string }) {
  if (!d) return null;
  return (
    <svg className={className} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d={d} />
    </svg>
  );
}

/** An agent's drawing; an MCP key ("mcp:…") is a key. */
export function DibujoAgente({ id }: { id: string }) {
  return <Trazo className="dibujo-agente" d={agentes[id.startsWith("mcp:") ? "llave" : id] ?? agentes.llave} />;
}

/** A permission's drawing. */
export function DibujoPermiso({ id }: { id: string }) {
  return <Trazo className="dibujo-permiso" d={permisos[id]} />;
}

/** The papel picado over the section: little flags of the town's colors that sway (unless movement is off). */
export function PicadoPermisos() {
  const colores = ["grana", "cempasuchil", "rosa", "anil", "nopal", "grana", "cempasuchil", "rosa", "anil", "nopal", "grana", "cempasuchil"];
  return (
    <div className="permisos-picado" aria-hidden="true">
      {colores.map((c, i) => <i key={i} className={`pp-${c}`} style={{ ["--d" as string]: `${(i % 5) * -0.6}s` }} />)}
    </div>
  );
}
