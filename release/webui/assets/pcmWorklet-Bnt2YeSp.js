// The capture end of the microphone graph, running on the audio thread.
//
// Deliberately almost empty. Anything done here is done inside a realtime
// callback that must return before the next 128 samples are due, and an
// overrun is heard as a dropout rather than reported as an error. Resampling,
// framing and encoding all happen on the main thread in pcmFrames.ts, where
// they can also be tested. This file only copies samples out.
//
// It ships as its own file rather than a Blob URL because the renderer's
// Content-Security-Policy allows scripts from 'self' only, and a worklet
// module is fetched under script-src.
class PcmCaptureProcessor extends AudioWorkletProcessor {
  process(inputs) {
    const channel = inputs[0]?.[0]
    // A disconnected or not-yet-flowing source gives an empty input. Returning
    // true keeps the processor alive, waiting for audio to arrive.
    if (!channel || channel.length === 0) return true
    // The render quantum's buffer belongs to the graph and is refilled as soon
    // as this returns, so what is posted has to be a copy rather than a view.
    this.port.postMessage(new Float32Array(channel))
    // Outputs are left untouched, which is silence. The node still has to be
    // connected onward for the graph to pull from it, and silence is what
    // keeps that connection from feeding the microphone back to the speakers.
    return true
  }
}

registerProcessor('pcm-capture', PcmCaptureProcessor)
