#!/bin/sh
# Regenerates <tool>.plan.json: applies before.tf, then plans after.tf against
# that state and writes the plan as `show -json` prints it. Run it once per
# tool, in a Linux container with this directory mounted at /work:
#
#   BIN=tofu OUT=tofu.plan.json sh /work/generate.sh
#   BIN=terraform OUT=terraform.plan.json sh /work/generate.sh
#
# Only terraform_data is used, so no provider or cloud account is needed. The
# plans hold the sensitive variable's value in plaintext under "variables",
# which is what the tests check never reaches the parsed changes.
set -e
work=$(mktemp -d)
cp /work/before.tf "$work/main.tf"
cd "$work"
$BIN init -input=false -no-color >/dev/null
$BIN apply -auto-approve -input=false -no-color >/dev/null
cp /work/after.tf main.tf
$BIN plan -input=false -no-color -out=tfplan >/dev/null
$BIN show -json tfplan >"/work/$OUT"
$BIN version | head -1
