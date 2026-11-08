import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api, timeAgo } from "../api";
import { lang, t, tn } from "../i18n";
import { usePoll } from "./ui";
import { useHistorial } from "./avisos";

/** One thing that waits for the person, and where to deal with it. */
interface Pendiente {
  clave: string;
  texto: string;
  a: string;
}

/**
 * The bell in the top bar: how many things wait for this person (changes to
 * production agents ask for, encargos delivered for review, open problems
 * for the platform's operators), and, opened, those things and what
 * happened since the page opened. Nothing that waits goes unseen.
 */
export function Campana({ role, operator }: { role?: string; operator?: boolean }) {
  const actua = !!role && role !== "guest";
  const { data: aprobaciones } = usePoll(() => (actua ? api.aprobaciones(true, lang) : Promise.resolve([])), [actua], 20000);
  const { data: encargos } = usePoll(() => (actua ? api.encargos() : Promise.resolve([])), [actua], 30000);
  const { data: problemas } = usePoll(() => (operator ? api.problemCount() : Promise.resolve({ open: 0 })), [operator], 30000);
  const historial = useHistorial();
  const [abierta, setAbierta] = useState(false);
  const caja = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!abierta) return;
    const fuera = (e: MouseEvent) => { if (caja.current && !caja.current.contains(e.target as Node)) setAbierta(false); };
    const tecla = (e: KeyboardEvent) => { if (e.key === "Escape") setAbierta(false); };
    document.addEventListener("mousedown", fuera);
    document.addEventListener("keydown", tecla);
    return () => { document.removeEventListener("mousedown", fuera); document.removeEventListener("keydown", tecla); };
  }, [abierta]);

  const pendientes: Pendiente[] = [
    ...(aprobaciones ?? []).map((a) => ({
      clave: `aprobacion-${a.id}`,
      texto: t("{agent} asks to roll {app} back to release #{n}", { agent: a.pedidaPor, app: a.app, n: a.version }),
      a: "/bitacora",
    })),
    ...(encargos ?? []).filter((e) => e.status === "delivered").map((e) => ({
      clave: `encargo-${e.id}`,
      texto: t("Encargo #{n} was delivered: review it", { n: e.id }),
      a: "/ruta?tab=encargos",
    })),
    ...((problemas?.open ?? 0) > 0 ? [{
      clave: "problemas",
      texto: tn(problemas!.open, "{n} problem is open", "{n} problems are open"),
      a: "/problems",
    }] : []),
  ];
  const n = pendientes.length;

  return (
    <div className="campana" ref={caja}>
      <button type="button" className={`campana-boton${n ? " con" : ""}`} aria-expanded={abierta} onClick={() => setAbierta(!abierta)}
        aria-label={n ? tn(n, "{n} thing waits for you", "{n} things wait for you") : t("Notices")} title={t("Notices")}>
        <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
          <path d="M18 8a6 6 0 1 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" /><path d="M13.7 21a2 2 0 0 1-3.4 0" />
        </svg>
        {n > 0 && <span className="nav-count">{n}</span>}
      </button>
      {abierta && (
        <div className="campana-panel" role="dialog" aria-label={t("Notices")}>
          <div className="small muted campana-titulo">{t("Waiting for you")}</div>
          {n === 0 && <div className="small">{t("Nothing waits for you.")}</div>}
          {pendientes.map((p) => (
            <Link key={p.clave} to={p.a} className="campana-item" onClick={() => setAbierta(false)}>{p.texto}</Link>
          ))}
          <div className="small muted campana-titulo">{t("What happened")}</div>
          {historial.length === 0 && <div className="small muted">{t("Nothing yet since you opened this page.")}</div>}
          {historial.map((h) => (
            <Link key={h.id} to={h.a} className={`campana-item ${h.tono}`} onClick={() => setAbierta(false)}>
              <span>{h.titulo}</span>
              <span className="small muted">{h.detalle ? `${h.detalle} · ` : ""}{timeAgo(h.at)}</span>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
