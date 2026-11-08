#!/bin/sh
# rendimiento's installer for one computer (Linux):
#
#   curl -fsSL https://rendimiento.ai/instalar | sh
#
# It uses the container engine the computer has (containerd with nerdctl,
# recommended, or Docker), or installs containerd when there is none; puts
# the rendimiento program in ~/.local/bin; and leaves it running as a
# service of your user that starts with the computer. Then it opens
# http://localhost:8080, where the setup page connects GitHub.
#
# To take it off the computer again:
#
#   curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar
#
# It stops and removes the service, rendimiento's containers and your apps',
# their networks and the program; your data (the database, the images, the
# secrets' key) only when you say so, and never without asking.
#
# Settings, all optional:
#   RENDIMIENTO_GITHUB   your GitHub login (asked when not set)
#   RENDIMIENTO_VERSION  a release such as v0.1.0 (default: the latest)
#   RENDIMIENTO_SI=1     answer yes to every question (no terminal needed);
#                        never to deleting the data
#   RENDIMIENTO_BORRAR_DATOS=1  with --desinstalar, delete the data too
set -eu

REPO="rendimiento-ai/rendimiento"
NERDCTL_VERSION="2.4.2"

# ---- messages, in Spanish or English as the computer is set ----
case "${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}" in
es*) ES=1 ;;
*) ES= ;;
esac
say() { if [ -n "$ES" ]; then printf '%s\n' "$2"; else printf '%s\n' "$1"; fi; }
step() { if [ -n "$ES" ]; then printf '\n==> %s\n' "$2"; else printf '\n==> %s\n' "$1"; fi; }
fail() { say "rendimiento: $1" "rendimiento: $2" >&2; exit 1; }

# ask "question en" "pregunta es": yes unless answered no.
ask() {
	[ "${RENDIMIENTO_SI:-}" = 1 ] && return 0
	tty_ok || fail "$1 (run with RENDIMIENTO_SI=1 to answer yes)" "$2 (córralo con RENDIMIENTO_SI=1 para contestar que sí)"
	if [ -n "$ES" ]; then printf '%s [S/n] ' "$2"; else printf '%s [Y/n] ' "$1"; fi
	read -r answer </dev/tty || answer=
	case "$answer" in
	[nN]*) return 1 ;;
	*) return 0 ;;
	esac
}

need() { command -v "$1" >/dev/null 2>&1; }

# tty_ok: there is a terminal to ask in (one that opens, not only exists).
tty_ok() { (: </dev/tty) 2>/dev/null; }

# askno "question en" "pregunta es": no unless answered yes. For what cannot
# be undone: RENDIMIENTO_SI does not answer it.
askno() {
	tty_ok || return 1
	if [ -n "$ES" ]; then printf '%s [s/N] ' "$2"; else printf '%s [y/N] ' "$1"; fi
	read -r answer </dev/tty || answer=
	case "$answer" in
	[yYsS]*) return 0 ;;
	*) return 1 ;;
	esac
}

