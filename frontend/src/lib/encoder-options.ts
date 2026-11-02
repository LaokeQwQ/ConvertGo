export function parseEncoderOptions(text: string): Record<string, string> | null {
  const options: Record<string, string> = Object.create(null)
  for (const line of text.split(/\r?\n/)) {
    if (!line.trim()) continue
    const separator = line.indexOf('=')
    if (separator <= 0) return null
    const key = line.slice(0, separator).trim()
    const value = line.slice(separator + 1).trim()
    if (!/^[a-zA-Z0-9_:-]+$/.test(key) || !value || key in options) return null
    options[key] = value
  }
  return options
}
