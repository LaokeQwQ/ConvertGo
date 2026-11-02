export function compareVersions(latest: string, current: string): -1 | 0 | 1 | null {
  const parse = (value: string) => /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/.exec(value)
  const left = parse(latest)
  const right = parse(current)
  if (!left || !right) return null
  for (let index = 1; index <= 3; index++) {
    const difference = Number(left[index]) - Number(right[index])
    if (difference !== 0) return difference > 0 ? 1 : -1
  }
  if (left[4] === right[4]) return 0
  if (!left[4]) return 1
  if (!right[4]) return -1
  const a = left[4].split('.')
  const b = right[4].split('.')
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    if (a[index] === undefined) return -1
    if (b[index] === undefined) return 1
    if (a[index] === b[index]) continue
    const an = /^\d+$/.test(a[index])
    const bn = /^\d+$/.test(b[index])
    if (an && bn) {
      const difference = Number(a[index]) - Number(b[index])
      if (difference !== 0) return difference > 0 ? 1 : -1
      continue
    }
    if (an !== bn) return an ? -1 : 1
    return a[index] > b[index] ? 1 : -1
  }
  return 0
}
