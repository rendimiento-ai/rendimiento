#!/usr/bin/env bash
# The quickstart on a clean machine, timed: runs the installer of this
# checkout (instalar/instalar.sh) as a newcomer would, waits for rendimiento
# to answer, deploys the example (rendimiento-ai/hola) until its address
# answers, and takes everything off again. It runs on GitHub's machines
# (.github/workflows/inicio-rapido.yml), which are new amd64 Ubuntu each
# time; never on a computer you keep, since it installs and uninstalls.
#
# What a person does on GitHub's pages (creating the GitHub App, giving it
# the repository, signing in) a stand-in answers (hack/github-de-prueba.py),
# and its time is not counted: only the installer and the deploy are.
#
#   MOTOR=containerd|docker   the engine the machine starts with
#                             (containerd: none, the installer brings it)
#   LIMITE=300                seconds the install may take
#   LIMITE_TOTAL=600          seconds the install and the deploy may take
#   BINARIO=                  a rendimiento built from this checkout, to
#                             test instead of the release the installer
#                             downloads
#   EJEMPLO=rendimiento-ai/hola   the repository to deploy
set -euo pipefail

MOTOR="${MOTOR:-containerd}"
LIMITE="${LIMITE:-300}"
LIMITE_TOTAL="${LIMITE_TOTAL:-600}"
EJEMPLO="${EJEMPLO:-rendimiento-ai/hola}"
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

# ---- from installed to an app with an address ----
api=http://127.0.0.1:8080
falso=127.0.0.1:8099
app=${EJEMPLO##*/}
tmp=$(mktemp -d)
REPO="$EJEMPLO" LOGIN=octocat python3 hack/github-de-prueba.py "$falso" 2>"$tmp/github.log" &
trap 'kill %1 2>/dev/null || true' EXIT
# detalle: the stand-in's last calls and the run's state, for a failure.
detalle() {
	echo "the stand-in for GitHub:"
	tail -n 8 "$tmp/github.log" 2>/dev/null | cut -c1-200
	echo "the run: $(curl -fsS -m 5 -b "$tmp/galletas" "$api/api/apps/$app/runs" 2>/dev/null | jq -c '.[0] | {id, status, message}' 2>/dev/null || true)"
}

[ -z "${BINARIO:-}" ] || install -m 0755 "$BINARIO" "$HOME/.local/bin/rendimiento"
mkdir -p "$HOME/.config/systemd/user/rendimiento.service.d"
printf '[Service]\nEnvironment=GITHUB_API_URL=http://%s\nEnvironment=GITHUB_URL=http://%s\n' "$falso" "$falso" \
	>"$HOME/.config/systemd/user/rendimiento.service.d/prueba.conf"
systemctl --user daemon-reload
systemctl --user restart rendimiento.service
i=0
until curl -fsS -m 2 "$api/healthz" >/dev/null 2>&1 && curl -fsS -m 2 "http://$falso/app" >/dev/null 2>&1; do
	i=$((i + 1))
	[ "$i" -lt 60 ] || falla "rendimiento did not come back with the stand-in for GitHub"
	sleep 1
done

# The setup page's return from GitHub, and the sign-in's.
ficha=$(cat "${XDG_CONFIG_HOME:-$HOME/.config}/rendimiento/ficha-configuracion")
codigo=$(curl -sS -m 30 -o /dev/null -w '%{http_code}' -G --data-urlencode "token=$ficha" --data-urlencode code=prueba "$api/api/setup/github/callback")
[ "$codigo" = 302 ] || falla "the setup did not take the GitHub App (HTTP $codigo)
$(detalle)"
curl -sS -m 10 -o /dev/null -c "$tmp/galletas" "$api/api/auth/login"
estado=$(awk '$6 == "oauth_state" { print $7 }' "$tmp/galletas")
codigo=$(curl -sS -m 30 -o /dev/null -w '%{http_code}' -b "$tmp/galletas" -c "$tmp/galletas" -G --data-urlencode "state=$estado" --data-urlencode code=prueba "$api/api/auth/callback")
[ "$codigo" = 302 ] || falla "the sign-in failed (HTTP $codigo)
$(detalle)"

# New app: choose the repository, then Deploy.
json() { curl -sS -m 60 -b "$tmp/galletas" -H 'Content-Type: application/json' -o "$tmp/respuesta" -w '%{http_code}' -d "$2" "$api$1"; }
codigo=$(json /api/propose "$(jq -n --arg repo "$EJEMPLO" '{installation: 1, repo: $repo, branch: "main"}')")
[ "$codigo" = 200 ] || falla "rendimiento could not read $EJEMPLO (HTTP $codigo): $(cut -c1-300 "$tmp/respuesta")
$(detalle)"
[ "$(jq -r .existing "$tmp/respuesta")" = true ] || falla "$EJEMPLO has no rendimiento.yaml that rendimiento can use"
pedido=$(jq --arg app "$app" '{installation: 1, repo, defaultBranch, name: $app, spec, files: {}, existing: true, adopt: false}' "$tmp/respuesta")
t1=$(date +%s)
codigo=$(json /api/apps "$pedido")
[ "$codigo" = 201 ] || falla "the app was not created (HTTP $codigo): $(cut -c1-300 "$tmp/respuesta")
$(detalle)"
until curl -fsS -m 3 --resolve "$app.localhost:8000:127.0.0.1" "http://$app.localhost:8000/" 2>/dev/null | grep -qi '<html'; do
	despliegue=$(($(date +%s) - t1))
	[ "$(curl -fsS -m 5 -b "$tmp/galletas" "$api/api/apps/$app/runs" 2>/dev/null | jq -r '.[0].status' 2>/dev/null)" != failed ] || falla "the example's run failed after ${despliegue}s
$(detalle)"
	[ $((total + despliegue)) -le $((LIMITE_TOTAL + 120)) ] || falla "no address after ${despliegue}s of deploy (the install took ${total}s)
$(detalle)"
	sleep 2
done
despliegue=$(($(date +%s) - t1))
aviso notice "inicio rápido ($MOTOR)" "$EJEMPLO answers at http://$app.localhost:8000 ${despliegue}s after Deploy; with the install, $((total + despliegue))s. The time on GitHub's pages is not counted."
[ $((total + despliegue)) -le "$LIMITE_TOTAL" ] || falla "the install and the deploy took $((total + despliegue))s, more than ${LIMITE_TOTAL}s"

RENDIMIENTO_SI=1 RENDIMIENTO_BORRAR_DATOS=1 sh instalar/instalar.sh --desinstalar || falla "the uninstaller failed"
! curl -fsS -m 2 --resolve "$app.localhost:8000:127.0.0.1" "http://$app.localhost:8000/" >/dev/null 2>&1 || falla "the example still answers after uninstalling"
! curl -fsS -m 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1 || falla "rendimiento still answers after uninstalling"
[ ! -e "$HOME/.local/bin/rendimiento" ] || falla "the program is still there after uninstalling"
