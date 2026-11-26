#!/usr/bin/env bash
# Pin every `uses: owner/repo@vN` in .github/workflows to the commit SHA of that tag (supply-chain hardening).
# Run on a machine with network access:   scripts/pin-actions.sh      (needs git; no token or API calls)
# Dependabot (github-actions ecosystem) then keeps the SHAs current. Idempotent: already-pinned lines are skipped.
set -euo pipefail
cd "$(dirname "$0")/.."

resolve() { # owner/repo tag -> commit sha (git ls-remote: no API rate limit, dereferences annotated tags)
  local repo=$1 tag=$2 out
  out=$(git ls-remote "https://github.com/$repo.git" "refs/tags/$tag" "refs/tags/$tag^{}")
  # the peeled line (^{}) is the commit for annotated tags; lightweight tags only have the plain line
  awk -v t="refs/tags/$tag" '$2==t"^{}"{p=$1} $2==t{l=$1} END{print (p!=""?p:l)}' <<<"$out"
}

for f in .github/workflows/*.yml; do
  grep -oE 'uses: [A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[A-Za-z0-9_./-]+)?@v[0-9][A-Za-z0-9.]*' "$f" | sort -u | while read -r _ spec; do
    path=${spec%@*}; tag=${spec#*@}; repo=$(cut -d/ -f1,2 <<<"$path")
    sha=$(resolve "$repo" "$tag")
    [[ $sha =~ ^[0-9a-f]{40}$ ]] || { echo "ERROR: could not resolve $path@$tag (tag not found or no network)" >&2; exit 1; }
    echo "$f: $path@$tag -> $sha"
    sed -i "s#uses: $path@$tag\$#uses: $path@$sha \# $tag#" "$f"
  done
done
