// app.js — progressive-enhancement keyboard navigation for rdr.
// Everything works without this file; it only improves the experience.
(function () {
  "use strict";

  // Ignore keypresses inside form controls.
  function inInput(e) {
    var tag = e.target.tagName;
    return (
      tag === "INPUT" ||
      tag === "TEXTAREA" ||
      tag === "SELECT" ||
      e.target.isContentEditable
    );
  }

  // --- Items list: j/k/ArrowDown/ArrowUp to navigate, Enter to open ---
  function initItemsList() {
    var articles = document.querySelectorAll("section > article");
    if (articles.length === 0) return;

    // Make articles focusable.
    articles.forEach(function (el) {
      el.setAttribute("tabindex", "-1");
    });

    var current = -1;

    function focus(index) {
      if (index < 0 || index >= articles.length) return;
      current = index;
      articles[current].focus();
    }

    document.addEventListener("keydown", function (e) {
      if (inInput(e)) return;

      switch (e.key) {
        case "j":
        case "ArrowDown":
          e.preventDefault();
          focus(current < 0 ? 0 : current + 1);
          break;
        case "k":
        case "ArrowUp":
          e.preventDefault();
          focus(current < 0 ? 0 : current - 1);
          break;
        case "Enter":
          if (current >= 0) {
            var link = articles[current].querySelector("a[href]");
            if (link) link.click();
          }
          break;
      }
    });
  }

  // --- h/l/arrows for prev/next navigation (item detail + pagination) ---
  function initPrevNext() {
    var prev =
      document.querySelector('a[aria-label="Older article"]') ||
      document.querySelector('a[aria-label="Older page"]');
    var next =
      document.querySelector('a[aria-label="Newer article"]') ||
      document.querySelector('a[aria-label="Newer page"]');
    if (!prev && !next) return;

    document.addEventListener("keydown", function (e) {
      if (inInput(e)) return;

      switch (e.key) {
        case "h":
          if (next) {
            e.preventDefault();
            next.click();
          }
          break;
        case "l":
          if (prev) {
            e.preventDefault();
            prev.click();
          }
          break;
      }
    });
  }

  // --- Sidebar: u/d to navigate feed/list links, Enter to follow ---
  function initSidebar() {
    var aside = document.querySelector("aside");
    if (!aside) return;

    var links = aside.querySelectorAll("a[href]");
    if (links.length === 0) return;

    // Start at the active link if one exists.
    var current = -1;
    links.forEach(function (el, i) {
      el.setAttribute("tabindex", "-1");
      if (el.getAttribute("aria-current") === "true") current = i;
    });

    function focus(index) {
      if (index < 0 || index >= links.length) return;
      current = index;
      links[current].focus();
    }

    document.addEventListener("keydown", function (e) {
      if (inInput(e)) return;

      switch (e.key) {
        case "d":
          e.preventDefault();
          focus(current < 0 ? 0 : current + 1);
          break;
        case "u":
          e.preventDefault();
          focus(current < 0 ? 0 : current - 1);
          break;
      }
    });
  }

  // --- Theme selector: auto-submit on change ---
  function initThemeSelect() {
    var select = document.querySelector('select[name="theme"]');
    if (!select) return;
    select.addEventListener("change", function () {
      select.form.submit();
    });
  }

  // --- Sync button: show spinner, submit via fetch, reload ---
  function initSyncButtons() {
    var forms = document.querySelectorAll(".sync-form");
    forms.forEach(function (form) {
      form.addEventListener("submit", function (e) {
        e.preventDefault();
        var btn = form.querySelector(".sync-button");
        if (!btn || btn.classList.contains("syncing")) return;
        btn.classList.add("syncing");
        fetch(form.action, {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded" },
        }).then(function () {
          setTimeout(function () {
            window.location.reload();
          }, 1000);
        });
      });
    });
  }

  // --- Keyboard shortcuts help overlay (? to toggle) ---
  function initShortcutsHelp() {
    var shortcuts = [
      ["j / ↓", "Next item"],
      ["k / ↑", "Previous item"],
      ["Enter", "Open item"],
      ["l", "Older article / page"],
      ["h", "Newer article / page"],
      ["d", "Next sidebar link"],
      ["u", "Previous sidebar link"],
      ["?", "Toggle this help"],
    ];

    // Build overlay DOM.
    var backdrop = document.createElement("div");
    backdrop.className = "shortcuts-backdrop";
    backdrop.setAttribute("role", "dialog");
    backdrop.setAttribute("aria-label", "Keyboard shortcuts");
    backdrop.hidden = true;

    var card = document.createElement("div");
    card.className = "shortcuts-card";

    var heading = document.createElement("h2");
    heading.textContent = "Keyboard shortcuts";
    card.appendChild(heading);

    var table = document.createElement("table");
    shortcuts.forEach(function (pair) {
      var tr = document.createElement("tr");
      var tdKey = document.createElement("td");
      var tdDesc = document.createElement("td");
      // Wrap each key in <kbd>.
      pair[0].split(" / ").forEach(function (key, i) {
        if (i > 0) tdKey.appendChild(document.createTextNode("  "));
        var kbd = document.createElement("kbd");
        kbd.textContent = key;
        tdKey.appendChild(kbd);
      });
      tdDesc.textContent = pair[1];
      tr.appendChild(tdKey);
      tr.appendChild(tdDesc);
      table.appendChild(tr);
    });
    card.appendChild(table);

    var closeHint = document.createElement("p");
    closeHint.className = "shortcuts-close";
    closeHint.textContent = "Press ? or Esc to close";
    card.appendChild(closeHint);

    backdrop.appendChild(card);
    document.body.appendChild(backdrop);

    // Footer hint — only shown when JS is active.
    var hint = document.createElement("footer");
    hint.className = "shortcuts-hint";
    hint.innerHTML = "Press <kbd>?</kbd> for keyboard shortcuts";
    document.body.appendChild(hint);

    function toggle() {
      backdrop.hidden = !backdrop.hidden;
    }

    function close() {
      backdrop.hidden = true;
    }

    document.addEventListener("keydown", function (e) {
      if (inInput(e)) return;

      if (e.key === "?") {
        e.preventDefault();
        toggle();
      } else if (e.key === "Escape" && !backdrop.hidden) {
        e.preventDefault();
        close();
      }
    });

    // Close on backdrop click (but not card click).
    backdrop.addEventListener("click", function (e) {
      if (e.target === backdrop) close();
    });
  }

  initItemsList();
  initPrevNext();
  initSidebar();
  initThemeSelect();
  initSyncButtons();
  initShortcutsHelp();
})();
