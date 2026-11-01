# OpenVerse

OpenPak's community space (verse.openpak.org): posts, drawings and conversations about the games you play. This repository is the web app: Go, `html/template`, the shared Open-site shell (the same one as OpenKart and OpenSplat), and sign-in through openpak.org single sign-on. Standard library only.

AGPL-3.0-only. See `NOTICE` for the credit to Pretendo Network's Juxtaposition, whose miiverse-api still stores the data and serves the Wii U and 3DS consoles in this first step.

- `docs/architecture.md`: how it is built, where the data lives, how the storage swap works.
- `docs/endpoints.md`: every miiverse-api endpoint the web app uses.

## Run it

Fixture data, no accounts, no network:

```sh
OPENVERSE_DEMO=1 OPENVERSE_SESSION_KEY=$(head -c 24 /dev/urandom | base64) OPENVERSE_ADDR=127.0.0.1:20195 go run ./cmd/openverse
```

Then open http://127.0.0.1:20195 and press Sign in (it signs in a made-up person; add `&as=new` to `/auth/login` to see the first-visit welcome page).

Against a real miiverse-api, copy `example.env`. The API needs `PN_MIIVERSE_API_WEB_KEY` set (the bridge answers 404 without it) and must be reachable on the private network only.

```sh
go vet ./... && go test ./...
```
