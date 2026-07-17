// Minimal ANSI SGR renderer for build logs. Parses escape sequences into
// styled segments the log view turns into spans; every non-SGR escape
// (cursor movement, OSC titles/hyperlinks) is stripped. Style state carries
// across lines — a color opened on one line tints the following lines until
// reset, which is how real tools (cargo, jest, docker) emit output.

export interface AnsiStyle {
  fg?: string
  bg?: string
  bold?: boolean
  dim?: boolean
  italic?: boolean
  underline?: boolean
  strike?: boolean
}

export interface AnsiSegment {
  text: string
  style: AnsiStyle
}

// GitHub-dark terminal palette — matches the log surface (#0d1117) the UI uses.
const PALETTE_16: string[] = [
  '#484f58', '#ff7b72', '#3fb950', '#d29922', '#58a6ff', '#bc8cff', '#39c5cf', '#b1bac4',
  '#6e7681', '#ffa198', '#56d364', '#e3b341', '#79c0ff', '#d2a8ff', '#56d4dd', '#f0f6fc',
]

// xterm 256-color cube levels.
const CUBE_LEVELS = [0, 95, 135, 175, 215, 255]

function color256(n: number): string {
  if (n < 16) return PALETTE_16[n] ?? '#b1bac4'
  if (n < 232) {
    const i = n - 16
    const r = CUBE_LEVELS[Math.floor(i / 36)]
    const g = CUBE_LEVELS[Math.floor(i / 6) % 6]
    const b = CUBE_LEVELS[i % 6]
    return `rgb(${r},${g},${b})`
  }
  const gray = 8 + (n - 232) * 10
  return `rgb(${gray},${gray},${gray})`
}

// Applies one SGR parameter list (the numbers in `ESC[…m`) to a style.
function applySgr(style: AnsiStyle, params: number[]): AnsiStyle {
  let s = { ...style }
  for (let i = 0; i < params.length; i++) {
    const p = params[i] ?? 0
    if (p === 0) s = {}
    else if (p === 1) s.bold = true
    else if (p === 2) s.dim = true
    else if (p === 3) s.italic = true
    else if (p === 4) s.underline = true
    else if (p === 9) s.strike = true
    else if (p === 22) { delete s.bold; delete s.dim }
    else if (p === 23) delete s.italic
    else if (p === 24) delete s.underline
    else if (p === 29) delete s.strike
    else if (p >= 30 && p <= 37) s.fg = PALETTE_16[p - 30]
    else if (p === 39) delete s.fg
    else if (p >= 40 && p <= 47) s.bg = PALETTE_16[p - 40]
    else if (p === 49) delete s.bg
    else if (p >= 90 && p <= 97) s.fg = PALETTE_16[p - 90 + 8]
    else if (p >= 100 && p <= 107) s.bg = PALETTE_16[p - 100 + 8]
    else if (p === 38 || p === 48) {
      // Extended color: 38;5;n (256-color) or 38;2;r;g;b (truecolor).
      const target = p === 38 ? 'fg' : 'bg'
      const mode = params[i + 1]
      if (mode === 5 && params[i + 2] != null) {
        s[target] = color256(params[i + 2]!)
        i += 2
      } else if (mode === 2 && params[i + 4] != null) {
        s[target] = `rgb(${params[i + 2]},${params[i + 3]},${params[i + 4]})`
        i += 4
      }
    }
  }
  return s
}

// ESC[ params final — CSI sequences; only final byte `m` (SGR) is interpreted.
// eslint-disable-next-line no-control-regex
const CSI_RE = /\x1b\[([0-9;]*)([\x40-\x7e])/
// ESC] … (BEL | ESC\) — OSC sequences (titles, hyperlinks); always stripped.
// eslint-disable-next-line no-control-regex
const OSC_RE = /\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/
// Any other lone escape (ESC( charset selection etc.).
// eslint-disable-next-line no-control-regex
const MISC_ESC_RE = /\x1b[\x40-\x5f]?/

export function hasAnsi(text: string): boolean {
  return text.includes('\x1b')
}

/**
 * Parses one line into styled segments, starting from (and returning) the
 * carry-over style state so multi-line colors survive line splitting.
 */
export function parseAnsiLine(
  raw: string,
  state: AnsiStyle = {},
): { segments: AnsiSegment[]; state: AnsiStyle } {
  // Progressive lines (pip/docker progress bars) overwrite themselves with \r;
  // render the final overwrite only. SGR state opened in a discarded prefix is
  // intentionally dropped with it.
  let text = raw
  if (text.endsWith('\r')) text = text.slice(0, -1)
  if (text.includes('\r')) {
    const parts = text.split('\r')
    text = parts[parts.length - 1] || parts[parts.length - 2] || ''
  }

  if (!text.includes('\x1b')) {
    return { segments: text ? [{ text, style: state }] : [], state }
  }

  const segments: AnsiSegment[] = []
  let style = state
  let rest = text
  while (rest.length > 0) {
    const esc = rest.indexOf('\x1b')
    if (esc < 0) {
      segments.push({ text: rest, style })
      break
    }
    if (esc > 0) {
      segments.push({ text: rest.slice(0, esc), style })
      rest = rest.slice(esc)
    }
    const csi = CSI_RE.exec(rest)
    if (csi && csi.index === 0) {
      if (csi[2] === 'm') {
        const params = csi[1] === '' ? [0] : csi[1]!.split(';').map((n) => parseInt(n || '0', 10))
        style = applySgr(style, params)
      }
      rest = rest.slice(csi[0].length)
      continue
    }
    const osc = OSC_RE.exec(rest)
    if (osc && osc.index === 0) {
      rest = rest.slice(osc[0].length)
      continue
    }
    const misc = MISC_ESC_RE.exec(rest)
    if (misc && misc.index === 0) {
      rest = rest.slice(Math.max(misc[0].length, 1))
      continue
    }
    rest = rest.slice(1)
  }

  return { segments: segments.filter((s) => s.text.length > 0), state: style }
}

/** Strips all escape sequences (and \r overwrites), returning plain text. */
export function stripAnsi(raw: string): string {
  return parseAnsiLine(raw).segments.map((s) => s.text).join('')
}
