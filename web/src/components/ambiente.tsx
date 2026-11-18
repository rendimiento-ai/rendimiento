// The look of Cotija, pueblo mágico: the roof and papel picado over every
// page (with faroles at night), the night sky, the guardapolvo at the foot,
// the day/night, colors and movement switches, and the cempasúchil each app has.
// Decorations are aria-hidden; everything they show is also said in text.
import { useEffect, useRef, useState, type KeyboardEvent as TeclaReact } from "react";
import { t } from "../i18n";
import { anotar } from "../uso";

// ---- day / night and movement ----

type Tema = "day" | "night";

/** Remembers a choice in this browser, when it can. */
function guardar(key: string, v: string) {
  try {
    localStorage.setItem(key, v);
  } catch {
    /* not remembered; still applies to this page */
  }
}

/** The theme in effect: the one picked, else the device's. */
function temaActual(): Tema {
  const t = document.documentElement.dataset.theme;
  if (t === "day" || t === "night") return t;
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "night" : "day";
}

/** Día / Noche: switches the mode and remembers it in this browser; beside it, the colors (FamiliaSwitch). */
export function TemaSwitch() {
  const [tema, setTema] = useState<Tema>(temaActual);
  useEffect(() => {
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    const sigue = () => setTema(temaActual());
    mq?.addEventListener("change", sigue);
    return () => mq?.removeEventListener("change", sigue);
  }, []);
  const otro: Tema = tema === "night" ? "day" : "night";
  const label = otro === "night" ? t("Switch to night") : t("Switch to day");
  return (
    <>
      <button className="icon-btn" onClick={() => {
        anotar("switch_theme");
        document.documentElement.dataset.theme = otro;
        guardar("rendimiento.tema", otro);
        setTema(otro);
      }} title={label} aria-label={label}>
        {otro === "night" ? (
          <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinejoin="round" /></svg>
        ) : (
          <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4.5" fill="none" stroke="currentColor" strokeWidth="2.2" /><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1L7 17M17 7l2.1-2.1" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" /></svg>
        )}
      </button>
      <FamiliaSwitch modo={tema} />
    </>
  );
}

// ---- the colors: the classic papel picado, or one of six families ----

/** A family of colors, with its day and its night (the tokens are in styles.css, under data-tema).
 *  Each swatch list is the background, the accent and the flower. Keep the ids in step with index.html. */
interface Familia { id: string; nombre: () => string; day: string[]; night: string[] }

const familias: Familia[] = [
  { id: "", nombre: () => t("Papel picado"), day: ["#FBF5EA", "#8E1B2C", "#E8890C"], night: ["#120F2A", "#F2A93B", "#FFC35C"] },
  { id: "sierra", nombre: () => t("Sierra"), day: ["#EEF3EE", "#2F6B4F", "#E8890C"], night: ["#0F1A16", "#6FC49A", "#F2A93B"] },
  { id: "atardecer", nombre: () => t("Sunset on the hill"), day: ["#FFF1E6", "#C2543A", "#E8890C"], night: ["#2A1E3A", "#FF9B6A", "#FFC857"] },
  { id: "bugambilia", nombre: () => t("Bougainvillea"), day: ["#FFF3F6", "#D81E6A", "#FF8A00"], night: ["#240A1C", "#FF5C9A", "#FFA733"] },
  { id: "talavera", nombre: () => t("Talavera"), day: ["#F4F7FC", "#1F3FA8", "#F2B400"], night: ["#0B1430", "#7FA2FF", "#F2C230"] },
  { id: "milpa", nombre: () => t("Milpa"), day: ["#F6F1DF", "#5E7F1E", "#E2A400"], night: ["#17190C", "#B5D05A", "#F2C230"] },
  { id: "adobe", nombre: () => t("Adobe and roof tile"), day: ["#F7EDE2", "#B4512B", "#D98E04"], night: ["#21140E", "#F08A5D", "#F2B04A"] },
];

/** The family in effect: the one picked, else the classic one. */
function familiaActual(): string {
  const f = document.documentElement.dataset.tema ?? "";
  return familias.some((x) => x.id === f) ? f : "";
}

function Muestras({ colores }: { colores: string[] }) {
  return (
    <span className="tema-muestras" aria-hidden="true">
      {colores.map((c, i) => <i key={i} style={{ background: c }} />)}
    </span>
  );
}

/** The colors: a button that opens the list of families, each with its swatches for the mode in effect.
 *  The choice is remembered in this browser, apart from the mode. */
