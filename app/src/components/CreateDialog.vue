<script setup lang="ts">
// Create anything from YAML, starting from a template. It goes through the
// same gate as every other change: the dry-run diff is reviewed before the
// object exists.
import { computed, ref, watch } from 'vue'
import { parse as parseYaml } from 'yaml'
import { ui } from '../state'
import { KINDS } from '../kinds'
import { propose } from '../composables/useChanges'
import { toast } from '../composables/useToasts'
import AppIcon from './AppIcon.vue'
import YamlEditor from './YamlEditor.vue'

const props = defineProps<{ open: boolean; kind?: string }>()
const emit = defineEmits<{ close: [] }>()

const ns = computed(() => ui.namespace || 'default')
const templates: Record<string, () => string> = {
  Namespace: () => `apiVersion: v1\nkind: Namespace\nmetadata:\n  name: my-namespace\n`,
  Deployment: () => `apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: my-app\n  namespace: ${ns.value}\nspec:\n  replicas: 2\n  selector:\n    matchLabels:\n      app: my-app\n  template:\n    metadata:\n      labels:\n        app: my-app\n    spec:\n      containers:\n        - name: web\n          image: nginx:1.27-alpine\n          ports:\n            - containerPort: 80\n          resources:\n            requests: { cpu: 50m, memory: 64Mi }\n            limits: { cpu: 250m, memory: 128Mi }\n`,
  Service: () => `apiVersion: v1\nkind: Service\nmetadata:\n  name: my-app\n  namespace: ${ns.value}\nspec:\n  selector:\n    app: my-app\n  ports:\n    - port: 80\n      targetPort: 80\n`,
  ConfigMap: () => `apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: my-config\n  namespace: ${ns.value}\ndata:\n  KEY: value\n`,
  Secret: () => `apiVersion: v1\nkind: Secret\nmetadata:\n  name: my-secret\n  namespace: ${ns.value}\ntype: Opaque\nstringData:\n  password: change-me\n`,
  Job: () => `apiVersion: batch/v1\nkind: Job\nmetadata:\n  name: my-job\n  namespace: ${ns.value}\nspec:\n  template:\n    spec:\n      restartPolicy: Never\n      containers:\n        - name: run\n          image: busybox:1.36\n          command: ["sh", "-c", "echo hello"]\n`,
  CronJob: () => `apiVersion: batch/v1\nkind: CronJob\nmetadata:\n  name: my-cron\n  namespace: ${ns.value}\nspec:\n  schedule: "*/10 * * * *"\n  jobTemplate:\n    spec:\n      template:\n        spec:\n          restartPolicy: Never\n          containers:\n            - name: run\n              image: busybox:1.36\n              command: ["sh", "-c", "date"]\n`,
  Ingress: () => `apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n  name: my-app\n  namespace: ${ns.value}\nspec:\n  rules:\n    - host: my-app.example.com\n      http:\n        paths:\n          - path: /\n            pathType: Prefix\n            backend:\n              service:\n                name: my-app\n                port:\n                  number: 80\n`,
  PersistentVolumeClaim: () => `apiVersion: v1\nkind: PersistentVolumeClaim\nmetadata:\n  name: my-data\n  namespace: ${ns.value}\nspec:\n  accessModes: [ReadWriteOnce]\n  resources:\n    requests:\n      storage: 1Gi\n`,
  StatefulSet: () => `apiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n  name: my-db\n  namespace: ${ns.value}\nspec:\n  serviceName: my-db\n  replicas: 1\n  selector:\n    matchLabels:\n      app: my-db\n  template:\n    metadata:\n      labels:\n        app: my-db\n    spec:\n      containers:\n        - name: db\n          image: postgres:16-alpine\n          env:\n            - name: POSTGRES_PASSWORD\n              value: change-me\n`,
  DaemonSet: () => `apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: my-agent\n  namespace: ${ns.value}\nspec:\n  selector:\n    matchLabels:\n      app: my-agent\n  template:\n    metadata:\n      labels:\n        app: my-agent\n    spec:\n      containers:\n        - name: agent\n          image: busybox:1.36\n          command: ["sh", "-c", "sleep infinity"]\n`,
}
const names = Object.keys(templates)
const chosen = ref('Namespace')
const text = ref('')
watch(() => [props.open, props.kind], () => {
  if (!props.open) return
  const spec = KINDS.find((k) => k.key === props.kind)
  chosen.value = spec && templates[spec.singular] ? spec.singular : 'Namespace'
  text.value = templates[chosen.value]!()
}, { immediate: true })
watch(chosen, (c) => (text.value = templates[c]!()))

