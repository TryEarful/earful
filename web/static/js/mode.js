// The display mode switcher, applied as it is chosen.
//
// The page works without this: the form posts the choice, the server
// remembers it in a cookie and draws the page again in that mode. Here
// the choice is drawn at once and posted in the background, so the page
// is not reloaded and nothing typed into it is lost, which matters on a
// survey half answered. If the post fails, the form is sent as it would
// be without a script.
(function () {
  "use strict";

  var form = document.querySelector(".js-mode-switcher");
  var select = document.querySelector(".js-mode-select");
  var button = document.querySelector(".js-mode-submit");
  if (!form || !select || !window.fetch || !window.FormData) return;

  if (button) button.hidden = true;

  // What the stylesheet and the browser's chrome read: data-mode on
  // <html>, absent to follow the system, and the theme-color tags.
  function draw(mode) {
    var root = document.documentElement;
    if (mode === "light" || mode === "dark") {
      root.setAttribute("data-mode", mode);
    } else {
      root.removeAttribute("data-mode");
    }
    var tags = document.querySelectorAll(".js-mode-color");
    for (var i = 0; i < tags.length; i++) {
      var tag = tags[i];
      var scheme = mode === "light" || mode === "dark" ? mode : tag.getAttribute("data-scheme");
      tag.setAttribute("content", tag.getAttribute("data-" + scheme));
    }
  }

  select.addEventListener("change", function () {
    draw(select.value);
    // The answer is a redirect back to this page, which is not wanted:
    // the page is already drawn. "manual" stops the fetch at it.
    fetch(form.action, {
      method: form.method,
      body: new URLSearchParams(new FormData(form)),
      credentials: "same-origin",
      redirect: "manual",
    }).catch(function () {
      form.submit();
    });
  });
})();
