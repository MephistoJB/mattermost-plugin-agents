# Codex shell in the QNAP Mattermost container

The Mattermost image has no `/bin/sh`. Codex needs it for `exec_command`.

On `nas1`, `/share/Docker/mattermost/data/codex-busybox-static` is a statically
linked Debian amd64 BusyBox binary. The existing Mattermost Compose application
mounts that file at both `/bin/busybox` and `/bin/sh` in the Mattermost container.
The file lives in the existing Mattermost data volume, so both mounts survive
container recreation. No separate application or container is needed.

The Compose file is
`/share/CACHEDEV1_DATA/.qpkg/container-station/data/application/mattermost/docker-compose.yml`.
The pre-change copy is `docker-compose.yml.codex-shell-20260922.bak` in the same
directory. QNAP Container Station may regenerate its Compose file during an
application edit; verify the two mounts after such changes.

The binary was extracted from Debian `busybox-static` version `1:1.37.0-6+b9`
for amd64. The package SHA256 is
`598a3fd92bdafc34cd81196b2952ad36e910e47a4ecf162f8ce5e8e262598e53`.
The extracted binary SHA256 is
`7ae95c260622412ceba6eb9f45d68f616cdc091a1d71d6c0a08b5875bb2c31dd`.

After a Mattermost restart, verify:

```sh
docker inspect -f '{{.State.Health.Status}}' mattermost
docker exec mattermost /bin/sh -lc 'echo SHELL_OK'
docker exec mattermost /mattermost/bin/mmctl --local plugin list
```

`ensure-mattermost-codex-shell.sh` is a manual repair fallback if the mounts are
lost. It is not scheduled.
