# Changelog

## Unreleased

- First version of OpenVerse as OpenPak's own web app (Go, `html/template`, the shared Open-site shell). Communities, community pages, posts with replies, the composer (text and a 320x120 drawing in the console's 1-bit BMP format), feed, activity, notifications, profiles, settings, welcome and About. Sign-in is openpak.org single sign-on (client `openverse`); sign-out ends only this site's session.
- Data goes through the miiverse-api web bridge (`/internal/v1/web`), so console and web posts stay one dataset.
