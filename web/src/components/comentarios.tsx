import { useEffect, useState } from "react";
import { api, timeAgo, type Comentario } from "../api";
import { ErrorBox } from "./ui";
import { t } from "../i18n";
import { TextoConFormato } from "./texto";

/** The comments of each cargo or encargo, from one list of the organization's. */
export function porTrabajo(list: Comentario[] | undefined, de: "cargoId" | "encargoId") {
  const out = new Map<number, Comentario[]>();
  for (const c of list ?? []) {
    const id = c[de];
    if (id) out.set(id, [...(out.get(id) ?? []), c]);
  }
  return out;
}

/**
 * What people said about an agent's work (a cargo's handover, an encargo)
 * and agents' answers, oldest first, with a box to say something without
 * sending it back. An agent finds a comment when it starts (ruta_inicio)
 * and answers it; the answer reaches the person in the bell, until they
 * see it here.
 */
export function Comentarios({ list, cargoId, encargoId, onChange }: { list: Comentario[]; cargoId?: number; encargoId?: number; onChange: () => void }) {
  const [open, setOpen] = useState(false);
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();

  // The answers to this person's comments are seen once they are on screen.
  const nuevos = list.filter((c) => c.new).map((c) => c.id).join(",");
  useEffect(() => {
    if (nuevos) api.verRespuestas(nuevos.split(",").map(Number)).catch(() => undefined);
  }, [nuevos]);

  const send = () => {
    setBusy(true); setErr(undefined);
    api.comentar({ cargoId, encargoId, body: body.trim() }).then(() => {
      setBusy(false); setBody(""); setOpen(false); onChange();
    }, (e) => { setBusy(false); setErr(e); });
  };

  return (
    <div className="comentarios">
      {list.map((c) => (
        <div key={c.id} className={`comentario${c.byAgent ? " de-agente" : ""}${c.new ? " nuevo" : ""}`}>
          <div className="small muted">
            {c.byAgent ? t("{agent} answered", { agent: c.author }) : c.author} · {timeAgo(c.createdAt)}
            {!c.byAgent && !c.answeredAt && <> · {t("no agent has answered yet")}</>}
          </div>
          <TextoConFormato text={c.body} />
        </div>
      ))}
      {open ? (
        <div className="stack" style={{ gap: 6 }}>
          <textarea rows={3} value={body} autoFocus onChange={(e) => setBody(e.target.value)}
            placeholder={t("What is wrong, or what you would like otherwise. The next agent reads it, and its answer reaches you in the bell.")} />
          <ErrorBox error={err} />
          <div className="row" style={{ gap: 8 }}>
            <button className="primary" disabled={busy || !body.trim()} onClick={send}>{t("Comment")}</button>
            <button className="link small" onClick={() => { setOpen(false); setErr(undefined); }}>{t("Cancel")}</button>
          </div>
        </div>
      ) : (
        <div><button className="link small" onClick={() => setOpen(true)}>{list.length ? t("Add a comment") : t("Comment on it")}</button></div>
      )}
    </div>
  );
}
