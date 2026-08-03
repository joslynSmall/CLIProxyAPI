# 227 Server Release Runbook

This runbook is the source of truth for releasing to host `227`. It records the active runtime layout and keeps ordinary code releases separate from production configuration changes.

## Runtime Facts

- SSH host: `root@192.168.15.227` (SSH alias: `227`)
- Working directory: `/opt/cliproxy`
- Listener: `18318`
- Start command: `./cli-proxy -config config.yaml`
- Target platform: Linux ARM64
- The remote Go toolchain is not suitable for this repository. Build a static Linux ARM64 binary locally; do not compile on the server.

## Configuration Boundary

| Remote path | Source of truth | Release behavior |
|---|---|---|
| `/opt/cliproxy/config.yaml` | Server-local protected configuration | Never commit, create, edit, or overwrite during a release |
| `/opt/cliproxy/state-store.local.ini` | Versioned file in this private repository | Compare its hash before a code release; never overwrite during an ordinary code release |
| `/opt/cliproxy/state-store.local.ini.new` | Ephemeral staging file | Never commit or create during a code release |
| `/opt/cliproxy/state-store.local.ini.bak.*` | Server-local backup | Never commit, overwrite, or delete during a release |
| `/opt/cliproxy/auths/` | Server-local credentials | Never commit or overwrite |
| `/opt/cliproxy/logs/` | Server-local operational data | Never commit or overwrite |

`config.example.yaml` and `state-store.example.ini` remain credential-free portable templates. The private, versioned `state-store.local.ini` is the current 227 Mongo runtime-state configuration and contains credentials. Restrict repository access accordingly.

Configuration changes must originate in the local repository, be reviewed and committed, and use the approved configuration release path. Do not use SSH shell editing, ad hoc file creation, or a normal binary release to modify production configuration.

## Preflight

Run locally:

```bash
git status --short
git diff --check
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -o /tmp/cli-proxy-linux-arm64 ./cmd/server
sha256sum /tmp/cli-proxy-linux-arm64
```

Verify the active service and configuration parity without printing configuration contents:

```bash
local_sha=$(sha256sum state-store.local.ini | awk '{print $1}')
remote_sha=$(ssh 227 'sha256sum /opt/cliproxy/state-store.local.ini | awk "{print \$1}"')
test "$local_sha" = "$remote_sha"

ssh 227 '
  set -e
  pid=$(pgrep -f "[.]\/cli-proxy -config config.yaml" | head -n 1)
  printf "pid=%s\n" "$pid"
  readlink -f "/proc/$pid/exe"
  (ss -ltnp || netstat -ltnp) 2>/dev/null | grep ":18318"
  stat -c "state-store.local.ini mode=%a" /opt/cliproxy/state-store.local.ini
'
```

Abort the code release if the state-store hashes differ. Reconcile the versioned configuration through the approved configuration path first.

## Sync Source

Synchronize source and static assets only. Every server-local runtime file is explicitly excluded, including the versioned state-store file, so `--delete` cannot remove or replace it.

```bash
rsync -az --delete \
  --exclude '.git' \
  --exclude 'graphify-out' \
  --exclude 'auths' \
  --exclude 'logs' \
  --exclude 'config.yaml' \
  --exclude 'config.yaml.*' \
  --exclude 'config-277.yaml' \
  --exclude 'config_hk.yaml' \
  --exclude 'state-store.local.ini' \
  --exclude 'state-store.local.ini.new' \
  --exclude 'state-store.local.ini.bak.*' \
  --exclude 'cli-proxy' \
  --exclude 'cli-proxy.new' \
  --exclude 'cli-proxy.bak.*' \
  --exclude 'server' \
  ./ 227:/opt/cliproxy/
```

Upload the prebuilt binary separately:

```bash
scp /tmp/cli-proxy-linux-arm64 227:/opt/cliproxy/cli-proxy.new
```

Before the switch, compare the local and remote SHA-256 values. A mismatch aborts the release.

## Binary Switch

Back up only the binary, wait for the previous process and listener to exit, then start the new binary with the unchanged `config.yaml`. If the new process or listener health check fails, restore the binary backup and restart the old version. Do not change any configuration files during this procedure.

```bash
ssh 227 'bash -s' <<'REMOTE'
set -euo pipefail
cd /opt/cliproxy
test -x cli-proxy.new
ts=$(date +%Y%m%d_%H%M%S)
backup="cli-proxy.bak.${ts}"
cp -f cli-proxy "$backup"
mv -f cli-proxy.new cli-proxy
chmod +x cli-proxy
pkill -f '[.]\/cli-proxy -config config.yaml' || true
for _ in $(seq 1 20); do
  ! pgrep -f '[.]\/cli-proxy -config config.yaml' >/dev/null && break
  sleep 1
done
if pgrep -f '[.]\/cli-proxy -config config.yaml' >/dev/null; then
  cp -f "$backup" cli-proxy
  exit 1
fi
nohup ./cli-proxy -config config.yaml > logs/app.log 2>&1 < /dev/null &
new_pid=$!
echo "$new_pid" > logs/app.pid
for _ in $(seq 1 20); do
  if kill -0 "$new_pid" 2>/dev/null && (ss -ltn || netstat -ltn) 2>/dev/null | grep -q ':18318'; then
    printf 'new_pid=%s\nbackup=%s\n' "$new_pid" "$backup"
    exit 0
  fi
  sleep 1
done
cp -f "$backup" cli-proxy
nohup ./cli-proxy -config config.yaml > logs/app.log 2>&1 < /dev/null &
echo $! > logs/app.pid
exit 1
REMOTE
```

## Post-release Checks

- Confirm the process is listening on `18318` and the deployed binary SHA-256 matches the release artifact.
- Run `/v1/models` using an existing production client credential without printing that credential or response body.
- Run endpoint-specific canaries only against a healthy, authorized upstream route. Record request IDs and categorical outcomes; never enable payload logging for a canary.
- For a Responses reasoning compatibility canary, use a request with only the legacy top-level `reasoning_effort` alias. Verify a successful response, endpoint-native normalization, no fallback attempt, and no circuit-open event.

## Rollback

Configuration is never rolled back by this procedure. To roll back code, restore a binary backup and restart with the same `config.yaml`:

```bash
ssh 227 'bash -s' <<'REMOTE'
set -euo pipefail
cd /opt/cliproxy
backup=$(ls -t cli-proxy.bak.* | head -n 1)
test -n "$backup"
cp -f "$backup" cli-proxy
pkill -f '[.]\/cli-proxy -config config.yaml' || true
for _ in $(seq 1 20); do
  ! pgrep -f '[.]\/cli-proxy -config config.yaml' >/dev/null && break
  sleep 1
done
nohup ./cli-proxy -config config.yaml > logs/app.log 2>&1 < /dev/null &
echo $! > logs/app.pid
REMOTE
```
