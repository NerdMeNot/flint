import { describe, it, expect } from 'vitest'
import { parseAnsiLine, stripAnsi } from './ansi'

describe('parseAnsiLine', () => {
  it('passes plain text through as one segment', () => {
    const { segments, state } = parseAnsiLine('hello world')
    expect(segments).toEqual([{ text: 'hello world', style: {} }])
    expect(state).toEqual({})
  })

  it('colors basic SGR foreground text and resets', () => {
    const { segments } = parseAnsiLine('\x1b[32mok\x1b[0m done')
    expect(segments).toHaveLength(2)
    expect(segments[0]).toMatchObject({ text: 'ok', style: { fg: '#3fb950' } })
    expect(segments[1]).toMatchObject({ text: ' done', style: {} })
  })

  it('combines bold with bright colors', () => {
    const { segments } = parseAnsiLine('\x1b[1;91mFAIL\x1b[m')
    expect(segments[0]).toMatchObject({ text: 'FAIL', style: { bold: true, fg: '#ffa198' } })
  })

  it('carries style state across lines', () => {
    const first = parseAnsiLine('\x1b[33mwarning: something')
    expect(first.state).toEqual({ fg: '#d29922' })
    const second = parseAnsiLine('  continued', first.state)
    expect(second.segments[0]).toMatchObject({ text: '  continued', style: { fg: '#d29922' } })
  })

  it('supports 256-color and truecolor', () => {
    const c256 = parseAnsiLine('\x1b[38;5;196mred\x1b[0m')
    expect(c256.segments[0]!.style.fg).toBe('rgb(255,0,0)')
    const tc = parseAnsiLine('\x1b[38;2;10;20;30mx')
    expect(tc.segments[0]!.style.fg).toBe('rgb(10,20,30)')
  })

  it('strips non-SGR CSI and OSC sequences', () => {
    expect(stripAnsi('\x1b[2K\x1b[1Gprogress')).toBe('progress')
    expect(stripAnsi('\x1b]0;title\x07text')).toBe('text')
    expect(stripAnsi('\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\')).toBe('link')
  })

  it('renders only the final overwrite of \\r progress lines', () => {
    expect(stripAnsi('Downloading 10%\rDownloading 55%\rDownloading 100%')).toBe('Downloading 100%')
    expect(stripAnsi('done\r')).toBe('done')
  })

  it('handles unterminated escapes without hanging', () => {
    expect(stripAnsi('text\x1b')).toBe('text')
    expect(stripAnsi('\x1b[abc')).toBe('bc')
  })
})
