// Copying a survey's share link.
//
// The link is on the page as text and works without this; the button is
// drawn hidden and shown only where there is a clipboard to copy to. Its
// label says it worked for a moment, then says what it does again.
(function () {
  "use strict";

  if (!navigator.clipboard || !navigator.clipboard.writeText) return;
  var button = document.querySelector(".js-copy-link");
  if (!button) return;

  var label = button.textContent;
  var restoring;
  button.hidden = false;
  button.addEventListener("click", function () {
    navigator.clipboard.writeText(button.getAttribute("data-copy")).then(function () {
      button.textContent = button.getAttribute("data-done");
      window.clearTimeout(restoring);
      restoring = window.setTimeout(function () {
        button.textContent = label;
      }, 2000);
    });
  });
})();
