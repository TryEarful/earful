// Copying a document as Markdown.
//
// The page works without this: the link beside the button serves the
// same Markdown as a file. The button is drawn hidden, and shown here
// once there is a clipboard to copy to.
//
// The Markdown is on the page already, in a JSON block, rather than
// fetched when the button is pressed: a browser lets a page write to the
// clipboard only while it is handling the press, and a fetch in between
// ends that.
(function () {
  "use strict";

  var button = document.querySelector("[data-copy-markdown]");
  var source = document.getElementById("document-markdown");
  var status = document.querySelector("[data-copy-status]");
  if (!button || !source || !status) return;
  if (!navigator.clipboard || !navigator.clipboard.writeText) return;

  var markdown;
  try {
    markdown = JSON.parse(source.textContent);
  } catch (err) {
    return;
  }

  button.hidden = false;

  var clearing;
  function say(text) {
    status.textContent = text;
    window.clearTimeout(clearing);
    clearing = window.setTimeout(function () {
      status.textContent = "";
    }, 4000);
  }

  button.addEventListener("click", function () {
    navigator.clipboard.writeText(markdown).then(
      function () {
        say(status.getAttribute("data-done"));
      },
      function () {
        say(status.getAttribute("data-failed"));
      }
    );
  });
})();
