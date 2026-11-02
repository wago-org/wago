#!/usr/bin/env bash
A=$(dirname "$(dirname "$(readlink -f "$0")")")
R=$A/bin/run.sh
for m in ordinary wagodebug wago_regalloccheck; do
  $R recheck-d246-r2 d246 $m '^TestDesign(Recheck|Repair|Review)'
  $R control-base-r2 base $m '^TestDesignRecheckBase'
  $R recheck-9180-r2 r9180 $m '^TestDesignRecheck'
  $R counterfactual-d246-r2 d246-cf $m '^TestDesign(Recheck|Repair|Review)'
done
$R race-d246-r2 d246 ordinary '^TestDesign(Recheck|Repair|Review)' -race
$R race-counterfactual-d246-r2 d246-cf ordinary '^TestDesign(Recheck|Repair|Review)' -race
echo MATRIX-R2-DONE >> $A/logs/exits.tsv
