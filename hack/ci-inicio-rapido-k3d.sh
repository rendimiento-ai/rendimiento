#!/usr/bin/env bash
# The quickstart on a cluster on one computer, timed: makes a k3d cluster
# with its registry as the book's page says, installs this checkout's chart
# with the image built from it, deploys the example (rendimiento-ai/hola)
# until http://hola.localhost:8080 answers, and deletes the cluster. It runs
# on GitHub's machines (.github/workflows/inicio-rapido-k3d.yml).
#
# What a person does on GitHub's pages a stand-in answers
# (hack/github-de-prueba.py), and its time is not counted: only the
# install and the deploy are.
#
#   IMAGEN=rendimiento:prueba     the image built from this checkout,
#                                 already in this machine's Docker
#   LIMITE_TOTAL=600              seconds the install and the deploy may take
#   EJEMPLO=rendimiento-ai/hola   the repository to deploy
set -euo pipefail

IMAGEN="${IMAGEN:-rendimiento:prueba}"
LIMITE_TOTAL="${LIMITE_TOTAL:-600}"
EJEMPLO="${EJEMPLO:-rendimiento-ai/hola}"
[ -n "${GITHUB_ACTIONS:-}" ] || { echo "this makes and deletes a k3d cluster named rendimiento: it only runs in GitHub Actions" >&2; exit 1; }

