#!/usr/bin/env bash
# FAKE agent for the demo (summary + a real, visible action). Reads the selected task
# from the THROWAWAY demo store, prints a summary, and — on confirm — actually resolves
# it via dstask so the board visibly updates on return. No LLM/network; only ever
# touches the demo store ($DSTASK_GIT_REPO, which the demo points at /tmp).
id=${1:-?}; shift || true; instr="${*:-summarise and close}"
d=$'\e[38;5;245m'; h=$'\e[38;5;183m'; ok=$'\e[38;5;120m'; r=$'\e[0m'

sm=$(dstask show-open 2>/dev/null | jq -r --argjson i "$id" '.[] | select(.id==$i) | .summary' 2>/dev/null)
sm=${sm:-the selected task}

printf '\n%s🤖 #%s%s %s\n%s   %s%s\n\n' "$h" "$id" "$r" "$sm" "$d" "$instr" "$r"
printf '%s   reading the thread …%s\n\n' "$d" "$r"; sleep 1.2
printf '%ssummary%s\n' "$h" "$r"
printf '  • Scope is agreed and a rough estimate exists.\n'
printf '  • One open question — who owns sign-off — was raised and answered in-thread.\n'
printf '  • Nothing blocks it on our side; safe to close once acknowledged.\n\n'
sleep 1.6
printf 'mark #%s done? [y/N] ' "$id"
read -r yn
if [[ ${yn:-} == [yY] ]]; then
	dstask "$id" done >/dev/null 2>&1 && printf '%s✓ resolved #%s — it moves to DONE on the board%s\n' "$ok" "$id" "$r"
else
	printf 'left open.\n'
fi
read -rp $'\n  (enter to return to the board) ' _
