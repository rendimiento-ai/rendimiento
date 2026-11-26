#!/usr/bin/env bash
# The Go side of `make test`, in a golang image: what rendimiento runs on
# every push to its own repository (rendimiento.yaml, builds: platform) and
# what hack/test-remote.sh runs in its pod; then the Windows app's module
# (escritorio/). The UI is checked by the image's build (npm run typecheck).
#
# It needs TEST_DATABASE_URL (or DATABASE_URL), an empty Postgres database,
# and keeps its tools under $TOOLS (default /cache/bin, the test's cache).
#
# CI_PARTE splits it into steps that run at once, each on its own node with
# its own Postgres (rendimiento.yaml): "api" and "plataforma" are those two
# packages alone, the longest ones; "resto" is everything else, with the
# checks (generate, vet) and the Windows app. Unset (make test-remote), it
# runs all of it here.
set -euo pipefail
parte=${CI_PARTE:-}
case "$parte" in
  ""|resto|api|plataforma) ;;
  *) echo "CI_PARTE must be resto, api or plataforma (or unset for all), got $parte"; exit 2 ;;
esac

CONTROLLER_GEN=${CONTROLLER_GEN:-v0.22.0}
SETUP_ENVTEST=${SETUP_ENVTEST:-v0.25.1}
ENVTEST_K8S=${ENVTEST_K8S:-1.37.0}
TOOLS=${TOOLS:-/cache/bin}
export TEST_DATABASE_URL=${TEST_DATABASE_URL:-${DATABASE_URL:-}}
# A throwaway database need not wait for its disk at each commit: the
# tests commit thousands of times (the migrations alone), and on a node
# busy with other steps each wait was long. pgx sends the setting when it
# connects.
case "$TEST_DATABASE_URL" in
  *synchronous_commit=*) ;;
  postgres://*\?*|postgresql://*\?*) TEST_DATABASE_URL="$TEST_DATABASE_URL&synchronous_commit=off" ;;
  postgres://*|postgresql://*) TEST_DATABASE_URL="$TEST_DATABASE_URL?synchronous_commit=off" ;;
esac
export GOBIN=$TOOLS PATH=$TOOLS:$PATH

cd "$(dirname "$0")/.."
git config --global --add safe.directory "$PWD" 2>/dev/null || true

# Compiling with an empty cache prints nothing for many minutes: each
# section says when it starts, and a line every minute says it is alive.
start=$(date +%s)
section() { echo "== $1 ($(($(date +%s) - start))s)"; }
(while sleep 60; do echo "   … still working, $(($(date +%s) - start))s"; done) &
heartbeat=$!
trap 'kill $heartbeat 2>/dev/null || true' EXIT

# The kept cache is a volume of its own (10Gi): Go's build cache only
# drops what went unused for days, so busy days fill it, and a full volume
# fails every step ("no space left on device"). Past 85%, the build cache
# is emptied; the modules and the tools stay, so the next compile only
# takes longer.
if [ -d /cache ] && used=$(df -P /cache 2>/dev/null | awk 'NR==2 {print $5+0}') && [ "${used:-0}" -ge 85 ]; then
  echo "== the cache is ${used}% full: emptying Go's build cache"
  go clean -cache || rm -rf "$(go env GOCACHE)"
fi

section tools
if { [ -z "$parte" ] || [ "$parte" = resto ]; } && ! controller-gen --version 2>/dev/null | grep -q "$CONTROLLER_GEN"; then
  echo "   installing controller-gen $CONTROLLER_GEN (the cache is empty or old)"
  go install "sigs.k8s.io/controller-tools/cmd/controller-gen@$CONTROLLER_GEN"
fi
if [ ! -x "$TOOLS/setup-envtest" ]; then
  echo "   installing setup-envtest $SETUP_ENVTEST"
  go install "sigs.k8s.io/controller-runtime/tools/setup-envtest@$SETUP_ENVTEST"
fi
# The built UI is not in git; vet and tests only need something to embed.
[ -n "$(ls -A web/dist 2>/dev/null)" ] || { mkdir -p web/dist && echo placeholder > web/dist/index.html; }

