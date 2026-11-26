// The Agents tab of the AI page (decision 193): the plaza, a stall for each of the platform's agents and each person's
// MCP key, with its Cotija drawing, what it does and how it goes; pressing one opens its drawer beside the page (a
// sheet from below on a phone) with its own choices and its permissions. The drawings are the agents' own
// (permisos-dibujos.tsx); the page's tabs have theirs here.
import { useEffect, useRef, type ReactNode } from "react";
import { api, dollars, type IAView } from "../api";
import { ErrorBox, usePoll } from "./ui";
import { t } from "../i18n";
import { DibujoAgente } from "./permisos-dibujos";
import { DibujoAlDia } from "./al-dia";
import { nombreSujeto, PermisosDe } from "./permisos";

/** The drawings of the page's tabs and of what it asks, in the same one-line stroke. */
const trazos: Record<string, string> = {
  sol: "M3 18.5h18M6.5 18.5a5.5 5.5 0 0 1 11 0M12 6.5V4M6.3 9.8 4.6 8.1M17.7 9.8l1.7-1.7M3.5 14.5h2M18.5 14.5h2",
  olla: "M7 8h10M8.2 8C5 9.6 4 12 4 14.5 4 18 7.6 20.5 12 20.5s8-2.5 8-6c0-2.5-1-4.9-4.2-6.5M9 8V6.3h6V8M11 4.3h2",
  telar: "M3 5h18M3 19h18M6 5v14M18 5v14M12 8l3 4-3 4-3-4zM9 5v3M15 5v3M9 16v3M15 16v3",
  peso: "M12 3a9 9 0 1 0 0 18a9 9 0 1 0 0-18M14.6 9.2c-.5-1-1.5-1.6-2.6-1.6-1.4 0-2.5.8-2.5 2s1.1 1.6 2.5 1.9 2.5.9 2.5 2.1-1.1 2-2.5 2c-1.1 0-2.1-.6-2.6-1.6M12 5.8v1.8M12 16.4v1.8",
  casa: "M2.5 10.5 12 4l9.5 6.5M5.5 9.5v11h13v-11M10 20.5v-4a2 2 0 0 1 4 0v4",
  vela: "M10 10.5h4V20h-4zM12 10.5V8.8M12 8.8c-1.6-1.4-1.6-3.6 0-5.3 1.6 1.7 1.6 3.9 0 5.3zM7.5 20.5h9",
};

export function DibujoIA({ id, className = "" }: { id: string; className?: string }) {
  return (
    <svg className={`dibujo-ia ${className}`} viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
      <path d={trazos[id] ?? trazos.sol} />
    </svg>
  );
}

/** What each of the platform's agents does, in a line. */
const queHace: Record<string, string> = {
  coder: "Does encargos on a branch and tests them",
  diagnostician: "Explains every failed run",
  estimator: "Weighs what each encargo will cost",
  "al-dia": "Reads each dependency update",
  vigia: "Reviews the week and leaves findings",
  clasificador: "Sorts the comments people send",
  platica: "Talks ideas over with you",
  resumen: "Tells the day in short",
  traductor: "Translates what people write",
  aprendiz: "Learns from what was delivered",
};

/** Each stall's color, from the town's. */
const tonos = ["anil", "rosa", "cempasuchil", "nopal", "morado", "grana"];

interface Puesto { id: string; nombre: string; que: string; como?: string; on: boolean; llave?: boolean }

const codeAgentText: Record<string, string> = { off: "Off", asked: "Only what you send it", system: "What rendimiento opens, if cheap", auto: "Every cheap one" };

/** The stalls: the platform's agents in the order the page lists them, «Put up to date» after the estimator, then the keys. */
function puestos(v: IAView, sujetos: { id: string; kind: string; name?: string }[], alDia?: { encendido: boolean; quien: string }): Puesto[] {
  const s = v.settings;
  const como: Record<string, { como: string; on: boolean }> = {
    coder: { como: codeAgentText[s.codeAgent] ?? s.codeAgent, on: s.codeAgent !== "off" },
    diagnostician: { como: s.autoDiagnose ? "Explains them" : "Off", on: s.autoDiagnose },
    estimator: { como: t("Up to {amount} for the platform's AI", { amount: dollars(s.encargoAutoMicros) }), on: true },
  };
  const out: Puesto[] = [];
  const agentes = sujetos.filter((x) => x.kind === "agent").map((x) => x.id);
  for (const id of agentes.length ? agentes : ["coder", "diagnostician", "estimator"]) {
    out.push({ id, nombre: nombreSujeto({ id }), que: queHace[id] ?? "", como: como[id]?.como, on: como[id]?.on ?? true });
    if (id === "estimator") {
      out.push({ id: "al-dia", nombre: t("Put up to date"), que: queHace["al-dia"], on: alDia?.encendido ?? true,
        como: alDia ? (alDia.encendido ? (alDia.quien === "local" ? "With your own agent" : "With the coding agent") : "Off") : undefined });
    }
  }
  for (const x of sujetos.filter((x) => x.kind === "mcp")) out.push({ id: x.id, nombre: nombreSujeto(x), que: "A person's agent, over MCP", on: true, llave: true });
  return out;
}

