#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <canary|nightly|release> <tag> <40-character-sha>" >&2
  exit 2
fi

channel=$1
tag=$2
sha=$3

case "$channel" in
  canary|nightly|release) ;;
  *)
    echo "unsupported documentation channel: $channel" >&2
    exit 2
    ;;
esac

if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo "invalid Wago commit SHA: $sha" >&2
  exit 2
fi

if [[ -z "${DOCS_SYNC_TOKEN:-}" ]]; then
  echo "::warning::DOCS_SYNC_TOKEN is unavailable; the docs repository's scheduled reconciler will recover this release"
  exit 0
fi

jq -n \
  --arg channel "$channel" \
  --arg tag "$tag" \
  --arg sha "$sha" \
  '{event_type: "code-release", client_payload: {channel: $channel, tag: $tag, sha: $sha}}' |
  GH_TOKEN="$DOCS_SYNC_TOKEN" gh api \
    --method POST \
    repos/wago-org/docs/dispatches \
    --input -

echo "Dispatched $channel documentation synchronization for $tag"
