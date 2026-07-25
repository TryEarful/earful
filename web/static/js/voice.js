// Spoken answers (M5, ADR-0004).
//
// Everything visible here is built by this script, so a respondent
// without JavaScript never sees a control they cannot use: the server
// renders the textarea and nothing else. Typing always works, and every
// failure — no microphone, permission refused, quota exhausted, socket
// gone — ends by saying "type your answer instead" (story 38, 39).
//
// Recognition happens on the device when the browser can prove it is
// local; otherwise the audio goes to our EU transcription and is
// discarded (ADR-0004). Non-local browser recognition is never used, so
// nobody's voice reaches Google or Apple without being told.
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

  // --- local recognition detection (M5-T1) -------------------------------
  //
  // Exposed for the browser test: it is a pure function of a window-like
  // object, so it can be checked against stubs instead of against six
  // real browsers.
  function detectLocalRecognition(win) {
    var Recognition = win.SpeechRecognition || win.webkitSpeechRecognition;
    if (!Recognition) return { available: false, reason: "no-api" };
    // On-device availability is a newer, separate capability. Without a
    // way to *prove* the audio stays on the device, we treat browser
    // recognition as unavailable rather than risk sending a respondent's
    // voice to a third party (ADR-0004).
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

  Array.prototype.slice
    .call(form.querySelectorAll('.respond-question[data-voice="1"]'))
    .forEach(function (question) {
      var field = question.querySelector("textarea, input[type=text]");
      if (field) attachMic(question, field);
    });

  function attachMic(question, field) {
    var wrap = document.createElement("div");
    wrap.className = "voice";

    var button = document.createElement("button");
    button.type = "button";
    button.className = "voice-button secondary";
    button.textContent = "Answer by speaking";

    var status = document.createElement("span");
    status.className = "voice-status";
    // Recording state and transcription progress are announced, not just
    // shown: this control is unusable otherwise.
    status.setAttribute("aria-live", "polite");

    wrap.appendChild(button);
    wrap.appendChild(status);
    field.parentNode.insertBefore(wrap, field.nextSibling);

    var recorder = null;

    button.addEventListener("click", function () {
      if (recorder) {
        stop();
        return;
      }
      askConsent(function () {
        start();
      });
    });

    function say(message) {
      status.textContent = message || "";
    }

    function reset() {
      recorder = null;
      button.textContent = "Answer by speaking";
      button.classList.remove("recording");
    }

    function stop() {
      if (!recorder) return;
      var current = recorder;
      recorder = null;
      button.textContent = "Answer by speaking";
      button.classList.remove("recording");
      current.stop();
    }

    function start() {
      say("Starting…");
      // On-device first, when the browser can prove it (ADR-0004): the
      // respondent's voice then never leaves their machine at all.
      // Otherwise the audio streams to our EU transcription.
      chooseEngine()
        .then(function (engine) {
          if (engine === "local") {
            return startLocalRecognition(field, say, function done() {
              reset();
            });
          }
          return startRecording(field, say, function done() {
            reset();
          });
        })
        .then(
          function (handle) {
            if (!handle) {
              reset();
              return;
            }
            recorder = handle;
            button.textContent = "Stop and transcribe";
            button.classList.add("recording");
            say("Listening… speak now.");
          },
          function () {
            reset();
            say("Microphone unavailable — please type your answer.");
          }
        );
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
        // Fine: we simply ask again next time.
      }
      close();
      proceed();
    });
  }

  // --- engine choice -----------------------------------------------------

  // chooseEngine asks the browser whether it can recognise this language
  // on the device, and believes only a definite yes. "downloadable" is a
  // no: the model is not there, and we will not stall a respondent
  // waiting for a download.
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
  function startLocalRecognition(field, say, done) {
    var Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    var recognition = new Recognition();
    recognition.processLocally = true;
    recognition.lang = pageLanguage();
    recognition.continuous = true;
    recognition.interimResults = false;

    var failed = false;

    recognition.onresult = function (event) {
      for (var i = event.resultIndex; i < event.results.length; i++) {
        if (event.results[i].isFinal) {
          // Each final result is a whole utterance, not a fragment, so
          // every one of them needs separating from what came before.
          writeAnswer(field, joinTakes(field.value, event.results[i][0].transcript));
        }
      }
    };
    recognition.onerror = function () {
      failed = true;
      say("Couldn't recognise that — please type your answer.");
    };
    recognition.onend = function () {
      if (!failed) {
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
    return Promise.resolve({
      stop: function () {
        say("Finishing…");
        recognition.stop();
      },
    });
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

  function startRecording(field, say, done) {
    var spoken = false; // has this take put anything in the field yet?
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
            say("Transcribed — edit it if it isn't quite right.");
            field.focus();
            cleanup();
          },
          onError: function (message) {
            say(message || "Voice isn't available right now — please type your answer.");
            cleanup();
          },
          onGone: function () {
            say("Connection lost — please type your answer.");
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
          say("Transcribing…");
        }

        return pump(context, stream, socket).then(function () {
          return { stop: finish };
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

  // Exposed so docs/voice-support.md's matrix can be filled in from real
  // browsers rather than from assumptions: open a survey and read
  // window.EarfulVoice.localRecognition in the console.
  window.EarfulVoice.localRecognition = localRecognition;
})();
