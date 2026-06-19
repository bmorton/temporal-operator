// Live-refresh controls shared across pages.
//
// A plain global flag drives the htmx polling filter (so polling pauses even
// before Alpine has initialised), while an Alpine store backs the header UI
// (button label, status dot and the "last updated" timestamp).
window.__uiLivePaused = false;

document.addEventListener("alpine:init", function () {
  Alpine.store("live", {
    paused: false,
    last: "",
    stamp: function () {
      this.last = new Date().toLocaleTimeString();
    },
    toggle: function () {
      this.paused = !this.paused;
      window.__uiLivePaused = this.paused;
      if (!this.paused) {
        // Resuming: refresh immediately so the view isn't stale.
        document.querySelectorAll("[data-live]").forEach(function (el) {
          if (window.htmx) {
            window.htmx.trigger(el, "live:resume");
          }
        });
      }
    },
  });
});

// Stamp the timestamp after every htmx swap (the polled regions).
document.addEventListener("DOMContentLoaded", function () {
  document.body.addEventListener("htmx:afterSettle", function () {
    if (window.Alpine && Alpine.store("live")) {
      Alpine.store("live").stamp();
    }
  });
});
