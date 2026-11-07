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

  // Scenarios the user can flip through with keys 1-5.
  var scenarios = {
    "1": { name: "engine running, supply charging (trickle)",
      power: { present: true,
        battery: { volts: 12.84, amps: 0.4, soc: 96 },
        supply: { volts: 14.21, connected: true, engine: true },
        solar: { volts: 18.6, connected: true, watts: 0 },
        charger: { source: "supply", mode: "trickle" },
        leds: { supply: "on", solar: "flash", battery: "blink" } } },
    "2": { name: "engine off, supply connected but below minimum",
      power: { present: true,
        battery: { volts: 12.41, amps: -1.1, soc: 70 },
        supply: { volts: 12.60, connected: true, engine: false },
        solar: { volts: 0, connected: false, watts: 0 },
        charger: { source: "none", mode: "off" },
        leds: { supply: "on", solar: "off", battery: "on" } } },
    "3": { name: "solar only, charging",
      power: { present: true,
        battery: { volts: 12.95, amps: 2.3, soc: 78 },
        supply: { volts: 0, connected: false, engine: false },
        solar: { volts: 17.9, connected: true, watts: 31 },
        charger: { source: "solar", mode: "bulk" },
        leds: { supply: "off", solar: "on", battery: "snore" } } },
    "4": { name: "low battery, nothing charging",
      power: { present: true,
        battery: { volts: 11.62, amps: -2.8, soc: 18 },
        supply: { volts: 0, connected: false, engine: false },
        solar: { volts: 9.1, connected: true, watts: 0 },
        charger: { source: "none", mode: "off" },
        leds: { supply: "off", solar: "off", battery: "red" } } },
    "5": { name: "PWRgate not connected",
      power: { present: false } }
  };

  var startKey = (location.search.match(/[?&]s=([1-5])/) || [0, "1"])[1];
  var state = {
    scenario: { key: startKey, name: scenarios[startKey].name },
    power: JSON.parse(JSON.stringify(scenarios[startKey].power)),
    network: {
      hostname: "pispot", ssid: "n1qzs-radios",
      wan: { iface: "wlan1", ssid: "HomeNet-5G", ip: "192.168.42.241", up: true, signal: -58, rxKbps: 1700, txKbps: 230 },
      lan: { iface: "eth0", ip: "192.168.42.119", up: true },
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
      p.battery.volts = jitter(p.battery.volts, 0.02, 10.5, 15);
      if (p.supply.connected) p.supply.volts = jitter(p.supply.volts, 0.04, 10.5, 15);
      if (p.solar.connected) p.solar.volts = jitter(p.solar.volts, 0.3, 0, 22);
      p.battery.amps = jitter(p.battery.amps, 0.1, -10, 10);
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

  document.addEventListener("keydown", function (e) {
    if (scenarios[e.key] && !e.metaKey && !e.ctrlKey) {
      state.scenario = { key: e.key, name: scenarios[e.key].name };
      state.power = JSON.parse(JSON.stringify(scenarios[e.key].power));
      publish();
    }
  });

  // Colour class for a reading: ok, warn, bad, or off (not connected / unknown).
  function voltClass(kind, p) {
    if (!p || !p.present) return "off";
    if (kind === "battery") {
      var b = p.battery.volts;
      return b >= 12.6 ? "ok" : b >= 12.0 ? "warn" : "bad";
    }
    if (kind === "supply") {
      if (!p.supply.connected) return "off";
      return p.supply.volts >= 12.99 ? "ok" : "warn"; // PWRgate won't charge below 12.99 V
    }
    if (kind === "solar") {
      if (!p.solar.connected || p.solar.volts < 1) return "off";
      return p.solar.volts >= 13 ? "ok" : "warn";
    }
    return "off";
  }

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
    fmtKbps: fmtKbps,
    hhmm: hhmm
  };
})();
