/* cymbal.sh — the whole build.
 *
 * The page is rendered from ../MANUAL.md. That file is the single source of
 * truth: it is the manual an agent reads after cloning the repo, and it is the
 * manual served at cymbal.sh. There is no second copy to drift.
 *
 * MANUAL.md is plain CommonMark — nothing in it is site-specific syntax. The
 * conventions below are how ordinary Markdown becomes the richer components:
 *
 *   # Title                 page title; the paragraphs under it become the hero
 *   ## Heading              a <section>, and one entry in the sticky rail
 *   ### Heading             a mono subhead
 *   #### name — note        a collapsible row; text after " — " is the muted tail
 *   > blockquote            an accent callout
 *   ```console              a terminal block: prompts tinted, comments dimmed
 *   ```console title="X"    the same, with a labelled copy bar
 *   | a | b |               a table that scrolls rather than overflowing
 *
 * Read on GitHub, all of that is just a well-formed Markdown document. marked
 * runs at build time only — the page still ships zero framework JavaScript, and
 * the ~15 lines at the bottom are the copy-button handler.
 */

import { execFileSync } from 'node:child_process'
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { Marked } from 'marked'

const root = (p) => new URL(p, import.meta.url)

const [md, css, stars] = await Promise.all([
  readFile(root('../MANUAL.md'), 'utf8'),
  readFile(root('src/page.css'), 'utf8'),
  readFile(root('src/stars.json'), 'utf8')
    .then(JSON.parse)
    .catch(() => ({ cymbal: null })),
])

/* Version comes from the repo's own tags, so the site can't drift from what
 * `cymbal version` reports. Falls back to unversioned rather than guessing. */
let version = ''
try {
  version = execFileSync('git', ['describe', '--tags', '--abbrev=0'], {
    cwd: new URL('..', import.meta.url).pathname,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'ignore'],
  }).trim()
} catch {
  version = ''
}

