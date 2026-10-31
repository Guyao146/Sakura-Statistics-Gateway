import MarkdownIt from 'markdown-it'
import type MarkdownToken from 'markdown-it/lib/token.mjs'
import DOMPurify from 'dompurify'
import integrationMarkdown from '../../docs/integration.md?raw'

export interface GuideHeading {
  id: string
  text: string
  level: 2 | 3
}

export interface GuideCodeBlock {
  id: string
  language: string
  content: string
}

export interface GuideDocument {
  html: string
  headings: GuideHeading[]
  codeBlocks: GuideCodeBlock[]
}

const repositoryMain = 'https://github.com/Guyao146/Sakura-Statistics-Gateway/blob/main'
const documentBase = 'https://guide.invalid/docs/integration.md'
export const guideSections = { web: 'guide-网页接入', desktop: 'guide-桌面客户端接入' } as const

// Only explicit HTTP(S), local headings, and repository docs/examples are links.
export function resolveGuideLink(href: string): string | null {
  const value = href.trim()
  if (!value || /[\u0000-\u001f\u007f\\]/.test(value) || value.startsWith('//')) return null
  if (value.startsWith('#')) return value
  try {
    if (/^[a-z][a-z\d+.-]*:/i.test(value)) {
      const url = new URL(value)
      return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : null
    }
    const url = new URL(value, documentBase)
    if (url.origin !== new URL(documentBase).origin || !/^\/(?:docs|examples)\//.test(url.pathname)) return null
    return `${repositoryMain}${url.pathname}${url.search}${url.hash}`
  } catch {
    return null
  }
}

function inlineText(tokens: MarkdownToken[] | null): string {
  return (tokens || []).map(token => {
    if (['text', 'code_inline', 'image'].includes(token.type)) return token.content
    if (['softbreak', 'hardbreak'].includes(token.type)) return ' '
    return token.children ? inlineText(token.children) : ''
  }).join('')
}

function headingSlug(text: string): string {
  const slug = text.normalize('NFKC').replace(/^\d+(?:\.\d+)*\.?\s+/, '').toLowerCase()
    .replace(/[^\p{Letter}\p{Number}]+/gu, '-').replace(/^-+|-+$/g, '')
  return `guide-${slug || '章节'}`
}

export function parseGuide(source: string): GuideDocument {
  const headings: GuideHeading[] = []
  const codeBlocks: GuideCodeBlock[] = []
  const ids = new Set<string>()
  const markdown = new MarkdownIt({ html: false, linkify: false, typographer: false })
  const escape = markdown.utils.escapeHtml
  markdown.validateLink = href => resolveGuideLink(href) !== null
  markdown.renderer.rules.link_open = (tokens, index, options, _env, renderer) => {
    const token = tokens[index]
    const href = resolveGuideLink(String(token.attrGet('href') || ''))
    if (href) {
      token.attrSet('href', href)
      if (!href.startsWith('#')) {
        token.attrSet('target', '_blank')
        token.attrSet('rel', 'noopener noreferrer')
      }
    } else {
      token.attrs = (token.attrs || []).filter(([name]) => !['href', 'target', 'rel'].includes(name))
    }
    return renderer.renderToken(tokens, index, options)
  }
  markdown.renderer.rules.image = (tokens, index) => `<span class="guide-image-note">[图片：${escape(tokens[index].content || '已省略')}；不加载图片]</span>`
  markdown.renderer.rules.table_open = () => '<div class="guide-table-wrap" tabindex="0" role="region" aria-label="表格，可横向滚动"><table>'
  markdown.renderer.rules.table_close = () => '</table></div>\n'
  const renderCode = (tokens: MarkdownToken[], index: number) => {
    const token = tokens[index]
    const language = token.info.trim().split(/\s+/)[0] || 'text'
    const id = `guide-code-${codeBlocks.length + 1}`
    // Keep the fence content separately: never copy escaped HTML or DOM text.
    codeBlocks.push({ id, language, content: token.content })
    return `<div class="guide-code"><div class="guide-code-toolbar"><span class="guide-code-language">${escape(language)}</span><button type="button" class="button guide-copy" data-copy-code="${id}" aria-label="复制 ${escape(language)} 代码">复制代码</button></div><pre tabindex="0" aria-label="${escape(language)} 代码，可横向滚动"><code>${escape(token.content)}</code></pre></div>\n`
  }
  markdown.renderer.rules.fence = renderCode
  markdown.renderer.rules.code_block = renderCode

  const tokens = markdown.parse(source, {})
  for (let index = 0; index < tokens.length; index++) {
    const token = tokens[index]
    if (token.type !== 'heading_open' || !['h2', 'h3'].includes(token.tag)) continue
    const text = inlineText(tokens[index + 1]?.children || null).trim()
    const base = headingSlug(text)
    let id = base
    for (let suffix = 2; ids.has(id); suffix++) id = `${base}-${suffix}`
    ids.add(id)
    token.attrSet('id', id)
    headings.push({ id, text, level: token.tag === 'h2' ? 2 : 3 })
  }
  const rendered = markdown.renderer.render(tokens, markdown.options, {})
  const html = DOMPurify.sanitize(rendered, {
    ALLOWED_TAGS: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'blockquote', 'ul', 'ol', 'li', 'strong', 'em', 's', 'a', 'code', 'pre', 'hr', 'br', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'div', 'span', 'button'],
    ALLOWED_ATTR: ['id', 'class', 'href', 'title', 'target', 'rel', 'type', 'aria-label', 'role', 'tabindex', 'data-copy-code', 'start'],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
    ALLOWED_URI_REGEXP: /^(?:https?:\/\/|#)/i,
    ADD_URI_SAFE_ATTR: ['target', 'rel', 'data-copy-code', 'aria-label', 'role', 'tabindex', 'start'],
    FORBID_ATTR: ['src', 'srcset', 'style'],
    RETURN_TRUSTED_TYPE: false,
  })
  return { html, headings, codeBlocks }
}

export const integrationGuide = parseGuide(integrationMarkdown)
