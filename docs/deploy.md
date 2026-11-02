# Deploying OpenVerse

Quadlet `openpak-openverse` (box: `~/.config/containers/systemd/openpak-openverse.container`), image `ghcr.io/openpak/openverse:latest` with `AutoUpdate=registry`, pinned IP (`pin-ips.sh --apply`), env file `~/openpak/deploy/openverse.env` (mode 600: bridge key, SSO client secret, session key). A `v*.*.*` tag builds and pushes the arm64 and amd64 image.

## How verse.openpak.org reaches it

The Cloudflare tunnel for verse.openpak.org points at `localhost:20100`. OpenVerse takes that host port (`PublishPort=127.0.0.1:20100:20195`, side port `20195` stays) and the Juxtaposition UI moved to `127.0.0.1:20101`. No Cloudflare or Traefik change: the console hosts (portal.olv, ctr.olv) and Lite (lite.verse) reach the UI by container name through Traefik.

- Switch: `~/openpak/deploy/openverse-switch.sh`
- Rollback (one command): `~/openpak/deploy/openverse-rollback.sh`
- Backups before the switch: `~/openpak/deploy/openverse-switch-20261102/` (Traefik `dynamic.yml`, `.env`, both Juxtaposition quadlets, listener list).
- `/admin` and `/login/callback` are proxied to the UI (`OPENVERSE_LEGACY_URL`), so moderation keeps working.
