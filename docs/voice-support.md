# Voice support matrix

How spoken answers behave per browser, and why. The rule underneath
everything is ADR-0004: **audio is transcribed and immediately discarded,
and browser recognition is used only when the browser can prove it runs
on the device.** An unproven guarantee counts as no guarantee.

## The decision, in order

`detectLocalRecognition(window)` in
[`web/static/js/voice.js`](../web/static/js/voice.js) answers one
question — can this browser recognise speech *without sending it
anywhere?* — and it answers "no" unless it can be sure:

1. No `SpeechRecognition` / `webkitSpeechRecognition` → **no** (`no-api`).
2. No on-device availability API (`SpeechRecognition.available`) → **no**
   (`no-locality-guarantee`). This is the case that matters: Chrome's
   classic Web Speech API streams audio to Google's servers, and Safari's
   streams to Apple's. Both would work, and both would quietly route a
   respondent's voice to a third party. We treat them as absent.
3. No `processLocally` on the prototype → **no** (`no-locality-guarantee`).
4. Otherwise → **on-device** available.

When the detector says yes, the engine choice asks one more question at
the moment of use: `SpeechRecognition.available({langs, processLocally:
true})` must answer exactly `"available"`. `"downloadable"` counts as a
no — the model is not there, and a respondent will not be left waiting
for a download. On-device recognition then runs with `processLocally =
true` set explicitly, so a browser that cannot honour it fails instead of
quietly routing the audio to a vendor, and the failure falls back to
typing.

Everything else takes the server path, which therefore has to be
production-grade regardless (ADR-0004's own consequence note).

One workaround worth knowing about: **headless Chromium crashes its own
renderer inside `SpeechRecognition.available()`** (reproduced against
Playwright's bundled build; the same call in a headed browser answers
normally — `"downloadable"` on a machine without the model). Automated
sessions (`navigator.webdriver`) therefore skip the probe and take the
server path. Nothing under automation speaks into a microphone, and the
server path is what the browser suite exists to exercise. Revisit when
the crash is fixed upstream.

## The server path

Microphone → `AudioWorklet` → 16 kHz mono PCM → WebSocket → in-memory
buffer → one `Transcribe` call → transcript streamed back into the
textarea, which the respondent edits before submitting.

PCM rather than `MediaRecorder`'s WebM/Opus because whisper.cpp and
Vertex both take PCM/WAV directly: no transcoder in the container, no
ffmpeg dependency for self-hosters. The worklet module is served from our
own origin, so the CSP needs no exception (ADR-0006).

## Matrix

| Browser | Microphone capture | Local recognition | What a respondent gets |
|---|---|---|---|
| Chrome (desktop) | AudioWorklet | API present; used when the language model is installed | On-device if available, else server |
| Edge (desktop) | AudioWorklet | API present | On-device if available, else server |
| Firefox (desktop) | AudioWorklet | no API | Server transcription |
| Safari (desktop) | AudioWorklet | no locality guarantee | Server transcription |
| Safari (iOS 14.5+) | AudioWorklet | no locality guarantee | Server transcription |
| Chrome (Android) | AudioWorklet | API present | On-device if available, else server |
| Older Safari (<14.1) | ScriptProcessor fallback | — | Server transcription |
| No `getUserMedia`, no `AudioContext`, no WebSocket, or JS off | — | — | **Typing only — no mic is rendered** |

The columns to fill in from a real browser are the third and fourth: open
a survey, run `EarfulVoice.localRecognition` in the console, then speak an
answer and note whether the status line says "Transcribed on your device"
(local) or "Transcribing…" (server).

The last row is the important one: the mic is built by JavaScript, so a
browser that cannot record never sees a control it cannot use, and the
textarea the server rendered is the whole interface (story 38).

## Manual check, per browser

1. `AI_PROVIDER=scripted TRANSCRIBE_PROVIDER=scripted docker compose --profile app up`
   (or point `TRANSCRIBE_PROVIDER` at a real whisper-cli model).
2. Open a published survey's share link with a long-text question.
3. **Consent**: the first click on *Dictate* shows the consent
   dialog and it says the voice is never stored. Decline → nothing
   happens, typing still works. Accept → the browser's own microphone
   prompt appears.
4. **Recording**: the button becomes *Stop and transcribe*, solid red with
   a pulsing dot, and the textarea gets a red outline; the status box at
   the top of the card announces "Listening…" and its left edge turns
   red. With reduced motion on, the dot is still.
5. **Transcript**: stopping streams text into the textarea, word by word,
   and the status line ends with "edit it if it isn't quite right".
6. **Hold to talk**: with the caret in the textarea, hold Space: the
   button reads *Release Space to transcribe* and the take ends when the
   key comes up. A quick tap of Space still types a space.
7. **Reset**: press Esc once (the status line asks for a second), then
   again: the textarea empties, and a reload does not bring the answer
   back. Do the same while holding Space: the take is dropped and nothing
   is transcribed. The *Reset* button does the same on click.
8. **Edit and submit**: change a word, submit, confirm the stored answer
   is the edited text.
9. **Cap**: keep talking past `VOICE_MAX_SECONDS_PER_ANSWER`; the take
   ends by itself and still transcribes what was said.
10. **Refusal**: with `AI_WORKSPACE_DAILY_TOKENS=1`, the status line reads
   "Voice isn't available right now — please type your answer", boxed in
   red, and typing still submits.
11. **Choosing a microphone**: once a take has started, a *Microphone*
   dropdown lists the inputs and marks the one in use. Pick another
   while recording: the take ends and transcribes, and the next take uses
   the new device (check the meter moves). Reload: the choice is kept.
   Unplug the chosen device and record again: the browser's default is
   used and the choice is forgotten, not "microphone unavailable".
12. **No microphone**: deny the browser's microphone prompt (or unplug the
   device). Both buttons grey out, the boxed error reads "Microphone
   unavailable", Space types a space and Esc Esc does nothing; typing
   still submits. A reload offers the mic again.
13. **Screen reader**: the status line is `aria-live="polite"`; the button
   label changes with state, so state is never colour-only.

## Notes

- The consent answer is remembered in `localStorage` per browser. It is
  not a tracking identifier and not sent anywhere; blocking storage just
  means being asked each time. The chosen microphone is kept the same
  way: a device id is an opaque token the browser mints per origin, and
  it is sent nowhere.
- On-device recognition, where a browser offers it, opens its own input
  and cannot be pointed at a device, so the dropdown does not appear on
  that path.
- Quotas: per answer (`VOICE_MAX_SECONDS_PER_ANSWER`), per response
  session (`VOICE_MAX_SECONDS_PER_RESPONSE`), per survey per day
  (`VOICE_SURVEY_DAILY_SECONDS`), plus the workspace token quota and the
  global € breaker. Every one of them degrades to typing.
- While a take is in progress the page shows a live spectrum of the
  input and the name of the device it comes from, on both paths. The
  browser chooses the input device silently, so a paired headset or a
  meeting app's virtual device can be capturing instead of the built-in
  microphone; the picture and the name make that visible before the
  transcript comes back empty. On the local path this is a second,
  monitor-only capture of the default device, opened best-effort.
- Audio never reaches disk, logs or the database. `internal/voice` is the
  only package that holds the bytes, and a build-time test fails if it
  ever gains a way to write them anywhere.
