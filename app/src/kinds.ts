// What the UI knows about each kind: where it sits in the nav, which columns
// its table shows, and what actions apply. The engine's registry is the
// source of truth for what exists; this is how it looks.

export type CellType = 'text' | 'mono' | 'num' | 'age' | 'time' | 'list' | 'cpu' | 'mem' | 'bool' | 'status' | 'message'

export interface Column {
  key: string          // row field (name, namespace, status, createdAt) or a cell key
  label: string
  width: string        // grid track
  type?: CellType
  cell?: boolean       // read from row.cells
  align?: 'right'
}

export interface KindSpec {
  key: string
  label: string
  singular: string
  group: 'Workloads' | 'Config' | 'Network' | 'Storage' | 'Access' | 'Cluster'
  namespaced: boolean
  columns: Column[]
  scalable?: boolean
  restarts?: boolean
  logs?: boolean
  exec?: boolean
  forward?: boolean
}

const name: Column = { key: 'name', label: 'Name', width: 'minmax(240px, 2fr)', type: 'mono' }
const ns: Column = { key: 'namespace', label: 'Namespace', width: '150px', type: 'mono' }
const age: Column = { key: 'createdAt', label: 'Age', width: '76px', type: 'age', align: 'right' }
const status: Column = { key: 'status', label: 'Status', width: '160px', type: 'status' }
const c = (key: string, label: string, width: string, type: CellType = 'text', align?: 'right'): Column => ({ key, label, width, type, cell: true, align })

export const KINDS: KindSpec[] = [
  { key: 'pods', label: 'Pods', singular: 'Pod', group: 'Workloads', namespaced: true, logs: true, exec: true, forward: true,
    columns: [name, ns, c('ready', 'Ready', '72px', 'num', 'right'), status, c('restarts', 'Restarts', '92px', 'num', 'right'), c('cpu', 'CPU', '84px', 'cpu', 'right'), c('mem', 'Memory', '92px', 'mem', 'right'), age, c('node', 'Node', 'minmax(140px, 1fr)', 'mono')] },
  { key: 'deployments', label: 'Deployments', singular: 'Deployment', group: 'Workloads', namespaced: true, scalable: true, restarts: true,
    columns: [name, ns, status, c('replicas', 'Desired', '76px', 'num', 'right'), c('updated', 'Updated', '76px', 'num', 'right'), c('available', 'Available', '84px', 'num', 'right'), c('images', 'Images', 'minmax(200px, 1.5fr)', 'list'), age] },
  { key: 'statefulsets', label: 'StatefulSets', singular: 'StatefulSet', group: 'Workloads', namespaced: true, scalable: true, restarts: true,
    columns: [name, ns, status, c('replicas', 'Desired', '76px', 'num', 'right'), c('images', 'Images', 'minmax(200px, 1.5fr)', 'list'), age] },
  { key: 'daemonsets', label: 'DaemonSets', singular: 'DaemonSet', group: 'Workloads', namespaced: true, restarts: true,
    columns: [name, ns, status, c('desired', 'Desired', '76px', 'num', 'right'), c('images', 'Images', 'minmax(200px, 1.5fr)', 'list'), age] },
  { key: 'replicasets', label: 'ReplicaSets', singular: 'ReplicaSet', group: 'Workloads', namespaced: true, scalable: true,
    columns: [name, ns, status, c('replicas', 'Desired', '76px', 'num', 'right'), c('owner', 'Owner', 'minmax(160px, 1fr)', 'mono'), age] },
  { key: 'jobs', label: 'Jobs', singular: 'Job', group: 'Workloads', namespaced: true,
    columns: [name, ns, status, c('completions', 'Completions', '100px', 'num', 'right'), c('duration', 'Duration', '90px', 'num', 'right'), age] },
  { key: 'cronjobs', label: 'CronJobs', singular: 'CronJob', group: 'Workloads', namespaced: true,
    columns: [name, ns, status, c('schedule', 'Schedule', '130px', 'mono'), c('active', 'Active', '70px', 'num', 'right'), c('lastRun', 'Last run', '110px', 'time'), age] },
  { key: 'configmaps', label: 'ConfigMaps', singular: 'ConfigMap', group: 'Config', namespaced: true,
    columns: [name, ns, c('keys', 'Keys', 'minmax(240px, 2fr)', 'list'), age] },
  { key: 'secrets', label: 'Secrets', singular: 'Secret', group: 'Config', namespaced: true,
    columns: [name, ns, c('type', 'Type', '220px', 'mono'), c('keys', 'Keys', 'minmax(200px, 1.5fr)', 'list'), age] },
  { key: 'services', label: 'Services', singular: 'Service', group: 'Network', namespaced: true,
    columns: [name, ns, c('type', 'Type', '110px'), c('clusterIP', 'Cluster IP', '130px', 'mono'), c('ports', 'Ports', 'minmax(160px, 1fr)', 'mono'), c('external', 'External', '160px', 'mono'), age] },
  { key: 'ingresses', label: 'Ingresses', singular: 'Ingress', group: 'Network', namespaced: true,
    columns: [name, ns, c('class', 'Class', '110px'), c('hosts', 'Hosts', 'minmax(200px, 1.5fr)', 'mono'), c('address', 'Address', '160px', 'mono'), age] },
  { key: 'persistentvolumeclaims', label: 'Volume Claims', singular: 'PersistentVolumeClaim', group: 'Storage', namespaced: true,
    columns: [name, ns, status, c('capacity', 'Capacity', '90px', 'num', 'right'), c('storageClass', 'Class', '130px'), c('volume', 'Volume', 'minmax(160px, 1fr)', 'mono'), age] },
  { key: 'nodes', label: 'Nodes', singular: 'Node', group: 'Cluster', namespaced: false,
    columns: [name, status, c('roles', 'Roles', '120px'), c('cpu', 'CPU', '92px', 'cpu', 'right'), c('mem', 'Memory', '100px', 'mem', 'right'), c('version', 'Version', '110px', 'mono'), c('ip', 'IP', '130px', 'mono'), age] },
  { key: 'namespaces', label: 'Namespaces', singular: 'Namespace', group: 'Cluster', namespaced: false,
    columns: [name, status, age] },
  { key: 'serviceaccounts', label: 'Service Accounts', singular: 'ServiceAccount', group: 'Access', namespaced: true,
    columns: [name, ns, c('secrets', 'Secrets', 'minmax(200px, 1fr)', 'list'), age] },
  { key: 'roles', label: 'Roles', singular: 'Role', group: 'Access', namespaced: true,
    columns: [name, ns, c('rules', 'Rules', '70px', 'num', 'right'), c('summary', 'Allows', 'minmax(280px, 2fr)', 'message'), age] },
  { key: 'rolebindings', label: 'Role Bindings', singular: 'RoleBinding', group: 'Access', namespaced: true,
    columns: [name, ns, c('role', 'Role', 'minmax(200px, 1fr)', 'mono'), c('subjects', 'Subjects', 'minmax(220px, 1.5fr)', 'list'), age] },
  { key: 'clusterroles', label: 'Cluster Roles', singular: 'ClusterRole', group: 'Access', namespaced: false,
    columns: [name, c('rules', 'Rules', '70px', 'num', 'right'), c('summary', 'Allows', 'minmax(320px, 3fr)', 'message'), age] },
  { key: 'clusterrolebindings', label: 'Cluster Role Bindings', singular: 'ClusterRoleBinding', group: 'Access', namespaced: false,
    columns: [name, c('role', 'Role', 'minmax(200px, 1fr)', 'mono'), c('subjects', 'Subjects', 'minmax(240px, 2fr)', 'list'), age] },
  { key: 'customresourcedefinitions', label: 'Custom Resources', singular: 'CustomResourceDefinition', group: 'Cluster', namespaced: false,
    columns: [name, c('kind', 'Kind', '180px'), c('group', 'API group', 'minmax(200px, 1fr)', 'mono'), c('scope', 'Scope', '110px'), c('versions', 'Versions', '140px', 'list'), age] },
  { key: 'events', label: 'Events', singular: 'Event', group: 'Cluster', namespaced: true,
    columns: [{ key: 'status', label: 'Type', width: '84px', type: 'status' }, c('reason', 'Reason', '170px'), c('object', 'Object', 'minmax(200px, 1fr)', 'mono'), c('message', 'Message', 'minmax(320px, 3fr)', 'message'), c('count', 'Count', '60px', 'num', 'right'), { key: 'createdAt', label: 'Last seen', width: '84px', type: 'age', align: 'right' }] },
]

