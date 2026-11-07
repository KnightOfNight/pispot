// Fake data shared by every mockup. The shapes are a first draft of what the
// display page would get from /api/stats and a future /api/power.
(function () {
  "use strict";

  var clients = [
    { name: "IC-7300", kind: "radio", ip: "10.10.10.21", mac: "a4:5e:60:11:22:33", signal: -47, rxKbps: 410, txKbps: 55, active: true, note: "programming" },
    { name: "FT-891", kind: "radio", ip: "10.10.10.22", mac: "f0:9f:c2:44:55:66", signal: -61, rxKbps: 0, txKbps: 0, active: false, note: "idle" },
    { name: "Shy's iPhone", kind: "phone", ip: "10.10.10.31", mac: "3c:22:fb:77:88:99", signal: -52, rxKbps: 1200, txKbps: 140, active: true, note: "" },
    { name: "ThinkPad", kind: "laptop", ip: "10.10.10.40", mac: "8c:16:45:aa:bb:cc", signal: -44, rxKbps: 85, txKbps: 30, active: true, note: "CHIRP" }
  ];

  // Power data mirrors the Epic PWRgate's serial status line and nothing more:
  //   " Charging  PS=14.05V  Batt=12.47V,  9.6A  Sol=0.00V  Min=14"
  // state: Testing | PS Off | Chrg Off | No Bat | Bad Bat x | Charged | Charging |
  //        Trickle | Suspend | Bad temp | MPPT | MPPT-T
  // amps is the charge current into the battery; the device reports no discharge current.
  // Settings read from the device menu (2026-11-01). minSupply is the only one the UI uses.
  var settings = { minSupply: 12.99, maxCharge: 14.40, maxAmps: 1.00, minAmps: 0.25, trickleAmps: 0.15 };
  function line(st, ps, batt, amps, sol, min) {
    return " " + st + "  PS=" + ps.toFixed(2) + "V Bat=" + batt.toFixed(2) + "V,  " + amps.toFixed(2) + "A  Sol=" + (sol < 10 ? " " : "") + sol.toFixed(2) + "V   Min=" + min;
  }
  var scenarios = {
    "1": { name: "engine running, truck bus charging the battery",
      power: { present: true, state: "Trickle", ps: 14.21, batt: 12.84, amps: 0.15, sol: 18.6, minutes: 212 } },
    "2": { name: "engine off, truck bus below the 12.99 V minimum",
      power: { present: true, state: "Chrg Off", ps: 12.60, batt: 12.23, amps: 0, sol: 6.38, minutes: 53 } },
    "3": { name: "solar only, charging",
      power: { present: true, state: "MPPT", ps: 0, batt: 12.95, amps: 0.98, sol: 17.9, minutes: 41 } },
    "4": { name: "low battery, nothing charging",
      power: { present: true, state: "PS Off", ps: 0, batt: 11.62, amps: 0, sol: 9.1, minutes: 188 } },
    "5": { name: "PWRgate not connected",
      power: { present: false } }
  };
  function finish(pw) { if (pw.present) pw.raw = line(pw.state, pw.ps, pw.batt, pw.amps, pw.sol, pw.minutes); return pw; }

  var startKey = (location.search.match(/[?&]s=([1-5])/) || [0, "1"])[1];
  var state = {
    scenario: { key: startKey, name: scenarios[startKey].name },
    power: finish(JSON.parse(JSON.stringify(scenarios[startKey].power))),
    settings: settings,
    network: {
      hostname: "pispot", ssid: "n1qzs-radios",
      wan: { iface: "wlan1", ssid: "HomeNet-5G", ip: "192.168.42.241", up: true, signal: -58, rxKbps: 1700, txKbps: 230 },
      lan: { iface: "eth0", ip: "192.168.42.119", up: true },
      // One entry per interface the Pi is configured for. state: up | down | missing
      // (missing = the device isn't there, which really only happens to the USB WAN dongle).
      interfaces: [
        { name: "wlan0", role: "hotspot", state: "up" },
        { name: "wlan1", role: "WAN", state: "up" },
        { name: "eth0", role: "LAN", state: "down" }
      ],
      clients: clients
    },
    weather: { tempF: 54, cond: "Partly cloudy", icon: "partly", windMph: 9, windDir: "NW", humidity: 61,
      pressureInHg: 30.12, sunrise: "06:31", sunset: "16:28", place: "Home QTH", age: "4 min ago" },
    gps: { fix: true, lat: 42.9956, lon: -71.4548, sats: 9, altFt: 180 },
    system: { tempC: 48.2, uptime: "3d 4h", load: 0.42, time: new Date() }
  };

  function jitter(v, amt, min, max) {
    var n = v + (Math.random() - 0.5) * amt;
    return Math.min(max, Math.max(min, n));
  }

  function tick() {
    var p = state.power;
    if (p.present) {
      p.batt = jitter(p.batt, 0.02, 10.5, 15);
      if (p.ps > 1) p.ps = jitter(p.ps, 0.04, 10.5, 15);
      if (p.sol > 1) p.sol = jitter(p.sol, 0.3, 1, 22);
      if (p.amps > 0) p.amps = jitter(p.amps, 0.04, 0.1, 1.1);
      if (state.system.time.getSeconds() === 0) p.minutes += 1;
      finish(p);
    }
    state.network.wan.rxKbps = Math.round(jitter(state.network.wan.rxKbps, 300, 20, 5000));
    state.network.wan.txKbps = Math.round(jitter(state.network.wan.txKbps, 60, 5, 1000));
    clients.forEach(function (c) {
      if (!c.active) return;
      c.rxKbps = Math.round(jitter(c.rxKbps, c.rxKbps * 0.4 + 5, 1, 4000));
      c.txKbps = Math.round(jitter(c.txKbps, c.txKbps * 0.4 + 5, 1, 1000));
    });
    state.system.time = new Date();
  }

  var subs = [];
  function publish() { subs.forEach(function (f) { f(state); }); }
  setInterval(function () { tick(); publish(); }, 1000);

  var ifaceStates = ["up", "down", "missing"];
  document.addEventListener("keydown", function (e) {
    if (e.key === "w") { // cycle the USB WAN interface through up, down, missing
      var wan = state.network.interfaces[1];
      wan.state = ifaceStates[(ifaceStates.indexOf(wan.state) + 1) % ifaceStates.length];
      state.network.wan.up = wan.state === "up";
      publish();
      return;
    }
    if (scenarios[e.key] && !e.metaKey && !e.ctrlKey) {
      state.scenario = { key: e.key, name: scenarios[e.key].name };
      state.power = finish(JSON.parse(JSON.stringify(scenarios[e.key].power)));
      publish();
    }
  });

  // Colour class for a reading. Rule: no guesses. Only the device's own numbers,
  // its state word, and its documented thresholds decide a colour.
  //   ok = blue/nominal, warn = yellow, bad = red, na = no judgement, off = no reading
  function voltClass(kind, p) {
    if (!p || !p.present) return "off";
    if (kind === "battery") {
      // The PWRgate's own LED thresholds: flashing green under 12 V, flashing red under 11.7 V.
      return p.batt >= 12.0 ? "ok" : p.batt >= 11.7 ? "warn" : "bad";
    }
    if (kind === "supply") {
      if (p.ps < 1) return "bad"; // no supply at all
      return p.ps >= settings.minSupply ? "ok" : "warn"; // the device won't charge from a supply below its minimum
    }
    if (kind === "solar") {
      // A panel below battery voltage can't push current into it; that's the only non-guess line we have.
      if (p.sol < 1) return "off";
      return p.sol > p.batt ? "ok" : "warn";
    }
    return "off";
  }
  function charging(p) { return p.present && p.amps > 0; }

  function fmtKbps(k) {
    if (k >= 1000) return (k / 1000).toFixed(1) + " Mb/s";
    return Math.round(k) + " kb/s";
  }
  function hhmm(d) {
    return d.toTimeString().slice(0, 5);
  }

  window.MOCK = {
    state: state,
    subscribe: function (fn) { subs.push(fn); fn(state); },
    voltClass: voltClass,
    charging: charging,
    fmtKbps: fmtKbps,
    hhmm: hhmm
  };
})();
