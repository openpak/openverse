# Changelog

## v0.2.1 — 2026-11-02

- Phones: a family's game chips sit in one row that scrolls sideways instead of stacking into a wall (Pokémon has 13 games), and the chosen game scrolls into view.

## v0.2.0 — 2026-11-02

- Franchise families. The communities browser lists one tile per franchise (Mario Kart, Splatoon, Zelda ...) instead of every game; a family page shows the posts of all its games with a chip per game ("All" plus the game and its console) and keeps the game's own community underneath. A game that stands alone behaves as before, with no chip row.
- Search still finds single games and opens their family with that game's chip chosen; a game's old address redirects the same way. Console pills match a family when any of its games is on that console. Popular now counts a game for its family.
- Following a family follows the family page; games followed on their own show under their family in Your communities.
- Posting from a family page asks which game (the chosen chip is the default, "All" requires a choice); posts always belong to a game.

## v0.1.4 — 2026-11-02

- Popular now appears only once at least three communities have posts from the last seven days.

## v0.1.3 — 2026-11-02

- Communities rebuilt for browsing: Popular now (busiest in the last seven days), Your communities, General rooms pinned apart from the games, console pills (only consoles that have games), a real search field, and the games sorted by activity then name, 30 at a time with Show more. Tiles show the game's consoles instead of "No posts yet"; the community art is no longer lazy-loaded. The composer's community picker is the same browser.
- Every author, actor and listed person shows their whole OpenPak display name (recorded at each web sign-in, read in one cached batch); people who never signed in on the web keep the console name.

## v0.1.2 — 2026-11-02

- Communities: busiest first, then A to Z; a quiet game says so instead of "0 followers · 0 posts". Your own profile shows your whole account name.

## v0.1.1 — 2026-11-02

- Profile posts asked the API for type "posts" (it wants "post"); fixed. Phone layout shows two community tiles per row.

## v0.1.0 — 2026-11-02

- Switch-over release: everything verse.openpak.org's web did is here. Linked devices (account menu, link code, list, Remove), parental levels (off, read, read + post, failing closed), profile tabs (Yeahs, Following, Followers, Friends, Activity), topics, related communities, spoiler mark, activity removal, data download, oEmbed, manifest, redirects from the old Juxtaposition web addresses, `/admin` proxied to the Juxtaposition UI.
- Spanish, French and German; other languages fall back to English. A locale test covers key parity, placeholders and raw keys.
- The footer no longer carries the Juxtaposition credit; it stays on About and in NOTICE.

- First version of OpenVerse as OpenPak's own web app (Go, `html/template`, the shared Open-site shell). Communities, community pages, posts with replies, the composer (text and a 320x120 drawing in the console's 1-bit BMP format), feed, activity, notifications, profiles, settings, welcome and About. Sign-in is openpak.org single sign-on (client `openverse`); sign-out ends only this site's session.
- Data goes through the miiverse-api web bridge (`/internal/v1/web`), so console and web posts stay one dataset.