export const kindByKey = (key: string) => KINDS.find((k) => k.key === key)
export const GROUPS = ['Workloads', 'Config', 'Network', 'Storage', 'Access', 'Cluster'] as const
export const TOP_SCREENS = [
  { key: 'overview', label: 'Overview' },
  { key: 'timeline', label: 'What changed' },
  { key: 'drift', label: 'Drift' },
  { key: 'changes', label: 'Changes' },
  { key: 'helm', label: 'Helm' },
] as const

// Topbar copy for the non-resource screens. Resource screens take their title
// from the KindSpec and carry no description.
export const SCREEN_META: Record<string, { title: string; desc: string }> = {
  overview: { title: 'Overview', desc: 'Cluster health at a glance' },
  timeline: { title: 'What changed', desc: 'Who changed it, and when' },
  drift: { title: 'Drift', desc: 'What is running that is not in Git, and vice versa' },
  changes: { title: 'Changes', desc: 'Every mutation, approved by a human, chained for audit' },
  helm: { title: 'Helm', desc: 'Releases decoded from cluster secrets, read-only' },
}

// A custom resource browsed through its CRD. The engine sends the CRD's own
// printer columns, so the table matches `kubectl get`.
export function customSpec(crdName: string, kindName: string, namespaced: boolean, columns: string[]): KindSpec {
  const cols: Column[] = [name]
  if (namespaced) cols.push(ns)
  for (const label of columns.slice(0, 5)) cols.push(c(label, label, 'minmax(120px, 1fr)'))
  cols.push(age)
  return { key: `crd:${crdName}`, label: kindName, singular: kindName, group: 'Cluster', namespaced, columns: cols }
}
