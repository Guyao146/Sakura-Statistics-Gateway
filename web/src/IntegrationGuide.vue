<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { integrationGuide } from './guide'

const props = defineProps<{ section: string }>()
const emit = defineEmits<{ copy: [content: string, code: HTMLElement] }>()
const article = ref<HTMLElement | null>(null)
const mobileToc = ref<HTMLDetailsElement | null>(null)

function scrollToSection(id: string) {
  const heading = article.value?.querySelector<HTMLElement>(`#${CSS.escape(id)}`)
  if (!heading) return
  heading.scrollIntoView({ block: 'start' })
  heading.setAttribute('tabindex', '-1')
  heading.focus({ preventScroll: true })
}

function chooseSection(id: string) {
  if (mobileToc.value) mobileToc.value.open = false
  scrollToSection(id)
}

function copyCode(event: MouseEvent) {
  if (!(event.target instanceof Element)) return
  const button = event.target.closest<HTMLButtonElement>('button[data-copy-code]')
  if (!button || !article.value?.contains(button)) return
  const block = integrationGuide.codeBlocks.find(block => block.id === button.dataset.copyCode)
  const code = button.closest('.guide-code')?.querySelector<HTMLElement>('pre code')
  if (block && code) emit('copy', block.content, code)
}

onMounted(() => { if (props.section) scrollToSection(props.section) })
watch(() => props.section, async section => { await nextTick(); if (section) scrollToSection(section) })
</script>

<template>
  <section class="guide-layout" aria-label="应用接入指南">
    <aside class="guide-toc" aria-labelledby="guide-toc-title">
      <h2 id="guide-toc-title">本页目录</h2>
      <nav aria-label="接入指南目录">
        <a v-for="heading in integrationGuide.headings" :key="heading.id" :href="`#${heading.id}`" :class="{ 'guide-toc-sub': heading.level === 3 }" @click.prevent="chooseSection(heading.id)">{{ heading.text }}</a>
      </nav>
    </aside>
    <div class="guide-main">
      <details ref="mobileToc" class="guide-mobile-toc">
        <summary>本页目录 · 点击展开</summary>
        <nav aria-label="接入指南目录（移动端）">
          <a v-for="heading in integrationGuide.headings" :key="heading.id" :href="`#${heading.id}`" :class="{ 'guide-toc-sub': heading.level === 3 }" @click.prevent="chooseSection(heading.id)">{{ heading.text }}</a>
        </nav>
      </details>
      <article ref="article" class="panel guide-document" aria-label="接入指南正文" @click="copyCode" v-html="integrationGuide.html"></article>
    </div>
  </section>
</template>

<style scoped>
.guide-layout{display:grid;grid-template-columns:232px minmax(0,1fr);gap:24px;align-items:start;min-width:0}
.guide-toc{position:sticky;top:24px;max-height:calc(100vh - 48px);overflow-y:auto;padding:20px 16px;background:#fff;border:1px solid var(--border);border-radius:12px;scrollbar-width:thin}
.guide-toc h2{font-size:13px;color:#515970;margin:0 8px 12px}
.guide-layout nav{display:grid;gap:3px}
.guide-layout nav a{padding:7px 8px;font-size:12px;line-height:1.55;color:#596174;border-radius:6px;overflow-wrap:anywhere}
.guide-layout nav a:hover{background:#f3f0ff;color:#6854c7}
.guide-layout nav .guide-toc-sub{padding-left:20px;font-size:11px;color:#737a8c}
.guide-main{min-width:0}
.guide-mobile-toc{display:none;background:#fff;border:1px solid var(--border);border-radius:10px;margin-bottom:16px;padding:12px 16px}
.guide-mobile-toc summary{cursor:pointer;font-size:13px;font-weight:600;color:#6854c7}
.guide-mobile-toc nav{margin-top:12px;max-height:50vh;overflow-y:auto}
.guide-document{padding:30px 32px;color:#41495d;font-size:14px;line-height:1.9;overflow-wrap:anywhere}
.guide-document :deep(h1){font-size:24px;color:#252c40;line-height:1.5;letter-spacing:-.4px;margin:0 0 20px}
.guide-document :deep(h2){font-size:21px;color:#30374c;line-height:1.6;margin:40px 0 16px;padding-bottom:9px;border-bottom:1px solid #eceef5;scroll-margin-top:28px}
.guide-document :deep(h3){font-size:17px;color:#41495d;line-height:1.6;margin:27px 0 12px;scroll-margin-top:28px}
.guide-document :deep(p){margin:12px 0}
.guide-document :deep(ul),.guide-document :deep(ol){margin:12px 0;padding-left:24px}
.guide-document :deep(li){margin:6px 0}
.guide-document :deep(a){color:#6d58c8;text-decoration:underline;text-underline-offset:3px}
.guide-document :deep(blockquote){margin:20px 0;padding:8px 18px;background:#f5f3fc;border-left:3px solid #a796e4;border-radius:0 8px 8px 0}
.guide-document :deep(strong){font-weight:650;color:#333b51}
.guide-document :deep(code){font-family:Consolas,Menlo,monospace;font-size:.9em;background:#f3f1f8;border-radius:4px;padding:2px 5px;color:#655587}
.guide-document :deep(.guide-code){margin:18px 0;border:1px solid #e7e5ef;border-radius:9px;overflow:hidden;background:#f8f8fc}
.guide-document :deep(.guide-code-toolbar){display:flex;justify-content:space-between;align-items:center;gap:12px;padding:9px 14px;background:#f2f0f8;border-bottom:1px solid #e7e5ef}
.guide-document :deep(.guide-code-language){font-family:Consolas,Menlo,monospace;font-size:11px;color:#716588}
.guide-document :deep(.guide-copy){padding:5px 10px;font-size:11px}
.guide-document :deep(pre){margin:0;padding:17px 18px;max-width:100%;overflow-x:auto;font-size:12px;line-height:1.8;white-space:pre;word-break:normal;overflow-wrap:normal;tab-size:4}
.guide-document :deep(pre code){display:block;background:none;border-radius:0;padding:0;font-size:inherit;color:#534b6a;white-space:inherit;overflow-wrap:normal;word-break:normal}
.guide-document :deep(.guide-table-wrap){max-width:100%;overflow-x:auto;margin:18px 0;border:1px solid #eceef5;border-radius:8px}
.guide-document :deep(table){white-space:normal;font-size:13px;min-width:480px}
.guide-document :deep(th),.guide-document :deep(td){font-size:12px;padding:11px 14px;line-height:1.8;vertical-align:top}
.guide-document :deep(th){color:#646d83;background:#f8f8fc;font-weight:600}
.guide-document :deep(.guide-image-note){color:#858b9c;font-size:12px}
@media(max-width:1200px){.guide-layout{grid-template-columns:200px minmax(0,1fr);gap:18px}.guide-document{padding:26px 24px}}
@media(max-width:900px){.guide-layout{display:block}.guide-toc{display:none}.guide-mobile-toc{display:block}}
@media(max-width:540px){.guide-document{padding:22px 16px;font-size:13px}.guide-document :deep(h1){font-size:21px}.guide-document :deep(h2){font-size:19px;scroll-margin-top:80px}.guide-document :deep(h3){font-size:16px;scroll-margin-top:80px}.guide-document :deep(pre){padding:14px 12px;font-size:11px}.guide-document :deep(.guide-code-toolbar){padding:8px 10px}}
</style>