const esc = (s) =>
  String(s).replace(
    /[&<>"']/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
  )

const slug = (s) =>
  s
    .toLowerCase()
    .replace(/[^\w\s-]/g, '')
    .trim()
    .replace(/\s+/g, '-')

/* ---------- inline ---------- */

const marked = new Marked({ gfm: true })

/* marked's inline pass handles emphasis, links and entities. The only thing it
 * gets wrong for this design is bare <code>, which needs the .inline class. */
const inline = (src) =>
  marked.parseInline(src).replace(/<code>/g, '<code class="inline">')

/* ---------- terminal blocks ---------- */

const SHELL = new Set(['console', 'bash', 'sh', 'shell', 'text', ''])

/* Highlighting is derived from shell shape, not from a grammar: a leading $ or
 * > is a prompt, a # run is a comment, and quoted strings are the argument a
 * reader is most likely scanning for. Everything else stays plain. */
const highlight = (line) => {
  const prompt = line.match(/^(\s*)([$>]) (.*)$/)
  const lead = prompt ? `${prompt[1]}<span class="p">${prompt[2]}</span> ` : ''
  let body = prompt ? prompt[3] : line

  if (/^\s*#/.test(body)) return `${lead}<span class="dim">${esc(body)}</span>`

  let trailing = ''
  const comment = body.match(/^(.*?)(\s+#\s.*)$/)
  if (comment) {
    body = comment[1]
    trailing = `<span class="dim">${esc(comment[2])}</span>`
  }

  const code = esc(body).replace(
    /(&quot;[^&]*?&quot;|&#39;[^&]*?&#39;)/g,
    '<span class="key">$1</span>',
  )
  return lead + code + trailing
}

const terminal = (text, title) => {
  const body = text.split('\n').map(highlight).join('\n')
  const pre = `<pre><code>${body}</code></pre>`
  if (!title) return `      ${pre}`

  /* Copy the commands, not the prompts — pasting a leading $ into a shell is
   * the single most common way a copied install line fails. */
  const copy = text
    .split('\n')
    .map((l) => l.replace(/^(\s*)[$>] /, '$1'))
    .join('\n')

  return `      <div class="copyblock">
        <div class="cb-head">
          <span>${esc(title)}</span>
          <button type="button" data-copy="${esc(copy)}">Copy</button>
        </div>
        ${pre}
      </div>`
}

/* ---------- block rendering ---------- */

const cell = (raw, first) => {
  const bare = raw.match(/^`([^`]+)`$/)
  if (bare) return `<td class="cmd">${esc(bare[1])}</td>`
  return `<td${first ? '' : ' class="no"'}>${inline(raw)}</td>`
}

const table = (t) => {
  const head = t.header.map((h) => `<th>${inline(h.text)}</th>`).join('')
  const rows = t.rows
    .map(
      (r) =>
        `            <tr>${r.map((c, i) => cell(c.text, i === 0)).join('')}</tr>`,
    )
    .join('\n')
  return `      <div class="scroll">
        <table>
          <thead><tr>${head}</tr></thead>
          <tbody>
${rows}
          </tbody>
        </table>
      </div>`
}

/* `note` marks the muted, smaller paragraph style used inside disclosures. */
const block = (tok, note) => {
  switch (tok.type) {
    case 'paragraph':
      return `      <p${note ? ' class="note"' : ''}>${inline(tok.text)}</p>`
    case 'heading':
      return `      <h${tok.depth}>${inline(tok.text)}</h${tok.depth}>`
    case 'code': {
      const [lang = '', ...rest] = (tok.lang || '').split(/\s+/)
      const title = (tok.lang || '').match(/title="([^"]+)"/)?.[1]
      if (!SHELL.has(lang)) return `      <pre><code>${esc(tok.text)}</code></pre>`
      void rest
      return terminal(tok.text, title)
    }
    case 'table':
      return table(tok)
    case 'blockquote':
      return `      <div class="callout">
${tok.tokens.map((t) => block(t, false)).join('\n')}
      </div>`
    case 'list':
      return `      <ul class="plain">
${tok.items.map((i) => `        <li>${inline(i.text)}</li>`).join('\n')}
      </ul>`
    case 'space':
      return ''
    default:
      return tok.raw ? `      ${tok.raw.trim()}` : ''
  }
}

/* ---------- document walk ---------- */

const tokens = marked.lexer(md)

let title = 'cymbal'
const intro = []
const sections = []

/* A #### heading opens a disclosure that swallows every block after it until
 * the next heading of equal or higher rank. Consecutive disclosures are wrapped
 * in one .discs run so their rules meet. */
let current = null
let disc = null

const flushDisc = () => {
  if (!disc) return
  current.blocks.push({ kind: 'discs', items: disc })
  disc = null
}

for (const tok of tokens) {
  if (tok.type === 'heading' && tok.depth === 1) {
    title = tok.text
    continue
  }

  if (tok.type === 'heading' && tok.depth === 2) {
    flushDisc()
    current = { id: slug(tok.text), label: tok.text, blocks: [] }
    sections.push(current)
    continue
  }

  if (!current) {
    if (tok.type === 'paragraph') intro.push(inline(tok.text))
    continue
  }

  if (tok.type === 'heading' && tok.depth === 4) {
    const [name, tail] = tok.text.split(/\s+—\s+/)
    if (!disc) disc = []
    disc.push({ name, tail, blocks: [] })
    continue
  }

  if (tok.type === 'heading' && tok.depth <= 3) flushDisc()

  if (disc) disc[disc.length - 1].blocks.push(tok)
  else current.blocks.push(tok)
}
flushDisc()

if (!sections.length) {
  throw new Error('no ## sections found in MANUAL.md — nothing to render')
}

const renderDiscs = (items) => `      <div class="discs">
${items
  .map(
    (d) => `        <details>
          <summary>${esc(d.name)}${d.tail ? ` <span class="sm">${esc(d.tail)}</span>` : ''}</summary>
          <div class="disc-body">
${d.blocks.map((t) => block(t, t.type === 'paragraph')).join('\n')}
          </div>
        </details>`,
  )
  .join('\n')}
      </div>`

const body = sections
  .map(
    (s) => `      <section id="${s.id}">
        <h2>${esc(s.label)}</h2>
${s.blocks
  .map((b) => (b.kind === 'discs' ? renderDiscs(b.items) : block(b, false)))
  .filter(Boolean)
  .join('\n')}
      </section>`,
  )
  .join('\n\n')

const rail = sections
  .map((s) => `          <li><a href="#${s.id}">${esc(s.label)}</a></li>`)
  .join('\n')

/* The meta description is the first sentence of the manual's own opening
 * paragraph, stripped of markup — one fewer string to keep in sync. */
const plain = (html) => html.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim()
const description = plain(intro[0] ?? '')

const GH_MARK = `<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z"/></svg>`

const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)} — code navigation for coding agents</title>
<meta name="description" content="${esc(description)}">
<link rel="canonical" href="https://cymbal.sh/">
<meta property="og:type" content="website">
<meta property="og:url" content="https://cymbal.sh/">
<meta property="og:title" content="${esc(title)}">
<meta property="og:description" content="${esc(description)}">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Serif:wght@400&display=swap">
<style>
${css.trim()}
</style>
</head>
<body>
<div class="wrap">

  <header class="masthead">
    <span class="mark">${esc(title)}</span>${version ? `\n    <span class="ver">${version}</span>` : ''}
    <nav>
      <a href="#install">install</a>
      <a class="gh" href="https://github.com/1broseidon/cymbal">
        ${GH_MARK}${
          typeof stars.cymbal === 'number'
            ? `\n        <span class="stars">${stars.cymbal}</span>`
            : ''
        }
      </a>
    </nav>
  </header>

  <div class="layout">
    <main class="col">

      <div class="hero">
        <h1>${esc(title)}</h1>
        <div class="rule"></div>
${intro.map((p, i) => `        <p class="${i === 0 ? 'lede' : 'sub'}">${p}</p>`).join('\n')}
      </div>

${body}

      <footer>
        <span>${esc(title)}${version ? ` ${version}` : ''}</span>
        <span>MIT licensed</span>
        <span>Built with tree-sitter and SQLite</span>
        <span class="spacer"><a href="https://chain.sh">chain.sh</a></span>
      </footer>

    </main>

    <aside class="rail">
      <nav aria-label="Contents">
        <p class="rail-label">Contents</p>
        <ol>
${rail}
        </ol>
      </nav>
    </aside>
  </div>

</div>

<script>
document.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-copy]')
  if (!btn) return
  navigator.clipboard.writeText(btn.dataset.copy).then(() => {
    const original = btn.textContent
    btn.textContent = 'Copied'
    btn.dataset.copied = '1'
    setTimeout(() => {
      btn.textContent = original
      delete btn.dataset.copied
    }, 1600)
  })
})
</script>
</body>
</html>
`

await rm(root('dist'), { recursive: true, force: true })
await mkdir(root('dist'), { recursive: true })
await writeFile(root('dist/index.html'), html)

const kb = (n) => `${(n / 1024).toFixed(2)} kB`
const discs = sections.reduce(
  (n, s) => n + s.blocks.filter((b) => b.kind === 'discs').reduce((m, b) => m + b.items.length, 0),
  0,
)
console.log(
  `built dist/index.html  ${kb(Buffer.byteLength(html))}  ·  ${sections.length} sections  ·  ${discs} disclosures  ·  ${version || 'no tag'}`,
)
