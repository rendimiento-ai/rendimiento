// «Copias vivas», at the top of the Caminos tab of an app with previews: (Ruta 180). Four figures (how many of its most,
// the memory they hold, the cluster's share of all apps' copies against the cap, the app's previews: settings), one card
// per copy (a short copy en seco dashed), those deleted lately and the Secrets a copy lacks. A person who may merge has
// a «⋯» menu on each pull request's copy: make it again with the last commit, keep it 3 more days, or delete it now,
// which asks first on the card itself. Every target is at least 44 px; it fits a 390 px phone.
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { api, ApiError, timeAgo, type App, type CopiaGone, type CopiaLive, type Copias, type CopiaWhy, type MemoryFrom } from "../api";
import { locale, t, tn } from "../i18n";
import { pullLabel, pullURL } from "../forja";
import { Confirmar } from "./encargos";
import { Esqueleto, Hueso } from "./esqueleto";

const fmtDay = new Intl.DateTimeFormat(locale, { weekday: "long", day: "numeric" });
const fmtHour = new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit" });
const mib = (n: number) => (n >= 1024 ? t("{n} GiB", { n: (n / 1024).toFixed(1) }) : t("{n} MiB", { n }));

/** Whether the person reading may act on the app's copies: whoever may merge (not a guest). */
export const mayActOnCopies = (app: App) => !!app.role && app.role !== "guest";

/** The anchor of a pull request's copy's card. */
export const copiaAnchor = (pull: number) => `copia-${pull}`;

