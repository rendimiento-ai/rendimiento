#!/bin/sh
# rendimiento's installer for one computer (Linux):
#
#   curl -fsSL https://rendimiento.ai/instalar | sh
#
# It uses the container engine the computer has (containerd with nerdctl,
# recommended, or Docker), or installs containerd when there is none; puts
# the rendimiento program in ~/.local/bin; and leaves it running as a
# service of your user that starts with the computer. Then it opens
# rendimiento's setup page, with its setup token, where you pick where your
# code lives (GitHub, GitLab or Bitbucket) and, by authorizing it, become
# the first administrator. Nobody has to be named before.
#
# To say ahead where your code lives (the setup page opens on it):
#
#   curl -fsSL https://rendimiento.ai/instalar | sh -s -- --forja gitlab --gitlab https://git.example.com
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
#   RENDIMIENTO_FORJA    where your code lives: github, gitlab or bitbucket
#                        (as --forja; asked when there is a terminal; empty:
#                        it is chosen on the setup page)
#   RENDIMIENTO_GITLAB   your GitLab's address (as --gitlab; default
#                        https://gitlab.com)
#   RENDIMIENTO_ADMINS   first administrators named ahead, for scripts, comma
#                        separated: ana (GitHub), ana@gitlab.com,
#                        ana@git.example.com, ana@bitbucket.org (ALLOWED_USERS;
#                        without it, whoever authorizes the forja on the setup
#                        page is the first)
#   RENDIMIENTO_GITHUB   the same as RENDIMIENTO_ADMINS, by its old name
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
	say "Still on this computer (the database, the images, the caches, your apps' data, your settings, and the key that opens the secrets):" \
		"Siguen en esta computadora (la base de datos, las imágenes, las cachés, los datos de sus aplicaciones, sus ajustes y la llave que abre los secretos):"
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

FORJA="${RENDIMIENTO_FORJA:-}"
GITLAB="${RENDIMIENTO_GITLAB:-}"
while [ $# -gt 0 ]; do
	case "$1" in
	--desinstalar | --uninstall)
		desinstalar
		exit 0
		;;
	--forja | --forge | --gitlab)
		[ $# -ge 2 ] || fail "$1 takes a value" "$1 lleva un valor"
		if [ "$1" = --gitlab ]; then GITLAB=$2; else FORJA=$2; fi
		shift
		;;
	--forja=* | --forge=*) FORJA=${1#*=} ;;
	--gitlab=*) GITLAB=${1#*=} ;;
	*) fail "unknown option $1 (they are --forja, --gitlab and --desinstalar)" "opción desconocida $1 (son --forja, --gitlab y --desinstalar)" ;;
	esac
	shift
done

# ---- where the code lives ----
# Only to open the setup page on it: the forja is connected there, and
# whoever authorizes it becomes the first administrator.
if [ -z "$FORJA" ] && [ -z "$GITLAB" ] && [ "${RENDIMIENTO_SI:-}" != 1 ] && tty_ok; then
	step "Where your code lives" "Dónde vive su código"
	say "rendimiento builds what you push to GitHub, GitLab or Bitbucket. The setup page connects it, and whoever authorizes it there becomes the first administrator; the others can be connected later." \
		"rendimiento compila lo que usted sube a GitHub, GitLab o Bitbucket. La página de configuración lo conecta, y quien lo autorice ahí queda como primer administrador; los demás se pueden conectar después."
	printf '  1) GitHub\n  2) GitLab\n  3) Bitbucket\n'
	say "  4) I will choose on the setup page" "  4) Lo elijo en la página de configuración"
	if [ -n "$ES" ]; then printf 'Elija [4]: '; else printf 'Choose [4]: '; fi
	read -r answer </dev/tty || answer=
	case "$answer" in
	1) FORJA=github ;;
	2) FORJA=gitlab ;;
	3) FORJA=bitbucket ;;
	esac
	if [ "$FORJA" = gitlab ]; then
		if [ -n "$ES" ]; then printf 'La dirección de su GitLab [https://gitlab.com]: '; else printf "Your GitLab's address [https://gitlab.com]: "; fi
		read -r GITLAB </dev/tty || GITLAB=
	fi
