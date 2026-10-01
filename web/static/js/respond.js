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

  // The wording, which the page carries (uitext.js). Without it there
  // is nothing to say, and the page is left as it works without a
  // script.
  var T = window.EarfulText;
  if (!T) return;

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

  var form = document.querySelector(".js-respond-form");
  if (!form) return;

  solveChallenge(form);
  var draft = attachDraft(form);
  // After the draft, so a restored answer is what the history starts from.
  attachVersions(form);

  // Esc leaves a text field (SPEC.md story 80). Inside a textarea Enter
  // is a newline, so a respondent who has finished typing has no plain
  // key that moves on; Esc gives them one, since from outside the field
  // Enter means Next. Attached to every respondent page, not only the
  // paged ones, so the key does one thing everywhere. The answer is
  // not touched: clearing it takes Shift+Esc, which voice.js owns.
  form.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    if (event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return;
    if (isTextField(event.target)) event.target.blur();
  });

  var questions = Array.prototype.slice.call(
    form.querySelectorAll(".js-respond-question")
  );
  // Nothing to page through.
  if (questions.length < 2) {
    stampStartTime();
    return;
  }

  // If the server re-rendered with validation errors, stay on the plain
  // long-form view: every problem is visible at once and the error summary
  // links work. Paging would hide most of them behind navigation.
  if (document.querySelector(".js-error-summary")) {
    stampStartTime();
    return;
  }

  // A creator previewing with every question on one page (?layout=all)
  // reads the form as it renders without a script. The server sets this
  // only on a preview, never on a respondent's page.
  if (form.getAttribute("data-layout") === "all") {
    stampStartTime();
    return;
  }

  var current = 0;
  form.classList.add("is-paged");

  var nav = document.createElement("div");
  nav.className = "respond-nav";

  var progress = document.createElement("p");
  progress.className = "respond-progress js-respond-progress";
  // Announce progress politely: a screen reader user hears the new
  // position after moving, without interrupting whatever they are reading.
  progress.setAttribute("aria-live", "polite");

  // The same position as a bar, for the eye. The words above it are what
  // a screen reader hears, so the bar is hidden from one.
  var meter = document.createElement("progress");
  meter.className = "respond-meter";
  meter.max = questions.length;
  meter.setAttribute("aria-hidden", "true");

  var backButton = document.createElement("button");
  backButton.type = "button";
  backButton.className = "secondary";
  // Each button points the way it goes: the arrow leads the word on
  // Back and follows it on Next, as the paper plane follows Submit.
  backButton.appendChild(
    buttonLabel([buttonIcon(["M19 12H5", "M12 19l-7-7 7-7"]), document.createTextNode(T.t("js.respond.back"))])
  );

  var nextButton = document.createElement("button");
  nextButton.type = "button";
  nextButton.appendChild(
    buttonLabel([document.createTextNode(T.t("js.respond.next")), buttonIcon(["M5 12h14", "M12 5l7 7-7 7"])])
  );
  // The buttons that move — Back, Next, and Submit on the last
  // question — name their keys, read left to right as the keys to
  // press. Each key is its symbol and then its name, because ↵ and ⇧
  // are pictures of keys not every keyboard has printed on it. ESC
  // comes first only while a textarea has focus: there Enter is a
  // newline, with Shift or without, and the way out is Esc first (the
  // listener above takes focus out of the field).
  var escHints = [];
  function addEnterHints(button, shifted) {
    var esc = keyCombo(T.parts("js.key.then", { Key: { key: T.t("js.key.esc") } }));
    esc.className += " key-esc js-key-esc";
    esc.hidden = true;
    escHints.push(esc);
    var enter = { key: T.t("js.key.enter") };
    var keys = shifted
      ? T.parts("js.key.combo", { First: { key: T.t("js.key.shift") }, Second: enter })
      : [enter];
    button.appendChild(keyCombo([esc].concat(keys)));
  }
  function showEsc(on) {
    escHints.forEach(function (hint) {
      hint.hidden = !on;
    });
  }
  addEnterHints(backButton, true);
  addEnterHints(nextButton, false);

  form.addEventListener("focusin", function (event) {
    showEsc(event.target.tagName === "TEXTAREA");
  });
  // relatedTarget is where focus is going; null when it is going
  // nowhere, which is what Esc does.
  form.addEventListener("focusout", function (event) {
    var next = event.relatedTarget;
    showEsc(!!next && next.tagName === "TEXTAREA" && form.contains(next));
  });

  nav.appendChild(backButton);
  nav.appendChild(nextButton);

  var actions = form.querySelector(".js-respond-actions");
  // The server renders Submit, so that it is there without JavaScript;
  // its hints are added here because the keys they name are.
  var submitButton = actions.querySelector('button[type="submit"]');
  if (submitButton) addEnterHints(submitButton, false);
  form.insertBefore(progress, form.querySelector(".js-respond-questions"));
  form.insertBefore(meter, form.querySelector(".js-respond-questions"));
  actions.parentNode.insertBefore(nav, actions);
  // Submit takes Next's place on the last question, so it takes its
  // place in the row as well, beside Back rather than under it.
  nav.appendChild(actions);

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
      // newline. On the last question it submits, and says so itself:
      // a browser submits on Enter from an input, but not on a
      // modified Enter from a textarea.
      if (event.key === "Enter") {
        event.preventDefault();
        if (current < questions.length - 1) show(current + 1);
        else submit();
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
        return;
      }
      // The last question. From an input or an option the browser
      // submits on Enter by itself, and is left to. With nothing
      // focused — which is where Esc leaves a respondent — it does
      // not, so the Enter that would have meant Next means Submit.
      if (event.target === document.body) {
        event.preventDefault();
        submit();
      }
      return;
    }

    // Shift+Space starts and stops recording. It is the only shortcut
    // here that overrides typing, since in a text field it would
    // otherwise insert a space. Plain Space belongs to voice.js, which
    // types a space on a tap and records while the key is held; it is
    // wired there, not here, so that it works on a one-question survey
    // too, where this layer never attaches.
    if (event.shiftKey && event.key === " ") {
      var mic = questions[current].querySelector(".js-voice-button");
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

  // A click rather than form.submit(): the click runs the browser's
  // validation and fires the submit event, which is what clears the
  // draft.
  function submit() {
    if (submitButton) submitButton.click();
  }

  function pickByValue(value) {
    var input = questions[current].querySelector(
      '.js-scale-point input[value="' + value + '"]'
    );
    if (!input) return false;
    input.click();
    return true;
  }

  // A date field is typed into: its digits are the day, month and year,
  // never a scale point, and its letters are not option keys.
  function isTextField(node) {
    if (node.tagName === "TEXTAREA") return true;
    return node.tagName === "INPUT" && (node.type === "text" || node.type === "date");
  }

  var disclosure = document.querySelector(".js-disclosure");

  function show(index, arriving) {
    if (index < 0 || index > questions.length - 1) return;
    current = index;

    questions.forEach(function (question, i) {
      var active = i === index;
      question.hidden = !active;
    });

    progress.textContent = T.t("respond.question.position", {
      Current: index + 1,
      Total: questions.length,
    });
    meter.value = index + 1;
    backButton.hidden = index === 0;
    nextButton.hidden = index === questions.length - 1;
    actions.hidden = index !== questions.length - 1;

    // What the survey is and what happens to the answers is read once;
    // past the first question only the link to the details stays.
    if (disclosure) disclosure.classList.toggle("is-collapsed", index > 0);
    if (draft) draft.rememberPosition(index);
    // On a touch screen, focusing a field opens the keyboard over the
    // page before the respondent has read it; there the first tap is
    // theirs. With a keyboard, typing can start at once.
    if (!arriving || !window.matchMedia("(pointer: coarse)").matches) {
      focusFirstControl(questions[index]);
    }
  }

  // buttonLabel keeps a button's word and its icon together as one
  // piece, so that in a narrow column the keys are what goes to the
  // next line and the icon never does.
  function buttonLabel(parts) {
    var label = document.createElement("span");
    label.className = "button-label";
    parts.forEach(function (part) {
      label.appendChild(part);
    });
    return label;
  }

  // buttonIcon draws a line icon from SVG path data. Decoration: the
  // button's word is its name.
  function buttonIcon(paths) {
    var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("class", "button-icon");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("focusable", "false");
    paths.forEach(function (d) {
      var path = document.createElementNS("http://www.w3.org/2000/svg", "path");
      path.setAttribute("d", d);
      svg.appendChild(path);
    });
    return svg;
  }

  // Navigation buttons are built here, so their key hints are too. A
  // combo is keys drawn as keycaps with the words that join them in
  // plain text between: "+" for keys pressed together, "then" for keys
  // pressed in turn. Parts are strings (words), { key } (a keycap), or
  // a node already built. As in the template, the whole is aria-hidden:
  // the button's accessible name must remain T.t("js.respond.next"), not "Next Enter".
  function keyCombo(parts) {
    var combo = document.createElement("span");
    combo.className = "key-combo js-key-combo";
    combo.setAttribute("aria-hidden", "true");
    fillCombo(combo, parts);
    return combo;
  }

  function fillCombo(combo, parts) {
    while (combo.firstChild) combo.removeChild(combo.firstChild);
    parts.forEach(function (part, index) {
      // Laid out by the flex gap; the space is for anything reading the
      // text, which would otherwise get "HoldSpace".
      if (index) combo.appendChild(document.createTextNode(" "));
      if (part.nodeType) {
        combo.appendChild(part);
        return;
      }
      var piece = document.createElement("span");
      piece.className = typeof part === "string" ? "key-word" : "key-hint js-key-hint";
      piece.textContent = typeof part === "string" ? part : part.key;
      combo.appendChild(piece);
    });
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
  show(draft ? draft.startAt(questions.length) : 0, true);
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

// Earlier versions of a dictated answer (SPEC.md story 81).
//
// Dictation changes an answer in large strokes — a take lands a
// paragraph, Reset removes one — and a stroke made by mistake has no
// undo: a value set from script is outside the browser's own history.
// So the answer is remembered as it goes, and any earlier state can be
// looked at and put back.
//
// Kept under the draft's rules, because it is the draft's kind of data:
// in this browser and nowhere else, scoped to the survey version,
// expired after a day, cleared on submit. Nothing here is ever sent.
//
// One version per GRAIN_MS of editing, taken at the end of the window.
// The exception is a large deletion, which first puts what it is about
// to destroy on record: without that, a Reset four seconds after the
// last version would erase exactly the text this exists to keep.
function attachVersions(form) {
  "use strict";

  // The wording, which the page carries (uitext.js). Without it there
  // is nothing to say, and the page is left as it works without a
  // script.
  var T = window.EarfulText;
  if (!T) return;

  var version = form.querySelector('[name="version_id"]');
  var survey = form.getAttribute("action") || location.pathname;
  if (!version || !version.value) return;

  var key = "earful.versions." + survey + "." + version.value;
  var MAX_AGE_MS = 24 * 60 * 60 * 1000;
  var GRAIN_MS = 5000;
  var KEEP = 40; // per answer; the oldest go first
  var LARGE_CUT = 20; // characters removed in one stroke

  var store;
  try {
    store = window.localStorage;
    if (!store) return;
    store.setItem(key + ".probe", "1");
    store.removeItem(key + ".probe");
  } catch (err) {
    return; // private mode, storage disabled, quota: no history, nothing else changes
  }

  var saved = load();

  // Offered where dictation is, which is where an answer changes in
  // strokes large enough to regret.
  Array.prototype.forEach.call(
    form.querySelectorAll(
      '.js-respond-question[data-voice="1"] textarea, .js-respond-question[data-voice="1"] input[type=text]'
    ),
    attach
  );

  form.addEventListener("submit", function () {
    try {
      store.removeItem(key);
    } catch (err) {
      /* nothing useful to do */
    }
  });

  function load() {
    try {
      var raw = store.getItem(key);
      var found = raw ? JSON.parse(raw) : null;
      if (found && found.fields && Date.now() - found.at <= MAX_AGE_MS) return found;
      if (raw) store.removeItem(key);
    } catch (err) {
      // Unreadable is the same as absent.
    }
    return { at: Date.now(), fields: {} };
  }

  function save() {
    saved.at = Date.now();
    try {
      store.setItem(key, JSON.stringify(saved));
    } catch (err) {
      // A full quota must never break answering.
    }
  }

  function attach(field) {
    if (!field.name) return;
    var list = saved.fields[field.name] || (saved.fields[field.name] = []);
    var before = field.value; // what the field held ahead of the change now arriving
    var timer = 0;

    var holder = document.createElement("p");
    holder.className = "versions";
    holder.hidden = list.length === 0;
    var link = document.createElement("button");
    link.type = "button";
    link.className = "versions-link button-link";
    link.textContent = T.t("js.respond.versions.title");
    holder.appendChild(link);
    field.parentNode.insertBefore(holder, field.nextSibling);

    field.addEventListener("input", function () {
      var was = before;
      var now = field.value;
      before = now;
      if (now === was) return;
      if (was && (!now || was.length - now.length >= LARGE_CUT)) keep(was);
      if (timer) return;
      timer = window.setTimeout(function () {
        timer = 0;
        keep(field.value);
      }, GRAIN_MS);
    });

    // A window still open when the page goes away would be a version
    // never taken.
    window.addEventListener("pagehide", function () {
      if (!timer) return;
      window.clearTimeout(timer);
      timer = 0;
      keep(field.value);
    });

    link.addEventListener("click", function () {
      show(field, list, link, forget);
    });

    // forget drops every version of this answer. The array is emptied
    // in place, since it is the one the store holds; the answer itself
    // is not touched. With nothing left to look at, the link goes too.
    function forget() {
      list.length = 0;
      save();
      holder.hidden = true;
    }

    function keep(text) {
      // An empty answer is not a version anyone will want back.
      if (!text.trim()) return;
      if (list.length && list[list.length - 1].v === text) return;
      list.push({ t: Date.now(), v: text });
      if (list.length > KEEP) list.splice(0, list.length - KEEP);
      save();
      holder.hidden = false;
    }
  }

  // The time is written as the page is worded. A browser set to the
  // same language writes it its own way, 24 hours or 12, as it always
  // did; one set to another language is not asked, or the time would be
  // in a language the rest of the page is not.
  function locales() {
    var same = (navigator.languages || []).filter(function (tag) {
      return tag === T.lang || tag.indexOf(T.lang + "-") === 0;
    });
    return same.length ? same : [T.lang];
  }

  function when(time) {
    var date = new Date(time);
    var clock = { hour: "2-digit", minute: "2-digit", second: "2-digit" };
    if (date.toDateString() === new Date().toDateString()) {
      return date.toLocaleTimeString(locales(), clock);
    }
    clock.month = "short";
    clock.day = "numeric";
    return date.toLocaleString(locales(), clock);
  }

  function show(field, list, opener, forget) {
    var backdrop = document.createElement("div");
    backdrop.className = "versions-backdrop";

    var dialog = document.createElement("div");
    dialog.className = "versions-dialog js-versions-dialog";
    dialog.setAttribute("role", "dialog");
    dialog.setAttribute("aria-modal", "true");
    dialog.setAttribute("aria-labelledby", "versions-title");

    var title = document.createElement("h2");
    title.id = "versions-title";
    title.textContent = T.t("js.respond.versions.title");

    var entries = document.createElement("ol");
    entries.className = "versions-list";
    // Newest first: the version wanted back is nearly always the last
    // one before the mistake.
    list
      .slice()
      .reverse()
      .forEach(function (entry) {
        var item = document.createElement("li");
        item.className = "versions-item js-versions-item";

        var meta = document.createElement("div");
        meta.className = "versions-meta";
        var time = document.createElement("time");
        time.setAttribute("datetime", new Date(entry.t).toISOString());
        time.textContent = when(entry.t);
        var restore = document.createElement("button");
        restore.type = "button";
        restore.className = "secondary";
        restore.textContent = T.t("js.respond.versions.restore");
        restore.setAttribute("aria-label", T.t("js.respond.versions.restore_from", { When: when(entry.t) }));
        restore.addEventListener("click", function () {
          field.value = entry.v;
          // Announced, so the draft keeps it and the answer it replaces
          // becomes a version in its turn.
          field.dispatchEvent(new Event("input", { bubbles: true }));
          close();
          field.focus();
        });
        meta.appendChild(time);
        meta.appendChild(restore);

        var text = document.createElement("p");
        text.className = "versions-text js-versions-text";
        text.textContent = entry.v;

        item.appendChild(meta);
        item.appendChild(text);
        entries.appendChild(item);
      });

    var actions = document.createElement("div");
    actions.className = "versions-actions";
    // The history is the respondent's, on what may be a shared device:
    // they can be rid of it without submitting or waiting a day. Kept
    // at the far end from Close, the button pressed without looking.
    var clear = document.createElement("button");
    clear.type = "button";
    clear.className = "secondary";
    clear.textContent = T.t("js.respond.versions.clear");
    var done = document.createElement("button");
    done.type = "button";
    done.textContent = T.t("js.respond.versions.close");
    actions.appendChild(clear);
    actions.appendChild(done);

    dialog.appendChild(title);
    dialog.appendChild(entries);
    dialog.appendChild(actions);
    document.body.appendChild(backdrop);
    document.body.appendChild(dialog);
    done.focus();

    var closed = false;
    function close() {
      if (closed) return;
      closed = true;
      document.body.removeChild(dialog);
      document.body.removeChild(backdrop);
    }
    function dismiss() {
      close();
      opener.focus();
    }
    done.addEventListener("click", dismiss);
    clear.addEventListener("click", function () {
      forget();
      close();
      // The link that opened this is hidden now, so focus goes to the
      // answer rather than to nothing.
      field.focus();
    });
    backdrop.addEventListener("click", dismiss);
    dialog.addEventListener("keydown", function (event) {
      if (event.key === "Escape") dismiss();
    });
  }
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