ns=rendimiento-system
api=http://rendimiento.localhost:8080
app=${EJEMPLO##*/}
tmp=$(mktemp -d)
# The names are this computer's own (.localhost), whatever its resolver says.
c() { curl --resolve "rendimiento.localhost:8080:127.0.0.1" --resolve "$app.localhost:8080:127.0.0.1" "$@"; }

# aviso level title message: an annotation on the run, readable without its log.
aviso() { printf '::%s title=%s::%s\n' "$1" "$2" "$(printf '%s' "$3" | sed -e 's/%/%25/g' | sed -e ':a;N;$!ba;s/\n/%0A/g')"; }
# detalle: what the cluster was doing, for the log of a failure.
detalle() {
	echo "---- pods"
	kubectl get pods -A -o wide 2>&1 | cut -c1-200 || true
	echo "---- ingresses"
	kubectl get ingress -A 2>&1 | cut -c1-200 || true
	echo "---- events"
	kubectl get events -A --sort-by=.lastTimestamp 2>&1 | tail -n 30 | cut -c1-250 || true
	for d in rendimiento rendimiento-control rendimiento-worker rendimiento-buildkitd; do
		echo "---- $d"
		kubectl -n "$ns" logs "deploy/$d" --tail 40 2>&1 | cut -c1-300 || true
	done
	echo "---- the stand-in for GitHub"
	tail -n 12 "$tmp/github.log" 2>/dev/null | cut -c1-200 || true
	echo "---- the run"
	c -fsS -m 5 -b "$tmp/galletas" "$api/api/apps/$app/runs" 2>/dev/null | jq -c '.[0]' 2>/dev/null | cut -c1-1500 || true
}
# pasoFallido: the end of the log of each step that failed in the example's
# last run, which says why (a build that fails names its cause only there).
pasoFallido() {
	run=$(c -fsS -m 5 -b "$tmp/galletas" "$api/api/apps/$app/runs" 2>/dev/null | jq -r '.[0].id // empty' 2>/dev/null || true)
	[ -n "$run" ] || return 0
	for paso in $(c -fsS -m 5 -b "$tmp/galletas" "$api/api/runs/$run" 2>/dev/null | jq -r '.steps[]? | select(.status == "failed") | .id' 2>/dev/null || true); do
		echo "-- step $paso"
		c -fsS -m 10 -b "$tmp/galletas" "$api/api/runs/$run/steps/$paso/log" 2>/dev/null | tail -n 25 | cut -c1-300 || true
	done
}
falla() {
	detalle
	echo "---- the failed steps"
	pasoFallido
	aviso error "inicio rápido (k3d)" "$1
the run: $(c -fsS -m 5 -b "$tmp/galletas" "$api/api/apps/$app/runs" 2>/dev/null | jq -c '.[0] | {id, status, message}' 2>/dev/null || true)
$(pasoFallido)
$(kubectl get pods -A --no-headers 2>/dev/null | grep -v -e Running -e Completed | cut -c1-150 | head -n 8 || true)
$(for d in rendimiento rendimiento-worker; do
	echo "-- $d"
	kubectl -n "$ns" logs "deploy/$d" --tail 6 2>&1 | cut -c1-400 || true
	kubectl -n "$ns" logs "deploy/$d" --previous --tail 4 2>/dev/null | cut -c1-400 || true
done)
-- the stand-in for GitHub
$(tail -n 4 "$tmp/github.log" 2>/dev/null | cut -c1-200 || true)"
	exit 1
}

# The stand-in listens where the cluster's pods reach this machine: the
# gateway of the cluster's Docker network.
REPO="$EJEMPLO" LOGIN=octocat python3 hack/github-de-prueba.py 0.0.0.0:8099 2>"$tmp/github.log" &
trap 'kill %1 2>/dev/null || true' EXIT

# ---- the page's commands: the cluster, then rendimiento ----
t0=$(date +%s)
k3d cluster create rendimiento -p "8080:80@loadbalancer" --registry-create rendimiento-registro || falla "k3d could not make the cluster"
cluster=$(($(date +%s) - t0))
maquina=$(docker network inspect k3d-rendimiento -f '{{range .IPAM.Config}}{{.Gateway}} {{end}}' | awk '{print $1}')
[ -n "$maquina" ] || falla "the cluster's Docker network has no gateway to reach this machine at"
# Not in the page: the image of this commit, in place of the published one.
k3d image import "$IMAGEN" -c rendimiento || falla "the image could not be put in the cluster"
importar=$(($(date +%s) - t0 - cluster))

t1=$(date +%s)
helm install rendimiento ./chart/rendimiento -n "$ns" --create-namespace \
	--set baseURL="$api" \
	--set firstAdmins=octocat \
	--set registry.host=rendimiento-registro:5000 \
	--set ingress.className=traefik \
	--set image.repository="${IMAGEN%:*}" --set image.tag="${IMAGEN##*:}" \
	--set config.GITHUB_API_URL="http://$maquina:8099" \
	--set config.GITHUB_URL="http://$maquina:8099" >"$tmp/helm.log" 2>&1 || falla "helm could not install the chart:
$(tail -n 12 "$tmp/helm.log" | cut -c1-400)"
cat "$tmp/helm.log"
i=0
until c -fsS -m 3 "$api/healthz" >/dev/null 2>&1 && c -fsS -m 3 "$api/" 2>/dev/null | grep -qi '<html'; do
	i=$((i + 1))
	[ "$i" -lt 240 ] || falla "rendimiento does not answer at $api $(($(date +%s) - t1))s after helm install"
	sleep 2
done
instalar=$(($(date +%s) - t1))
total=$((cluster + instalar))
aviso notice "inicio rápido (k3d)" "the cluster took ${cluster}s and rendimiento answered ${instalar}s after helm install: ${total}s. (Putting this commit's image in the cluster, which the page does not do, took ${importar}s more.)"

# ---- from installed to an app with an address ----
# The setup page's return from GitHub, and the sign-in's.
ficha=$(kubectl -n "$ns" get secret rendimiento-setup -o jsonpath='{.data.token}' | base64 -d)
[ -n "$ficha" ] || falla "the setup token the chart's notes point at is not there"
codigo=$(c -sS -m 30 -o "$tmp/respuesta" -w '%{http_code}' -G --data-urlencode "token=$ficha" --data-urlencode code=prueba "$api/api/setup/github/callback")
[ "$codigo" = 302 ] || falla "the setup did not take the GitHub App (HTTP $codigo): $(cut -c1-300 "$tmp/respuesta")"
c -sS -m 10 -o /dev/null -c "$tmp/galletas" "$api/api/auth/login"
estado=$(awk '$6 == "oauth_state" { print $7 }' "$tmp/galletas")
codigo=$(c -sS -m 30 -o /dev/null -w '%{http_code}' -b "$tmp/galletas" -c "$tmp/galletas" -G --data-urlencode "state=$estado" --data-urlencode code=prueba "$api/api/auth/callback")
[ "$codigo" = 302 ] || falla "the sign-in failed (HTTP $codigo)"

# New app: choose the repository, then Deploy.
json() { c -sS -m 60 -b "$tmp/galletas" -H 'Content-Type: application/json' -o "$tmp/respuesta" -w '%{http_code}' -d "$2" "$api$1"; }
codigo=$(json /api/propose "$(jq -n --arg repo "$EJEMPLO" '{installation: 1, repo: $repo, branch: "main"}')")
[ "$codigo" = 200 ] || falla "rendimiento could not read $EJEMPLO (HTTP $codigo): $(cut -c1-300 "$tmp/respuesta")"
[ "$(jq -r .existing "$tmp/respuesta")" = true ] || falla "$EJEMPLO has no rendimiento.yaml that rendimiento can use"
pedido=$(jq --arg app "$app" '{installation: 1, repo, defaultBranch, name: $app, spec, files: {}, existing: true, adopt: false}' "$tmp/respuesta")
t2=$(date +%s)
codigo=$(json /api/apps "$pedido")
[ "$codigo" = 201 ] || falla "the app was not created (HTTP $codigo): $(cut -c1-300 "$tmp/respuesta")"
until c -fsS -m 3 "http://$app.localhost:8080/" 2>/dev/null | grep -qi '<html'; do
	despliegue=$(($(date +%s) - t2))
	[ "$(c -fsS -m 5 -b "$tmp/galletas" "$api/api/apps/$app/runs" 2>/dev/null | jq -r '.[0].status' 2>/dev/null)" != failed ] || falla "the example's run failed after ${despliegue}s"
	[ $((total + despliegue)) -le $((LIMITE_TOTAL + 300)) ] || falla "no address after ${despliegue}s of deploy (the cluster and rendimiento took ${total}s)"
	sleep 2
done
despliegue=$(($(date +%s) - t2))
# The address the page promises is the one rendimiento shows.
direccion=
for _ in $(seq 30); do
	direccion=$(c -fsS -m 5 -b "$tmp/galletas" "$api/api/apps/$app" 2>/dev/null | jq -r '[.status.services[]?.url // empty] | first // empty' 2>/dev/null || true)
	[ -z "$direccion" ] || break
	sleep 2
done
aviso notice "inicio rápido (k3d)" "$EJEMPLO answers at http://$app.localhost:8080 ${despliegue}s after Deploy (rendimiento shows: ${direccion:-no address}); with the cluster and the install, $((total + despliegue))s. The time on GitHub's pages is not counted."
[ "$direccion" = "http://$app.localhost:8080" ] || falla "rendimiento shows '${direccion}' as the app's address, not http://$app.localhost:8080"
[ $((total + despliegue)) -le "$LIMITE_TOTAL" ] || falla "the cluster, the install and the deploy took $((total + despliegue))s, more than ${LIMITE_TOTAL}s"

k3d cluster delete rendimiento || falla "k3d could not delete the cluster"
! c -fsS -m 2 "$api/healthz" >/dev/null 2>&1 || falla "rendimiento still answers after deleting the cluster"