# The checks are the whole tree's: they run once, in the step with the rest.
if [ -z "$parte" ] || [ "$parte" = resto ]; then
  section generate
  controller-gen object paths=./api/... paths=./internal/spec/...
  controller-gen crd paths=./api/... output:crd:dir=deploy/crds
  cp deploy/crds/*.yaml chart/rendimiento/crds/   # the Helm chart ships the same CRDs
  if [ -n "${CHECK_GENERATED:-}" ] && ! git diff --quiet -- api internal/spec deploy/crds chart/rendimiento/crds; then
    git diff --stat -- api internal/spec deploy/crds chart/rendimiento/crds
    echo "generated files are out of date: run make generate (or make test-remote) and commit them"
    exit 1
  fi
  section 'vet (compiles everything: the longest part with an empty cache)'
  go vet ./...
fi
section test
KUBEBUILDER_ASSETS=$(setup-envtest use "$ENVTEST_K8S" -p path --bin-dir "$(dirname "$TOOLS")/envtest")
export KUBEBUILDER_ASSETS
# --8<-- [start:pruebas]
# store, platform and api share one test database, so in one step they run
# one at a time; the rest run beside them, two packages at a time (with the
# memory the test step has). Split into steps, api and platform have a
# database each, on another node.
case "$parte" in
  api) shared='/internal/api$' rest='^$' ;;
  plataforma) shared='/internal/platform$' rest='^$' ;;
  resto) shared='/internal/store$' rest='/internal/(api|platform|store)$' ;;
  *) shared='/internal/(api|platform|store)$' rest='/internal/(api|platform|store)$' ;;
esac
# internal/controller's envtest (a test API server and its etcd) runs after
# them, alone, with the step's cores to itself. Beside the store's tests and
# their Postgres, another envtest or a compile, its etcd now and then took
# longer than its 15 seconds to apply a write ("etcdserver: request timed
# out": runs 1073, 1190, 1209 and 1896), even without fsync; at its start,
# one such write failed every test of the package, and a retry passed.
alone='/internal/controller$'
out=$(mktemp -d)
go test -v -p 1 $(go list ./... | grep -E "$shared") >"$out/shared.log" 2>&1 &
a=$!
others=$(go list ./... | grep -vE "$rest" | grep -vE "$shared" | grep -vE "$alone" || true)
if [ -n "$parte" ] && [ "$parte" != resto ]; then others=""; fi
if [ -n "$others" ]; then
  go test -v -p 2 $others >"$out/rest.log" 2>&1 &
  b=$!
else
  : >"$out/rest.log"
  b=""
fi
# --8<-- [end:pruebas]
# Each package as it finishes, so a long run shows where it is (tail ends
# with this script, and the rest of the pipe with it).
: >"$out/envtest.log"
tail -n +1 -F --pid=$$ "$out/shared.log" "$out/rest.log" "$out/envtest.log" 2>/dev/null | grep --line-buffered -E '^(ok|FAIL)[[:space:]]' | sed -u 's/^/   finished: /' &
failed=()
wait $a || failed+=("$out/shared.log")
[ -z "$b" ] || wait $b || failed+=("$out/rest.log")
envtest=$(go list ./... | grep -E "$alone" || true)
if { [ -z "$parte" ] || [ "$parte" = resto ]; } && [ -n "$envtest" ]; then
  section 'envtest (internal/controller, alone)'
  go test -v $envtest >"$out/envtest.log" 2>&1 || failed+=("$out/envtest.log")
fi
sleep 2 # the last packages' lines, before the summary

# rendimiento for Windows (escritorio/) is a module of its own, so the
# ./... above never see it. It goes after the tests, not beside them: a
# compile next to the envtest makes its etcd time out. Its log joins the
# tests', so the summary lists its packages and, when it fails, ends with
# the step that stopped it.
if [ -z "$parte" ] || [ "$parte" = resto ]; then
  section 'escritorio (the Windows app: on Linux, then for windows/amd64)'
  (
    step() { echo "   $1" >&3; echo "== escritorio: $1"; }
    cd escritorio
    step gofmt
    unformatted=$(gofmt -l .)
    [ -z "$unformatted" ] || { echo "not formatted (gofmt -w fixes them):"; echo "$unformatted"; exit 1; }
    # On Linux the files that open the window are left out (//go:build
    # windows); the wsl package's tests run with a fake wsl.exe.
    step 'vet and test (linux)'
    go vet ./...
    tests=0
    go test -v ./... || tests=$?
    grep -E '^(ok|FAIL)[[:space:]]' "$out/escritorio.log" | sed 's/^/   finished: /' >&3 || true
    [ "$tests" -eq 0 ]
    # What .github/workflows/binarios.yml publishes with each release, built
    # the same way: Wails' tags, no console window. go build -o, never go
    # install: with GOBIN set (above) it refuses to cross-compile.
    step 'vet and build (windows/amd64)'
    export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
    go vet -tags desktop,production ./...
    go build -tags desktop,production -trimpath -ldflags '-s -w -H windowsgui' -o "$out/rendimiento-windows-amd64.exe" .
    echo "   rendimiento-windows-amd64.exe: $(($(stat -c %s "$out/rendimiento-windows-amd64.exe") / 1024)) KiB" >&3
  ) 3>&1 >"$out/escritorio.log" 2>&1 &
  c=$!
  wait $c || { failed+=("$out/escritorio.log"); echo '   failed: the summary ends with its output'; }
fi
section summary

# What `go test` prints without -v: one line per package.
grep -hE '^(ok|FAIL)[[:space:]]' "$out"/*.log | sort -k2
for log in "${failed[@]}"; do
  # Each failed test with its output, then the end of the log (build
  # errors, panics and timeouts are there).
  echo "== failures in $(basename "$log" .log) packages"
  awk '/^=== RUN/ && index($3, "/") == 0 { buf = "" } { buf = buf $0 "\n" } /^--- FAIL/ { printf "%s", buf; buf = "" }' "$log"
  tail -40 "$log"
done
echo '== slowest tests'
# awk, not head: head stops reading early, sort then dies of SIGPIPE, and
# with pipefail the whole step failed (exit 141) after every test passed.
grep -hE '^--- (PASS|FAIL): ' "$out"/*.log | sed -E 's/^--- [A-Z]+: ([^ ]+) \(([0-9.]+)s\)$/\2s \1/' | sort -rn | awk 'NR <= 12'
[ ${#failed[@]} -eq 0 ]
