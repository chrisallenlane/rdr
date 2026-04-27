/* sidebar-init.js — pre-paint restoration of <details> sidebar state.
 *
 * Loaded synchronously from <head> so it runs before the body parses.
 * When localStorage indicates a section should be closed:
 *   1. Inject a <style> hiding details[data-persist-id] elements.
 *   2. Use MutationObserver to set `open=false` on the right elements
 *      as the parser adds them to the DOM.
 *   3. Remove the hide-style once both elements have been seen, so the
 *      sidebar appears with correct state in a single paint.
 *
 * Common case (no closed state) is a fast path: the script reads
 * localStorage, sees nothing to do, and returns. The server's default
 * `<details ... open>` markup is correct and renders directly.
 *
 * The persistence write logic and DOMContentLoaded backstop live in
 * app.js (loaded at end of <body>).
 */
(function () {
  "use strict";

  var STORAGE_KEY = "rdr.sidebar.closed";
  var ALLOWED = { feeds: true, lists: true };
  var EXPECTED = 2;

  var closed = {};
  try {
    var raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw) {
      raw.split(",").forEach(function (id) {
        if (ALLOWED[id]) closed[id] = true;
      });
    }
  } catch (e) {
    void e;
    return;
  }

  var closedCount = 0;
  for (var k in closed) {
    if (Object.prototype.hasOwnProperty.call(closed, k)) closedCount++;
  }
  if (closedCount === 0) return;

  var style = document.createElement("style");
  style.textContent = "details[data-persist-id]{visibility:hidden}";
  document.head.appendChild(style);

  function reveal() {
    if (style.parentNode) style.parentNode.removeChild(style);
  }

  var seen = 0;
  var observer = new window.MutationObserver(function (mutations) {
    for (var i = 0; i < mutations.length; i++) {
      var added = mutations[i].addedNodes;
      for (var j = 0; j < added.length; j++) {
        var node = added[j];
        if (node.nodeType !== 1) continue;
        var matches = [];
        if (node.matches && node.matches("details[data-persist-id]")) {
          matches.push(node);
        }
        if (node.querySelectorAll) {
          var nested = node.querySelectorAll("details[data-persist-id]");
          for (var m = 0; m < nested.length; m++) matches.push(nested[m]);
        }
        for (var n = 0; n < matches.length; n++) {
          var el = matches[n];
          var id = el.getAttribute("data-persist-id");
          if (closed[id]) el.open = false;
          seen++;
        }
      }
    }
    if (seen >= EXPECTED) {
      observer.disconnect();
      reveal();
    }
  });

  observer.observe(document.documentElement, {
    childList: true,
    subtree: true,
  });

  // Sidebarless pages won't reach EXPECTED matches; reveal on DOMContentLoaded.
  document.addEventListener("DOMContentLoaded", function () {
    observer.disconnect();
    reveal();
  });
})();
