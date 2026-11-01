#!/usr/bin/env bash
set -eu
T=/home/dev/.local/share/wago-tinygo-investigation
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
export PATH="$GOROOT/bin:$PATH"
go version
sha256sum "$GOROOT/bin/go" "$T/tinygo042.tar.gz" "$T/tinygo/bin/tinygo" "$T/tinygo/src/internal/task/task_threads.c" "$T/tinygo/src/internal/task/task_threads.go"
"$T/tinygo/bin/tinygo" version
/home/dev/.local/share/mise/installs/tinygo/0.43.0/tinygo/bin/tinygo version
sha256sum /home/dev/.local/share/mise/installs/tinygo/0.43.0/tinygo/bin/tinygo
sha256sum "$T"/debugger/*.deb
cat /proc/sys/kernel/core_pattern
for p in /usr/lib/systemd/systemd-coredump /var/lib/systemd/coredump; do if [ -e "$p" ]; then ls -ld "$p"; else echo "absent: $p"; fi; done
