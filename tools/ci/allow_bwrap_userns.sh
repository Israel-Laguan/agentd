#!/usr/bin/env bash
# Lets the bubblewrap jail create user namespaces on GitHub's Ubuntu 24.04
# runners. There kernel.apparmor_restrict_unprivileged_userns=1 blocks
# unprivileged user namespaces for unconfined binaries, so bwrap cannot start
# and the jail tests used to skip silently (B-049). The narrow fix is an
# AppArmor profile that grants userns to /usr/bin/bwrap alone. Where the
# restriction is off (or the sysctl is absent) this changes nothing.
set -euo pipefail

restrict="$(sysctl -n kernel.apparmor_restrict_unprivileged_userns 2>/dev/null || echo unset)"
echo "kernel.apparmor_restrict_unprivileged_userns=${restrict}"
if [ "${restrict}" = "1" ]; then
  sudo install -m 0644 "$(dirname "$0")/bwrap.apparmor" /etc/apparmor.d/agentd-bwrap
  sudo apparmor_parser -r /etc/apparmor.d/agentd-bwrap
fi

# Diagnostic only: the jail tests decide pass or fail. This prints bwrap's own
# error (above) when the jail still cannot start, so the CI log names the cause.
bwrap --unshare-user --ro-bind / / true && echo "bwrap user namespace: ok" || echo "bwrap user namespace: blocked"
