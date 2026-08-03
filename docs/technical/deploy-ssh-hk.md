# HK Server Release Runbook

This runbook is the source of truth for releasing code to the HK production service. It keeps ordinary binary releases separate from configuration changes.

## Runtime Facts

- SSH alias: `hk`
- Working directory: `/home/ubuntu/cli-proxy-hk`
- Public request path: `https://charge.jiongka.online/llm/v1`
- Nginx route: `location ^~ /llm/` proxies to `http://127.0.0.1:18318/`
- Application listener: `127.0.0.1:18318`
- Binary: `/home/ubuntu/cli-proxy-hk/cli-proxy-api.bin`
- Start command: `/home/ubuntu/cli-proxy-hk/cli-proxy-api.bin -config /home/ubuntu/cli-proxy-hk/config_hk.yaml`
- Target platform: Linux AMD64 (`x86_64`)

The HK directory is a runtime directory, not a Git checkout. Build the static Linux AMD64 binary locally; do not compile on HK.

## Configuration Boundary

| Remote path | Source of truth | Ordinary binary release behavior |
|---|---|---|
| `/home/ubuntu/cli-proxy-hk/config_hk.yaml` | Versioned `config_hk.yaml` in this private repository | Compare hash; never create, edit, overwrite, or delete |
| `/home/ubuntu/cli-proxy-hk/state-store.hk.ini` | Versioned `state-store.hk.ini` in this private repository | Compare hash; never create, edit, overwrite, or delete |
| `/home/ubuntu/cli-proxy-hk/config_hk.yaml.new` | Ephemeral staging file | Never commit, create, overwrite, or delete during a binary release |
| `/home/ubuntu/cli-proxy-hk/config_hk.yaml.bak.*` | Server-local backup | Never commit, overwrite, or delete |
| `/home/ubuntu/cli-proxy-hk/state-store.hk.ini.new` | Ephemeral staging file | Never commit, create, overwrite, or delete during a binary release |
| `/home/ubuntu/cli-proxy-hk/state-store.hk.ini.bak.*` | Server-local backup | Never commit, overwrite, or delete |
| `/home/ubuntu/cli-proxy-hk/auths/` | Server-local credentials | Never commit, overwrite, or delete |
| `/home/ubuntu/cli-proxy-hk/logs/` | Server-local operational data | Never commit, overwrite, or delete |

`config_hk.yaml` and `state-store.hk.ini` are private configuration files and can contain credentials. Restrict repository access accordingly. Configuration changes must originate in the local repository, be reviewed and committed, then use the approved configuration-release path. Do not use SSH shell editing, ad hoc file creation, source synchronization, or an ordinary binary release to change HK configuration.

## Preflight

Run locally. The hash checks deliberately print only digests, never configuration contents.

```bash
git status --short
git diff --check
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/cli-proxy-api-linux-amd64 ./cmd/server
sha256sum /tmp/cli-proxy-api-linux-amd64

for file in config_hk.yaml state-store.hk.ini; do
  local_sha=$(sha256sum "$file" | awk '{print $1}')
  remote_sha=$(ssh hk "sha256sum /home/ubuntu/cli-proxy-hk/$file | awk '{print \$1}'")
  test "$local_sha" = "$remote_sha"
done
```

Abort the binary release if either configuration hash differs. Reconcile the versioned configuration through the approved configuration-release path first.

Verify the active service without reading configuration contents:

```bash
ssh hk 'bash -s' <<'REMOTE'
set -euo pipefail
pid=$(pgrep -f "[c]li-proxy-api.bin -config /home/ubuntu/cli-proxy-hk/config_hk.yaml" | head -n 1)
printf "pid=%s\n" "$pid"
readlink -f "/proc/$pid/exe"
(ss -ltnp || netstat -ltnp) 2>/dev/null | grep ':18318'
REMOTE
```

## Binary-only Upload

Do not run `rsync` or copy the repository into `/home/ubuntu/cli-proxy-hk/`. An ordinary release uploads exactly one candidate binary. It excludes every configuration file, `auths/`, and `logs/` path by not transferring them at all.

