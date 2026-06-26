#!/usr/bin/env bash
# Seed a THROWAWAY dstask store with fake tasks for the demo recording.
# Never touches your real ~/.dstask. Regenerate any time: ./demo/seed.sh
set -euo pipefail

store="${DECK_DEMO_DIR:-/tmp/deck-demo}"
cards="${store}-cards"
xdg="${store}-xdg" # isolated XDG_CONFIG_HOME so the demo ignores your real ~/.config/deck
rm -rf "$store" "$cards" "$xdg"
mkdir -p "$store" "$cards" "$xdg/deck"
git init -q "$store"
export DSTASK_GIT_REPO="$store"

# demo config: just point the detail-pane cards at the demo dir (no real hooks)
cat > "$xdg/deck/config.toml" <<EOF
[cards]
dir = "$cards"
EOF

add() { dstask add "$@" >/dev/null; }
resolve() { # <summary> — add then mark resolved (lands in DONE/today)
	dstask add "$1" project:work >/dev/null
	local id
	id=$(dstask show-open | jq -r --arg s "$1" '.[] | select(.summary==$s) | .id' | head -1)
	[ -n "$id" ] && dstask "$id" done >/dev/null
}

# TODAY (+now)
add "fix flaky login test on CI" project:work +deep +now P1
add "review PR #128: dark-mode toggle [gl!128]" project:team +quick +now P2 / "https://gitlab.example.com/acme/web/-/merge_requests/128"

# NEXT (the actionable pool)
add "migrate config loader to TOML [gl#42]" project:work +deep P2 / "https://gitlab.example.com/acme/core/-/issues/42"
add "add retry + backoff to the file uploader" project:work +deep P2
add "triage incoming bug reports" project:team +quick P2
add "renew domain + TLS certificates" project:personal +quick P2
add "spike: websockets vs SSE for live updates" project:work +deep P2
add "write the getting-started guide" project:team +low P2

# WAITING (+waiting)
add "design sign-off for the new nav" project:team +waiting
add "vendor API key provisioning" project:work +waiting

# DONE (resolved today)
resolve "ship v0.4.1 hotfix"
resolve "answer the security questionnaire"
resolve "merge the i18n branch"

# detail-pane cards for the two tasks with [ref]s
cat > "$cards/gl_42.md" <<'CARD'
Migrate the config loader from env vars to a TOML file.
Status: in progress — schema drafted, parser wired up.
Done = config.toml read at startup; env still works as a fallback.
CARD
cat > "$cards/gl_128.md" <<'CARD'
Add a dark-mode toggle to the settings page.
Status: in review — 2 approvals, 1 note on the CSS-var naming.
Done = choice persists across sessions and respects prefers-color-scheme.
CARD

echo "seeded $store (+ cards in $cards)"
