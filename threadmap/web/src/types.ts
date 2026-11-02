// The JSON sent by GET api/topology; meshMap in parse.go is the source of truth.
export type Device = {
  rloc: string
  label: string
  mac?: string
  kind: 'router' | 'child'
  leader?: boolean
  border?: boolean
}

export type Link = {
  a: string
  b: string
  quality: number
}

export type MeshMap = {
  nodes: Device[]
  links: Link[]
  warnings: string[]
}
