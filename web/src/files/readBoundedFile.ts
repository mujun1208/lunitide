export function readBoundedFile(file: File, maxBytes: number, timeoutMessage = '读取文件超时，请重新选择'): Promise<ArrayBuffer> {
  if (!Number.isFinite(file.size) || file.size < 0 || file.size > maxBytes) {
    return Promise.reject(new Error(`文件超过 ${Math.round(maxBytes / 1024 / 1024)} MiB 限制`))
  }
  return new Promise((resolve, reject) => {
    let reader: FileReader | undefined
    let settled = false
    const fail = (error: unknown) => {
      if (settled) return
      settled = true
      window.clearTimeout(timer)
      reject(error)
    }
    const timer = window.setTimeout(() => {
      fail(new Error(timeoutMessage))
      reader?.abort()
    }, 10_000)
    const finish = (buffer: ArrayBuffer) => {
      if (settled) return
      if (buffer.byteLength > maxBytes) {
        fail(new Error(`文件超过 ${Math.round(maxBytes / 1024 / 1024)} MiB 限制`))
        return
      }
      settled = true
      window.clearTimeout(timer)
      resolve(buffer)
    }
    try {
      if (typeof file.arrayBuffer === 'function') {
        file.arrayBuffer().then(finish, fail)
      } else {
        reader = new FileReader()
        reader.onload = () => finish(reader!.result as ArrayBuffer)
        reader.onerror = () => fail(reader!.error ?? new Error('读取文件失败'))
        reader.onabort = () => fail(new Error('文件读取已取消'))
        reader.readAsArrayBuffer(file)
      }
    } catch (error) { fail(error) }
  })
}