# desinstalar takes rendimiento off this computer: the service, the
# containers of rendimiento and of the apps it ran, their networks, and the
# program. The data (volumes, and DATA_DIR with the secrets' key) goes only
# when the person says so, after seeing what it is. The container engine
# stays: it may serve other things.
desinstalar() {
	step "Uninstalling rendimiento" "Desinstalando rendimiento"
	ask "Stop rendimiento and remove its service, its containers and your apps' containers, and the program? Your data stays (you are asked about it after)." \
		"¿Detener rendimiento y quitar su servicio, sus contenedores y los de sus aplicaciones, y el programa? Sus datos se quedan (se le pregunta después)." || exit 0
	systemctl --user disable --now rendimiento.service >/dev/null 2>&1 || true
	rm -f "$HOME/.config/systemd/user/rendimiento.service"
	rm -rf "$HOME/.config/systemd/user/rendimiento.service.d"
	systemctl --user daemon-reload >/dev/null 2>&1 || true
	volumes=
	for e in nerdctl docker; do
		need "$e" && "$e" info >/dev/null 2>&1 || continue
		# The apps it ran, from their containers' labels, before they go:
		# their Postgres and Redis keep data in volumes named after them.
		ids=$( ("$e" ps -aq --filter label=rendimiento.ai/app; "$e" ps -aq --filter label=rendimiento.ai/run) 2>/dev/null | sort -u)
		apps=
		[ -z "$ids" ] || apps=$("$e" inspect --format '{{index .Config.Labels "rendimiento.ai/app"}}' $ids 2>/dev/null | grep . | sort -u)
		[ -z "$ids" ] || "$e" rm -f $ids >/dev/null 2>&1 || true
		"$e" rm -f rendimiento-postgres rendimiento-registro rendimiento-buildkit >/dev/null 2>&1 || true
		"$e" network ls --format '{{.Name}}' 2>/dev/null | grep '^rendimiento-' | xargs -r "$e" network rm >/dev/null 2>&1 || true
		existing=$("$e" volume ls -q 2>/dev/null)
		for v in rendimiento-postgres rendimiento-registro rendimiento-buildkit $(for a in $apps; do printf '%s-postgres-data %s-redis-data ' "$a" "$a"; done); do
			printf '%s\n' "$existing" | grep -qx "$v" && volumes="$volumes $e:$v"
		done
		for label in rendimiento.ai/app rendimiento.ai/cache-of rendimiento.ai/run; do
			for v in $("$e" volume ls -q --filter "label=$label" 2>/dev/null); do volumes="$volumes $e:$v"; done
		done
	done
	rm -f "$HOME/.local/bin/rendimiento"
	say "rendimiento is uninstalled: no service, no containers, no program." "rendimiento está desinstalado: sin servicio, sin contenedores, sin programa."

	data="${XDG_CONFIG_HOME:-$HOME/.config}/rendimiento"
	# Those found by an earlier uninstall that kept them: their apps'
	# containers are gone, so only this list knows them now.
	if [ -f "$data/volumenes" ]; then
		for v in $(cat "$data/volumenes"); do
			"${v%%:*}" volume inspect "${v#*:}" >/dev/null 2>&1 && volumes="$volumes $v"
		done
	fi
	volumes=$(printf '%s\n' $volumes | sort -u)
	if [ -z "$volumes" ] && [ ! -d "$data" ]; then
		return 0
	fi
	step "Your data" "Sus datos"
	say "Still on this computer (the database, the images, the caches, your apps' data, and the key that opens the secrets):" \
		"Siguen en esta computadora (la base de datos, las imágenes, las cachés, los datos de sus aplicaciones y la llave que abre los secretos):"
	for v in $volumes; do printf '  %s (volume)\n' "${v#*:}"; done
	[ -d "$data" ] && printf '  %s\n' "$data"
	if [ "${RENDIMIENTO_BORRAR_DATOS:-}" = 1 ] || askno "Delete them too? This cannot be undone." "¿Borrarlos también? No se puede deshacer."; then
		for v in $volumes; do "${v%%:*}" volume rm "${v#*:}" >/dev/null 2>&1 || say "  could not delete ${v#*:}" "  no se pudo borrar ${v#*:}"; done
		rm -rf "$data"
		say "Deleted." "Borrados."
	else
		[ ! -d "$data" ] || printf '%s\n' $volumes >"$data/volumenes"
		say "Kept. Installing again picks them up where they were." "Se quedan. Si vuelve a instalar, rendimiento los retoma donde estaban."
	fi
	say "The container engine stays, as it may serve other things." "El motor de contenedores se queda, porque puede servir para otras cosas."
}

# ---- the computer ----
[ "$(uname -s)" = Linux ] || fail "this installer is for Linux for now; Windows (WSL2) and Mac come later" "este instalador es para Linux por ahora; Windows (WSL2) y Mac vienen después"
case "$(uname -m)" in
x86_64 | amd64) ARCH=amd64 ;;
aarch64 | arm64) ARCH=arm64 ;;
*) fail "this processor ($(uname -m)) is not supported" "este procesador ($(uname -m)) no está soportado" ;;
esac
[ "$(id -u)" != 0 ] || fail "run it as your own user, not root: it asks for sudo when it needs it" "córralo con su propio usuario, no como root: pide sudo cuando lo necesita"
need curl || fail "curl is needed" "se necesita curl"
need systemctl || fail "systemd is needed (rendimiento runs as a service of your user)" "se necesita systemd (rendimiento corre como servicio de su usuario)"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
export PATH="$HOME/.local/bin:/usr/local/bin:$PATH"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