export function FamiliaSwitch({ modo }: { modo: Tema }) {
  const [familia, setFamilia] = useState(familiaActual);
  const [abierta, setAbierta] = useState(false);
  const caja = useRef<HTMLDivElement>(null);
  const boton = useRef<HTMLButtonElement>(null);
  const lista = useRef<HTMLUListElement>(null);

  useEffect(() => {
    if (!abierta) return;
    lista.current?.querySelector<HTMLButtonElement>('button[aria-pressed="true"]')?.focus();
    const fuera = (e: MouseEvent) => { if (caja.current && !caja.current.contains(e.target as Node)) setAbierta(false); };
    const tecla = (e: KeyboardEvent) => {
      if (e.key === "Escape") { setAbierta(false); boton.current?.focus(); }
    };
    document.addEventListener("mousedown", fuera);
    document.addEventListener("keydown", tecla);
    return () => {
      document.removeEventListener("mousedown", fuera);
      document.removeEventListener("keydown", tecla);
    };
  }, [abierta]);

  const elegir = (id: string) => {
    if (id) document.documentElement.dataset.tema = id;
    else delete document.documentElement.dataset.tema;
    guardar("rendimiento.colores", id || "papel-picado");
    setFamilia(id);
    setAbierta(false);
    boton.current?.focus();
  };

  // Up and down (and Home, End) move between the families; Tab leaves as usual.
  const mover = (e: TeclaReact<HTMLUListElement>) => {
    const botones = [...(lista.current?.querySelectorAll<HTMLButtonElement>("button") ?? [])];
    const i = botones.indexOf(document.activeElement as HTMLButtonElement);
    const a = e.key === "ArrowDown" ? (i + 1) % botones.length : e.key === "ArrowUp" ? (i - 1 + botones.length) % botones.length
      : e.key === "Home" ? 0 : e.key === "End" ? botones.length - 1 : -1;
    if (a < 0) return;
    e.preventDefault();
    botones[a]?.focus();
  };

  const actual = familias.find((f) => f.id === familia) ?? familias[0];
  const label = t("Colors: {name}", { name: actual.nombre() });
  return (
    <div className="tema-selector" ref={caja}>
      <button type="button" ref={boton} className="icon-btn tema-boton" aria-expanded={abierta} aria-controls="tema-lista"
        title={label} aria-label={label} onClick={() => setAbierta(!abierta)}>
        <Muestras colores={actual[modo]} />
      </button>
      {abierta && (
        <div className="tema-panel">
          <p className="tema-panel-titulo" id="tema-lista-titulo">{t("Colors")}</p>
          <ul id="tema-lista" ref={lista} aria-labelledby="tema-lista-titulo" onKeyDown={mover}>
            {familias.map((f) => (
              <li key={f.id || "papel-picado"}>
                <button type="button" aria-pressed={f.id === familia} onClick={() => elegir(f.id)}>
                  <Muestras colores={f[modo]} />
                  <span>{f.nombre()}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

/** Movement on / off (the papel picado, the flowers, the candles). */
export function MovimientoSwitch() {
  const [quieto, setQuieto] = useState(document.documentElement.dataset.motion === "off");
  const label = quieto ? t("Turn movement on") : t("Turn movement off");
  return (
    <button className="icon-btn" aria-pressed={!quieto} title={label} aria-label={label} onClick={() => {
      const next = !quieto;
      if (next) document.documentElement.dataset.motion = "off";
      else delete document.documentElement.dataset.motion;
      guardar("rendimiento.movimiento", next ? "off" : "on");
      setQuieto(next);
    }}>
      <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
        <path d="M3 12c2.5-4 5-4 7.5 0s5 4 7.5 0 2.5-2 3-2" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
        {quieto && <path d="M4 4l16 16" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />}
      </svg>
    </button>
  );
}

// ---- the roof, the papel picado and the night sky ----

const banderas = ["var(--grana)", "var(--cempasuchil)", "var(--anil)", "var(--rosa)", "var(--nopal)", "var(--maiz)"];

/** Roof tiles and papel picado; at night every fourth flag is a farol. With a family of colors chosen
 *  (data-tema), its band shows instead (styles.css). */
export function Techo() {
  return (
    <div className="techo" aria-hidden="true">
      <div className="tema-franja"><span className="tema-sol" /></div>
      <div className="techo-viga" />
      <div className="techo-tejas" />
      <div className="papel-picado">
        {Array.from({ length: 14 }, (_, i) => (
          <div key={i} className="hueco">
            <span className={`bandera${i % 4 === 1 ? " cede" : ""}`}
              style={{ background: banderas[i % banderas.length], animationDelay: `${(-i * 0.33).toFixed(2)}s` }}>
              <i className="corte c1" /><i className="corte c2" /><i className="corte c3" />
            </span>
            {i % 4 === 1 && <span className="farol" style={{ animationDelay: `${(-i * 0.4).toFixed(2)}s` }}><i /></span>}
          </div>
        ))}
      </div>
    </div>
  );
}

/** Stars and the moon behind everything; shown only at night. */
export function Cielo() {
  return <div className="cielo" aria-hidden="true"><span className="luna" /></div>;
}

/** Which rendimiento runs here, as the foot of every page says it. */
export interface Sistema {
  version?: string;
  modo?: "local" | "kubernetes";
  os: string;
  arch: string;
}

const sistemas: Record<string, string> = { linux: "Linux", windows: "Windows", darwin: "macOS" };

/** "v0.2.1 · Linux arm64 · on a computer": the release (or the commit it was built from), the system and how it runs. */
export function queCorre(s: Sistema): string {
  const v = s.version ?? "";
  const version = /^\d+\.\d+/.test(v) ? "v" + v
    : /^[0-9a-f]{7,40}$/.test(v) ? t("commit {sha}", { sha: v.slice(0, 7) })
    : v || t("unknown version");
  const donde = s.modo === "kubernetes" ? t("on a Kubernetes cluster") : s.modo === "local" ? t("on a computer") : "";
  return [version, `${sistemas[s.os] ?? s.os} ${s.arch}`, donde].filter(Boolean).join(" · ");
}

/** The guardapolvo band at the foot of every page, with which rendimiento runs here. */
export function Guardapolvo({ sistema }: { sistema?: Sistema }) {
  return (
    <footer className="guardapolvo">
      <span>rendimiento · {t("gives your code life")}</span>
      {sistema && <span className="que-corre" title={t("The rendimiento that answers here")}>{queCorre(sistema)}</span>}
      {sistema && <a className="que-corre al-equipo-enlace" href="/escribir">{t("Write to the team")}</a>}
    </footer>
  );
}

// ---- the cempasúchil ----

export type FlorEstado = "brote" | "viva" | "marchita";

/** A cempasúchil: a green bud while the app has never gone live; alive (it breathes); withered (it droops and drops
 *  petals); reviving with a burst of petals when it comes back; and, the first time it goes live, born: the bud
 *  opens petal by petal. */
export function Flor({ estado, size = 56, label }: { estado: FlorEstado; size?: number; label?: string }) {
  const [cambio, setCambio] = useState<"" | "reviviendo" | "naciendo">("");
  const antes = useRef(estado);
  useEffect(() => {
    const de = antes.current;
    antes.current = estado;
    if (estado !== "viva" || (de !== "marchita" && de !== "brote")) return;
    setCambio(de === "brote" ? "naciendo" : "reviviendo");
    const timer = setTimeout(() => setCambio(""), de === "brote" ? 2600 : 1600);
    return () => clearTimeout(timer);
  }, [estado]);
  const reviviendo = cambio !== "";
  const clase = `flor ${cambio || estado}`;
  return (
    <span className={clase} style={{ width: size, height: size }} role={label ? "img" : undefined} aria-label={label} aria-hidden={label ? undefined : true}>
      <svg viewBox="0 0 88 88" width={size} height={size}>
        <g className="flor-cabeza">
          <circle className="p1" cx="44" cy="16" r="14" /><circle className="p2" cx="64" cy="24" r="14" />
          <circle className="p1" cx="72" cy="44" r="14" /><circle className="p2" cx="64" cy="64" r="14" />
          <circle className="p1" cx="44" cy="72" r="14" /><circle className="p2" cx="24" cy="64" r="14" />
          <circle className="p1" cx="16" cy="44" r="14" /><circle className="p2" cx="24" cy="24" r="14" />
          <circle className="centro" cx="44" cy="44" r="16" />
        </g>
      </svg>
      {estado === "marchita" && !reviviendo && (
        <>
          <i className="petalo-cae" style={{ ["--dx" as string]: "10px" }} />
          <i className="petalo-cae" style={{ ["--dx" as string]: "-8px", animationDelay: ".9s" }} />
        </>
      )}
      {reviviendo && Array.from({ length: 8 }, (_, i) => {
        const a = (i / 8) * Math.PI * 2;
        return <i key={i} className="petalo-chispa" style={{ ["--dx" as string]: `${Math.round(Math.cos(a) * size * 0.9)}px`, ["--dy" as string]: `${Math.round(Math.sin(a) * size * 0.9)}px`,
          animationDelay: cambio === "naciendo" ? "1.3s" : undefined }} />;
      })}
    </span>
  );
}

/** A burst of petals over the page, for a release that just went live. */
export function LluviaDePetalos({ cuando }: { cuando: number }) {
  const [activa, setActiva] = useState(false);
  useEffect(() => {
    if (!cuando) return;
    setActiva(true);
    const timer = setTimeout(() => setActiva(false), 2600);
    return () => clearTimeout(timer);
  }, [cuando]);
  if (!activa) return null;
  return (
    <div className="lluvia" aria-hidden="true">
      {Array.from({ length: 28 }, (_, i) => (
        <i key={i} style={{
          left: `${(i * 37) % 100}%`, animationDelay: `${((i * 0.13) % 1.2).toFixed(2)}s`,
          background: banderas[i % banderas.length], ["--dx" as string]: `${((i % 5) - 2) * 18}px`,
        }} />
      ))}
    </div>
  );
}
