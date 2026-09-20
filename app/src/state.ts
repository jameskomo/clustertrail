// Global UI state: what is selected and what is open. Small on purpose; data
// lives in composables bound to subscriptions.
import { reactive } from 'vue'

export type Screen = 'overview' | 'changes' | string // or a kind key

export interface Selection { kind: string; key: string; namespace?: string; name: string }

export interface CustomKind { crd: string; kind: string; namespaced: boolean; columns: string[] }

export const ui = reactive({
  custom: null as CustomKind | null,
  cluster: null as string | null,
  namespace: '' as string,
  screen: 'overview' as Screen,
  filter: '',
  selection: null as Selection | null,
  drawerTab: 'overview' as 'overview' | 'events' | 'explain' | 'logs' | 'yaml' | 'terminal',
  paletteOpen: false,
})

export function select(kind: string, key: string, namespace: string | undefined, name: string, tab?: typeof ui.drawerTab) {
  ui.selection = { kind, key, namespace, name }
  if (tab) ui.drawerTab = tab
  else if (ui.drawerTab === 'logs' || ui.drawerTab === 'terminal') ui.drawerTab = kind === 'pods' ? ui.drawerTab : 'overview'
}

export function closeDrawer() { ui.selection = null }
