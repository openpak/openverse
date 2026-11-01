# miiverse-api endpoints used

Every call goes to `{OPENVERSE_API_URL}/internal/v1/web/api/v1/...` (the web bridge in `apps/miiverse-api/src/services/web-bridge`) with `X-Internal-Key` and, for a signed-in person, `X-Trusted-Pid`. The bridge forwards to the same internal API the Juxtaposition UI uses over gRPC; the contract is `apps/miiverse-api/internal.openapi.json`. Pictures are read from `{OPENVERSE_API_URL}/cdn/<key>` (no key needed; the web app only proxies `paintings/ screenshots/ icons/ headers/ mii/`).

| Page or action | Call | Notes |
| --- | --- | --- |
| Sign-in | `POST /sso/resolve` `{accountId, displayName, wiiuPid}` | finds or creates the person; answers `pid` and `rulesAccepted` |
| Welcome, Accept | `POST /sso/accept-rules` | |
| Signed-in checks | `GET /self` | `readOnly` (supervised), moderator, followed communities and people |
| Notification count | `GET /self/notifications` | `unreadNotifications` |
| Communities | `GET /communities?category=listed&limit=` | |
| Community page | `GET /communities/{id}`, `GET /communities/{id}/stats` | post count from `totalPosts` |
| Community posts | `GET /communities/{id}/feed/fresh` or `/feed/popular` | |
| Follow a community | `POST` or `DELETE /communities/{id}/followers/@me` | |
| Feed | `GET /activity-feed/following` or `/activity-feed/fresh` | |
| Activity | `GET /activity?scope=` | |
| Post | `GET /posts/{id}` | `null` or 404 means missing |
| Replies | `GET /posts?parent_id=&include_replies=true&sort=oldest\|newest` | |
| New post | `POST /communities/{id}/posts` | `{body, feelingId, isSpoiler, isAppJumpable, painting:{file,isBmp}}` |
| Reply | `POST /posts/{id}/replies` | same body |
| Yeah | `POST /posts/{id}/empathies` `{action: add\|remove}` | |
| Delete | `DELETE /posts/{id}` | |
| Report | `POST /posts/{id}/report` `{reasonId, message}` | |
| Profile | `GET /users/{id}/profile`, `GET /users/{id}/posts` | |
| Follow a person | `POST` or `DELETE /users/{id}/followers/@me` | |
| Notifications | `GET /notifications?markAsRead=true` | reading the page marks them read |
| Settings | `GET` and `POST /users/@me/settings`, `GET` and `POST /activity/settings` | the user settings call replaces everything, so the web app reads first and sends the fields it does not edit back unchanged |

Never used: `/admin/*` (the bridge refuses it; moderation stays in the Juxtaposition UI for now), console routes, device links, onboarding.

## What was added to the miiverse-api

One thing: `services/web-bridge` (`PN_MIIVERSE_API_WEB_KEY`). With no key it answers 404; a request that came through the public proxy (`X-Forwarded-For`) answers 404; a wrong key answers 401. It forwards only `/api/v1/` paths (never `/api/v1/admin/`) and only the `x-trusted-pid` header, so a caller cannot send a console or OAuth token through it. `dispatchInternal` in `services/internal/server.ts` is the function both the gRPC door and the bridge use.

## Known gaps in this step

- Communities in the list have no post count: the list endpoint does not send one, and asking `/stats` for every tile would be one call each. Tiles show followers; the community page shows the post count.
- A web-only person has no Mii. Pages draw their initial; the console sees the default face the API already stores for them.
- Console drawings and web drawings share the stored format, but no console has been used to check a web drawing yet (the whole stack is untested on hardware; see the rebuild plan).
- English only. The web app's own strings are not in the i18n catalogue yet.