```bash
artifact=/tmp/cli-proxy-api-linux-amd64
local_sha=$(sha256sum "$artifact" | awk '{print $1}')
scp "$artifact" hk:/home/ubuntu/cli-proxy-hk/cli-proxy-api.bin.new
remote_sha=$(ssh hk "sha256sum /home/ubuntu/cli-proxy-hk/cli-proxy-api.bin.new | awk '{print \$1}'")
test "$local_sha" = "$remote_sha"
```

Do not proceed on a candidate-hash mismatch.

## Binary Switch

Back up only the active binary, wait for the old process and listener to exit, then start the candidate with the unchanged `config_hk.yaml`. If the candidate does not remain alive and listen on `18318`, restore the binary backup and restart it with the same configuration. The command does not create or overwrite configuration, authentication, or log files.

```bash
ssh hk 'bash -s' <<'REMOTE'
set -euo pipefail
workdir=/home/ubuntu/cli-proxy-hk
binary="$workdir/cli-proxy-api.bin"
candidate="$workdir/cli-proxy-api.bin.new"
config="$workdir/config_hk.yaml"
pattern='[c]li-proxy-api.bin -config /home/ubuntu/cli-proxy-hk/config_hk.yaml'

cd "$workdir"
test -f "$candidate"
ts=$(date +%Y%m%d_%H%M%S)
backup="$workdir/cli-proxy-api.bin.bak.$ts"
cp -f "$binary" "$backup"
mv -f "$candidate" "$binary"
chmod 0755 "$binary"

pkill -f "$pattern" || true
for _ in $(seq 1 20); do
  ! pgrep -f "$pattern" >/dev/null && break
  sleep 1
done
if pgrep -f "$pattern" >/dev/null; then
  cp -f "$backup" "$binary"
  exit 1
fi

nohup "$binary" -config "$config" </dev/null >/dev/null 2>&1 &
new_pid=$!
for _ in $(seq 1 20); do
  if kill -0 "$new_pid" 2>/dev/null && (ss -ltn || netstat -ltn) 2>/dev/null | grep -q ':18318'; then
    printf 'new_pid=%s\nbackup=%s\n' "$new_pid" "$backup"
    exit 0
  fi
  sleep 1
done

cp -f "$backup" "$binary"
nohup "$binary" -config "$config" </dev/null >/dev/null 2>&1 &
exit 1
REMOTE
```

## Post-release Checks

- Confirm the running binary SHA-256 matches the uploaded artifact and the process listens on `18318`.
- Confirm Nginx still routes `/llm/` to the local listener before testing the public path.
- Run `/v1/models` and endpoint-specific canaries with an existing production credential without printing credentials or response bodies.
- For a Responses reasoning compatibility canary, use the public `/llm/v1/responses` route and only the legacy top-level `reasoning_effort` alias. Record request IDs and categorical outcomes. Never enable payload logging.
- Investigate any request error, fallback, or circuit event before considering the release complete.

## Rollback

Configuration is never rolled back by this procedure. Restore only the most recent binary backup and restart it with the unchanged `config_hk.yaml`:

```bash
ssh hk 'bash -s' <<'REMOTE'
set -euo pipefail
workdir=/home/ubuntu/cli-proxy-hk
binary="$workdir/cli-proxy-api.bin"
config="$workdir/config_hk.yaml"
pattern='[c]li-proxy-api.bin -config /home/ubuntu/cli-proxy-hk/config_hk.yaml'
backup=$(find "$workdir" -maxdepth 1 -type f -name 'cli-proxy-api.bin.bak.*' -printf '%T@ %p\n' | sort -nr | head -n 1 | cut -d' ' -f2-)
test -n "$backup"

pkill -f "$pattern" || true
for _ in $(seq 1 20); do
  ! pgrep -f "$pattern" >/dev/null && break
  sleep 1
done
cp -f "$backup" "$binary"
nohup "$binary" -config "$config" </dev/null >/dev/null 2>&1 &
REMOTE
```
