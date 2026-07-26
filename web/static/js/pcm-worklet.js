// AudioWorklet that forwards raw microphone samples to the page, which
// converts them to 16-bit PCM and sends them over the voice socket.
//
// A worklet rather than MediaRecorder because the server wants plain PCM:
// whisper.cpp and Vertex both take it directly, so no transcoder — and no
// ffmpeg dependency for self-hosters — is involved anywhere (M5-T2). The
// module is served first-party, so the CSP needs no exception.
class EarfulPCM extends AudioWorkletProcessor {
  process(inputs) {
    const input = inputs[0];
    if (input && input[0] && input[0].length) {
      // Copy: the audio thread reuses this buffer once process() returns.
      this.port.postMessage(new Float32Array(input[0]));
    }
    return true;
  }
}

registerProcessor("earful-pcm", EarfulPCM);
