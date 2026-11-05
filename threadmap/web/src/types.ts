// The JSON sent by GET api/topology; meshMap in parse.go is the source of truth.
export type Device = {
  rloc: string
  label: string
  mac?: string
  kind: 'router' | 'child'
  leader?: boolean
  border?: boolean
  omr?: string
  ha?: HADevice
}

// A device in Home Assistant's device registry, known only for Matter devices.
export type HADevice = {
  id: string
  name: string
  manufacturer?: string
  model?: string
  firmware?: string
  area?: string
}

export type Link = {
  a: string
  b: string
  quality: number // the weaker direction
  // Router links know both directions. Child links know only how the parent (a) hears the child (b).
  aToB?: number
  bToA?: number
  margin?: number // dB
}

export type MeshMap = {
  nodes: Device[]
  links: Link[]
  warnings: string[]
}

export function role(device: Device) {
  if (device.kind === 'child') return 'Child'
  const roles = [device.border ? 'Border router' : 'Router']
  if (device.leader) roles.push('Leader')
  return roles.join(', ')
}

// Thread's names for its link quality levels.
export const qualities = [
  { level: 3, name: 'Good', meaning: 'More than 20 dB of link margin' },
  { level: 2, name: 'Reasonable', meaning: 'More than 10 dB of link margin' },
  { level: 1, name: 'Poor', meaning: 'More than 2 dB of link margin' },
  { level: 0, name: 'None', meaning: 'Heard in one direction only, or 2 dB of link margin or less' },
]

export function qualityName(level: number) {
  return `q${level} ${qualities.find((q) => q.level === level)?.name ?? ''}`.trim()
}