/** Goes to a copy's card (or the section) and gives it the focus. */
export function goToCopia(id: string) {
  const el = document.getElementById(id);
  if (!el) return;
  el.scrollIntoView({ block: "start", behavior: window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
  el.focus({ preventScroll: true });
}

const WHY: Record<CopiaWhy, string> = {
  merged: "when merged",
  closed: "when closed",
  idle: "with no push",
  max: "to make room",
  person: "by a person",
  app: "with its app",
  seco: "when its checks ended",
};

const HEALTH: Record<string, { cls: string; label: string }> = {
  Healthy: { cls: "ok", label: "Healthy" },
  Progressing: { cls: "warn live", label: "Starting" },
  WaitingForBuild: { cls: "warn", label: "Waiting" },
  Waiting: { cls: "warn", label: "Waiting" },
  Degraded: { cls: "bad", label: "Not healthy" },
  Error: { cls: "bad", label: "Not healthy" },
  Suspended: { cls: "", label: "Asleep" },
};
const healthOf = (h: string) => HEALTH[h] ?? { cls: "", label: h };

const memWord = (from?: MemoryFrom) => (from === "usage" ? t("in use") : from === "requests" ? t("asked for") : from === "mixed" ? t("in use or asked for") : "");

/** The skeleton of the section while it comes. */
export function EsqCopias() {
  return (
    <Esqueleto className="copias">
      <Hueso w="38%" h={22} r={6} />
      <div className="copias-cifras">{[0, 1, 2, 3].map((i) => <Hueso key={i} h={64} r={12} />)}</div>
      <Hueso h={150} r={14} />
    </Esqueleto>
  );
}

/** The section: its figures, a card per copy, those deleted lately and the Secrets a copy lacks. */
export function CopiasVivas({ app, data, onChange }: { app: App; data: Copias; onChange: () => void }) {
  const loc = useLocation();
  const p = data.previews;
  // Arriving from the glance (…/caminos#copias) or a badge: go there once the cards are drawn.
  useEffect(() => {
    if (loc.hash.startsWith("#copia")) goToCopia(loc.hash.slice(1));
  }, [loc.hash]);
  if (!p) return null;
  const n = data.live.length;
  const may = mayActOnCopies(app);
  const gone = data.gone.slice(0, 6);
  return (
    <section className="copias" id="copias" tabIndex={-1} aria-labelledby="copias-titulo">
      <h2 id="copias-titulo">{t("Live copies")} <span className="muted">· {t("{n} of {max}", { n, max: p.max })}</span></h2>
      <div className="copias-cifras">
        <Cifra label={t("Live copies")} value={t("{n} of {max}", { n, max: p.max })} note={t("one per pull request")} />
        <Cifra label={t("Memory they hold")} value={mib(data.memoryMiB)} note={memWord(data.memoryFrom)} />
        {data.cluster ? (
          <Cifra label={t("Of the cluster, all apps")} value={t("{n}% of {cap}%", { n: (data.cluster.percent < 10 ? Number(data.cluster.percent.toFixed(1)) : Math.round(data.cluster.percent)).toLocaleString(locale), cap: data.cluster.cap })}
            note={t("{used} of {total}", { used: mib(data.cluster.copiesMiB), total: mib(data.cluster.clusterMiB) })} warn={data.cluster.percent > data.cluster.cap} />
        ) : (
          <Cifra label={t("Of the cluster, all apps")} value="—" note={data.where === "computer" ? t("On a computer: no cluster to share") : t("The nodes could not be read")} />
        )}
        <Cifra label={t("How previews are asked for")} value={`keep: ${p.keep}, max: ${p.max}`} note={t("Change it in rendimiento.yaml")} code />
      </div>

      {n === 0 ? (
        <p className="muted">{t("No live copy now. A pull request whose branch passed gets one; it goes when the pull request is merged or closed, or after {n} days without a push.", { n: p.keep })}</p>
      ) : (
        <div className="copias-lista">
          {data.live.map((c) => (c.short ? <CopiaCorta key={c.name} app={app} c={c} /> : <CopiaCard key={c.name} app={app} c={c} keep={p.keep} extend={p.extend} data={p.data} may={may} onChange={onChange} />))}
        </div>
      )}

      {gone.length > 0 && (
        <p className="copias-idas small">
          <strong>{t("Deleted lately")}</strong>{" "}
          {gone.map((g, i) => <span key={`${g.pull}-${g.goneAt}`}>{i > 0 && " · "}<Ida g={g} /></span>)}
        </p>
      )}

      {(data.missingSecrets?.length ?? 0) > 0 && (
        <div className="alert info copias-secretos" role="note">
          <strong>{t("Secrets of the copies:")}</strong>{" "}
          {tn(data.missingSecrets!.length, "the copy does not have the secret {names}.", "the copies do not have the secrets {names}.", { names: data.missingSecrets!.join(", ") })}{" "}
          {data.where === "computer"
            ? <>{t("On this computer a copy reads its app's secrets: set it in Settings.")} <Link to={`/apps/${app.name}/settings`}>{t("Settings")}</Link></>
            : t("A copy reads its secrets in its own namespace, and nothing copies them there: it starts, but what needs them does not work.")}
        </div>
      )}
      {!may && n > 0 && <p className="muted small">{t("Only people who may merge can make a copy again, keep it longer or delete it.")}</p>}
    </section>
  );
}

function Cifra({ label, value, note, warn, code }: { label: string; value: string; note?: string; warn?: boolean; code?: boolean }) {
  return (
    <div className={`copias-cifra${warn ? " aviso" : ""}`}>
      <div className="label">{label}</div>
      <div className="value">{code ? <code>{value}</code> : value}</div>
      {note && <div className="note">{note}</div>}
    </div>
  );
}

function Ida({ g }: { g: CopiaGone }) {
  const why = g.why ? t(WHY[g.why]) : "";
  return <>#{g.pull} {why}{g.why === "person" && g.by ? ` (${g.by})` : ""} <span className="muted">({timeAgo(g.goneAt)})</span></>;
}

/** When a pull request's copy goes, in words. */
function deletes(c: CopiaLive, keep: number): string {
  if (!c.deletesOn) return "";
  const day = fmtDay.format(new Date(c.deletesOn));
  return c.extended ? t("Deleted on {day} (kept longer by a person), or when merged", { day }) : t("Deleted on {day} ({n} days without a push), or when merged", { day, n: keep });
}

/** The line of its own tests and its checks en seco. */
function Pruebas({ c }: { c: CopiaLive }) {
  const parts: ReactNode[] = [];
  if (c.tests) {
    const failed = c.tests.failed ?? [];
    parts.push(failed.length
      ? <span key="t" className="copias-mal">{t("Own tests: {n} of {total} · failed «{names}»", { n: c.tests.passed, total: c.tests.total, names: failed.join("», «") })}</span>
      : <span key="t">{t("Own tests: {n} of {total}", { n: c.tests.passed, total: c.tests.total })}</span>);
  } else parts.push(<span key="t" className="muted">{t("Own tests: not yet")}</span>);
  if (c.dry) {
    const d = c.dry;
    parts.push(<span key="d" className={d.failed ? "copias-mal" : undefined}>{d.running ? t("en seco: running") : d.failed ? t("en seco: {names} did not pass", { names: (d.names ?? []).join(", ") || t("a check") }) : t("en seco: passed")}</span>);
  }
  return <div className="copias-pruebas">{parts.map((p, i) => <span key={i}>{i > 0 && " · "}{p}</span>)}</div>;
}

function CopiaCard({ app, c, keep, extend, data, may, onChange }: { app: App; c: CopiaLive; keep: number; extend: number; data: string; may: boolean; onChange: () => void }) {
  const [confirma, setConfirma] = useState(false);
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string }>();
  const pull = c.pull ?? 0;
  const h = healthOf(c.health);
  const failed = (c.tests?.failed?.length ?? 0) > 0 || !!c.dry?.failed;
  const act = async (what: string, f: () => Promise<string>) => {
    setBusy(what);
    setMsg(undefined);
    try {
      setMsg({ ok: true, text: await f() });
      onChange();
    } catch (e) {
      setMsg({ ok: false, text: e instanceof ApiError || e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy("");
    }
  };
  const rehacer = () => act("rehacer", async () => {
    const r = await api.rehacerCopia(app.name, pull);
    return r.restarted ? t("It already ran the last commit that passed (run {run}): it is restarting.", { run: r.run }) : t("It is being made again with run {run}.", { run: r.run });
  });
  const extender = () => act("extender", async () => {
    const r = await api.extenderCopia(app.name, pull);
    return t("It is kept until {day}.", { day: fmtDay.format(new Date(r.deletesOn)) });
  });
  const borrar = () => act("borrar", async () => {
    await api.borrarCopia(app.name, pull);
    setConfirma(false);
    return t("Deleted. The next push to its branch makes it again.");
  });
  const items = [
    { label: t("Make it again with the last commit"), onClick: rehacer },
    ...(c.canExtend ? [{ label: tn(extend, "Keep it {n} more day", "Keep it {n} more days"), onClick: extender }] : []),
    { label: t("Delete it now"), onClick: () => setConfirma(true), peligro: true },
  ];
  return (
    <article className="card copias-card" id={copiaAnchor(pull)} tabIndex={-1} aria-labelledby={`copia-titulo-${pull}`} aria-busy={busy !== "" || undefined}>
      <div className="copias-cabeza">
        <h3 id={`copia-titulo-${pull}`}>#{pull}{c.title ? ` · ${c.title}` : ""}{c.bot && c.author && <span className="badge copias-bot">{c.author.replace(/\[bot\]$/, "")}</span>}</h3>
        <span className={`badge ${h.cls}`} title={c.message || undefined}>{t(h.label)}</span>
      </div>
      {c.url && <a className="copias-url" href={c.url} target="_blank" rel="noreferrer">{c.url.replace(/^https?:\/\//, "")} ↗</a>}
      <div className="small muted copias-meta">
        {[
          c.branch && t("branch {name}", { name: c.branch }),
          c.sha && t("commit {sha}", { sha: c.sha.slice(0, 7) }),
          c.run && t("run {id}", { id: c.run }),
          c.author && !c.bot && t("by {who}", { who: c.author }),
        ].filter(Boolean).join(" · ")}
      </div>
      <Pruebas c={c} />
      <div className="small muted">
        {[t("1 replica per service"), `${mib(c.memoryMiB)} ${memWord(c.memoryFrom)}`.trim(), data === "seed" ? t("sample data") : t("empty database")].join(" · ")}
      </div>
      {c.deletesOn && <div className="small">{deletes(c, keep)}</div>}
      {(c.missingSecrets?.length ?? 0) > 0 && <div className="small copias-mal">{tn(c.missingSecrets!.length, "It does not have the secret {names}.", "It does not have the secrets {names}.", { names: c.missingSecrets!.join(", ") })}</div>}

      {confirma ? (
        <Confirmar pregunta={t("Delete the copy of #{n}?", { n: pull })} si={busy === "borrar" ? t("Deleting…") : t("Delete it")} onSi={borrar} onNo={() => setConfirma(false)} />
      ) : (
        <div className="copias-acciones">
          {c.url && <a className="et-boton principal" href={c.url} target="_blank" rel="noreferrer">{t("Open the copy")}</a>}
          {failed && c.run
            ? <Link className="et-boton" to={`/apps/${app.name}/runs/${c.run}`}>{t("See what failed")}</Link>
            : <a className="et-boton" href={pullURL(app, pull)} target="_blank" rel="noreferrer">{pullLabel(app, pull)}</a>}
          {may && <Menu label={t("More for the copy of #{n}", { n: pull })} items={items} disabled={busy !== ""} />}
        </div>
      )}
      {confirma && <p className="small muted">{t("Its namespace goes, with its database. The pull request stays open; the next push makes the copy again.")}</p>}
      {msg && <p className={`small ${msg.ok ? "muted" : "copias-mal"}`} role="status">{msg.text}</p>}
    </article>
  );
}

function CopiaCorta({ app, c }: { app: App; c: CopiaLive }) {
  const h = healthOf(c.health);
  const until = c.deletesOn ? fmtHour.format(new Date(c.deletesOn)) : "";
  return (
    <article className="card copias-card corta" aria-label={t("Short copy en seco of run {id}", { id: c.run ?? 0 })}>
      <div className="copias-cabeza">
        <h3>{t("Short copy en seco")} · {t("run {id}", { id: c.run ?? 0 })}{c.branch ? `, ${t("branch {name}", { name: c.branch })}` : ""}</h3>
        <span className={`badge ${h.cls}`}>{t(h.label)}</span>
      </div>
      <p className="small muted">{t("No pull request yet: it lives only while the run's checks en seco run, and counts toward the most copies. At the latest it goes at {time}.", { time: until })}</p>
      <Pruebas c={c} />
      <div className="copias-acciones">
        {c.url && <a className="et-boton" href={c.url} target="_blank" rel="noreferrer">{t("Open the copy")}</a>}
        {c.run ? <Link className="et-boton" to={`/apps/${app.name}/runs/${c.run}`}>{t("See the run")}</Link> : null}
      </div>
    </article>
  );
}

/** The «⋯» menu of a copy. Escape, a tap outside or a choice closes it. It opens toward the button's left, and is moved
 *  back inside the screen, 8 px from its edges, when that would leave it out (the button at a phone's left edge). */
function Menu({ label, items, disabled }: { label: string; items: { label: string; onClick: () => void; peligro?: boolean }[]; disabled?: boolean }) {
  const [open, setOpen] = useState(false);
  const caja = useRef<HTMLDivElement>(null);
  const lista = useRef<HTMLDivElement>(null);
  const [mover, setMover] = useState(0);
  useLayoutEffect(() => {
    if (!open) { setMover(0); return; }
    const r = lista.current?.getBoundingClientRect();
    if (!r) return;
    const ancho = document.documentElement.clientWidth;
    if (r.left < 8) setMover(8 - r.left);
    else if (r.right > ancho - 8) setMover(ancho - 8 - r.right);
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const fuera = (ev: PointerEvent) => !caja.current?.contains(ev.target as Node) && setOpen(false);
    const tecla = (ev: KeyboardEvent) => ev.key === "Escape" && setOpen(false);
    document.addEventListener("pointerdown", fuera);
    document.addEventListener("keydown", tecla);
    caja.current?.querySelector<HTMLButtonElement>("[role=menuitem]")?.focus();
    return () => { document.removeEventListener("pointerdown", fuera); document.removeEventListener("keydown", tecla); };
  }, [open]);
  return (
    <div className="et-menu" ref={caja}>
      <button type="button" className="et-boton redondo" aria-haspopup="menu" aria-expanded={open} aria-label={label} title={label} disabled={disabled} onClick={() => setOpen(!open)}>⋯</button>
      {open && (
        <div role="menu" aria-label={label} className="et-menu-lista" ref={lista} style={mover ? { transform: `translateX(${mover}px)` } : undefined}>
          {items.map((it) => (
            <button key={it.label} type="button" role="menuitem" className={it.peligro ? "peligro" : undefined} onClick={() => { setOpen(false); it.onClick(); }}>{it.label}</button>
          ))}
        </div>
      )}
    </div>
  );
}
