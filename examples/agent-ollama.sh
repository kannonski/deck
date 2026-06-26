#!/usr/bin/env bash
# Example DECK_AGENT_CMD backed by a LOCAL model via Ollama — nothing leaves your machine.
#
#   export DECK_AGENT_CMD="$PWD/examples/agent-ollama.sh"
#   export DECK_OLLAMA_MODEL=llama3.2      # any model you've `ollama pull`ed
#
# Then press `:` on a card in deck and type an instruction. deck suspends the TUI and
# runs this in the foreground, so you can read the answer; press enter to return.
#
# Needs: dstask (deck shares its store), jq, and a running `ollama serve`.
set -euo pipefail

id=${1:?task id required}; shift
instruction="$*"
model="${DECK_OLLAMA_MODEL:-llama3.2}"

task=$(dstask show-open 2>/dev/null \
  | jq -r --argjson i "$id" '.[] | select(.id==$i) | "\(.summary)\n\(.notes // "")"')
[ -n "$task" ] || { echo "no open task #$id"; exit 1; }

printf '\n── task ──\n%s\n── instruction ──\n%s\n\n── %s ──\n' "$task" "$instruction" "$model"
ollama run "$model" "You are helping with a personal task. Be concise and concrete.

Task:
$task

Instruction: $instruction"

read -rp $'\n(enter to return to the board) ' _
