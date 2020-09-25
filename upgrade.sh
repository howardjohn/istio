#!/usr/bin/env bash

pod="${1:?pod to upgrade}"
original="$(kubectl get pod "${pod}" -oyaml | kubectl neat)"

updated="$(<<< "$original" sed '/INITIAL_EPOCH/{n;s/.*/      value: "0"/}' | sed '/^  name:/ s/$/-updated/')"

echo "${updated}"
