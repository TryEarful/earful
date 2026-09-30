// Spoken answers (M5, ADR-0004).
//
// Everything visible here is built by this script, so a respondent
// without JavaScript never sees a control they cannot use: the server
// renders the textarea and nothing else. Typing always works, and every
// failure — no microphone, permission refused, quota exhausted, socket
// gone — ends by saying "type your answer instead" (story 38, 39).
//
// Recognition happens on the device when the browser can prove it is
// local; otherwise the audio streams to the EU transcription service and
// is discarded (ADR-0004). Non-local browser recognition is never used,
// so no voice reaches a third-party recogniser undisclosed.
//
// Capture is 16 kHz mono PCM via an AudioWorklet, not MediaRecorder: the
// server passes the samples straight to whisper.cpp or Vertex without a
// transcoder, which is what keeps self-hosting free of ffmpeg.
(function () {
  "use strict";

  // The wording, which the page carries (uitext.js). Without it there
  // is nothing to say, and the page is left as it works without a
  // script.
  var T = window.EarfulText;
  if (!T) return;

  var form = document.querySelector(".js-respond-form");
  if (!form) return;
  var voicePath = form.getAttribute("data-voice-path");
  if (!voicePath) return;

  var maxSeconds = parseInt(form.getAttribute("data-voice-max-seconds"), 10) || 120;
  var CONSENT_KEY = "earful-voice-consent";
  var DEVICE_KEY = "earful-voice-device";
  var SAMPLE_RATE = 16000;
  var RECORDING_HINT = T.t("js.voice.recording");
  // A Space press longer than this is a hold, not a tap. Long enough for
  // the hold bar to be seen filling, which is the respondent's only sign
  // that holding is doing something before the microphone opens. The
  // stylesheet's voice-hold animation runs to the same figure.
  var HOLD_MS = 400;
  var HOLD_HINT = T.parts("js.voice.hint.hold", { Key: { key: T.t("js.key.space") } });
  var COLLAPSE_KEY = "earful-voice-collapsed";
  var SVG_NS = "http://www.w3.org/2000/svg";
  var MIC_ICON = [
    "M12 3a3 3 0 0 1 3 3v6a3 3 0 0 1-6 0V6a3 3 0 0 1 3-3z",
    "M6 11a6 6 0 0 0 12 0",
    "M12 17v4",
    "M9 21h6",
  ];
  var TRASH_ICON = ["M4 7h16", "M9 7V4h6v3", "M6 7l1 14h10l1-14", "M10 11v6", "M14 11v6"];

  // --- local recognition detection (M5-T1) -------------------------------
  //
  // Exposed for the browser test: it is a pure function of a window-like
  // object, so it can be checked against stubs instead of against six
  // real browsers.
  function detectLocalRecognition(win) {
    var Recognition = win.SpeechRecognition || win.webkitSpeechRecognition;
    if (!Recognition) return { available: false, reason: "no-api" };
    // On-device availability is a newer, separate capability. Without a
    // way to prove the audio stays on the device, browser recognition is
    // treated as unavailable rather than risk sending a respondent's voice
    // to a third party (ADR-0004).
    if (typeof Recognition.available !== "function") {
      return { available: false, reason: "no-locality-guarantee" };
    }
    if (!("processLocally" in Recognition.prototype)) {
      return { available: false, reason: "no-locality-guarantee" };
    }
    return { available: true, reason: "on-device" };
  }
  window.EarfulVoice = { detectLocalRecognition: detectLocalRecognition };

  var canRecord =
    typeof navigator !== "undefined" &&
    navigator.mediaDevices &&
    typeof navigator.mediaDevices.getUserMedia === "function" &&
    typeof window.AudioContext !== "undefined" &&
    window.EarfulSocket &&
    window.EarfulSocket.supported;
  if (!canRecord) return;

  var localRecognition = detectLocalRecognition(window);
  var engineAtLoad = null; // chooseEngine's answer when the page opened, shared by every mic

  var mics = [];
  Array.prototype.slice
    .call(form.querySelectorAll('.js-respond-question[data-voice="1"]'))
    .forEach(function (question) {
      var field = question.querySelector("textarea, input[type=text]");
      if (field) mics.push(attachMic(question, field));
    });
  if (mics.length) attachKeys(mics);

  function attachMic(question, field) {
    var wrap = document.createElement("div");
    wrap.className = "voice js-voice";
    // idle, recording or transcribing: one attribute the stylesheet and
    // the browser suite can both read, instead of inferring the state
    // from which pieces happen to be hidden.
    wrap.setAttribute("data-state", "idle");

    // The card says what it is, so the status box, the buttons and the
    // microphone row read as one feature rather than as loose controls
    // under the answer; and it can be put away by a respondent who is
    // going to type, which takes the keys away with it.
    var uid = "voice-" + (field.name || "answer");
    wrap.setAttribute("role", "group");
    wrap.setAttribute("aria-labelledby", uid + "-title");
    var head = document.createElement("div");
    head.className = "voice-head";
    var title = document.createElement("span");
    title.className = "voice-title";
    title.id = uid + "-title";
    title.textContent = T.t("js.voice.title");
    var toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = "voice-toggle";
    toggle.setAttribute("aria-controls", uid + "-body");
    // A word rather than a glyph: a minus sign beside a title says
    // "remove" as readily as it says "put away".
    var toggleLabel = document.createTextNode(T.t("js.voice.hide"));
    toggle.appendChild(toggleLabel);
    // The listening owl, from the page (a <template>); the stylesheet
    // shows the sound it hears only while the microphone is open.
    var owlTemplate = document.querySelector(".js-voice-owl");
    var mark = document.createElement("span");
    mark.className = "voice-mark";
    if (owlTemplate) mark.appendChild(owlTemplate.content.cloneNode(true));
    mark.appendChild(title);
    head.appendChild(mark);
    head.appendChild(toggle);
    var body = document.createElement("div");
    body.className = "voice-body";
    body.id = uid + "-body";

    var button = document.createElement("button");
    button.type = "button";
    button.className = "voice-button secondary js-voice-button";
    button.appendChild(icon(MIC_ICON));
    // The label is its own text node so the key hint beside it survives
    // every label change; setting button.textContent would delete it.
    var label = document.createTextNode(T.t("js.voice.dictate"));
    button.appendChild(label);
    // Holding Space records for as long as it is held (attachKeys);
    // Shift+Space, which respond.js owns, still starts and stops a take.
    // The hint is aria-hidden so the button is named T.t("js.voice.dictate"), not
    // "Dictate Hold Space".
    var micHint = keyCombo(HOLD_HINT);
    button.appendChild(micHint);

    function setLabel(text) {
      label.nodeValue = text;
    }

    // Starting over. Shift+Esc does the same, so a respondent who
    // dictated the wrong thing is one gesture from a blank field rather
    // than a paragraph of deleting.
    var resetButton = document.createElement("button");
    resetButton.type = "button";
    resetButton.className = "voice-reset secondary js-voice-reset";
    resetButton.appendChild(icon(TRASH_ICON));
    resetButton.appendChild(document.createTextNode(T.t("js.voice.reset")));
    resetButton.appendChild(
      keyCombo(T.parts("js.key.combo", { First: { key: T.t("js.key.shift") }, Second: { key: T.t("js.key.esc") } }))
    );

    // The status box heads the card. It always says something — what to
    // do when idle, what is happening otherwise — so the card never
    // opens with an empty frame, and its edge takes the colour of the
    // state (see the stylesheet). Recording state and transcription
    // progress are announced, not just shown: this control is unusable
    // otherwise.
    var status = document.createElement("div");
    status.className = "voice-status js-voice-status";
    status.setAttribute("aria-live", "polite");
    var statusText = document.createElement("span");
    statusText.className = "voice-status-text";
    status.appendChild(statusText);

    // Transcription reports no progress — the model answers when it
    // answers — so this bar is indeterminate on purpose. A percentage
    // here would be invented, and inventing one on the single screen
    // where this product asks to be trusted is a poor trade for a few
    // seconds of reassurance. aria-hidden because the status line beside
    // it already announces T.t("voice.status.transcribing"); a screen reader does not
    // need the same fact twice.
    var progress = document.createElement("span");
    progress.className = "voice-progress js-voice-progress";
    progress.setAttribute("aria-hidden", "true");
    progress.hidden = true;

    // What the microphone is hearing, and which microphone. The browser
    // picks the input device and says nothing about it: a headset left
    // paired, or a meeting app's virtual device, is chosen as readily as
    // the built-in microphone, and the only sign of a wrong choice is a
    // transcript that comes back empty. A live spectrum and the device's
    // name make it visible while there is still time to fix it.
    var monitor = document.createElement("span");
    monitor.className = "voice-monitor js-voice-monitor";
    monitor.hidden = true;

    var spectrum = document.createElement("canvas");
    spectrum.className = "voice-spectrum js-voice-spectrum";
    // The status line announces the device name; the bars only repeat
    // what a sighted respondent can already hear.
    spectrum.setAttribute("aria-hidden", "true");
    // There only while there is something to show. An empty meter
    // beside the picker is a blank box that looks like a field, and it
    // takes room the device's name can use.
    spectrum.hidden = true;

    var input = document.createElement("span");
    input.className = "voice-input";

    // Which microphone, as a choice rather than a fact. The browser
    // picks the input silently, and the only sign of a wrong pick is a
    // flat meter and an empty transcript; here the pick is changeable.
    // The browser reveals device names only once permission has been
    // granted. Where it already has been, the picker is there from the
    // start; where it has not, a button asks for it in the picker's
    // place, so the row never shows a list it cannot fill.
    var picker = document.createElement("label");
    picker.className = "voice-picker";
    picker.hidden = true;
    picker.appendChild(document.createTextNode(T.t("js.voice.microphone.label") + " "));
    var select = document.createElement("select");
    select.className = "voice-device";
    picker.appendChild(select);

    var grant = document.createElement("button");
    grant.type = "button";
    grant.className = "voice-grant secondary";
    grant.textContent = T.t("js.voice.microphone.grant");
    grant.hidden = true;

    // Fills while Space is held, along the foot of the Dictate button —
    // the control the key is standing in for. Until the hold is long
    // enough to count, nothing else on the page says that holding is
    // the right thing to be doing. Decoration as far as a screen reader
    // is concerned; the status line announces the take.
    var holdBar = document.createElement("span");
    holdBar.className = "voice-hold js-voice-hold";
    holdBar.setAttribute("aria-hidden", "true");
    holdBar.hidden = true;

    // The card, top to bottom: the status box (with the transcription
    // bar along its foot), the two buttons, then the microphone and its
    // meter side by side.
    // Transcription can be called off while it is in flight. A link
    // rather than a third button: it exists for a few seconds at a time
    // and belongs to the sentence it sits beside.
    var cancelLink = document.createElement("button");
    cancelLink.type = "button";
    cancelLink.className = "voice-cancel button-link";
    cancelLink.textContent = T.t("js.voice.cancel");
    cancelLink.hidden = true;
    status.appendChild(cancelLink);
    status.appendChild(progress);
    button.appendChild(holdBar);
    var actions = document.createElement("div");
    actions.className = "voice-actions";
    actions.appendChild(button);
    actions.appendChild(resetButton);
    monitor.appendChild(picker);
    monitor.appendChild(grant);
    monitor.appendChild(input);
    monitor.appendChild(spectrum);
    body.appendChild(status);
    body.appendChild(actions);
    body.appendChild(monitor);
    wrap.appendChild(head);
    wrap.appendChild(body);
    field.parentNode.insertBefore(wrap, field.nextSibling);
    say("");

    // While a take is running the field says what is about to land in it.
    // A browser hides a placeholder as soon as the field has content, so
    // the first transcribed word clears this without any help from here.
    var placeholder = field.getAttribute("placeholder");
    var meter = null;
    var ui = {
      recording: function (handle) {
        holdBar.hidden = true;
        cancelLink.hidden = true;
        wrap.setAttribute("data-state", "recording");
        // The box the words land in lights up too: on a long answer the
        // button can be scrolled out of view while the microphone is
        // still open.
        field.classList.add("is-live");
        field.setAttribute("placeholder", RECORDING_HINT);
        progress.hidden = true;
        if (!handle.monitor) return;
        var device = handle.monitor.device;
        var listed = showInputs(handle.inputs || [], handle.monitor.deviceId);
        input.textContent = device && !listed ? T.t("js.voice.microphone.input", { Device: device }) : "";
        // Unhidden before the meter starts, so the canvas has a size to
        // read; a hidden element measures zero by zero.
        monitor.hidden = false;
        spectrum.hidden = false;
        meter = startMeter(handle.monitor.context, handle.monitor.stream, spectrum, function quiet() {
          say(
            device ? T.t("js.voice.quiet.named", { Device: device }) : T.t("js.voice.quiet.unnamed")
          );
        });
      },
      transcribing: function () {
        wrap.setAttribute("data-state", "transcribing");
        field.classList.remove("is-live");
        stopMeter();
        progress.hidden = false;
        cancelLink.hidden = false;
      },
      // fail is the status line for something that went wrong: the same
      // line, boxed and bordered, so it is not read as a progress update.
      fail: function (message) {
        statusText.textContent = message;
        status.classList.add("is-error");
      },
      settled: function () {
        holdBar.hidden = true;
        cancelLink.hidden = true;
        wrap.setAttribute("data-state", "idle");
        field.classList.remove("is-live");
        if (placeholder === null) field.removeAttribute("placeholder");
        else field.setAttribute("placeholder", placeholder);
        stopMeter();
        progress.hidden = true;
      },
    };

    // Once the picker has devices to show, the row stays: the choice is
    // for the next take, and it should not vanish the moment there is
    // nothing to choose it for. Without a picker the row was only ever
    // the meter and the device name, and both go with the take.
    function stopMeter() {
      if (meter) meter.stop();
      meter = null;
      spectrum.hidden = true;
      monitor.hidden = picker.hidden && grant.hidden;
      if (picker.hidden) input.textContent = "";
    }

    var recorder = null; // the live take, once the microphone is open
    var finishing = null; // a take that has stopped and is being transcribed
    var starting = null; // a start() still opening the microphone

    button.addEventListener("click", function () {
      if (recorder) {
        stop();
        return;
      }
      // Shift+Space presses this button from respond.js whether or not
      // it can be seen; a card that has been put away stays put away.
      if (starting || collapsed) return;
      askConsent(function () {
        // The consent dialog took focus and is now gone. Put it back on
        // the field the words are about to land in, so the respondent
        // can edit as they speak and the keyboard shortcuts keep working
        // instead of talking to <body>.
        field.focus();
        start("toggle");
      });
    });

    resetButton.addEventListener("click", clear);

    function say(message) {
      status.classList.remove("is-error");
      statusText.textContent = message || idleHint();
    }

    // showInputs fills the picker with the microphones the browser will
    // let this page use and marks the one in use. It returns whether
    // there was anything to show: a browser that withholds names, or a
    // machine with no listed input, leaves the picker hidden and the
    // "Input:" line beside the meter does what it did before.
    function showInputs(devices, currentId) {
      if (!devices.length) {
        picker.hidden = true;
        return false;
      }
      while (select.firstChild) select.removeChild(select.firstChild);
      devices.forEach(function (device) {
        var option = document.createElement("option");
        option.value = device.deviceId;
        option.textContent = device.label;
        select.appendChild(option);
      });
      // The track says which device it is on browsers that report it;
      // otherwise the remembered choice, then the browser's own default.
      var candidates = [currentId, preferredDevice(), "default", devices[0].deviceId];
      for (var i = 0; i < candidates.length; i++) {
        if (candidates[i] && hasOption(candidates[i])) {
          select.value = candidates[i];
          break;
        }
      }
      picker.hidden = false;
      grant.hidden = true;
      return true;
    }

    // The microphone row from the start. Names come back only where
    // permission was granted on an earlier visit; without them the row
    // offers to ask. On-device recognition opens its own input and
    // cannot be pointed at one, so where that engine would be used the
    // row offers neither. Having the API is not the test — most
    // browsers that have it still lack the model — so the question is
    // put to chooseEngine, once for the page.
    if (!engineAtLoad) engineAtLoad = chooseEngine();
    engineAtLoad
      .then(function (engine) {
        return engine === "server" ? listInputs() : null;
      })
      .then(function (devices) {
        if (!devices || recorder || starting) return; // a take got there first and fills the row itself
        if (!showInputs(devices, "")) grant.hidden = false;
        monitor.hidden = false;
      });

    // Granting access is its own act, separate from dictating: the
    // microphone is opened only long enough to be allowed and named,
    // and nothing is recorded or sent. Consent comes first here too —
    // it precedes every first use of the microphone, whichever button
    // that is.
    grant.addEventListener("click", function () {
      if (recorder || starting) return;
      askConsent(function () {
        field.focus();
        say(T.t("js.voice.microphone.waiting"));
        openMicrophone().then(
          function (stream) {
            // Listed while the stream is still open: some browsers
            // withhold the names again the moment it closes.
            return listInputs().then(function (devices) {
              var current = currentDeviceId(stream);
              stream.getTracks().forEach(function (track) {
                track.stop();
              });
              if (showInputs(devices, current)) {
                say(T.t("js.voice.microphone.ready"));
                return;
              }
              // Allowed, but this browser names nothing: there is no
              // list to show and nothing left to ask for.
              grant.hidden = true;
              monitor.hidden = true;
              say("");
            });
          },
          function () {
            disable(T.t("js.voice.microphone.unavailable"));
          }
        );
      });
    });

    function hasOption(value) {
      for (var i = 0; i < select.options.length; i++) {
        if (select.options[i].value === value) return true;
      }
      return false;
    }

    // A new choice applies to the next take: the capture graph is built
    // around one stream, and swapping it mid-take would mean two takes
    // sharing one transcript. So a live take is stopped — what was said
    // is transcribed — and the respondent is told to speak again.
    select.addEventListener("change", function () {
      rememberDevice(select.value);
      var name = select.options[select.selectedIndex].textContent;
      if (recorder) {
        stop();
        say(T.t("js.voice.microphone.switched", { Device: name }));
        return;
      }
      say(T.t("js.voice.microphone.chosen", { Device: name }));
    });

    // A headset plugged in or pulled out after the list was made.
    if (typeof navigator.mediaDevices.addEventListener === "function") {
      navigator.mediaDevices.addEventListener("devicechange", function () {
        if (picker.hidden) return;
        listInputs().then(function (devices) {
          showInputs(devices, select.value);
        });
      });
    }

    // A microphone that cannot be opened — permission refused, no
    // device, an insecure page — greys out both controls rather than
    // inviting a second attempt at the same failure, and the keys stop
    // claiming Space and Esc so typing is exactly what it always was.
    // A reload is the way back once permission is granted or a device
    // is plugged in.
    var disabled = false;
    function disable(message) {
      disabled = true;
      button.disabled = true;
      resetButton.disabled = true;
      select.disabled = true;
      grant.disabled = true;
      wrap.setAttribute("data-state", "unavailable");
      ui.fail(message);
    }

    function reset() {
      recorder = null;
      finishing = null;
      setLabel(T.t("js.voice.dictate"));
      fillCombo(micHint, HOLD_HINT);
      button.classList.remove("is-recording");
      ui.settled();
    }

    function stop() {
      if (!recorder) return;
      var current = recorder;
      recorder = null;
      finishing = current;
      setLabel(T.t("js.voice.dictate"));
      fillCombo(micHint, HOLD_HINT);
      button.classList.remove("is-recording");
      current.stop();
    }

    // cancel calls off a transcription in flight. What the take had
    // already written is taken back, so the answer is what it was
    // before the take; anything typed or dictated earlier is untouched.
    function cancel() {
      var take = finishing || recorder;
      if (!take) return;
      finishing = null;
      recorder = null;
      take.cancel();
      reset();
      say(T.t("js.voice.cancelled"));
      field.focus();
    }
    cancelLink.addEventListener("click", cancel);

    // collapse puts the card away or brings it back. The microphone is
    // never left open behind a closed card: a live take is stopped, and
    // what was said is still transcribed.
    var collapsed = false;
    function collapse(on) {
      if (on && starting) starting.cancelled = "cleared";
      if (on && recorder) stop();
      collapsed = on;
      body.hidden = on;
      wrap.classList.toggle("is-collapsed", on);
      toggle.setAttribute("aria-expanded", on ? "false" : "true");
      toggleLabel.nodeValue = on ? T.t("js.voice.show") : T.t("js.voice.hide");
      toggle.setAttribute("aria-label", on ? T.t("js.voice.show_aria") : T.t("js.voice.hide_aria"));
    }
    // Not wanting dictation is a fact about the respondent, not about
    // one question, so every card on the page follows and the choice is
    // remembered.
    toggle.addEventListener("click", function () {
      var on = !collapsed;
      rememberCollapsed(on);
      mics.forEach(function (mic) {
        mic.collapse(on);
      });
    });

    // hold and release are the two ends of a held Space. A take begun
    // this way ends when the key comes up; one begun by a click or
    // Shift+Space is a toggle and pays the key no attention.
    function hold() {
      if (recorder || starting || collapsed) return;
      // Consent is asked at most once per browser, and never answered by
      // a key release: if the dialog has to appear, the hold is over by
      // the time it is accepted, and the take then runs as a toggle.
      var immediate = true;
      askConsent(function () {
        field.focus();
        start(immediate ? "hold" : "toggle");
      });
      immediate = false;
    }

    function release() {
      if (starting) {
        // Released before the microphone even opened. There is nothing
        // worth transcribing, and a take left running with nobody
        // holding the key would be worse than none.
        starting.cancelled = "released";
        return;
      }
      if (recorder && recorder.mode === "hold") stop();
    }

    // clear empties the answer and, if a take is live, throws it away
    // untranscribed: Reset means "start this answer over".
    function clear() {
      if (starting) starting.cancelled = "cleared";
      var live = recorder || finishing;
      if (live) {
        recorder = null;
        finishing = null;
        live.abort();
      }
      writeAnswer(field, "");
      reset();
      say(T.t("js.voice.cleared"));
      field.focus();
    }

    function start(mode) {
      var pending = { cancelled: null };
      starting = pending;
      say(T.t("js.voice.starting"));
      // On-device first, when the browser can prove it (ADR-0004): the
      // respondent's voice then never leaves their machine at all.
      // Otherwise the audio streams to the EU transcription service.
      chooseEngine()
        .then(function (engine) {
          if (engine === "local") {
            return startLocalRecognition(field, say, ui, function done() {
              reset();
            });
          }
          return startRecording(field, say, ui, function done() {
            reset();
          });
        })
        .then(
          function (handle) {
            starting = null;
            if (!handle) {
              reset();
              return;
            }
            if (pending.cancelled) {
              handle.abort();
              reset();
              if (pending.cancelled === "released") {
                say(T.t("js.voice.hint.longer"));
              }
              return;
            }
            handle.mode = mode;
            recorder = handle;
            // One label for a live take, however it began: the button
            // ends it either way. The hint carries the difference. A
            // held take ends when the key comes up, and says so; a take
            // begun by a click has no key to release, so it says
            // nothing rather than something untrue.
            setLabel(T.t("js.voice.stop"));
            fillCombo(
              micHint,
              mode === "hold" ? T.parts("js.voice.hint.release", { Key: { key: T.t("js.key.space") } }) : []
            );
            button.classList.add("is-recording");
            ui.recording(handle);
            var device = handle.monitor && handle.monitor.device;
            say(device ? T.t("js.voice.listening.named", { Device: device }) : T.t("js.voice.listening.unnamed"));
          },
          function () {
            starting = null;
            reset();
            disable(T.t("js.voice.microphone.unavailable"));
          }
        );
    }

    collapse(collapsedPreference());

    return {
      question: question,
      field: field,
      wrap: wrap,
      hold: hold,
      release: release,
      clear: clear,
      say: say,
      collapse: collapse,
      // A take being transcribed counts: Shift+Esc must reach it
      // wherever focus is, or the transcript lands in a cleared field.
      isRecording: function () {
        return recorder !== null || finishing !== null;
      },
      // Put away is unavailable as far as the keys are concerned.
      isDisabled: function () {
        return disabled || collapsed;
      },
      // pressing shows the hold bar for a Space press in flight. A take
      // already running has nothing to start, so it shows nothing.
      pressing: function (on) {
        if (on && (recorder || starting)) return;
        holdBar.hidden = !on;
      },
    };
  }

  // --- icons -------------------------------------------------------------
  //
  // Two line icons, drawn here because there is no icon set to draw
  // from. Decorative only: each button's text is its name, so the
  // picture is hidden from assistive technology, as the chart bars are.
  function icon(paths) {
    var svg = document.createElementNS(SVG_NS, "svg");
    svg.setAttribute("class", "voice-icon");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("focusable", "false");
    paths.forEach(function (d) {
      var path = document.createElementNS(SVG_NS, "path");
      path.setAttribute("d", d);
      svg.appendChild(path);
    });
    return svg;
  }

  // What the status box says when nothing is happening. Space is only
  // mentioned where a keyboard is likely, by the same test the
  // stylesheet uses to show the key hints.
  function idleHint() {
    var keyboard =
      typeof window.matchMedia === "function" &&
      window.matchMedia("(hover: hover) and (pointer: fine)").matches;
    return keyboard
      ? T.t("js.voice.idle.keyboard")
      : T.t("js.voice.idle.touch");
  }

  // As in respond.js: keys drawn as keycaps, with the words around
  // them in plain text. Parts are strings (words) or { key } (a
  // keycap). The whole is aria-hidden, so it never joins the button's
  // accessible name.
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

  // --- keys: hold Space, Esc Esc (story 80) ------------------------------
  //
  // respond.js's key layer exists only on a paged survey; a one-question
  // survey gets no shortcuts from it at all. Push-to-talk and the reset
  // gesture belong to the mic, so they are wired here, wherever the mic
  // is. Shift+Space stays with respond.js as the start/stop toggle.
  //
  // Plain Space is the hard case: inside the answer field it types a
  // space, and the caret is in that field almost always (respond.js
  // focuses it on every question, and the mic hands focus back to it).
  // So the key is claimed on the way down and decided on the way up: a
  // press shorter than HOLD_MS was a tap and types the space it would
  // have typed; a longer one was a hold and recorded meanwhile. The
  // costs are that a held key no longer auto-repeats spaces, and that a
  // tapped space lands on release rather than on press.
  function attachKeys(mics) {
    var press = null; // the Space press in flight: { mic, target, timer, held }

    // micFor finds the mic a key event is about: the one whose question
    // holds the focused element, or — with focus on <body>, which is
    // where the consent dialog leaves it — the only voice question in
    // view. On a paged survey that is always exactly one.
    function micFor(target) {
      if (target === document.body) {
        var visible = mics.filter(function (mic) {
          return !mic.question.hidden;
        });
        return visible.length === 1 ? visible[0] : null;
      }
      for (var i = 0; i < mics.length; i++) {
        if (mics[i].question.contains(target)) return mics[i];
      }
      return null;
    }

    function isVoiceControl(mic, target) {
      return target.tagName === "BUTTON" && mic.wrap.contains(target);
    }

    // Any dialog, not only the consent one: a dialog has its own Esc
    // and its own buttons, and a key pressed inside it is about the
    // dialog, whatever is recording behind it.
    function insideConsent(target) {
      return typeof target.closest === "function" && target.closest('[role="dialog"]') !== null;
    }

    document.addEventListener("keydown", function (event) {
      if (event.altKey || event.metaKey || event.ctrlKey) return;
      var target = event.target;
      if (!target || insideConsent(target)) return;
      // Shift+Esc resets. Esc alone is not claimed here: it leaves the
      // field (respond.js), and that is all it does.
      if (event.key === "Escape") {
        if (event.shiftKey) onReset(event, target);
        return;
      }
      if (event.shiftKey) return; // Shift+Space belongs to respond.js
      if (event.key === " ") onSpaceDown(event, target);
    });

    document.addEventListener("keyup", function (event) {
      if (event.key !== " " || !press) return;
      // Cancelling the keyup is what stops a focused button from firing
      // its own click on release; a tap clicks it deliberately below.
      event.preventDefault();
      endPress(false);
    });

    // A take must not stay open because the respondent switched windows
    // with the key still down: the keyup would go to the other window.
    window.addEventListener("blur", function () {
      if (press) endPress(true);
    });

    function onSpaceDown(event, target) {
      if (press) {
        // Auto-repeat while held. Typing nothing is the whole point.
        event.preventDefault();
        return;
      }
      var mic = micFor(target);
      if (!mic || mic.isDisabled()) return;
      if (target !== mic.field && target !== document.body && !isVoiceControl(mic, target)) return;
      event.preventDefault();
      press = { mic: mic, target: target, held: false, timer: 0 };
      mic.pressing(true);
      press.timer = window.setTimeout(function () {
        press.held = true;
        mic.hold();
      }, HOLD_MS);
    }

    function endPress(cancelled) {
      var current = press;
      press = null;
      window.clearTimeout(current.timer);
      current.mic.pressing(false);
      if (current.held) {
        current.mic.release();
        return;
      }
      if (cancelled) return;
      if (current.target === current.mic.field) insertSpace(current.mic.field);
      else if (current.target.tagName === "BUTTON") current.target.click();
    }

    // The space a tap would have typed, put where the caret is. The
    // input event is dispatched for the same reason writeAnswer does it:
    // the draft only hears about changes that announce themselves.
    function insertSpace(field) {
      var start = field.selectionStart;
      var end = field.selectionEnd;
      if (typeof start === "number" && typeof field.setRangeText === "function") {
        field.setRangeText(" ", start, end, "end");
      } else {
        field.value += " ";
      }
      field.dispatchEvent(new Event("input", { bubbles: true }));
    }

    // Shift+Esc clears the answer, in one press. A chord, because Esc by
    // itself has a use — it leaves the field — and a key that leaves on
    // one press must not destroy on the next. It applies to the field
    // and the voice controls, to the page body when one voice question
    // is in view (which is where Esc leaves a respondent), and to a take
    // that is live or being transcribed wherever focus happens to be.
    function onReset(event, target) {
      var mic = null;
      for (var i = 0; i < mics.length; i++) {
        if (mics[i].isRecording()) mic = mics[i];
      }
      if (!mic) {
        mic = micFor(target);
        if (!mic) return;
        if (target !== mic.field && target !== document.body && !isVoiceControl(mic, target)) return;
      }
      if (mic.isDisabled()) return;
      event.preventDefault();
      mic.clear();
    }
  }

  // --- choosing a microphone ---------------------------------------------
  //
  // The choice is remembered per browser, like consent. A device id is
  // an opaque, per-origin token the browser mints; it names nothing
  // outside this page and is sent nowhere.
  function collapsedPreference() {
    try {
      return window.localStorage.getItem(COLLAPSE_KEY) === "yes";
    } catch (err) {
      return false;
    }
  }

  function rememberCollapsed(on) {
    try {
      if (on) window.localStorage.setItem(COLLAPSE_KEY, "yes");
      else window.localStorage.removeItem(COLLAPSE_KEY);
    } catch (err) {
      // Harmless: the card opens expanded next time.
    }
  }

  function preferredDevice() {
    try {
      return window.localStorage.getItem(DEVICE_KEY) || "";
    } catch (err) {
      return "";
    }
  }

  function rememberDevice(id) {
    try {
      if (id) window.localStorage.setItem(DEVICE_KEY, id);
      else window.localStorage.removeItem(DEVICE_KEY);
    } catch (err) {
      // Harmless: the browser's default is used next time.
    }
  }

  // openMicrophone opens the chosen input when there is one, and falls
  // back to the browser's default when that device is gone: a headset
  // left at the office must not become "microphone unavailable". The
  // stale choice is forgotten so the fallback does not repeat.
  function openMicrophone() {
    var base = { channelCount: 1, echoCancellation: true, noiseSuppression: true };
    var wanted = preferredDevice();
    if (!wanted) return navigator.mediaDevices.getUserMedia({ audio: base });
    var exact = { deviceId: { exact: wanted } };
    for (var key in base) exact[key] = base[key];
    return navigator.mediaDevices.getUserMedia({ audio: exact }).catch(function () {
      rememberDevice("");
      return navigator.mediaDevices.getUserMedia({ audio: base });
    });
  }

  // listInputs names the microphones this page may use. Names are only
  // revealed once permission has been granted, so it is asked with a
  // stream already open; anything the browser withholds is left out.
  function listInputs() {
    if (typeof navigator.mediaDevices.enumerateDevices !== "function") return Promise.resolve([]);
    return navigator.mediaDevices.enumerateDevices().then(
      function (devices) {
        return devices.filter(function (device) {
          return device.kind === "audioinput" && device.label;
        });
      },
      function () {
        return [];
      }
    );
  }

  // currentDeviceId is the id of the device a stream is on, where the
  // browser reports it (Chrome and Firefox do; the fake device in the
  // browser suite does not).
  function currentDeviceId(stream) {
    var tracks = stream.getAudioTracks();
    if (!tracks.length || typeof tracks[0].getSettings !== "function") return "";
    return tracks[0].getSettings().deviceId || "";
  }

  // --- consent (M5-T3 / M8-T5) -------------------------------------------
  //
  // Asked once per browser, before the first getUserMedia call, and it
  // states the promise plainly: the voice is not stored.
  function askConsent(proceed) {
    var granted = false;
    try {
      granted = window.localStorage.getItem(CONSENT_KEY) === "yes";
    } catch (err) {
      granted = false; // storage blocked: ask every time rather than assume
    }
    if (granted) {
      proceed();
      return;
    }

    var dialog = document.createElement("div");
    dialog.className = "voice-consent";
    dialog.setAttribute("role", "dialog");
    dialog.setAttribute("aria-modal", "true");
    dialog.setAttribute("aria-labelledby", "voice-consent-title");

    var title = document.createElement("h2");
    title.id = "voice-consent-title";
    title.textContent = T.t("js.voice.consent.title");

    var body = document.createElement("p");
    body.textContent =
      T.t("js.voice.consent.body");

    var actions = document.createElement("div");
    actions.className = "voice-consent-actions";

    var cancel = document.createElement("button");
    cancel.type = "button";
    cancel.className = "secondary";
    cancel.textContent = T.t("js.voice.consent.decline");

    var accept = document.createElement("button");
    accept.type = "button";
    accept.textContent = T.t("js.voice.consent.accept");

    actions.appendChild(cancel);
    actions.appendChild(accept);
    dialog.appendChild(title);
    dialog.appendChild(body);
    dialog.appendChild(actions);
    document.body.appendChild(dialog);
    accept.focus();

    function close() {
      if (dialog.parentNode) dialog.parentNode.removeChild(dialog);
    }
    cancel.addEventListener("click", close);
    dialog.addEventListener("keydown", function (event) {
      if (event.key === "Escape") close();
    });
    accept.addEventListener("click", function () {
      try {
        window.localStorage.setItem(CONSENT_KEY, "yes");
      } catch (err) {
        // Harmless: consent is simply requested again next time.
      }
      close();
      proceed();
    });
  }

  // --- engine choice -----------------------------------------------------

  // chooseEngine asks the browser whether it can recognise this language
  // on the device, and believes only a definite yes. "downloadable" is a
  // no: the model is absent, and a respondent is never made to wait for
  // a download.
  function chooseEngine() {
    if (!localRecognition.available) return Promise.resolve("server");
    // Headless Chromium crashes its own renderer inside the availability
    // probe (reproduced in Playwright's build; a headed browser answers
    // it fine). Nothing under automation is going to speak into a
    // microphone anyway, so automated sessions take the server path —
    // which is what the browser suite is there to exercise.
    if (navigator.webdriver) return Promise.resolve("server");
    // Recognition on the device has to be told what language it is
    // listening for, and cannot work it out. Where the language of the
    // survey is not known, the server can.
    var lang = voiceLanguage();
    if (!lang) return Promise.resolve("server");
    var Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    var query;
    try {
      query = Recognition.available({ langs: [lang], processLocally: true });
    } catch (err) {
      return Promise.resolve("server");
    }
    return Promise.resolve(query).then(
      function (state) {
        return state === "available" ? "local" : "server";
      },
      function () {
        return "server";
      }
    );
  }

  // The language dictation listens for is the language of the
  // questions, which the form says: the one the respondent chose for the
  // survey, or nothing where they are reading it as it was written, whose
  // language is not recorded anywhere. Nothing is sent as nothing, and the
  // server works out what was said. It is read when a take begins, so it
  // is what the recogniser on the device is set to and what the server
  // is sent. The page's own declaration is read only by a page served
  // before the form said.
  function voiceLanguage() {
    var declared = form.getAttribute("data-voice-lang");
    if (declared !== null) return declared;
    return document.documentElement.lang || "";
  }

  // --- on-device recognition (M5-T1) -------------------------------------
  //
  // No socket, no server, no quota: the transcript appears from the
  // browser's own model. processLocally is set explicitly, so if the
  // browser cannot honour it the call fails rather than silently routing
  // the audio to a vendor.
  function startLocalRecognition(field, say, ui, done) {
    var Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    var recognition = new Recognition();
    recognition.processLocally = true;
    // Only reached with a language: chooseEngine sends the rest to the
    // server.
    recognition.lang = voiceLanguage();
    recognition.continuous = true;
    recognition.interimResults = false;

    var failed = false;
    var ended = false;
    var aborted = false;
    var monitor = null;

    recognition.onresult = function (event) {
      if (aborted) return;
      for (var i = event.resultIndex; i < event.results.length; i++) {
        if (event.results[i].isFinal) {
          // Each final result is a whole utterance, not a fragment, so
          // every one of them needs separating from what came before.
          writeAnswer(field, joinTakes(field.value, event.results[i][0].transcript));
        }
      }
    };
    recognition.onerror = function () {
      if (aborted) return;
      failed = true;
      ui.fail(T.t("js.voice.unrecognised"));
    };
    recognition.onend = function () {
      ended = true;
      closeMonitor(monitor);
      if (!failed && !aborted) {
        say(T.t("js.voice.transcribed.device"));
        field.focus();
      }
      done();
    };

    try {
      recognition.start();
    } catch (err) {
      return Promise.reject(err);
    }
    // The recogniser opens the microphone itself and never says which
    // one. A second, monitor-only capture of the same default device
    // feeds the spectrum; if it cannot be opened, recognition runs
    // without the picture rather than not at all.
    return openMonitor().then(function (opened) {
      if (ended) {
        closeMonitor(opened);
        opened = null;
      }
      monitor = opened;
      return {
        monitor: monitor,
        stop: function () {
          ui.transcribing();
          say(T.t("js.voice.finishing"));
          recognition.stop();
        },
        // Reset mid-take: whatever was said is dropped, not transcribed.
        abort: function () {
          aborted = true;
          recognition.abort();
        },
        // Results here land while the respondent is still speaking, and
        // were read as they did; there is nothing in flight to take back.
        cancel: function () {
          aborted = true;
          recognition.abort();
        },
      };
    });
  }

  function openMonitor() {
    return navigator.mediaDevices.getUserMedia({ audio: true }).then(
      function (stream) {
        var context = new (window.AudioContext || window.webkitAudioContext)();
        return { context: context, stream: stream, device: deviceName(stream) };
      },
      function () {
        return null;
      }
    );
  }

  function closeMonitor(monitor) {
    if (!monitor) return;
    monitor.stream.getTracks().forEach(function (track) {
      track.stop();
    });
    if (monitor.context.state !== "closed") monitor.context.close();
  }

  // deviceName is the label the browser gives the capture track: the
  // device's own name once permission is granted, empty on browsers that
  // withhold it.
  function deviceName(stream) {
    var tracks = stream.getAudioTracks();
    return tracks.length ? tracks[0].label || "" : "";
  }

  // --- capture -----------------------------------------------------------

  // joinTakes puts a take after whatever is already in the field —
  // typed or spoken — without gluing two sentences together and without
  // adding a stray space to an empty field.
  function joinTakes(existing, text) {
    if (!existing) return text;
    if (/\s$/.test(existing) || /^\s/.test(text)) return existing + text;
    return existing + " " + text;
  }

  // Setting field.value from script does not fire an input event, and
  // anything listening for one therefore never hears about a spoken
  // answer — including the draft that keeps answers across a reload
  // (story 79). A transcript is the most expensive answer to lose and
  // the last one anybody wants to repeat, so say it out loud.
  function writeAnswer(field, value) {
    field.value = value;
    field.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function startRecording(field, say, ui, done) {
    var spoken = false; // has this take put anything in the field yet?
    var discarded = false; // reset mid-take: ignore whatever still arrives
    var written = ""; // what this take has put in the field, separator included
    return openMicrophone().then(function (stream) {
        var context = new (window.AudioContext || window.webkitAudioContext)({
          sampleRate: SAMPLE_RATE,
        });
        // A take begins when the server has its start message, not when
        // the microphone opens: audio is never queued, so everything
        // spoken before the socket is open is lost, and a stop sent
        // during the handshake would reach a session that has not
        // started. Until then the respondent sees T.t("js.voice.starting") and no
        // stop control. opened settles false when the take ends first.
        var announceOpen;
        var opened = new Promise(function (resolve) {
          announceOpen = resolve;
        });
        var socket = window.EarfulSocket.open(voicePath, {
          onOpen: function () {
            socket.send({
              action: "start",
              params: {
                token: form.querySelector('[name="form_ts"]').value,
                nonce: form.querySelector('[name="form_nonce"]').value,
                lang: voiceLanguage(),
              },
            });
            announceOpen(true);
          },
          onStatus: say,
          onChunk: function (text) {
            if (discarded) return;
            // The transcript lands in the textarea as it arrives, so the
            // respondent reads and edits their own words before
            // submitting (story 36).
            //
            // Only the FIRST chunk of a take gets a separator: the rest
            // are fragments of one sentence and must join seamlessly, or
            // words break apart mid-transcription. Without this, a second
            // take ran straight into the first — "…can you hear me?Yes"
            // — which is what a respondent saw in production.
            if (!spoken) {
              spoken = true;
              var joined = joinTakes(field.value, text);
              written = joined.slice(field.value.length);
              writeAnswer(field, joined);
              return;
            }
            written += text;
            writeAnswer(field, field.value + text);
          },
          onDone: function () {
            if (discarded) return;
            say(T.t("js.voice.transcribed.server"));
            field.focus();
            cleanup();
          },
          onError: function (message) {
            if (discarded) return;
            ui.fail(message || T.t("voice.error.unavailable"));
            cleanup();
          },
          onGone: function () {
            if (discarded) return;
            ui.fail(T.t("js.voice.lost"));
            cleanup();
          },
        });

        // Every way a take ends comes through here, and the socket goes
        // with it: a take is one session, and a socket left open after
        // its done or error frame would reconnect and send start again.
        var cleaned = false;
        function cleanup() {
          if (cleaned) return;
          cleaned = true;
          socket.close();
          stream.getTracks().forEach(function (track) {
            track.stop();
          });
          if (context.state !== "closed") context.close();
          announceOpen(false);
          done();
        }

        var stopTimer = window.setTimeout(function () {
          say(T.t("js.voice.longest"));
          finish();
        }, maxSeconds * 1000);

        function finish() {
          window.clearTimeout(stopTimer);
          socket.send({ action: "stop" });
          stream.getTracks().forEach(function (track) {
            track.stop();
          });
          ui.transcribing();
          say(T.t("voice.status.transcribing"));
        }

        // abort is Reset mid-take: the socket is closed without a stop,
        // so the server transcribes nothing, and anything it had already
        // sent back is ignored rather than landing in a cleared field.
        function abort() {
          discarded = true;
          window.clearTimeout(stopTimer);
          socket.close();
          cleanup();
        }

        // cancel is abort that tidies up after itself: the part of the
        // transcript that had already arrived is taken back, provided it
        // is still the end of the answer. If the respondent has edited
        // past it, their edit wins and the text stays.
        function cancel() {
          var value = field.value;
          abort();
          if (written && value.slice(-written.length) === written) {
            writeAnswer(field, value.slice(0, value.length - written.length));
          }
        }

        // The device list is fetched alongside the capture graph, not
        // before it: names are only available once this stream is open.
        // On-device recognition (startLocalRecognition) opens its own
        // input and cannot be pointed at a device, so it lists none.
        return Promise.all([pump(context, stream, socket), listInputs(), opened]).then(function (
          results
        ) {
          // The take ended before the connection opened; what went wrong
          // is already on the status line.
          if (!results[2]) return null;
          return {
            stop: finish,
            abort: abort,
            cancel: cancel,
            inputs: results[1],
            monitor: {
              context: context,
              stream: stream,
              device: deviceName(stream),
              deviceId: currentDeviceId(stream),
            },
          };
        });
      });
  }

  // pump wires the microphone to the socket as 16-bit PCM. It prefers an
  // AudioWorklet and falls back to ScriptProcessor for older Safari; the
  // worklet module is same-origin, so the CSP needs no exception.
  function pump(context, stream, socket) {
    var source = context.createMediaStreamSource(stream);

    function sendSamples(samples) {
      var pcm = new DataView(new ArrayBuffer(samples.length * 2));
      for (var i = 0; i < samples.length; i++) {
        var s = Math.max(-1, Math.min(1, samples[i]));
        pcm.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
      }
      socket.sendBinary(pcm.buffer);
    }

    if (context.audioWorklet) {
      return context.audioWorklet
        .addModule(form.getAttribute("data-voice-worklet") || "/static/js/pcm-worklet.js")
        .then(function () {
          var node = new AudioWorkletNode(context, "earful-pcm");
          node.port.onmessage = function (event) {
            sendSamples(event.data);
          };
          source.connect(node);
          // Keep the graph alive without playing anything back.
          node.connect(context.destination);
        })
        .catch(function () {
          scriptProcessor();
        });
    }
    scriptProcessor();
    return Promise.resolve();

    function scriptProcessor() {
      var node = context.createScriptProcessor(4096, 1, 1);
      node.onaudioprocess = function (event) {
        sendSamples(event.inputBuffer.getChannelData(0));
      };
      source.connect(node);
      node.connect(context.destination);
    }
  }

  // --- the live picture --------------------------------------------------

  // startMeter draws the input's spectrum onto the canvas until stopped
  // and hands every frame's level to the quiet watcher. The analyser is a
  // second tap on the stream, in parallel with the capture graph, so it
  // cannot change a sample of what is transcribed.
  function startMeter(context, stream, canvas, onQuiet) {
    var analyser = context.createAnalyser();
    // 1024 points is 64 ms at 16 kHz: fine enough to look alive, and it
    // leaves enough bins under 4 kHz to fill the bars whatever rate the
    // context runs at (the monitor-only context uses the device's own).
    analyser.fftSize = 1024;
    analyser.smoothingTimeConstant = 0.6;
    context.createMediaStreamSource(stream).connect(analyser);

    var bins = new Uint8Array(analyser.frequencyBinCount);
    var wave = new Uint8Array(analyser.fftSize);
    // Speech lives below 4 kHz; bins above it would be a flat strip of
    // nothing taking up half the picture.
    var usable = Math.max(1, Math.round((4000 / (context.sampleRate / 2)) * bins.length));
    var bars = Math.min(32, usable);
    var perBar = Math.floor(usable / bars);

    // A coarser meter under reduced motion, not a frozen one: this is a
    // live level, the thing the respondent is watching for, not
    // decoration.
    var reduced =
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    var frameGap = reduced ? 125 : 0;

    var scale = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.round(canvas.clientWidth * scale));
    canvas.height = Math.max(1, Math.round(canvas.clientHeight * scale));
    var paint = canvas.getContext("2d");
    var colour = getComputedStyle(canvas).getPropertyValue("--accent").trim() || "#6d4aff";
    var gap = Math.max(1, Math.round(scale));
    var barWidth = (canvas.width - gap * (bars - 1)) / bars;

    var watch = quietWatch();
    var stopped = false;
    var last = -Infinity;
    var frame = 0;

    function draw(now) {
      if (stopped) return;
      frame = window.requestAnimationFrame(draw);
      if (now - last < frameGap) return;
      last = now;

      analyser.getByteTimeDomainData(wave);
      var peak = 0;
      for (var i = 0; i < wave.length; i++) {
        var amplitude = Math.abs(wave[i] - 128) / 128;
        if (amplitude > peak) peak = amplitude;
      }
      if (watch.sample(peak, now)) onQuiet();

      analyser.getByteFrequencyData(bins);
      paint.clearRect(0, 0, canvas.width, canvas.height);
      paint.fillStyle = colour;
      for (var b = 0; b < bars; b++) {
        var sum = 0;
        for (var k = 0; k < perBar; k++) sum += bins[b * perBar + k];
        var level = sum / perBar / 255;
        var height = Math.max(gap, level * canvas.height);
        paint.fillRect(b * (barWidth + gap), canvas.height - height, barWidth, height);
      }
    }
    frame = window.requestAnimationFrame(draw);

    return {
      stop: function () {
        stopped = true;
        window.cancelAnimationFrame(frame);
        analyser.disconnect();
        paint.clearRect(0, 0, canvas.width, canvas.height);
      },
    };
  }

  // quietWatch decides when a take has been silent for long enough that
  // the respondent should be told, instead of finding out from an empty
  // transcript. sample(peak, now) is called once per drawn frame with
  // that frame's peak amplitude (0 is digital silence, 1 is full scale)
  // and the frame's timestamp in milliseconds; it returns true when the
  // warning is due. The caller announces the warning every time it
  // returns true.
  //
  // A live microphone in a quiet room still shows a small noise floor;
  // a muted, unplugged or wrong device sits at exactly zero. The floor
  // is well under the threshold here, so the warning means "this input
  // is dead", not "you are speaking softly". Three seconds is long
  // enough for a respondent to gather their thoughts before speaking,
  // and the timer restarts whenever sound returns, so a pause mid-answer
  // passes unremarked. The warning fires once per take: the status line
  // would otherwise repeat it on every frame.
  function quietWatch() {
    var THRESHOLD = 0.02;
    var PATIENCE = 3000;
    var quietSince = null;
    var warned = false;
    return {
      sample: function (peak, now) {
        if (peak > THRESHOLD) {
          quietSince = null;
          return false;
        }
        if (quietSince === null) quietSince = now;
        if (warned || now - quietSince < PATIENCE) return false;
        warned = true;
        return true;
      },
    };
  }

  // Exposed so docs/voice-support.md's matrix can be filled in from real
  // browsers rather than from assumptions: open a survey and read
  // window.EarfulVoice.localRecognition in the console.
  window.EarfulVoice.localRecognition = localRecognition;
})();
