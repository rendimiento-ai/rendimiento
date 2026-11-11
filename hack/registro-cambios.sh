#!/bin/sh
# The changelog for a release: hack/registro-cambios.sh <tag> [date]
#
# Each change says what it is for whoever runs rendimiento in a file of its
# own, cambios/<branch>.md (one or more "- **What:** ..." lines), so two
# branches never write the same line of CHANGELOG.md. A release gathers them:
# under a new "## <tag> (<date>)" go the notes, the newest first, and then
# whatever was written under "## Unreleased" by hand; the notes are removed.
# The binarios workflow runs it after each release.
set -eu

tag=${1:?usage: hack/registro-cambios.sh <tag> [date]}
date=${2:-$(date -u +%F)}
cd "$(dirname "$0")/.."

if grep -q "^## $tag " CHANGELOG.md; then
  echo "already there"
  exit 0
fi
if ! grep -q '^## Unreleased' CHANGELOG.md; then
  echo "no Unreleased section"
  exit 0
fi

# The notes, the newest first (by when each was last committed; by name
# where there is no history to tell).
notas=$(mktemp)
orden=$(mktemp)
trap 'rm -f "$notas" "$orden"' EXIT
for f in cambios/*.md; do
  [ -f "$f" ] || continue
  t=$(git log -1 --format=%ct -- "$f" 2>/dev/null || true)
  printf '%s\t%s\n' "${t:-0}" "$f" >>"$orden"
done
sort -t "$(printf '\t')" -k1,1nr -k2,2 "$orden" | cut -f2 | while read -r f; do
  grep -v '^[[:space:]]*$' "$f" >>"$notas" || true
done

awk -v t="$tag" -v d="$date" -v notas="$notas" '
  !done && /^## Unreleased/ {
    print; print ""; print "## " t " (" d ")"; print ""
    while ((getline l < notas) > 0) print l
    done = 1; salta = 1; next
  }
  # The blank line that followed "## Unreleased" is already written.
  salta && /^[[:space:]]*$/ { salta = 0; next }
  { salta = 0; print }
' CHANGELOG.md >CHANGELOG.new
mv CHANGELOG.new CHANGELOG.md

for f in cambios/*.md; do
  [ -f "$f" ] || continue
  git rm -q "$f" 2>/dev/null || rm -f "$f"
done
echo "CHANGELOG.md: $tag ($date)"
