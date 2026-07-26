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
  var draft = attachDraft(form);

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
  addKeyHint(backButton, "⇧↵");

  var nextButton = document.createElement("button");
  nextButton.type = "button";
  nextButton.textContent = "Next";
  addKeyHint(nextButton, "↵");

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

  // Answering from the keyboard (SPEC.md story 80): letters select
  // options, digits select scale points, Y/N answer yes-no questions,
  // Enter advances. Options take letters because digits are reserved for
  // rating scales, where a digit already names a value.
  //
  // Every branch below is additive. Tab, the arrow keys within a radio
  // group and Space to toggle keep their native behaviour, which is what
  // assistive technology relies on.
  var digits = "";
  var digitTimer = null;

  // Bound to the document rather than the form: closing the voice consent
  // dialog leaves focus on <body>, where a form-scoped listener would not
  // receive the key that stops recording. Controls outside the form — the
  // language picker, the dialog's own buttons — keep their own keyboard
  // behaviour and are ignored.
  document.addEventListener("keydown", function (event) {
    var target = event.target;
    if (target !== document.body && !form.contains(target)) return;

    if (event.altKey || event.metaKey || event.ctrlKey) {
      // Cmd/Ctrl+Enter advances from a textarea, where plain Enter is a
      // newline. On the last question it falls through to the browser,
      // which submits the form.
      if (event.key === "Enter" && current < questions.length - 1) {
        event.preventDefault();
        show(current + 1);
      }
      return;
    }

    var typing = isTextField(event.target);

    if (event.key === "Enter") {
      // Enter and Shift+Enter remain newlines inside a textarea. Long
      // answers are frequently dictated and then edited, so advancing
      // mid-paragraph would cost the respondent more than the keystroke
      // saves.
      if (event.target.tagName === "TEXTAREA") return;
      if (event.shiftKey) {
        if (current > 0) {
          event.preventDefault();
          show(current - 1);
        }
        return;
      }
      if (current < questions.length - 1) {
        event.preventDefault();
        show(current + 1);
      }
      return;
    }

    // Shift+Space starts and stops recording. It is the only shortcut
    // that overrides typing, since in a text field it would otherwise
    // insert a space; plain Space still does, and voice is offered only
    // on text questions.
    if (event.shiftKey && event.key === " ") {
      var mic = questions[current].querySelector(".voice-button");
      if (mic) {
        event.preventDefault();
        mic.click();
      }
      return;
    }

    if (typing || event.shiftKey) return;

    if (event.key >= "0" && event.key <= "9") {
      // Buffered, because a 0–10 scale needs "1" then "0" to mean ten
      // rather than one.
      digits += event.key;
      if (digitTimer) window.clearTimeout(digitTimer);
      if (!pickByValue(digits) && digits.length > 1) pickByValue(event.key);
      digitTimer = window.setTimeout(function () {
        digits = "";
      }, 600);
      return;
    }

    var control = questions[current].querySelector(
      '[data-key="' + event.key.toUpperCase() + '"]'
    );
    if (control) {
      var input = control.parentNode.querySelector("input");
      if (input) {
        event.preventDefault();
        input.click();
      }
    }
  });

  function pickByValue(value) {
    var input = questions[current].querySelector(
      '.scale-point input[value="' + value + '"]'
    );
    if (!input) return false;
    input.click();
    return true;
  }

  function isTextField(node) {
    return node.tagName === "TEXTAREA" || (node.tagName === "INPUT" && node.type === "text");
  }

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

    if (draft) draft.rememberPosition(index);
    focusFirstControl(questions[index]);
  }

  // Navigation buttons are built here, so their key hints are too. As in
  // the template, the hint is aria-hidden: the button's accessible name
  // must remain "Next", not "Next ↵".
  function addKeyHint(button, key) {
    var hint = document.createElement("span");
    hint.className = "key-hint";
    hint.setAttribute("aria-hidden", "true");
    hint.textContent = key;
    button.appendChild(hint);
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
  show(draft ? draft.startAt(questions.length) : 0);
})();

