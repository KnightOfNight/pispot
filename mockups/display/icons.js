// Inline SVG sprite so <use href="#truck"> works from file:// too.
(function () {
  "use strict";
  var sprite = '<svg xmlns="http://www.w3.org/2000/svg" style="display:none">' +
    // pickup truck, side view
    '<symbol id="truck" viewBox="0 0 64 40"><path d="M2 26h4V12a3 3 0 0 1 3-3h22l8 10h16a4 4 0 0 1 4 4v3h3v6h-5" fill="none" stroke="currentColor" stroke-width="3" stroke-linejoin="round"/><path d="M14 9v17M31 9l7 10H14" fill="none" stroke="currentColor" stroke-width="3"/><circle cx="16" cy="30" r="5" fill="none" stroke="currentColor" stroke-width="3"/><circle cx="48" cy="30" r="5" fill="none" stroke="currentColor" stroke-width="3"/><path d="M21 30h22" stroke="currentColor" stroke-width="3"/></symbol>' +
    // solar panel with sun
    '<symbol id="solar" viewBox="0 0 64 48"><path d="M10 20h44l6 18H4z" fill="none" stroke="currentColor" stroke-width="3" stroke-linejoin="round"/><path d="M24 20l-4 18M40 20l4 18M7 29h50" stroke="currentColor" stroke-width="2"/><path d="M32 38v7M24 45h16" stroke="currentColor" stroke-width="3"/><circle cx="50" cy="9" r="5" fill="none" stroke="currentColor" stroke-width="2.5"/><path d="M50 0v2M50 16v2M41 9h2M57 9h2M44 3l1 1M55 14l1 1M56 3l-1 1M45 14l-1 1" stroke="currentColor" stroke-width="2"/></symbol>' +
    // battery
    '<symbol id="battery" viewBox="0 0 64 36"><rect x="2" y="6" width="52" height="24" rx="4" fill="none" stroke="currentColor" stroke-width="3"/><rect x="56" y="13" width="6" height="10" rx="1.5" fill="currentColor"/><rect class="fill" x="7" y="11" width="42" height="14" rx="2" fill="currentColor"/></symbol>' +
    // handheld / mobile radio
    '<symbol id="radio" viewBox="0 0 48 64"><rect x="8" y="18" width="32" height="42" rx="4" fill="none" stroke="currentColor" stroke-width="3"/><rect x="14" y="24" width="20" height="12" rx="1" fill="none" stroke="currentColor" stroke-width="2.5"/><path d="M34 18V4" stroke="currentColor" stroke-width="3.5"/><circle cx="16" cy="48" r="2.5" fill="currentColor"/><circle cx="24" cy="48" r="2.5" fill="currentColor"/><circle cx="32" cy="48" r="2.5" fill="currentColor"/></symbol>' +
    // phone
    '<symbol id="phone" viewBox="0 0 40 64"><rect x="4" y="2" width="32" height="60" rx="6" fill="none" stroke="currentColor" stroke-width="3"/><path d="M14 8h12" stroke="currentColor" stroke-width="2.5"/><circle cx="20" cy="54" r="2.5" fill="currentColor"/></symbol>' +
    // laptop
    '<symbol id="laptop" viewBox="0 0 64 44"><rect x="8" y="4" width="48" height="30" rx="3" fill="none" stroke="currentColor" stroke-width="3"/><path d="M2 40h60" stroke="currentColor" stroke-width="3.5"/></symbol>' +
    // cloud (internet)
    '<symbol id="cloud" viewBox="0 0 64 44"><path d="M18 38h30a10 10 0 0 0 1-20 14 14 0 0 0-27-3 11 11 0 0 0-4 23z" fill="none" stroke="currentColor" stroke-width="3" stroke-linejoin="round"/></symbol>' +
    // wifi
    '<symbol id="wifi" viewBox="0 0 64 48"><path d="M4 18a40 40 0 0 1 56 0M13 28a27 27 0 0 1 38 0M22 37a14 14 0 0 1 20 0" fill="none" stroke="currentColor" stroke-width="4" stroke-linecap="round"/><circle cx="32" cy="44" r="3.5" fill="currentColor"/></symbol>' +
    // raspberry-ish router box
    '<symbol id="pispot" viewBox="0 0 64 48"><rect x="6" y="18" width="52" height="24" rx="4" fill="none" stroke="currentColor" stroke-width="3"/><path d="M16 18V6M48 18V6" stroke="currentColor" stroke-width="3"/><circle cx="16" cy="30" r="2.5" fill="currentColor"/><circle cx="26" cy="30" r="2.5" fill="currentColor"/><circle cx="36" cy="30" r="2.5" fill="currentColor"/></symbol>' +
    // weather: sun, partly cloudy, rain
    '<symbol id="sun" viewBox="0 0 64 64"><circle cx="32" cy="32" r="12" fill="none" stroke="currentColor" stroke-width="3.5"/><path d="M32 4v8M32 52v8M4 32h8M52 32h8M12 12l6 6M46 46l6 6M52 12l-6 6M18 46l-6 6" stroke="currentColor" stroke-width="3.5" stroke-linecap="round"/></symbol>' +
    '<symbol id="partly" viewBox="0 0 64 64"><circle cx="22" cy="22" r="10" fill="none" stroke="currentColor" stroke-width="3"/><path d="M22 4v5M4 22h5M9 9l4 4M35 9l-4 4" stroke="currentColor" stroke-width="3" stroke-linecap="round"/><path d="M24 56h26a9 9 0 0 0 1-18 12 12 0 0 0-23-3 10 10 0 0 0-4 21z" fill="#0b0e12" stroke="currentColor" stroke-width="3" stroke-linejoin="round"/></symbol>' +
    '<symbol id="rain" viewBox="0 0 64 64"><path d="M18 40h28a9 9 0 0 0 1-18 12 12 0 0 0-23-3 10 10 0 0 0-6 21z" fill="none" stroke="currentColor" stroke-width="3" stroke-linejoin="round"/><path d="M22 48l-3 8M32 48l-3 8M42 48l-3 8" stroke="currentColor" stroke-width="3" stroke-linecap="round"/></symbol>' +
    // engine (bolt) and gps pin
    '<symbol id="bolt" viewBox="0 0 32 48"><path d="M18 2L4 28h11l-3 18 16-28H17z" fill="currentColor"/></symbol>' +
    '<symbol id="pin" viewBox="0 0 32 48"><path d="M16 46S4 28 4 16a12 12 0 0 1 24 0c0 12-12 30-12 30z" fill="none" stroke="currentColor" stroke-width="3"/><circle cx="16" cy="16" r="4.5" fill="currentColor"/></symbol>' +
    '</svg>';
  document.addEventListener("DOMContentLoaded", function () {
    document.body.insertAdjacentHTML("afterbegin", sprite);
  });
})();
