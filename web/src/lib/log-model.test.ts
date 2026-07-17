import { describe, it, expect } from 'vitest'
import { buildLogModel, defaultOpen } from './log-model'

const at = (secs: number) => new Date(1700000000000 + secs * 1000).toISOString()

describe('buildLogModel', () => {
  it('puts everything in one header-less preamble when there are no markers', () => {
    const groups = buildLogModel([{ content: 'a' }, { content: 'b' }])
    expect(groups).toHaveLength(1)
    expect(groups[0]).toMatchObject({ title: null, marker: null })
    expect(groups[0]!.entries.map((e) => e.line)).toEqual([1, 2])
    expect(defaultOpen(groups[0]!)).toBe(true)
  })

  it('splits sections on ---/+++/~~~ markers with the right defaults', () => {
    const groups = buildLogModel([
      { content: '~~~ Preparing machine' },
      { content: 'pulled image' },
      { content: '--- Restoring cache' },
      { content: 'cache hit' },
      { content: '+++ Running tests' },
      { content: 'ok 12 tests' },
    ])
    expect(groups.map((g) => [g.title, g.marker])).toEqual([
      ['Preparing machine', 'muted'],
      ['Restoring cache', 'collapsed'],
      ['Running tests', 'expanded'],
    ])
    expect(groups.map(defaultOpen)).toEqual([false, false, true])
    // Marker lines are excluded from entries but keep global numbering.
    expect(groups[1]!.entries[0]).toMatchObject({ content: 'cache hit', line: 4 })
  })

  it('recognizes ::group::/::endgroup:: and continues header-less after close', () => {
    const groups = buildLogModel([
      { content: '::group::Install deps' },
      { content: 'npm ci' },
      { content: '::endgroup::' },
      { content: 'after' },
    ])
    expect(groups).toHaveLength(2)
    expect(groups[0]).toMatchObject({ title: 'Install deps', marker: 'collapsed' })
    expect(groups[1]).toMatchObject({ title: null })
    expect(groups[1]!.entries[0]!.content).toBe('after')
  })

  it('detects markers behind ANSI coloring', () => {
    const groups = buildLogModel([{ content: '\x1b[90m--- Cleanup\x1b[0m' }, { content: 'rm -rf tmp' }])
    expect(groups[0]).toMatchObject({ title: 'Cleanup', marker: 'collapsed' })
  })

  it('computes group durations from marker line to next marker', () => {
    const groups = buildLogModel([
      { content: '--- Build', timestamp: at(0) },
      { content: 'compiling', timestamp: at(1) },
      { content: '--- Test', timestamp: at(10) },
      { content: 'testing', timestamp: at(11) },
      { content: 'done', timestamp: at(14) },
    ])
    expect(groups[0]!.durationMs).toBe(10_000)
    // Final group falls back to its last line.
    expect(groups[1]!.durationMs).toBe(4_000)
  })

  it('leaves duration null without timestamps', () => {
    const groups = buildLogModel([{ content: '--- Build' }, { content: 'x' }])
    expect(groups[0]!.durationMs).toBeNull()
  })
})
