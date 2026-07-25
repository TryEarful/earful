# testdata

## `jfk.wav`

Eleven seconds of John F. Kennedy's 1961 inaugural address — "ask not
what your country can do for you" — as 16-bit PCM, mono, 16 kHz, which
is exactly the format `internal/voice` produces.

**Provenance**: a work of the United States federal government, in the
public domain. This copy comes from the `samples/` directory of
[whisper.cpp](https://github.com/ggerganov/whisper.cpp) (MIT), where it
is the standard transcription sample.

**Why a real recording is committed rather than generated.** Anything a
test can synthesize — a tone, or text-to-speech — is machine-generated
audio, and sending a stream of that to a hosted speech API proves
nothing about transcription while looking a great deal like probing it.
That is not hypothetical: our staging project was suspended hours after
Vertex was first enabled on it. Real recorded speech is what a speech
model is for, so the two places audio is used both use this file:

- the browser suite's synthetic microphone (`fakeMicrophone` in
  `e2e/tests/helpers.ts`), which never reaches a real model anyway
  because voice tests run against the scripted provider only;
- the opt-in `internal/ai` and whisper.cpp integration tests, which do
  reach a real model — deliberately, once, with a human voice.
