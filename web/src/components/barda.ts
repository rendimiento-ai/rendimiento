// The wall between organizations in words, for the Universe, the app page and Environment.
import { t } from "../i18n";

/** Why an app is outside the wall, in words. */
export function outsideWhy(why: string | undefined, nodes?: string[]) {
  if (why === "shared") return t("It uses a shared namespace, which belongs to someone else: rendimiento never walls it.");
  if (why === "node") return t("It runs on {nodes}, whose network does not enforce the policies (the node carries rendimiento.ai/network-policy=no).", { nodes: (nodes ?? []).join(", ") });
  if (why === "off") return t("The boundary is off: any namespace reaches it.");
  return "";
}
