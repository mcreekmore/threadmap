import sample from '../dev/topology.json'
import type { Device, MeshMap } from './types'

type Sample = Omit<MeshMap, 'nodes'> & { nodes: (Omit<Device, 'kind'> & { kind: string })[] }

const _check: Sample = sample
