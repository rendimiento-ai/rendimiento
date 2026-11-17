// The top bar's link to the Problems page, with the number of open ones.
// Apart from pages/Problems, which loads when opened.
import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { api } from "../api";
import { usePoll } from "./ui";
import { t } from "../i18n";

export function ProblemsNavLink({ dibujo }: { dibujo?: ReactNode }) {
  const { data } = usePoll(api.problemCount, [], 30000);
  const n = data?.open ?? 0;
  return (
    <NavLink to="/problems" className="nav-link" aria-label={n ? t("Problems, {n} open", { n }) : t("Problems")}>
      {dibujo}{t("Problems")}{n > 0 && <span className="nav-count">{n}</span>}
    </NavLink>
  );
}
