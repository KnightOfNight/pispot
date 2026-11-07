// Scale the 1280x720 stage to the window, and add the skin switcher.
(function () {
  "use strict";
  function fit() {
    var s = Math.min(window.innerWidth / 1280, window.innerHeight / 720);
    document.querySelectorAll(".stage").forEach(function (el) {
      el.style.transform = "translate(-50%, -50%) scale(" + s + ")";
    });
  }
  window.addEventListener("resize", fit);
  document.addEventListener("DOMContentLoaded", function () {
    fit();
    if (location.search.indexOf("kiosk") >= 0) document.body.classList.add("kiosk");
    var here = location.pathname.split("/").pop();
    var nav = document.createElement("div");
    nav.className = "mock-nav";
    nav.innerHTML = "skins:" +
      ["index.html", "dsky.html", "plain.html", "netmap.html", "lcars.html"].map(function (p) {
        return '<a href="' + p + '"' + (p === here ? ' class="here"' : "") + ">" + p.replace(".html", "") + "</a>";
      }).join("") + '<span class="scenario" id="mock-scenario"></span>';
    document.body.appendChild(nav);
    if (window.MOCK) {
      MOCK.subscribe(function (s) {
        var el = document.getElementById("mock-scenario");
        if (el) el.textContent = "scenario " + s.scenario.key + ": " + s.scenario.name + " (keys 1-5, w = WAN state)";
      });
    }
  });
})();
