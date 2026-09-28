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

  var form = document.querySelector(".respond-form");
  if (!form) return;
  var voicePath = form.getAttribute("data-voice-path");
  if (!voicePath) return;

  var maxSeconds = parseInt(form.getAttribute("data-voice-max-seconds"), 10) || 120;
  var CONSENT_KEY = "earful-voice-consent";
  var SAMPLE_RATE = 16000;
  var RECORDING_HINT = "Recording in progress. Your transcription will be shown here.";
  var HOLD_MS = 250; // a Space press longer than this is a hold, not a tap
  var ESC_WINDOW_MS = 700; // two Esc presses this close together clear the answer
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

  var mics = [];
  Array.prototype.slice
    .call(form.querySelectorAll('.respond-question[data-voice="1"]'))
    .forEach(function (question) {
      var field = question.querySelector("textarea, input[type=text]");
      if (field) mics.push(attachMic(question, field));
    });
  if (mics.length) attachKeys(mics);

  function attachMic(question, field) {
    var wrap = document.createElement("div");
    wrap.className = "voice";
    // idle, recording or transcribing: one attribute the stylesheet and
    // the browser suite can both read, instead of inferring the state
    // from which pieces happen to be hidden.
    wrap.setAttribute("data-state", "idle");

    var button = document.createElement("button");
    button.type = "button";
    button.className = "voice-button secondary";
    button.appendChild(icon(MIC_ICON));
    // The label is its own text node so the key hint beside it survives
    // every label change; setting button.textContent would delete it.
    var label = document.createTextNode("Answer by speaking");
    button.appendChild(label);
    // Holding Space records for as long as it is held (attachKeys);
    // Shift+Space, which respond.js owns, still starts and stops a take.
    // The hint is aria-hidden so the button is named "Answer by
    // speaking", not "Answer by speaking Hold Space".
    button.appendChild(keyHint("Hold Space"));

    function setLabel(text) {
      label.nodeValue = text;
    }

    // Starting over. Two presses of Esc do the same, so a respondent who
    // dictated the wrong thing is one gesture from a blank field rather
    // than a paragraph of deleting.
    var resetButton = document.createElement("button");
    resetButton.type = "button";
    resetButton.className = "voice-reset secondary";
    resetButton.appendChild(icon(TRASH_ICON));
    resetButton.appendChild(document.createTextNode("Reset"));
    resetButton.appendChild(keyHint("Esc Esc"));

    var status = document.createElement("span");
    status.className = "voice-status";
    // Recording state and transcription progress are announced, not just
    // shown: this control is unusable otherwise.
    status.setAttribute("aria-live", "polite");

    // Transcription reports no progress — the model answers when it
    // answers — so this bar is indeterminate on purpose. A percentage
    // here would be invented, and inventing one on the single screen
    // where this product asks to be trusted is a poor trade for a few
    // seconds of reassurance. aria-hidden because the status line beside
    // it already announces "Transcribing…"; a screen reader does not
    // need the same fact twice.
    var progress = document.createElement("span");
    progress.className = "voice-progress";
    progress.setAttribute("aria-hidden", "true");
    progress.hidden = true;

    // What the microphone is hearing, and which microphone. The browser
    // picks the input device and says nothing about it: a headset left
    // paired, or a meeting app's virtual device, is chosen as readily as
    // the built-in microphone, and the only sign of a wrong choice is a
    // transcript that comes back empty. A live spectrum and the device's
    // name make it visible while there is still time to fix it.
    var monitor = document.createElement("span");
    monitor.className = "voice-monitor";
    monitor.hidden = true;

    var spectrum = document.createElement("canvas");
    spectrum.className = "voice-spectrum";
    // The status line announces the device name; the bars only repeat
    // what a sighted respondent can already hear.
    spectrum.setAttribute("aria-hidden", "true");

    var input = document.createElement("span");
    input.className = "voice-input";

    monitor.appendChild(spectrum);
    monitor.appendChild(input);

    wrap.appendChild(button);
    wrap.appendChild(resetButton);
    wrap.appendChild(status);
    wrap.appendChild(monitor);
    wrap.appendChild(progress);
    field.parentNode.insertBefore(wrap, field.nextSibling);

    // While a take is running the field says what is about to land in it.
    // A browser hides a placeholder as soon as the field has content, so
    // the first transcribed word clears this without any help from here.
    var placeholder = field.getAttribute("placeholder");
    var meter = null;
    var ui = {
      recording: function (handle) {
        wrap.setAttribute("data-state", "recording");
        // The box the words land in lights up too: on a long answer the
        // button can be scrolled out of view while the microphone is
        // still open.
        field.classList.add("voice-live");
        field.setAttribute("placeholder", RECORDING_HINT);
        progress.hidden = true;
        if (!handle.monitor) return;
        var device = handle.monitor.device;
        input.textContent = device ? "Input: " + device : "";
        // Unhidden before the meter starts, so the canvas has a size to
        // read; a hidden element measures zero by zero.
        monitor.hidden = false;
        meter = startMeter(handle.monitor.context, handle.monitor.stream, spectrum, function quiet() {
          say(
            "Nothing is coming through" +
              (device ? " " + device : "") +
              " — check which microphone your browser is using, or type your answer."
          );
        });
      },
      transcribing: function () {
        wrap.setAttribute("data-state", "transcribing");
        field.classList.remove("voice-live");
        stopMeter();
        progress.hidden = false;
      },
      // fail is the status line for something that went wrong: the same
      // line, boxed and bordered, so it is not read as a progress update.
      fail: function (message) {
        status.textContent = message;
        status.classList.add("voice-error");
      },
      settled: function () {
        wrap.setAttribute("data-state", "idle");
        field.classList.remove("voice-live");
        if (placeholder === null) field.removeAttribute("placeholder");
        else field.setAttribute("placeholder", placeholder);
        stopMeter();
        progress.hidden = true;
      },
    };

    function stopMeter() {
      if (meter) meter.stop();
      meter = null;
      monitor.hidden = true;
    }

    var recorder = null; // the live take, once the microphone is open
    var starting = null; // a start() still opening the microphone

    button.addEventListener("click", function () {
      if (recorder) {
        stop();
        return;
      }
      if (starting) return;
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
      status.classList.remove("voice-error");
      status.textContent = message || "";
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
      wrap.setAttribute("data-state", "unavailable");
      ui.fail(message);
    }

    function reset() {
      recorder = null;
      setLabel("Answer by speaking");
      button.classList.remove("recording");
      ui.settled();
    }

    function stop() {
      if (!recorder) return;
      var current = recorder;
      recorder = null;
      setLabel("Answer by speaking");
      button.classList.remove("recording");
      current.stop();
    }

    // hold and release are the two ends of a held Space. A take begun
    // this way ends when the key comes up; one begun by a click or
    // Shift+Space is a toggle and pays the key no attention.
    function hold() {
      if (recorder || starting) return;
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
      if (recorder) {
        var live = recorder;
        recorder = null;
        live.abort();
      }
      writeAnswer(field, "");
      reset();
      say("Cleared — type or speak your answer again.");
      field.focus();
    }

    function start(mode) {
      var pending = { cancelled: null };
      starting = pending;
      say("Starting…");
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
                say("Hold Space while you speak, and release it when you are done.");
              }
              return;
            }
            handle.mode = mode;
            recorder = handle;
            setLabel(mode === "hold" ? "Release Space to transcribe" : "Stop and transcribe");
            button.classList.add("recording");
            ui.recording(handle);
            var device = handle.monitor && handle.monitor.device;
            say(device ? "Listening on " + device + "… speak now." : "Listening… speak now.");
          },
          function () {
            starting = null;
            reset();
            disable("Microphone unavailable — please type your answer.");
          }
        );
    }

    return {
      question: question,
      field: field,
      wrap: wrap,
      hold: hold,
      release: release,
      clear: clear,
      say: say,
      isRecording: function () {
        return recorder !== null;
      },
      isDisabled: function () {
        return disabled;
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

  // As in respond.js and the template: the hint is aria-hidden, so it
  // never joins the button's accessible name.
  function keyHint(text) {
    var hint = document.createElement("span");
    hint.className = "key-hint";
    hint.setAttribute("aria-hidden", "true");
    hint.textContent = text;
    return hint;
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
    var armed = null; // after a first Esc: { mic, timer }

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

    function insideConsent(target) {
      return typeof target.closest === "function" && target.closest(".voice-consent") !== null;
    }

    document.addEventListener("keydown", function (event) {
      if (event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return;
      var target = event.target;
      if (!target || insideConsent(target)) return;
      if (event.key === " ") onSpaceDown(event, target);
      else if (event.key === "Escape") onEscape(event, target);
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
      press.timer = window.setTimeout(function () {
        press.held = true;
        mic.hold();
      }, HOLD_MS);
    }

    function endPress(cancelled) {
      var current = press;
      press = null;
      window.clearTimeout(current.timer);
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

    // Esc twice clears the answer. It applies to the field and the voice
    // controls, and to a live take wherever focus happens to be — while
    // recording, the take is what the respondent is interacting with.
    // One Esc arms and says so; the second inside the window clears.
    function onEscape(event, target) {
      var mic = null;
      for (var i = 0; i < mics.length; i++) {
        if (mics[i].isRecording()) mic = mics[i];
      }
      if (!mic) {
        mic = micFor(target);
        if (!mic || (target !== mic.field && !isVoiceControl(mic, target))) return;
      }
      if (mic.isDisabled()) return;
      event.preventDefault();
      if (armed && armed.mic === mic) {
        disarm();
        mic.clear();
        return;
      }
      disarm();
      armed = { mic: mic, timer: window.setTimeout(disarm, ESC_WINDOW_MS) };
      mic.say("Press Esc again to clear this answer.");
    }

    function disarm() {
      if (!armed) return;
      window.clearTimeout(armed.timer);
      armed = null;
    }
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
    title.textContent = "Answer by speaking";

    var body = document.createElement("p");
    body.textContent =
      "Your browser will ask for microphone access. What you say is turned into " +
      "text you can read and edit before it becomes your answer. Your voice is " +
      "never stored — no recording is kept, here or anywhere else. Typing stays " +
      "available at any time.";

    var actions = document.createElement("div");
    actions.className = "voice-consent-actions";

    var cancel = document.createElement("button");
    cancel.type = "button";
    cancel.className = "secondary";
    cancel.textContent = "Not now";

    var accept = document.createElement("button");
    accept.type = "button";
    accept.textContent = "Use the microphone";

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
    var Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    var lang = pageLanguage();
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

  function pageLanguage() {
    return document.documentElement.lang || "en";
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
    recognition.lang = pageLanguage();
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
      ui.fail("Couldn't recognise that — please type your answer.");
    };
    recognition.onend = function () {
      ended = true;
      closeMonitor(monitor);
      if (!failed && !aborted) {
        say("Transcribed on your device — edit it if it isn't quite right.");
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
          say("Finishing…");
          recognition.stop();
        },
        // Reset mid-take: whatever was said is dropped, not transcribed.
        abort: function () {
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
    return navigator.mediaDevices
      .getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } })
      .then(function (stream) {
        var context = new (window.AudioContext || window.webkitAudioContext)({
          sampleRate: SAMPLE_RATE,
        });
        var socket = window.EarfulSocket.open(voicePath, {
          onOpen: function () {
            socket.send({
              action: "start",
              params: {
                token: form.querySelector('[name="form_ts"]').value,
                nonce: form.querySelector('[name="form_nonce"]').value,
                lang: document.documentElement.lang || "",
              },
            });
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
              writeAnswer(field, joinTakes(field.value, text));
              return;
            }
            writeAnswer(field, field.value + text);
          },
          onDone: function () {
            if (discarded) return;
            say("Transcribed — edit it if it isn't quite right.");
            field.focus();
            cleanup();
          },
          onError: function (message) {
            if (discarded) return;
            ui.fail(message || "Voice isn't available right now — please type your answer.");
            cleanup();
          },
          onGone: function () {
            if (discarded) return;
            ui.fail("Connection lost — please type your answer.");
            cleanup();
          },
        });

        var cleaned = false;
        function cleanup() {
          if (cleaned) return;
          cleaned = true;
          stream.getTracks().forEach(function (track) {
            track.stop();
          });
          if (context.state !== "closed") context.close();
          done();
        }

        var stopTimer = window.setTimeout(function () {
          say("That's the longest answer I can take — transcribing.");
          finish();
        }, maxSeconds * 1000);

        function finish() {
          window.clearTimeout(stopTimer);
          socket.send({ action: "stop" });
          stream.getTracks().forEach(function (track) {
            track.stop();
          });
          ui.transcribing();
          say("Transcribing…");
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

        return pump(context, stream, socket).then(function () {
          return {
            stop: finish,
            abort: abort,
            monitor: { context: context, stream: stream, device: deviceName(stream) },
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
