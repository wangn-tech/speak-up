class PCMProcessor extends AudioWorkletProcessor {
  process(inputs) {
    const samples = inputs[0]?.[0]
    if (!samples?.length) return true
    const ratio = sampleRate / 16000
    const length = Math.floor(samples.length / ratio)
    const pcm = new Int16Array(length)
    for (let index = 0; index < length; index += 1) {
      const sample = Math.max(-1, Math.min(1, samples[Math.floor(index * ratio)]))
      pcm[index] = sample < 0 ? sample * 0x8000 : sample * 0x7fff
    }
    this.port.postMessage(pcm.buffer, [pcm.buffer])
    return true
  }
}

registerProcessor('pcm-processor', PCMProcessor)
