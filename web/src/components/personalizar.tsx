// Customize home: the home page's sections, each with a switch, in the order
// they show. Drag a row to move it, or use its arrows (the keyboard's way).
// The page changes as it is edited; Save keeps it for this person, on every
// device. Back to the usual forgets their own; an administrator of the
// platform can make what they see the default for whoever never chose.
import { useState, type DragEvent } from "react";
import { api, session, type InicioView, type SeccionInicio } from "../api";
import { t } from "../i18n";
import { anotar } from "../uso";
import { NOMBRES, completar, type IdSeccion } from "../inicio";
import { ErrorBox, Switch } from "./ui";

/** Whether a section is offered to this person: Delivery is the platform's operators'. */
export function ofrecida(id: string): boolean {
  return id !== "entregas" || session.operator;
}

export function Personalizar({ lista, poner, vista, guardado, cerrar }: {
  lista: SeccionInicio[];
  poner: (l: SeccionInicio[]) => void;
  vista?: InicioView;
  guardado: (v: InicioView) => void;
  cerrar: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [dicho, setDicho] = useState("");
  const [arrastra, setArrastra] = useState<string>();
  const [sobre, setSobre] = useState<string>();
  const visibles = lista.filter((s) => ofrecida(s.id));

  const cambiar = (id: string, on: boolean) => poner(lista.map((s) => (s.id === id ? { ...s, on } : s)));
  /** Moves a section to where another is (the ones not offered keep their place among the rest). */
  const llevar = (id: string, a: string) => {
    if (id === a) return;
    const sin = lista.filter((s) => s.id !== id);
    const i = sin.findIndex((s) => s.id === a);
    const desde = lista.findIndex((s) => s.id === id), hasta = lista.findIndex((s) => s.id === a);
    sin.splice(desde < hasta ? i + 1 : i, 0, lista[desde]);
    poner(sin);
  };
  const mover = (id: string, paso: -1 | 1) => {
    const i = visibles.findIndex((s) => s.id === id);
    const otro = visibles[i + paso];
    if (!otro) return;
    llevar(id, otro.id);
    setDicho(t("{section} moved to place {n}", { section: t(NOMBRES[id as IdSeccion]), n: i + paso + 1 }));
  };

  const hacer = (que: () => Promise<InicioView>, aviso: string, alFinal?: (v: InicioView) => void) => {
    setBusy(true);
    setError(undefined);
    que().then((v) => { guardado(v); setDicho(aviso); alFinal?.(v); }, setError).finally(() => setBusy(false));
  };
  const guardar = () => { anotar("save_home"); hacer(() => api.guardarInicio(lista), t("Saved."), cerrar); };

  const soltar = (e: DragEvent, a: string) => {
    e.preventDefault();
    if (arrastra) llevar(arrastra, a);
    setArrastra(undefined);
    setSobre(undefined);
  };

  return (
    <section className="card inicio-ajustes" id="inicio-ajustes" aria-labelledby="inicio-ajustes-t">
      <header>
        <h2 id="inicio-ajustes-t">{t("What your home shows")}</h2>
        <span className="small muted">{t("Only for you, the same on every device.")}</span>
      </header>
      <ol className="inicio-ajustes-lista">
        {visibles.map((s, i) => {
          const nombre = t(NOMBRES[s.id as IdSeccion]);
          return (
            <li key={s.id} data-seccion={s.id} draggable
              className={[arrastra === s.id && "arrastrando", sobre === s.id && arrastra && arrastra !== s.id && "sobre"].filter(Boolean).join(" ") || undefined}
              onDragStart={(e) => { setArrastra(s.id); e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("text/plain", s.id); }}
              onDragOver={(e) => { e.preventDefault(); if (sobre !== s.id) setSobre(s.id); }}
              onDrop={(e) => soltar(e, s.id)}
              onDragEnd={() => { setArrastra(undefined); setSobre(undefined); }}>
              <span className="inicio-asa" aria-hidden="true">⋮⋮</span>
              <span className="inicio-ajuste-nombre">{nombre}</span>
              <span className="inicio-flechas">
                <button type="button" className="inicio-mover" disabled={i === 0} onClick={() => mover(s.id, -1)} aria-label={t("Move {section} up", { section: nombre })} title={t("Move up")}>↑</button>
                <button type="button" className="inicio-mover" disabled={i === visibles.length - 1} onClick={() => mover(s.id, 1)} aria-label={t("Move {section} down", { section: nombre })} title={t("Move down")}>↓</button>
              </span>
              <Switch checked={s.on} onChange={(on) => cambiar(s.id, on)} label={nombre} />
            </li>
          );
        })}
      </ol>
      <p className="small muted" style={{ margin: 0 }}>{t("Drag a row to change the order, or use its arrows.")}</p>
      <ErrorBox error={error} />
      <div className="row inicio-ajustes-botones">
        <button type="button" className="primary" disabled={busy} onClick={guardar}>{t("Save")}</button>
        <button type="button" disabled={busy} onClick={cerrar}>{t("Cancel")}</button>
        <button type="button" className="link" disabled={busy || !vista} onClick={() => hacer(() => api.inicioDeSiempre(), t("Back to the usual."), (v) => poner(completar(v.secciones)))}>
          {t("Back to the usual")}
        </button>
      </div>
      {vista?.puedeFijar && (
        <div className="inicio-ajustes-admin">
          <button type="button" disabled={busy} onClick={() => hacer(() => api.fijarInicio(lista), t("This is now the default for everyone who never customized their home."))}>
            {t("Make this the default for everyone")}
          </button>
          {vista.predeterminadoPor && (
            <button type="button" className="link" disabled={busy} onClick={() => hacer(() => api.inicioDeFabrica(), t("The default is the built-in one again."))}>
              {t("Back to the built-in default")}
            </button>
          )}
          <span className="small muted">
            {vista.predeterminadoPor
              ? t("The default was set by {who}; it is what people who never customized their home see.", { who: vista.predeterminadoPor })
              : t("For administrators of the platform: what people who never customized their home see.")}
          </span>
        </div>
      )}
      <p className="sr-only" role="status" aria-live="polite">{dicho}</p>
    </section>
  );
}
