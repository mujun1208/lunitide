export const MESSAGE_MAX_CHARACTERS = 32768
export const MESSAGE_MAX_BYTES = 131072
export const MESSAGE_LIMIT_ERROR = '消息需为 1–32768 个 Unicode 字符、最多 131072 字节且不能包含 NUL。'
export const messageSize = (value: string) => ({ characters: Array.from(value).length, bytes: new TextEncoder().encode(value).length })
export const validMessageText = (value: string): boolean => {
  if (!value.length || value.includes('\0')) return false
  const size = messageSize(value)
  return size.characters <= MESSAGE_MAX_CHARACTERS && size.bytes <= MESSAGE_MAX_BYTES
}
