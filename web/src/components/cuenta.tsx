// The account's pieces the top bar shows on every page: the role's name and
// the organization switch. Apart from pages/Members, which loads when opened.
import { api, type MemberRole, type Org } from "../api";
import { usePoll } from "./ui";
import { t } from "../i18n";

export const memberRoleLabel: Record<MemberRole, string> = { guest: "Guest", collaborator: "Collaborator", admin: "Administrator" };

/**
 * The organization the person works in, as a choice in the top bar when
 * they belong to more than one; choosing another reloads the page in it.
 */
export function OrgSwitch({ current }: { current?: Org }) {
  const { data } = usePoll(api.orgs, [], 0);
  if (!current || !data || data.length < 2) return null;
  return (
    <select className="org-switch" aria-label={t("Organization")} value={current.id} onChange={(e) => {
      api.switchOrg(Number(e.target.value)).then(() => location.assign("/"), (err: Error) => alert(err.message));
    }}>
      {data.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
    </select>
  );
}
