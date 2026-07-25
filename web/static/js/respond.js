// One-question-at-a-time flow for respondent pages.
//
// This file is an enhancement, never a requirement. The server renders the
// complete form and the browser submits it in one POST; everything here
// does is hide all but one question at a time and add navigation. With
// JavaScript disabled, or if this script fails to load, the same form
// still works as an ordinary long page (SPEC.md story 29).
//
// No third-party code, no inline script: the CSP on respondent pages
// allows first-party sources only (ADR-0006, M4-T7).
(function () {
  "use strict";

  // The language picker (M11-T1) submits on change when JavaScript is
  // available; its button is what makes it work when it is not. No
  // inline handler: the CSP forbids them, deliberately.
  var picker = document.querySelector("[data-language-picker]");
  if (picker && picker.form) {
    var pickerButton = picker.form.querySelector('button[type="submit"]');
    if (pickerButton) pickerButton.hidden = true;
    picker.addEventListener("change", function () {
      picker.form.submit();
    });
  }

  var form = document.querySelector(".respond-form");
  if (!form) return;

  solveChallenge(form);

  var questions = Array.prototype.slice.call(
    form.querySelectorAll(".respond-question")
  );
  // Nothing to page through.
  if (questions.length < 2) {
    stampStartTime();
    return;
  }

  // If the server re-rendered with validation errors, stay on the plain
  // long-form view: every problem is visible at once and the error summary
  // links work. Paging would hide most of them behind navigation.
  if (document.querySelector(".error-summary")) {
    stampStartTime();
    return;
  }

  var current = 0;
  form.classList.add("paged");

  var nav = document.createElement("div");
  nav.className = "respond-nav";

  var progress = document.createElement("p");
  progress.className = "respond-progress";
  // Announce progress politely: a screen reader user hears the new
  // position after moving, without interrupting whatever they are reading.
  progress.setAttribute("aria-live", "polite");

  var backButton = document.createElement("button");
  backButton.type = "button";
  backButton.className = "secondary";
  backButton.textContent = "Back";

  var nextButton = document.createElement("button");
  nextButton.type = "button";
  nextButton.textContent = "Next";

  nav.appendChild(backButton);
  nav.appendChild(nextButton);

  var actions = form.querySelector(".respond-actions");
  form.insertBefore(progress, form.querySelector(".respond-questions"));
  actions.parentNode.insertBefore(nav, actions);

  backButton.addEventListener("click", function () {
    show(current - 1);
  });
  nextButton.addEventListener("click", function () {
    show(current + 1);
  });

  // Enter moves on rather than submitting early — except in a textarea,
  // where Enter is a newline and must stay one.
  form.addEventListener("keydown", function (event) {
    if (event.key !== "Enter") return;
    if (event.target.tagName === "TEXTAREA") return;
    if (current < questions.length - 1) {
      event.preventDefault();
      show(current + 1);
    }
  });

  function show(index) {
    if (index < 0 || index > questions.length - 1) return;
    current = index;

    questions.forEach(function (question, i) {
      var active = i === index;
      question.hidden = !active;
    });

    progress.textContent =
      "Question " + (index + 1) + " of " + questions.length;
    backButton.hidden = index === 0;
    nextButton.hidden = index === questions.length - 1;
    actions.hidden = index !== questions.length - 1;

    focusFirstControl(questions[index]);
  }

  function focusFirstControl(question) {
    var control = question.querySelector(
      "textarea, input:not([type=hidden]), select"
    );
    if (control) control.focus();
  }

  function stampStartTime() {
    var field = form.querySelector("[data-started-at]");
    if (field) field.value = String(Date.now());
  }

  stampStartTime();
  show(0);
})();

// ALTCHA proof-of-work, first-party (ADR-0006). Instead of vendoring the
// upstream widget (a web component that spins up a blob: worker the CSP
// would have to allow), this solves the same wire protocol in ~40 lines:
// fetch {algorithm, challenge, salt, maxNumber, signature}, find the
// number whose SHA-256(salt + number) equals the challenge, and post the
// solution back base64-encoded. Failure is silent by design — the server
// falls back to its tighter no-challenge rate bucket.
function solveChallenge(form) {
  "use strict";
  var url = form.getAttribute("data-challenge-url");
  var field = form.querySelector("[data-altcha]");
  if (!url || !field || !window.crypto || !window.crypto.subtle) return;

  fetch(url)
    .then(function (response) {
      if (!response.ok) throw new Error("challenge unavailable");
      return response.json();
    })
    .then(function (challenge) {
      return findNumber(challenge).then(function (number) {
        if (number === null) return;
        field.value = btoa(
          JSON.stringify({
            algorithm: challenge.algorithm,
            challenge: challenge.challenge,
            number: number,
            salt: challenge.salt,
            signature: challenge.signature,
          })
        );
      });
    })
    .catch(function () {
      /* no challenge: the strict rate bucket applies server-side */
    });

  function findNumber(challenge) {
    var encoder = new TextEncoder();
    var target = challenge.challenge;
    var max = challenge.maxNumber;

    function attempt(n) {
      if (n > max) return Promise.resolve(null);
      return window.crypto.subtle
        .digest("SHA-256", encoder.encode(challenge.salt + n))
        .then(function (digest) {
          if (hex(digest) === target) return n;
          return attempt(n + 1);
        });
    }
    return attempt(0);
  }

  function hex(buffer) {
    var bytes = new Uint8Array(buffer);
    var out = "";
    for (var i = 0; i < bytes.length; i++) {
      out += bytes[i].toString(16).padStart(2, "0");
    }
    return out;
  }
}
