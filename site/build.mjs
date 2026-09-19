/* cymbal.sh — the whole build.
 *
 * Reads src/manual.html + src/page.css, derives the sticky rail from the
 * section headings, injects the current version and star count, and writes one
 * self-contained dist/index.html. No framework, no bundler, no dependencies.
 *
 * The only JavaScript that ships is the copy-button handler at the bottom —
 * roughly fifteen lines, for the one genuinely interactive thing on the page.
 */

import { execFileSync } from 'node:child_process'
import { mkdir, readFile, rm, writeFile } from 'node:fs/promises'

const root = (p) => new URL(p, import.meta.url)

const [manual, css, stars] = await Promise.all([
  readFile(root('src/manual.html'), 'utf8'),
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

/* The rail is derived, not hand-maintained: every <section id> gets an entry,
 * in document order. Add a section and it appears.
 *
 * The rail label is the section's eyebrow, not its <h2>. The eyebrows are
 * already short ("Choosing a command"); the headings are sentences ("Which
 * direction are you asking about?") and shred into four lines in an 11rem
 * column. The heading rides along as the link's title attribute. */
const sections = [
  ...manual.matchAll(
    /<section id="([^"]+)">\s*<p class="eyebrow">([^<]+)<\/p>\s*<h2>([^<]+)<\/h2>/g,
  ),
].map(([, id, label, heading]) => ({
  id,
  label: label.trim(),
  heading: heading.trim(),
}))

if (!sections.length) {
  throw new Error('no sections matched — check the section/eyebrow/h2 shape in src/manual.html')
}

const rail = sections
  .map(
    (s) =>
      `        <li><a href="#${s.id}" title="${s.heading.replace(/"/g, '&quot;')}">${s.label}</a></li>`,
  )
  .join('\n')

const GH_MARK = `<svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z"/></svg>`

const AGENT_BRIEF = `Install cymbal and index this repo for me.
1. Run: brew install 1broseidon/tap/cymbal
2. Run \`cymbal index .\` at the repo root.
3. Run \`cymbal structure\` and summarise the entry points for me.
4. Run \`cymbal hook install claude-code\` so you keep using it.
From here on, prefer \`cymbal search\` over grep for symbol lookup,
\`cymbal show <sym>\` over reading a whole file, and
\`cymbal impact <sym>\` before changing anything shared.`

const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>cymbal — code navigation for coding agents</title>
<meta name="description" content="Cymbal indexes a repo with tree-sitter, then answers structural questions about it: what is this symbol, who calls it, what does it call, and what breaks if it changes.">
<link rel="canonical" href="https://cymbal.sh/">
<meta property="og:type" content="website">
<meta property="og:url" content="https://cymbal.sh/">
<meta property="og:title" content="cymbal">
<meta property="og:description" content="Language-agnostic code navigation CLI powered by tree-sitter.">
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
    <span class="mark">cymbal</span>${version ? `\n    <span class="ver">${version}</span>` : ''}
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
        <h1>cymbal</h1>
        <div class="rule"></div>
        <p class="lede">
          A code navigation CLI that indexes a repository with tree-sitter, then
          answers structural questions about it — what a symbol is, who calls it,
          what it calls, and what breaks if it changes.
        </p>
        <p class="sub">
          For people, it replaces a chain of <code class="inline">grep</code> and
          jump-to-definition. For agents, it turns a dozen file reads into one
          call with <code class="inline">--json</code>.
        </p>

        <div class="copyblock">
          <div class="cb-head">
            <span>Install</span>
            <button type="button" data-copy="brew install 1broseidon/tap/cymbal">Copy</button>
          </div>
          <pre><code><span class="p">$</span> brew install 1broseidon/tap/cymbal</code></pre>
        </div>

        <div class="copyblock">
          <div class="cb-head">
            <span>Or hand it to your agent</span>
            <button type="button" data-copy="${AGENT_BRIEF.replace(/"/g, '&quot;')}">Copy</button>
          </div>
          <pre><code>${AGENT_BRIEF.replace(/`([^`]+)`/g, '<span class="key">$1</span>')}</code></pre>
        </div>
      </div>

${manual.trim()}

      <footer>
        <span>cymbal${version ? ` ${version}` : ''}</span>
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
console.log(
  `built dist/index.html  ${kb(Buffer.byteLength(html))}  ·  ${sections.length} sections  ·  ${version || 'no tag'}`,
)
