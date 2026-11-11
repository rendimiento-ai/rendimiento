#!/usr/bin/env bash
# The quickstart on a clean machine, timed: runs the installer of this
# checkout (instalar/instalar.sh) as a newcomer would, waits for rendimiento
# to answer, and takes it off again. It runs on GitHub's machines
# (.github/workflows/inicio-rapido.yml), which are new amd64 Ubuntu each
# time; never on a computer you keep, since it installs and uninstalls.
#
#   MOTOR=containerd|docker   the engine the machine starts with
#                             (containerd: none, the installer brings it)
#   LIMITE=300                seconds the install may take
set -euo pipefail

MOTOR="${MOTOR:-containerd}"
LIMITE="${LIMITE:-300}"
[ -n "${GITHUB_ACTIONS:-}" ] || { echo "this installs and uninstalls rendimiento: it only runs in GitHub Actions" >&2; exit 1; }

# aviso level title message: an annotation on the run, readable without its log.
aviso() { printf '::%s title=%s::%s\n' "$1" "$2" "$(printf '%s' "$3" | sed -e 's/%/%25/g' | sed -e ':a;N;$!ba;s/\n/%0A/g')"; }
falla() {
	aviso error "inicio rápido ($MOTOR)" "$1
$( (journalctl --user -u rendimiento -n 25 --no-pager 2>/dev/null || true) | cut -c1-300)"
	exit 1
}

if [ "$MOTOR" = containerd ]; then
	# A machine with no container engine: GitHub's come with Docker.
	sudo systemctl disable --now docker.socket docker.service containerd.service >/dev/null 2>&1 || true
	for b in docker nerdctl; do
		while p=$(command -v "$b"); do sudo mv "$p" "$p.fuera"; done
	done
fi

registro=$(mktemp)
t0=$(date +%s)
RENDIMIENTO_SI=1 RENDIMIENTO_GITHUB=octocat sh instalar/instalar.sh 2>&1 |
	while IFS= read -r linea; do
		printf '%4ds %s\n' $(($(date +%s) - t0)) "$linea"
	done | tee "$registro" || falla "the installer failed:
$(tail -n 15 "$registro" | cut -c1-300)"
total=$(($(date +%s) - t0))

export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
curl -fsS -m 5 http://127.0.0.1:8080/healthz >/dev/null || falla "rendimiento does not answer after the installer said it was ready"
curl -fsS -m 5 http://127.0.0.1:8080/ | grep -qi '<html' || falla "the UI does not answer on :8080"
[ -s "${XDG_CONFIG_HOME:-$HOME/.config}/rendimiento/ficha-configuracion" ] || falla "the setup token the installer points at is not there"
systemctl --user is-enabled rendimiento.service >/dev/null || falla "the service is not enabled: it would not start with the computer"
loginctl show-user "$(id -un)" | grep -q '^Linger=yes' || falla "lingering is off: rendimiento would stop when the person signs out"

# When each step of the installer began ("==> step"), for the book's page.
pasos=$(grep -E '^ *[0-9]+s ==> ' "$registro" | sed -E 's/^ *([0-9]+)s ==> (.*)$/\2 at \1s/' | paste -sd ';' - | sed 's/;/; /g')
aviso notice "inicio rápido ($MOTOR)" "installed and answering in ${total}s. $pasos"
[ "$total" -le "$LIMITE" ] || falla "the install took ${total}s, more than ${LIMITE}s"

RENDIMIENTO_SI=1 RENDIMIENTO_BORRAR_DATOS=1 sh instalar/instalar.sh --desinstalar || falla "the uninstaller failed"
! curl -fsS -m 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1 || falla "rendimiento still answers after uninstalling"
[ ! -e "$HOME/.local/bin/rendimiento" ] || falla "the program is still there after uninstalling"
