// Insight Summaries, streamed (M10-T2).
//
// The form underneath works without this: it posts, the server runs the
// analysis, and the page comes back with the summary. This turns the
// wait into reading — the same operation, the same single model call,
// and a cached run costs nothing either way.
(function () {
  "use strict";

  var panel = document.querySelector(".insight");
  if (!panel) return;
  var path = panel.getAttribute("data-insight-path");
  var form = panel.querySelector(".insight-form");
  var output = panel.querySelector(".insight-output");
  var button = form && form.querySelector("button");
  if (!path || !form || !output || !button || !window.EarfulSocket || !window.EarfulSocket.supported) {
    return;
  }

  // Same marker as generate.js: the streaming path is wired now, and a
  // submit that arrives before this is the plain POST instead.
  form.setAttribute("data-enhanced", "1");

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    button.disabled = true;
    button.textContent = "Reading the answers…";
    output.hidden = false;
    output.textContent = "";

    var socket = window.EarfulSocket.open(path, {
      onOpen: function () {
        socket.send({ action: "analyze" });
      },
      onChunk: function (chunk) {
        output.textContent += chunk;
      },
      onDone: function () {
        socket.close();
        // Reload so the summary appears with its model-and-timestamp
        // label: an unlabelled block of prose is exactly what this
        // feature must never leave on screen.
        window.location.reload();
      },
      onError: function (message) {
        output.textContent = message;
        restore();
        socket.close();
      },
      onGone: function () {
        output.textContent = "Lost the connection — running it the ordinary way…";
        form.submit();
      },
    });

    function restore() {
      button.disabled = false;
      button.textContent = "Analyse the responses";
    }
  });
})();
