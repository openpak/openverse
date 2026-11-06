# OpenVerse web app: architecture

## What this is

OpenVerse is OpenPak's community space. This service is its web front end: it renders every page for browsers at verse.openpak.org and has no business logic of its own beyond what a page needs. It is phase 2 of the rebuild plan (the plan itself is private): OpenPak's own app, built the way OpenKart and OpenMaker are, not a restyle of Juxtaposition's web views. Juxtaposition's UI keeps serving the console hosts (portal and ctr) until those migrate.

## Shape

```
browser ──HTTPS──> openverse (this repo)                       Go, html/template, stdlib only
                      │  store.Store (interface)
                      ├── internal/miiverse ──HTTP+key──> miiverse-api web bridge ──> internal API ──> Postgres, Mongo, MinIO
                      └── internal/demo    (fixtures, OPENVERSE_DEMO=1 only)
consoles ──> Juxtaposition UI and miiverse-api (unchanged)
```

- `cmd/openverse`: wires config, the store and the HTTP server.
- `internal/web`: handlers, sign-in, session, templates, static files. Templates are the shared Open-site shell (`_layout.html`, copied from OpenKart's layout and extended), parts (`_parts.html`: icon, avatar, post card, empty state, pager) and one file per page.
- `internal/store`: the types a page needs (`Post`, `Community`, `Profile`, `Notification`...) and the `Store` interface. Nothing in it names a console wire format.
- `internal/miiverse`: the only package that knows the miiverse-api JSON. It maps wire types to store types and speaks to the web bridge.
- `internal/demo`: an in-memory Store with made-up data and generated pictures, for screenshots, local work and tests.

## Pages and navigation

The top bar is the only primary navigation: the OpenVerse mark and name, then Feed (signed in), Communities, Activity (signed in), Notifications with an unread count, About; on the right a New post button and the account menu (profile, settings, linked devices, sign out) or Sign in. There is no sidebar. On phones the top bar keeps the mark, New post and the account chip, and the main sections move to a bottom tab bar, as on openpak.org.

| Route | Page |
| --- | --- |
| `/` | signed in: redirect to `/feed`; signed out: Communities |
| `/communities`, `/communities/{id}`, `/communities/{id}/related` | tile grid (every game has one); community header, Follow, Post here, recent or popular posts; sub-communities |
| `/posts/{id}` | post with replies and the reply box; delete, spoiler mark or report |
| `/posts/{id}/oembed.json` | link-preview card for a post |
| `/topics?topic_tag=` | posts with a topic tag |
| `/new`, `/new?community=` | community picker; composer (text and a 320x120 drawing) |
| `/feed`, `/activity`, `/notifications` | following or everyone; opt-in game activity (Everyone, Following); Yeahs, replies, follows |
| `/me`, `/users/{id}`, `/users/{id}/{yeahs,following,followers,friends,activity}` | profile with its tabs |
| `/settings` | game-activity opt-ins, privacy, notification choices, account links, data download (`/users/downloadUserData.json`) |
| `/devices` (+ `/devices/check`, `/approve`, `/revoke`) | Linked devices: link code, device list, Remove. Account menu only |
| `/welcome`, `/welcome/off` | community rules, one card, two buttons; the page a parent-disabled account is sent to |
| `/about` | what OpenVerse is, the AGPL credit |
| `/auth/login`, `/auth/callback`, `/auth/logout` | openpak.org single sign-on; sign-out ends only this site's session and returns to `/about` |
| `/cdn/...` | read-only proxy for pictures from the API's media store (`paintings/ screenshots/ icons/ headers/ mii/`) |
| `/notifications/count` | JSON for the badge |
| `/healthz`, `/robots.txt`, `/manifest.json`, `/favicon.ico`, `/static/...` | housekeeping |
| `/admin/...`, `/login/callback` | moderation: proxied unchanged to the Juxtaposition UI (`OPENVERSE_LEGACY_URL`) |

Addresses of the Juxtaposition web UI keep working as permanent redirects (`/titles`, `/titles/{id}`, `/users/me`, `/activity/settings`, `/feed/people`, `/news/...`, `/login`, `/logout`, ...); the post, community and profile ids are the same.

Yeah, follow, reply, post, delete and report are plain HTML forms that redirect back; a little script turns Yeah into a fetch and adds copy-link, the account menu, the badge poll and the drawing pad. Every page works without it.

## Languages

English is the source. Spanish, French and German are translated; every other language falls back to English. The language is `?lang=xx` (remembered in a cookie), else Accept-Language. Catalogues are flat JSON in `internal/i18n/locales/`; a plural is `<key>.one` and `<key>.other`. A test checks that es, fr and de have every key with the same placeholders, that every key a template or handler asks for exists, and that no page in any language shows a raw key.

## Parental levels

The decision is the miiverse-api's, through the same parental lookup it makes for every console and web request (`web-identity/access.ts`): none (OpenVerse off), read, or read + post, failing closed when the parental core cannot be reached. `/self` reports `openverseOff` and `readOnly`. Off: every page but `/welcome/off` and sign-out redirects there. Read: every form that writes is hidden (Yeah, follow, reply, New post, delete, report, settings) and a forced write is refused with 403 here and by the API. Read + post is full access.

## Linked devices

The page uses the miiverse-api `deviceLinks` routes through the bridge (`list`, `revoke`) and the Postgres link codes the Lite service writes (`verse-lite:code:<CODE>`), see `internal/linkcodes`. Every form carries a token derived from the session; checking a code is limited to ten tries a minute per account.

## Sign-in and session

openpak.org single sign-on, client `openverse`, with PKCE; the exchange is server to server at `{OPENVERSE_CORE_URL}/internal/sso/token` with HTTP Basic `openverse:SSO_CLIENT_OPENVERSE_SECRET`. The site has no password form. After the exchange the app calls `Store.Resolve`, which finds or creates the OpenVerse person for the OpenPak account (with or without a Wii U), and keeps `{account, name, id, rulesAccepted}` in an HMAC-signed cookie. Until the rules are accepted every page redirects to `/welcome`. Writes require a same-site request (`Sec-Fetch-Site` or `Origin`). The content security policy allows only this origin's scripts and styles.

## Data: today and the swap

Today `internal/miiverse` calls the miiverse-api through the web bridge (`docs/endpoints.md`), so a post made here is the same row a Wii U or 3DS sees. The plan's later phases move storage into this service (Postgres keyed by the OpenPak account) and re-implement the console endpoints. That is a second `Store`: it implements the same interface, `cmd/openverse` picks it by configuration, and no template or handler changes. Two rules keep the swap cheap:

1. Pages use `store` types only. Console fields (`feelingId`, `appData`, `searchKey`, platform ids) never reach a template.
2. A person's id in a URL is whatever `Store` calls an id. Today it is the number the API knows them by; a Postgres store would use its own stable id and redirect the old ones.

## Drawings

The pad is 320x120, black and white, with the console's two pens and an eraser. The browser encodes the pixels as a 1-bit BMP and posts it as base64 in `painting` (`bmp=true` on the wire). The server checks it is a BMP of a sane size and passes it on; the miiverse-api re-validates, crops, makes it bilevel and stores the zlib TGA the consoles read plus the PNGs this site shows. The encoder in `static/openverse.js` is a re-statement of the one in Juxtaposition's `bmp.ts` (reference only).

## Not done yet

- Languages beyond en, es, fr and de fall back to English.
- Not carried over from the Juxtaposition web UI: console friend requests (accepted on the console), attaching a screenshot (web posts are text and drawings), and the `/images` paths of old notification rows.
- No moderation UI here (the Juxtaposition admin pages still do that).
- Mobile phone and desktop layouts follow openpak.org; the app (Flutter) is phase 3.
- The notification badge is rendered on every page from `Store.UnreadNotifications` with a two-second budget and refreshed by polling once a minute.
- Console behaviour has never been checked on hardware.
