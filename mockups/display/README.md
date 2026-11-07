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

## Decisions (2026-11-07)

1. **Screen owner:** `cage`, a Wayland kiosk compositor, started by a
   systemd unit as a `kiosk` user. No desktop.
2. **Renderer:** Chromium in kiosk mode showing the dashboard's display
   page. `cog` (WPE WebKit) stays as a lighter swap-in if memory or boot
   time ever bites; the page doesn't change.
3. **Kiosk login:** a second dashboard listener on `127.0.0.1` only, plain
   HTTP. Every request arriving there is treated as logged in as a fixed
   Linux account, `pispot-display`, a member of `pispot-ro`. From there the
   existing code decides everything: the group ladder gives it a role,
   routes check that role, the Users tab lists it, and its Admin box is
   how the owner decides what a person touching the screen may do. It can
   never be in `pispot-owner`. All writes still go through `pispot-authd`.
   The display page stays available on the TLS listener behind the normal
   login for remote testing. Loopback is a safe trust boundary because
   only owners (SSH), root and the kiosk user can run programs on the Pi.
4. **Backlight:** day and night brightness switched by sunrise/sunset from
   the weather lookup, fixed clock times as the fallback. Plus blanking
   after a configurable timeout, only while the PWRgate reports the
   battery carrying the load; any touch wakes it. Every skin gets an
   easy override: toggle the automation on/off, lock day or night, and
   set the timeout (DSKY by verb/noun, the others by buttons). Settings
   persist on the Pi; the backlight write itself goes through
   `pispot-authd`, since it needs root.
5. **Skin selection:** switchable on the screen. Each skin has a control
   that cycles or picks the skin (buttons on plain and LCARS, a verb/noun
   on DSKY). The choice persists in the display settings on the Pi, each
   skin remembers its own view, and the kiosk falls back to the plain skin
   if the configured one fails to load. The web UI can set it too.

The display settings file therefore holds: skin, brightness mode (auto,
day, night), blank timeout, and each skin's last view.

## Rule: no guesses in the UI

Set by the user on 2026-11-07. The display shows only what a device
reports, or what follows directly from a documented setting or threshold.
For the PWRgate that means the serial status line (state word, supply,
battery and solar volts, charge current, minutes in state) and the
settings read from its menu. Nothing inferred: no engine state, no
battery percentage, no discharge current, no solar watts, no LED states,
no "trickle"/"bulk" wording of our own. Truck is "nominal" in blue at or
above the 12.99 V minimum-supply setting, "undervolt" in yellow below it,
and 0.00 V in red when there is no supply.
Solar shows its real reading even at 0.00 V; it is "nominal" in blue when
the panel voltage is above the battery voltage (the only line that isn't a
guess: below that no current can flow in), "undervolt" in yellow when
there is voltage but not that much, and nothing when dark. Battery
colours follow the device's own LED thresholds (12.0 V and 11.7 V).

Zeros only when the device actually reports zero. If a reading is absent
(no data at all, as in scenario 5, or a field missing from the line) show
the dashed placeholder, never 0.00. Open question until the serial
capture: whether the PWRgate prints 0.00 V or drops the field on a hard
disconnect of the supply or the panel.

## Fonts

`fonts/` holds DSEG7 and DSEG14 (keshikan, SIL Open Font License 1.1,
see `fonts/DSEG-LICENSE.txt`). LCARS uses Antonio (OFL, `fonts/Antonio-LICENSE.txt`),
the usual free stand-in for the Swiss 911 face, with a condensed system
font as fallback.