const save = async (yaml: string) => {
  let obj: any
  try { obj = parseYaml(yaml) } catch (e: any) { toast(`That is not valid YAML: ${e.message}`, 'bad'); return }
  const kind = obj?.kind, name = obj?.metadata?.name
  if (!kind || !name) { toast('The YAML needs a kind and metadata.name.', 'bad'); return }
  const spec = KINDS.find((k) => k.singular === kind)
  if (!spec) { toast(`ClusterTrail cannot create ${kind} yet.`, 'bad'); return }
  const namespace = spec.namespaced ? obj.metadata?.namespace ?? ns.value : ''
  if (spec.namespaced && !obj.metadata?.namespace) yaml = yaml.replace(/^metadata:\n/m, `metadata:\n  namespace: ${namespace}\n`)
  emit('close')
  await propose(ui.cluster!, `Create ${kind} ${name}${namespace ? ` in ${namespace}` : ''}`, [{ op: 'apply', kind: spec.key, namespace, name, yaml }])
}
</script>

<template>
  <Transition name="fade">
    <div v-if="open" class="scrim" @click.self="emit('close')">
      <div class="dialog" role="dialog" aria-modal="true">
        <header>
          <h2>Create from YAML</h2>
          <button class="icon-btn close" @click="emit('close')"><AppIcon name="close" :size="14" /></button>
        </header>
        <p class="muted">Edit, then save. You will see the dry-run result before anything is created on <span class="mono">{{ ui.cluster }}</span>.</p>
        <div class="body">
          <div class="tpls">
            <div class="tpls-head">Start from</div>
            <button v-for="n in names" :key="n" class="tpl" :class="{ on: chosen === n }" @click="chosen = n">
              <AppIcon name="resources" :size="14" />
              <span class="mono">{{ n }}</span>
            </button>
          </div>
          <YamlEditor :text="text" :loading="false" save-label="Review and create" @save="save" @reload="text = templates[chosen]!()" class="editor" />
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.scrim { position: fixed; inset: 0; background: rgba(9, 12, 18, 0.72); display: flex; align-items: center; justify-content: center; z-index: 40; }
.dialog { width: min(900px, calc(100vw - 48px)); height: min(720px, calc(100vh - 64px)); display: flex; flex-direction: column; gap: 12px; padding: 20px 22px; background: var(--panel); border: 1px solid var(--line); border-radius: var(--r-lg); box-shadow: var(--shadow-2); }
header { display: flex; align-items: center; gap: 12px; }
h2 { margin: 0; font-size: 15px; font-weight: 600; letter-spacing: -0.01em; }
.close { margin-left: auto; }
p { margin: 0; }
.body { flex: 1; min-height: 0; display: grid; grid-template-columns: 208px minmax(0, 1fr); gap: 14px; }
.tpls { min-height: 0; overflow: auto; display: flex; flex-direction: column; gap: 2px; padding: 6px; background: var(--panel-2); border: 1px solid var(--line); border-radius: var(--r); }
.tpls-head { padding: 5px 9px 7px; font-size: 11px; font-weight: 600; letter-spacing: 0.05em; text-transform: uppercase; color: var(--text-3); }
.tpl { display: flex; align-items: center; gap: 8px; height: 30px; padding: 0 9px; background: none; border: 0; border-radius: var(--r-sm); color: var(--text-2); text-align: left; box-shadow: inset 2px 0 0 transparent; transition: background 120ms var(--ease), color 120ms var(--ease), box-shadow 120ms var(--ease); }
.tpl:hover { background: var(--raised); color: var(--text-0); }
.tpl.on { background: var(--accent-soft); color: var(--text-0); box-shadow: inset 2px 0 0 var(--accent); }
.tpl .mono { font-size: 12.5px; }
.editor { min-height: 0; }
.fade-enter-active { transition: opacity 140ms; }
.fade-leave-active { transition: opacity 100ms; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
