#!/bin/sh
# Retry dependency preparation only. CI bounds this step separately from tests.
set -eu

attempt=1
while :; do
	echo "Preparing corpus Go dependencies (attempt $attempt/3)"
	# Resolve the suite's imports (including tests) to fetch only its dependencies.
	# go list loads packages but does not compile or execute tests.
	if go list -deps -test ./suite >/dev/null; then
		exit 0
	else
		status=$?
	fi
	if [ "$attempt" -eq 3 ]; then
		echo "Go dependency preparation failed after 3 attempts (exit $status)" >&2
		exit "$status"
	fi
	delay=$((attempt * 5))
	echo "Go dependency preparation failed (exit $status); retrying in ${delay}s" >&2
	sleep "$delay"
	attempt=$((attempt + 1))
done
