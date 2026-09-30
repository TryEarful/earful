// Insight Summaries, streamed (M10-T2).
//
// The form underneath works without this: it posts, the server runs the
// analysis, and the page comes back with the summary. This turns the
// wait into reading — the same operation, the same single model call,
// and a cached run costs nothing either way.
(function () {
  "use strict";

  // The wording, which the page carries (uitext.js). Without it there
  // is nothing to say, and the page is left as it works without a
  // script.
  var T = window.EarfulText;
  if (!T) return;

  var panel = document.querySelector(".js-insight");
  if (!panel) return;
  var path = panel.getAttribute("data-insight-path");
  var form = panel.querySelector(".js-insight-form");
  var output = panel.querySelector(".js-insight-output");
  var button = form && form.querySelector("button");
  if (!path || !form || !output || !button || !window.EarfulSocket || !window.EarfulSocket.supported) {
    return;
  }

  // Same marker as generate.js: the streaming path is wired now, and a
  // submit that arrives before this is the plain POST instead.
  form.setAttribute("data-enhanced", "1");

  form.addEventListener("submit", function (event) {
    event.preventDefault();
    // What the button said, to say again if this does not finish. It
    // is read from the button, which the page worded.
    var label = button.textContent;
    button.disabled = true;
    button.textContent = T.t("js.insights.working");
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
        output.textContent = T.t("js.insights.lost");
        form.submit();
      },
    });

    function restore() {
      button.disabled = false;
      button.textContent = label;
    }
  });
})();
