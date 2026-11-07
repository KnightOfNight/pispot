# Display mockups

Static HTML/CSS mockups for the 5" Touch Display 2 (1280×720 landscape).
Open `index.html` in any browser. Everything runs from fake data in
`mock-data.js`; press keys 1–5 in any mockup to switch scenarios
(engine running, engine off, solar only, low battery, no power data).

Nothing here is wired to the Pi yet. These pages are the proof of
concept for the `display.html` page that the kiosk browser will show.

## Buckets (from the 2026-11-07 brain dump)

1. **Display plumbing** (hardware plan from 2026-11-06, decisions pending):
   display on CAM/DISP 0, rotation, kiosk compositor + browser, loopback
   listener, Ansible playbook.
2. **Skins** — one page, several looks, chosen per display (and later by tap):
   - `dsky.html` Apollo DSKY homage: VERB/NOUN keypad, three registers,
     no caution-and-warning lamp panel. Small-font client list on a NOUN.
   - `plain.html` basic skin: one row per power source with an icon
     (truck, solar, battery), colour-coded voltages, charging indicator,
     weather and network summary. Taps through to the network map.
   - `netmap.html` network map for the plain skin: pictures and notes per
     host, moving dotted lines while a connection is active.
   - `lcars.html` Star Trek TNG LCARS skin.
3. **Power data** — needs the serial capture round first; `/api/power`
   shape is sketched in `mock-data.js` (`power` object).
4. **Network data** — mostly exists in `/api/stats` (clients, WAN, rates).
   New: an "active" flag from traffic, and per-host identity settings.
5. **Host identity settings** — the real admin page (dashboard) gets
   controls to set, reset and clear a client's icon/picture and display
   name (plus a free-text note such as "programming"), keyed by MAC and
   stored on the Pi. The touch-display map only reads them. If the code
   shares cleanly, one extra "admin" skin on the touch display could edit
   them too; that needs an on-screen keyboard, and room for it is
   something to test in a mockup, not assume.
6. **Weather** — periodic lookup from the WAN side (Open-Meteo or NWS, no
   key needed), located by GPS when a USB GPS is present, else a fixed
   location from config. Cached on the Pi so the display never waits.
7. **GPS over USB** — optional; `gpsd` or read NMEA directly in
   `pispot-authd`. Also gives time without network.

## Fonts

`fonts/` holds DSEG7 and DSEG14 (keshikan, SIL Open Font License 1.1,
see `fonts/DSEG-LICENSE.txt`). LCARS uses Antonio (OFL, `fonts/Antonio-LICENSE.txt`),
the usual free stand-in for the Swiss 911 face, with a condensed system
font as fallback.
