<script setup lang="ts">
// CodeMirror with YAML mode. Save proposes a Change; nothing is written until
// the diff is approved in the dialog.
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { EditorView, keymap, lineNumbers, highlightActiveLine } from '@codemirror/view'
import { EditorState, Compartment } from '@codemirror/state'
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
import { yaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import { syntaxHighlighting, defaultHighlightStyle, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'
import { isDark } from '../composables/useTheme'

const props = defineProps<{ text: string; loading: boolean; error?: string | null; saveLabel?: string }>()
const emit = defineEmits<{ save: [yaml: string]; reload: [] }>()

const host = ref<HTMLElement | null>(null)
let view: EditorView | null = null
const dirty = ref(false)
const readonly = new Compartment()
const dark = new Compartment()

const theme = EditorView.theme({
  '&': { height: '100%', fontSize: '13px', backgroundColor: 'var(--panel-2)' },
  '.cm-scroller': { fontFamily: 'var(--mono)', lineHeight: '1.55' },
  '.cm-gutters': { backgroundColor: 'var(--panel-2)', color: 'var(--text-3)', border: 'none' },
  '.cm-activeLine': { backgroundColor: 'var(--raised)' },
  '.cm-activeLineGutter': { backgroundColor: 'var(--raised)' },
  '&.cm-focused': { outline: 'none' },
})

// Light counterpart to oneDark, painted from the light token palette.
const lightTheme = EditorView.theme({
  '.cm-cursor': { borderLeftColor: 'var(--text-0)' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground': { backgroundColor: 'var(--accent-soft)' },
}, { dark: false })
const lightSyntax = HighlightStyle.define([
  { tag: tags.propertyName, color: '#4f46e5' },
  { tag: tags.string, color: '#059669' },
  { tag: [tags.number, tags.bool, tags.null], color: '#b45309' },
  { tag: tags.comment, color: '#97a1b1' },
  { tag: tags.punctuation, color: '#5b6575' },
])
const light = [lightTheme, syntaxHighlighting(lightSyntax)]

onMounted(() => {
  view = new EditorView({
    parent: host.value!,
    state: EditorState.create({
      doc: props.text,
      extensions: [
        lineNumbers(), highlightActiveLine(), history(), yaml(), dark.of(isDark.value ? oneDark : light), theme,
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        keymap.of([{ key: 'Mod-s', run: () => { save(); return true } }, indentWithTab, ...defaultKeymap, ...historyKeymap]),
        readonly.of(EditorState.readOnly.of(props.loading)),
        EditorView.updateListener.of((u) => { if (u.docChanged) dirty.value = true }),
      ],
    }),
  })
})
onUnmounted(() => view?.destroy())

watch(() => props.text, (t) => {
  if (!view) return
  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: t } })
  dirty.value = false
})
watch(() => props.loading, (l) => view?.dispatch({ effects: readonly.reconfigure(EditorState.readOnly.of(l)) }))
watch(isDark, (d) => view?.dispatch({ effects: dark.reconfigure(d ? oneDark : light) }))

const save = () => { if (view && dirty.value) emit('save', view.state.doc.toString()) }
const revert = () => { if (view) { view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: props.text } }); dirty.value = false } }
</script>

<template>
  <div class="yaml">
    <div class="frame">
      <div class="ebar">
        <span class="muted status">{{ loading ? 'Loading…' : dirty ? 'Edited, not saved' : saveLabel ? 'Template' : 'Live object' }}</span>
        <span class="spacer"></span>
        <button class="btn sm ghost" @click="emit('reload')">{{ saveLabel ? 'Reset' : 'Reload' }}</button>
        <button v-if="!saveLabel" class="btn sm" :disabled="!dirty" @click="revert">Revert</button>
        <button class="btn sm" :disabled="!dirty" @click="save">{{ saveLabel ?? 'Review and save' }} <span class="kbd">⌘S</span></button>
      </div>
      <div v-if="error" class="err mono">{{ error }}</div>
      <div ref="host" class="editor"></div>
    </div>
  </div>
</template>

<style scoped>
.yaml { display: flex; flex-direction: column; height: 100%; min-height: 0; }
.frame { flex: 1; min-height: 0; display: flex; flex-direction: column; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); overflow: hidden; }
.ebar { display: flex; align-items: center; gap: 8px; padding: 5px 10px; border-bottom: 1px solid var(--line); flex-shrink: 0; }
.status { font-size: 12.5px; }
.spacer { flex: 1; }
.editor { flex: 1; min-height: 0; overflow: hidden; }
.editor :deep(.cm-editor) { height: 100%; }
.err { color: var(--bad); padding: 6px 10px; font-size: 12px; border-bottom: 1px solid var(--line); }
</style>