/** The plaza and, when one is chosen, its drawer. */
export function Plaza({ v, agente, onAgente, onCerrar, cajon }: {
  v: IAView; agente?: string; onAgente: (id: string) => void; onCerrar: () => void; cajon: (id: string) => ReactNode;
}) {
  const { data, error } = usePoll(api.permisos, [], 0);
  const { data: alDia } = usePoll(() => api.alDiaAjustes().catch(() => undefined), [], 0);
  const lista = puestos(v, data?.sujetos ?? [], alDia);
  const elegido = agente ? lista.find((p) => p.id === agente) : undefined;
  return (
    <>
      <p className="small muted" style={{ margin: "0 0 12px" }}>{t("Each agent in its stall. Press one to see what it does by itself, what it may do and how it goes.")}</p>
      <ErrorBox error={error} />
      <div className="ia-plaza">
        {lista.map((p, i) => (
          <button key={p.id} type="button" id={`agente-${p.id}`} className={`ia-puesto tono-${tonos[i % tonos.length]}${p.on ? "" : " apagado"}${agente === p.id ? " on" : ""}`}
            style={{ animationDelay: `${Math.min(i, 10) * 0.04}s` }} aria-haspopup="dialog" aria-expanded={agente === p.id} onClick={() => onAgente(p.id)}>
            <span className="ia-puesto-dibujo">{p.id === "al-dia" ? <DibujoAlDia id="sol" /> : <DibujoAgente id={p.id} />}</span>
            <strong>{p.nombre}</strong>
            <span className="small muted">{t(p.que)}</span>
            {p.como && <span className={`ia-puesto-como small${p.on ? "" : " off"}`}>{t(p.como)}</span>}
          </button>
        ))}
      </div>
      {agente && (
        <Cajon id={agente} nombre={elegido?.nombre ?? agente} que={elegido ? t(elegido.que) : ""} onCerrar={onCerrar}>
          {cajon(agente)}
          {agente !== "al-dia" && <PermisosDe sujeto={agente} />}
        </Cajon>
      )}
    </>
  );
}

/** An agent's drawer: beside the page on a computer, a sheet from below on a phone. Escape or × closes it. */
function Cajon({ id, nombre, que, onCerrar, children }: { id: string; nombre: string; que: string; onCerrar: () => void; children: ReactNode }) {
  const cerrar = useRef<HTMLButtonElement>(null);
  const alCerrar = useRef(onCerrar);
  alCerrar.current = onCerrar;
  // Focus goes to the drawer once, when it opens for an agent: the page re-renders while one writes in it.
  useEffect(() => {
    cerrar.current?.focus();
    const tecla = (e: KeyboardEvent) => { if (e.key === "Escape") alCerrar.current(); };
    document.addEventListener("keydown", tecla);
    return () => document.removeEventListener("keydown", tecla);
  }, [id]);
  return (
    <>
      <div className="ia-cajon-fondo" aria-hidden="true" onClick={onCerrar} />
      <aside className="ia-cajon" role="dialog" aria-modal="false" aria-labelledby="ia-cajon-titulo" key={id}>
        <header className="ia-cajon-cabeza">
          <span className="ia-puesto-dibujo">{id === "al-dia" ? <DibujoAlDia id="sol" /> : <DibujoAgente id={id} />}</span>
          <div style={{ flex: 1, minWidth: 0 }}>
            <h2 id="ia-cajon-titulo">{nombre}</h2>
            {que && <span className="small muted">{que}</span>}
          </div>
          <button ref={cerrar} type="button" className="ia-cajon-cerrar" aria-label={t("Close")} onClick={onCerrar}>
            <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M6 6l12 12M18 6 6 18" /></svg>
          </button>
        </header>
        <div className="ia-cajon-cuerpo">{children}</div>
      </aside>
    </>
  );
}
