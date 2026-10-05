#!/usr/bin/env bash
#
# Runs the tests with coverage in every module and fails when any package is
# below its floor. Packages without test files count as 0 %.
#
# Floors: 80 % for every package, 60 % for cmd/server. There the tests cover
# run() (wiring, serving, graceful shutdown, config and listen errors); only the
# few lines of main() (signal setup and os.Exit) cannot be exercised in-process.
#
#   scripts/check-coverage.sh
#   MIN_COVERAGE=90 MIN_CMD_COVERAGE=70 scripts/check-coverage.sh
set -euo pipefail

MIN="${MIN_COVERAGE:-80}"
MIN_CMD="${MIN_CMD_COVERAGE:-60}"
cd "$(dirname "$0")/.."

status=0
for module in service-a service-b; do
  echo "==> ${module} (floor ${MIN}%, cmd ${MIN_CMD}%)"
  # -count=1 so cached results never hide a regression.
  output="$(cd "${module}" && go test -race -count=1 -cover ./... 2>&1)" || { echo "${output}"; exit 1; }

  while IFS= read -r line; do
    pkg="$(awk '{print ($1=="ok" || $1=="?") ? $2 : $1}' <<<"${line}")"
    if [[ "${line}" == *"no test files"* ]]; then
      pct="0.0"
    else
      pct="$(sed -n 's/.*coverage: \([0-9.]*\)%.*/\1/p' <<<"${line}")"
    fi
    [[ -z "${pct}" ]] && continue
    floor="${MIN}"
    [[ "${pkg}" == */cmd/server ]] && floor="${MIN_CMD}"
    printf '    %6s%%  %s\n' "${pct}" "${pkg}"
    if awk -v p="${pct}" -v m="${floor}" 'BEGIN { exit !(p < m) }'; then
      echo "    ^ below the ${floor}% floor" >&2
      status=1
    fi
  done <<<"$(grep -E '^(ok|\?|FAIL)' <<<"${output}")"
done

exit "${status}"