case "${1:-}" in
--desinstalar | --uninstall)
	desinstalar
	exit 0
	;;
"") ;;
*) fail "unknown option $1 (the only one is --desinstalar)" "opción desconocida $1 (la única es --desinstalar)" ;;
esac

# ---- the container engine ----
step "Container engine" "Motor de contenedores"
ENGINE=
if need nerdctl && nerdctl info >/dev/null 2>&1; then
	ENGINE=nerdctl
	say "containerd (nerdctl) is ready." "containerd (nerdctl) está listo."
elif need docker && docker info >/dev/null 2>&1; then
	ENGINE=docker
	say "Docker is ready. It works; containerd is recommended (lighter, and without root)." \
		"Docker está listo. Funciona; se recomienda containerd (más ligero, y sin root)."
elif need nerdctl && need containerd-rootless-setuptool.sh; then
	ENGINE=nerdctl # installed; it is set up for this user below
	say "containerd is installed; it gets set up for your user." "containerd está instalado; se configura para su usuario."
elif need docker; then
	fail "Docker is installed, but your user cannot use it: add yourself to the docker group (sudo usermod -aG docker $USER), sign in again, and run this again" \
		"Docker está instalado, pero su usuario no puede usarlo: agréguese al grupo docker (sudo usermod -aG docker $USER), vuelva a entrar y corra esto de nuevo"
fi

if [ -z "$ENGINE" ]; then
	say "There is no container engine. rendimiento can install containerd $NERDCTL_VERSION (with nerdctl and BuildKit) for your user, without root." \
		"No hay motor de contenedores. rendimiento puede instalar containerd $NERDCTL_VERSION (con nerdctl y BuildKit) para su usuario, sin root."
	say "It needs sudo once: to put it in /usr/local, install uidmap, and, on recent Ubuntu, let rootlesskit use user namespaces." \
		"Necesita sudo una vez: para ponerlo en /usr/local, instalar uidmap y, en Ubuntu reciente, dejar que rootlesskit use espacios de nombres de usuario."
	ask "Install containerd now?" "¿Instalar containerd ahora?" || fail "install containerd (nerdctl) or Docker, then run this again" "instale containerd (nerdctl) o Docker, y corra esto de nuevo"
	need sudo || fail "sudo is needed to install containerd" "se necesita sudo para instalar containerd"
	if need apt-get; then
		sudo apt-get install -y uidmap
	elif need dnf; then
		sudo dnf install -y shadow-utils
	fi
	need newuidmap || fail "newuidmap (the uidmap package) is needed" "se necesita newuidmap (el paquete uidmap)"
	curl -fsSL -o "$tmp/nerdctl.tgz" "https://github.com/containerd/nerdctl/releases/download/v$NERDCTL_VERSION/nerdctl-full-$NERDCTL_VERSION-linux-$ARCH.tar.gz"
	sudo tar -C /usr/local -xzf "$tmp/nerdctl.tgz"
	if [ -f /proc/sys/kernel/apparmor_restrict_unprivileged_userns ] && [ "$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns)" = 1 ]; then
		sudo tee /etc/apparmor.d/usr.local.bin.rootlesskit >/dev/null <<'EOF'
abi <abi/4.0>,
include <tunables/global>
/usr/local/bin/rootlesskit flags=(unconfined) {
  userns,
  include if exists <local/usr.local.bin.rootlesskit>
}
EOF
		sudo systemctl reload apparmor
	fi
	ENGINE=nerdctl
fi

