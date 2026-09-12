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
    // The label is its own text node so the key hint beside it survives
    // every label change; setting button.textContent would delete it.
    var label = document.createTextNode("Answer by speaking");
    button.appendChild(label);
    // Shift+Space toggles recording (story 80); respond.js owns the key,
    // this is only the label for it. aria-hidden so the button is named
    // "Answer by speaking", not "Answer by speaking ⇧Space".
    var micHint = document.createElement("span");
    micHint.className = "key-hint";
    micHint.setAttribute("aria-hidden", "true");
    micHint.textContent = "⇧Space";
    button.appendChild(micHint);

    function setLabel(text) {
      label.nodeValue = text;
    }

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
        stopMeter();
        progress.hidden = false;
      },
      settled: function () {
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

    var recorder = null;

    button.addEventListener("click", function () {
      if (recorder) {
        stop();
        return;
      }
      askConsent(function () {
        // The consent dialog took focus and is now gone. Put it back on
        // the field the words are about to land in, so the respondent
        // can edit as they speak and the keyboard shortcuts keep working
        // instead of talking to <body>.
        field.focus();
        start();
      });
    });

    function say(message) {
      status.textContent = message || "";
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

    function start() {
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
            if (!handle) {
              reset();
              return;
            }
            recorder = handle;
            setLabel("Stop and transcribe");
            button.classList.add("recording");
            ui.recording(handle);
            var device = handle.monitor && handle.monitor.device;
            say(device ? "Listening on " + device + "… speak now." : "Listening… speak now.");
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
    var monitor = null;

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
      ended = true;
      closeMonitor(monitor);
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
          ui.transcribing();
          say("Transcribing…");
        }

        return pump(context, stream, socket).then(function () {
          return {
            stop: finish,
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