// Draft answers that survive a reload (SPEC.md story 79, M4-T8).
//
// Kept in this browser and nowhere else. A server-side draft would need
// a key, and for an anonymous respondent the only available keys are a
// cookie or a fingerprint — the identification ADR-0003 refuses. Local
// storage preserves the work without identifying the respondent.
//
// Returns null when there is nothing to store into (private browsing
// throws on access in some browsers), and the form simply behaves as it
// did before.
function attachDraft(form) {
  "use strict";

  var version = form.querySelector('[name="version_id"]');
  var survey = form.getAttribute("action") || location.pathname;
  if (!version || !version.value) return null;

  // Scoped to the exact version: a republished survey must never restore
  // an answer to a question whose wording has changed.
  var key = "earful.draft." + survey + "." + version.value;
  var MAX_AGE_MS = 24 * 60 * 60 * 1000;

  var store;
  try {
    store = window.localStorage;
    if (!store) return null;
    store.setItem(key + ".probe", "1");
    store.removeItem(key + ".probe");
  } catch (err) {
    return null; // private mode, storage disabled, quota — all fine
  }

  // Never persisted and never restored. The render timestamp and the
  // proof-of-work solution belong to a single page load, so a stale one
  // would either fail the anti-abuse checks or weaken them; the honeypot
  // must stay empty; the CSRF token is not the draft's to cache.
  var SKIP = ["version_id", "form_ts", "form_nonce", "altcha", "_csrf"];

  function answerable(field) {
    if (!field.name || SKIP.indexOf(field.name) !== -1) return false;
    if (field.hasAttribute("data-altcha")) return false;
    if (field.hasAttribute("data-started-at")) return false;
    // The honeypot is the hidden field bots fill in; a real respondent
    // never touches it and nothing should ever put a value back into it.
    if (field.type === "hidden") return false;
    return true;
  }

  function read() {
    var answers = {};
    Array.prototype.forEach.call(form.elements, function (field) {
      if (!answerable(field)) return;
      if (field.type === "radio" || field.type === "checkbox") {
        if (field.checked) {
          answers[field.name] = answers[field.name] || [];
          answers[field.name].push(field.value);
        }
        return;
      }
      if (field.value) answers[field.name] = field.value;
    });
    return answers;
  }

  var position = 0;

  function save() {
    try {
      store.setItem(
        key,
        JSON.stringify({ at: Date.now(), position: position, answers: read() })
      );
    } catch (err) {
      // A full quota must never break answering.
    }
  }

  function load() {
    try {
      var raw = store.getItem(key);
      if (!raw) return null;
      var saved = JSON.parse(raw);
      if (!saved || Date.now() - saved.at > MAX_AGE_MS) {
        store.removeItem(key);
        return null;
      }
      return saved;
    } catch (err) {
      return null;
    }
  }

  function restore(saved) {
    Array.prototype.forEach.call(form.elements, function (field) {
      if (!answerable(field)) return;
      var value = saved.answers[field.name];
      if (value === undefined) return;
      if (field.type === "radio" || field.type === "checkbox") {
        field.checked = value.indexOf(field.value) !== -1;
        return;
      }
      field.value = value;
    });
  }

  var saved = load();
  if (saved) restore(saved);

  form.addEventListener("input", save);
  form.addEventListener("change", save);
  // Submission is the point at which the draft has served its purpose.
  // Clearing it matters most on a shared device, where an unsubmitted
  // answer left in storage is readable by the next person to use it.
  form.addEventListener("submit", function () {
    try {
      store.removeItem(key);
    } catch (err) {
      /* nothing useful to do */
    }
  });

  return {
    rememberPosition: function (index) {
      position = index;
      save();
    },
    startAt: function (count) {
      if (!saved || typeof saved.position !== "number") return 0;
      if (saved.position < 0 || saved.position > count - 1) return 0;
      return saved.position;
    },
  };
}

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
