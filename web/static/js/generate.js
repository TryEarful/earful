// AI-drafted questions, streamed (M6-T3).
//
// The form underneath works on its own: submit it and the server
// generates, appends and re-renders. This script upgrades that wait into
// a live feed against the same handler — one generation either way, so
// the enhancement costs nothing extra.
(function () {
  "use strict";

  var form = document.querySelector(".generate-form");
  if (!form) return;
  var path = form.getAttribute("data-generate-path");
  if (!path || !window.EarfulSocket || !window.EarfulSocket.supported) return;

  var output = document.querySelector(".generate-output");
  var button = form.querySelector('button[type="submit"]');
  var prompt = form.querySelector('[name="prompt"]');
  if (!output || !button || !prompt) return;

  form.addEventListener("submit", function (event) {
    var text = prompt.value.trim();
    if (!text) return; // let the server say what it wants said
    event.preventDefault();

    button.disabled = true;
    button.textContent = "Drafting…";
    output.hidden = false;
    output.textContent = "";

    var socket = window.EarfulSocket.open(path, {
      onOpen: function () {
        socket.send({ action: "generate", params: { prompt: text } });
      },
      onChunk: function (chunk) {
        output.textContent += chunk;
      },
      onStatus: function (message) {
        // The closing status is the summary ("Added 5 questions…"), which
        // is worth reading before the page reloads under it.
        output.textContent = message;
      },
      onDone: function () {
        socket.close();
        // The questions are in the draft now. Give the summary a moment
        // to be read — it is the only place the "2 were skipped" count
        // appears in this path — then show the editor with them in it.
        window.setTimeout(function () {
          window.location.reload();
        }, 1500);
      },
      onError: function (message) {
        output.textContent = message;
        restore();
        socket.close();
      },
      onGone: function () {
        // Fall back to the plain form: it does the same thing, slower.
        output.textContent = "Lost the connection — submitting the ordinary way…";
        form.submit();
      },
    });

    function restore() {
      button.disabled = false;
      button.textContent = "Draft questions";
    }
  });
})();
