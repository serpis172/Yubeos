#!/usr/bin/env bash
# Pin every `uses: owner/repo@vN` in .github/workflows to the commit SHA of that tag (supply-chain hardening).
# Run on a machine with network access:   scripts/pin-actions.sh      (needs curl + python3; set GITHUB_TOKEN to avoid rate limits)
# Dependabot (github-actions ecosystem) then keeps the SHAs current. Idempotent: already-pinned lines are skipped.
set -euo pipefail
cd "$(dirname "$0")/.."
auth=()
[ -n "${GITHUB_TOKEN:-}" ] && auth=(-H "Authorization: Bearer $GITHUB_TOKEN")

resolve() { # owner/repo tag -> sha (dereferences annotated tags)
  local repo=$1 tag=$2 json type sha url
  json=$(curl -fsS "${auth[@]}" "https://api.github.com/repos/$repo/git/ref/tags/$tag")
  type=$(python3 -c 'import sys,json; print(json.load(sys.stdin)["object"]["type"])' <<<"$json")
  sha=$(python3 -c 'import sys,json; print(json.load(sys.stdin)["object"]["sha"])' <<<"$json")
  if [ "$type" = "tag" ]; then
    url=$(python3 -c 'import sys,json; print(json.load(sys.stdin)["object"]["url"])' <<<"$json")
    sha=$(curl -fsS "${auth[@]}" "$url" | python3 -c 'import sys,json; print(json.load(sys.stdin)["object"]["sha"])')
  fi
  echo "$sha"
}

for f in .github/workflows/*.yml; do
  grep -oE 'uses: [A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[A-Za-z0-9_./-]+)?@v[0-9][A-Za-z0-9.]*' "$f" | sort -u | while read -r _ spec; do
    path=${spec%@*}; tag=${spec#*@}; repo=$(cut -d/ -f1,2 <<<"$path")
    sha=$(resolve "$repo" "$tag")
    echo "$f: $path@$tag -> $sha"
    sed -i "s#uses: $path@$tag\$#uses: $path@$sha \# $tag#" "$f"
  done
done
