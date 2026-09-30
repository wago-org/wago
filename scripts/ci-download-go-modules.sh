#!/bin/sh
# Retry dependency preparation only. CI bounds this step separately from tests.
set -eu

attempt=1
while :; do
	echo "Downloading Go modules (attempt $attempt/3)"
	# No `all`: use the workspace's selected dependencies, not every module's tests.
	if go mod download; then
		exit 0
	else
		status=$?
	fi
	if [ "$attempt" -eq 3 ]; then
		echo "Go module download failed after 3 attempts (exit $status)" >&2
		exit "$status"
	fi
	delay=$((attempt * 5))
	echo "Go module download failed (exit $status); retrying in ${delay}s" >&2
	sleep "$delay"
	attempt=$((attempt + 1))
done