if [ "$ENGINE" = nerdctl ]; then
	# containerd and BuildKit for this user, started with the computer.
	if ! loginctl show-user "$(id -un)" 2>/dev/null | grep -q '^Linger=yes'; then
		say "sudo may ask for your password, to let your services run while you are not signed in." \
			"sudo puede pedir su contraseña, para que sus servicios corran aunque usted no haya entrado."
		sudo loginctl enable-linger "$(id -un)"
	fi
	nerdctl info >/dev/null 2>&1 || containerd-rootless-setuptool.sh install
	[ -S "$XDG_RUNTIME_DIR/buildkit/buildkitd.sock" ] || containerd-rootless-setuptool.sh install-buildkit
fi

# ---- the program ----
step "rendimiento" "rendimiento"
VERSION="${RENDIMIENTO_VERSION:-latest}"
if [ "$VERSION" = latest ]; then
	base="https://github.com/$REPO/releases/latest/download"
else
	base="https://github.com/$REPO/releases/download/$VERSION"
fi
mkdir -p "$HOME/.local/bin"
curl -fsSL -o "$tmp/rendimiento" "$base/rendimiento-linux-$ARCH" || fail "could not download rendimiento ($base)" "no se pudo descargar rendimiento ($base)"
curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || fail "could not download its checksums" "no se pudieron descargar sus sumas de verificación"
want=$(grep " rendimiento-linux-$ARCH\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
got=$(sha256sum "$tmp/rendimiento" | cut -d' ' -f1)
[ -n "$want" ] && [ "$want" = "$got" ] || fail "the download does not match its checksum; nothing was installed" "la descarga no coincide con su suma de verificación; no se instaló nada"
install -m 0755 "$tmp/rendimiento" "$HOME/.local/bin/rendimiento"
say "Installed in ~/.local/bin/rendimiento." "Instalado en ~/.local/bin/rendimiento."

# ---- who signs in first ----
GITHUB="${RENDIMIENTO_GITHUB:-}"
if [ -z "$GITHUB" ]; then
	tty_ok || fail "set RENDIMIENTO_GITHUB to your GitHub login" "ponga su usuario de GitHub en RENDIMIENTO_GITHUB"
	if [ -n "$ES" ]; then printf 'Su usuario de GitHub (el primer administrador): '; else printf 'Your GitHub login (the first administrator): '; fi
	read -r GITHUB </dev/tty
fi
case "$GITHUB" in
"" | *[!A-Za-z0-9-]*) fail "that is not a GitHub login" "ese no es un usuario de GitHub" ;;
esac

# ---- the service ----
step "Service" "Servicio"
unit="$HOME/.config/systemd/user/rendimiento.service"
mkdir -p "$(dirname "$unit")"
cat >"$unit" <<EOF
[Unit]
Description=rendimiento: deploy any app; AI agents fix what breaks
After=containerd.service buildkit.service
Wants=containerd.service buildkit.service

[Service]
Environment=MODE=local
Environment=ALLOWED_USERS=$GITHUB
Environment=PATH=$HOME/.local/bin:/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/bin:/sbin
ExecStart=$HOME/.local/bin/rendimiento
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable rendimiento.service >/dev/null
systemctl --user restart rendimiento.service

i=0
until curl -fsS -m 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || fail "rendimiento did not start; see: journalctl --user -u rendimiento" "rendimiento no arrancó; vea: journalctl --user -u rendimiento"
	sleep 2
done

step "Ready" "Listo"
say "rendimiento runs at http://localhost:8080, and your apps at http://<app>.localhost:8000." \
	"rendimiento corre en http://localhost:8080, y sus aplicaciones en http://<app>.localhost:8000."
say "The setup page asks for a token; read it with:" "La página de configuración pide una ficha; léala con:"
printf '  cat %s\n' "${XDG_CONFIG_HOME:-$HOME/.config}/rendimiento/ficha-configuracion"
say "Stop it with: systemctl --user stop rendimiento" "Deténgalo con: systemctl --user stop rendimiento"
say "Take it off the computer with: curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar" \
	"Quítelo de la computadora con: curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar"
if need xdg-open && [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
	xdg-open http://localhost:8080 >/dev/null 2>&1 || true
fi
