const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const ts = require('typescript');
const { JSDOM } = require('jsdom');
const createDOMPurify = require('dompurify');

const dom = new JSDOM('');
const source = fs.readFileSync(path.join(__dirname, '../src/guide.ts'), 'utf8');
const markdownSource = fs.readFileSync(path.join(__dirname, '../../docs/integration.md'), 'utf8');
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
}).outputText;
const exportsObject = {};
vm.runInNewContext(compiled, {
  exports: exportsObject, URL,
  require: name => name.endsWith('integration.md?raw') ? markdownSource : name === 'dompurify' ? createDOMPurify(dom.window) : require(name),
});
const { parseGuide, resolveGuideLink, integrationGuide, guideSections } = exportsObject;
const body = guide => JSDOM.fragment(guide.html);

test('guide renders the single integration document and main-section anchors match its TOC', () => {
  const document = body(integrationGuide);
  assert.equal(document.querySelector('h1').textContent, 'Sakura Statistics Gateway 接入指南');
  assert.ok(integrationGuide.headings.length > 20);
  assert.ok(integrationGuide.codeBlocks.length > 10);
  for (const heading of integrationGuide.headings) {
    const element = document.getElementById(heading.id);
    assert.ok(element);
    assert.equal(element.tagName, `H${heading.level}`);
    assert.equal(element.textContent, heading.text);
  }
  assert.equal(document.getElementById(guideSections.web).textContent, '2. 网页接入');
  assert.equal(document.getElementById(guideSections.desktop).textContent, '3. 桌面客户端接入');
  assert.equal(document.querySelectorAll('script, img, iframe, object').length, 0);
});

test('relative docs and examples links map to the repository main branch without local requests', () => {
  const main = 'https://github.com/Guyao146/Sakura-Statistics-Gateway/blob/main';
  assert.equal(resolveGuideLink('protocol.md'), `${main}/docs/protocol.md`);
  assert.equal(resolveGuideLink('./web-sdk.md#配置'), `${main}/docs/web-sdk.md#%E9%85%8D%E7%BD%AE`);
  assert.equal(resolveGuideLink('../examples/python/sakura_sdk.py'), `${main}/examples/python/sakura_sdk.py`);
  assert.equal(resolveGuideLink('#guide-网页接入'), '#guide-网页接入');
  const document = body(parseGuide('[协议](protocol.md) / [示例](../examples/python/README.md) / [网站](https://example.test/help)'));
  const links = [...document.querySelectorAll('a')];
  assert.deepEqual(links.map(link => link.getAttribute('href')), [
    `${main}/docs/protocol.md`, `${main}/examples/python/README.md`, 'https://example.test/help',
  ]);
  for (const link of links) {
    assert.equal(link.target, '_blank');
    assert.equal(link.rel, 'noopener noreferrer');
  }
});

test('fence language and copied content retain indentation, line breaks and literal characters', () => {
  const code = 'const label = "<head> & 中文";\n  console.log(label);\n';
  const guide = parseGuide(`\n\`\`\`js\n${code}\`\`\`\n`);
  assert.equal(guide.codeBlocks.length, 1);
  assert.equal(guide.codeBlocks[0].content, code);
  assert.equal(guide.codeBlocks[0].language, 'js');
  const document = body(guide);
  assert.equal(document.querySelector('pre code').textContent, code);
  assert.equal(document.querySelector('.guide-code-language').textContent, 'js');
  assert.equal(document.querySelector('button').dataset.copyCode, guide.codeBlocks[0].id);
  assert.equal(document.querySelectorAll('head').length, 0);
});

test('duplicate and formatted h2/h3 headings have unique IDs shared by the body and TOC', () => {
  const guide = parseGuide('## 1. 相同 **标题**\n\n### 相同 标题\n\n## 相同 标题\n\n### `代码` 标题\n');
  const ids = guide.headings.map(heading => heading.id);
  assert.equal(new Set(ids).size, 4);
  assert.deepEqual(Array.from(ids), ['guide-相同-标题', 'guide-相同-标题-2', 'guide-相同-标题-3', 'guide-代码-标题']);
  const document = body(guide);
  for (const heading of guide.headings) assert.equal(document.getElementById(heading.id).textContent, heading.text);
});

test('ordinary untrusted schemes and paths are not links; images and HTML stay inert', () => {
  for (const href of ['file:///manual.pdf', 'ftp://example.test/manual', 'mailto:help@example.test', '//example.test/manual', '../backend/main.go', '../../README.md']) {
    assert.equal(resolveGuideLink(href), null);
  }
  assert.equal(resolveGuideLink('http://example.test/help'), 'http://example.test/help');
  assert.equal(resolveGuideLink('https://example.test/help'), 'https://example.test/help');
  const document = body(parseGuide('[本地文档](file:///manual.pdf)\n\n<aside>原文说明</aside>\n\n![架构图](https://example.test/diagram.png)'));
  assert.equal(document.querySelectorAll('a[href], aside, img, [src], [srcset]').length, 0);
  assert.ok(document.textContent.includes('<aside>原文说明</aside>'));
  assert.ok(document.textContent.includes('架构图'));
});