fi
FORJA=$(printf '%s' "$FORJA" | tr '[:upper:]' '[:lower:]')
[ -n "$FORJA" ] || [ -z "$GITLAB" ] || FORJA=gitlab
case "$FORJA" in
"" | github | gitlab | bitbucket) ;;
*) fail "$FORJA is not a forja: github, gitlab or bitbucket" "$FORJA no es una forja: github, gitlab o bitbucket" ;;
esac
if [ "$FORJA" = gitlab ]; then
	case "$GITLAB" in
	"") GITLAB=https://gitlab.com ;;
	http://* | https://*) ;;
	*) GITLAB="https://$GITLAB" ;;
	esac
	GITLAB=${GITLAB%/}
	case "${GITLAB#*://}" in
	"" | *[!A-Za-z0-9.:/-]*) fail "$GITLAB is not a GitLab address like https://git.example.com" "$GITLAB no es la dirección de un GitLab, como https://git.example.com" ;;
	esac
fi

# ---- who administers first, named ahead (optional) ----
# The same names as Members and ALLOWED_USERS: ana on GitHub,
# ana@gitlab.com (or a GitLab's own address), ana@bitbucket.org.
ADMINS=
set -f # a name is never a file pattern
for a in $(printf '%s' "${RENDIMIENTO_ADMINS:-${RENDIMIENTO_GITHUB:-}}" | tr ',' ' '); do
	printf '%s\n' "$a" | grep -Eqx '[A-Za-z0-9](-?[A-Za-z0-9]){0,38}|[A-Za-z0-9_][A-Za-z0-9_.-]{0,254}@[a-z0-9]([a-z0-9.-]{0,252}[a-z0-9])?(:[0-9]{1,5})?' ||
		fail "$a is not a username: ana for GitHub, ana@gitlab.com (or your GitLab's address) for GitLab, ana@bitbucket.org for Bitbucket" \
			"$a no es un nombre de usuario: ana para GitHub, ana@gitlab.com (o la dirección de su GitLab) para GitLab, ana@bitbucket.org para Bitbucket"
	ADMINS="${ADMINS:+$ADMINS,}$a"
done
set +f

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

# Services of this user run while nobody is signed in, and start with the
# computer: without it, rendimiento would stop when the terminal that
# installed it closes (a server reached over SSH).
if ! loginctl show-user "$(id -un)" 2>/dev/null | grep -q '^Linger=yes'; then
	if ! loginctl enable-linger "$(id -un)" 2>/dev/null; then
		say "sudo may ask for your password, to let your services run while you are not signed in." \
			"sudo puede pedir su contraseña, para que sus servicios corran aunque usted no haya entrado."
		if ! { need sudo && sudo loginctl enable-linger "$(id -un)"; }; then
			[ "$ENGINE" != nerdctl ] || fail "could not let your services run while you are signed out (loginctl enable-linger)" "no se pudo dejar que sus servicios corran sin que usted haya entrado (loginctl enable-linger)"
			say "Could not do it: rendimiento will stop when you sign out. Later: sudo loginctl enable-linger $(id -un)" \
				"No se pudo: rendimiento se detendrá cuando usted salga. Después: sudo loginctl enable-linger $(id -un)"
		fi
	fi
fi
# The user's systemd may take a moment to come up the first time.
i=0
until systemctl --user show-environment >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -lt 30 ] || fail "your user's systemd does not answer (systemctl --user)" "el systemd de su usuario no responde (systemctl --user)"
	sleep 1
done

if [ "$ENGINE" = nerdctl ]; then
	# containerd and BuildKit for this user, started with the computer.
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

# ---- the service ----
step "Service" "Servicio"
# The person's own settings: a file of NAME=value lines that the service
# reads at every start. It is made once, with a header that says how, and
# never written again: the unit below is rewritten on every install and
# update, so what a person changes goes here, not there.
data="${XDG_CONFIG_HOME:-$HOME/.config}/rendimiento"
settings="$data/env"
[ -d "$data" ] || mkdir -p -m 0700 "$data"
if [ ! -e "$settings" ]; then
	(
		umask 077
		cat >"$settings" <<'EOF'
# rendimiento's settings on this computer.
# One NAME=value per line, such as NOTIFY_LANG=es; lines starting with # are
# notes. They win over the service's own (MODE, PATH). Every
# variable is in the book's Settings page:
#   https://learn.rendimiento.ai/reference/config/
# After a change, restart rendimiento to read it:
#   systemctl --user restart rendimiento
# The installer never rewrites this file.
#
# Los ajustes de rendimiento en esta computadora.
# Un NOMBRE=valor por línea, como NOTIFY_LANG=es; las líneas que empiezan
# con # son notas. Ganan sobre los del servicio (MODE, PATH).
# Cada variable está en la página Ajustes del libro:
#   https://aprende.rendimiento.ai/reference/config/
# Después de un cambio, reinicie rendimiento para que lo lea:
#   systemctl --user restart rendimiento
# El instalador nunca vuelve a escribir este archivo.
EOF
	)
fi
unit="$HOME/.config/systemd/user/rendimiento.service"
mkdir -p "$(dirname "$unit")"
# systemd applies the file after every Environment= line, whatever their
# order (systemd.exec: "Settings from these files override settings made
# with Environment="), so a value in it wins over the defaults here. The
# leading - lets the service start if the file is gone. SETTINGS_FILE tells
# rendimiento where it is, for the Environment page. ALLOWED_USERS is there
# only when administrators were named ahead: it counts while rendimiento
# has no members, so leaving it out on an update changes nothing.
{
	cat <<EOF
[Unit]
Description=rendimiento: deploy any app; AI agents fix what breaks
After=containerd.service buildkit.service
Wants=containerd.service buildkit.service

[Service]
Environment=MODE=local
EOF
	[ -z "$ADMINS" ] || printf 'Environment=ALLOWED_USERS=%s\n' "$ADMINS"
	cat <<EOF
Environment=PATH=$HOME/.local/bin:/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/bin:/sbin
Environment=SETTINGS_FILE=$settings
EnvironmentFile=-$settings
ExecStart=$HOME/.local/bin/rendimiento
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
} >"$unit"
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
token_file="$data/ficha-configuracion"
page=http://localhost:8080
if curl -fsS -m 5 http://127.0.0.1:8080/api/setup/status 2>/dev/null | grep -q '"configured":true'; then
	say "It is already set up: sign in there." "Ya está configurado: entre ahí."
else
	# The setup page, with its token in the address (it is the key: it goes
	# to this computer's browser and terminal, not to a log) and on the
	# forja chosen.
	query=
	[ ! -s "$token_file" ] || query="token=$(cat "$token_file")"
	[ -z "$FORJA" ] || query="${query:+$query&}forja=$FORJA"
	[ "$FORJA" != gitlab ] || query="$query&gitlab=$GITLAB"
	[ -z "$query" ] || page="http://localhost:8080/?$query"
	say "Finish on the setup page: pick where your code lives and authorize it; you come back signed in as the first administrator." \
		"Termine en la página de configuración: elija dónde vive su código y autorícelo; regresa con su sesión iniciada como primer administrador."
	if [ -t 1 ]; then
		say "Open it (the address carries the setup token: keep it to yourself):" "Ábrala (la dirección lleva la ficha de configuración: no la comparta):"
		printf '  %s\n' "$page"
	fi
	say "Or open http://localhost:8080 and paste the setup token; read it with:" "O abra http://localhost:8080 y pegue la ficha de configuración; léala con:"
	printf '  cat %s\n' "$token_file"
	case "$FORJA" in
	gitlab)
		say "On $GITLAB, create an OAuth application (Settings → Applications) with the redirect URI http://localhost:8080/api/forjas/gitlab/callback, confidential, scopes api and read_user." \
			"En $GITLAB, cree una aplicación OAuth (Configuración → Aplicaciones) con el URI de redirección http://localhost:8080/api/forjas/gitlab/callback, confidencial, con los alcances api y read_user."
		;;
	bitbucket)
		say "On Bitbucket, add an OAuth consumer (the workspace's Settings → OAuth consumers) with the callback URL http://localhost:8080/api/forjas/bitbucket/callback, private." \
			"En Bitbucket, agregue un consumidor OAuth (Configuración del espacio de trabajo → Consumidores OAuth) con la URL de retorno http://localhost:8080/api/forjas/bitbucket/callback, privado."
		;;
	esac
	case "$FORJA" in
	gitlab | bitbucket)
		say "A plain http://localhost address works there: it is your browser that follows it. GitLab and Bitbucket cannot reach this computer with webhooks, so rendimiento asks them for new commits every minute." \
			"Una dirección http://localhost, sin https, sirve ahí: es su navegador el que la sigue. GitLab y Bitbucket no pueden llegar a esta computadora con avisos web, así que rendimiento les pregunta por confirmaciones nuevas cada minuto."
		;;
	esac
fi
say "Your settings go in $settings (restart it after a change: systemctl --user restart rendimiento)." \
	"Sus ajustes van en $settings (reinícielo después de un cambio: systemctl --user restart rendimiento)."
say "Stop it with: systemctl --user stop rendimiento" "Deténgalo con: systemctl --user stop rendimiento"
say "Take it off the computer with: curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar" \
	"Quítelo de la computadora con: curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar"
if need xdg-open && [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
	xdg-open "$page" >/dev/null 2>&1 || true
fi
