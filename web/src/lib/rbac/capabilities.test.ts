import { describe, expect, it } from 'vitest'

import type { Permission } from '#/lib/api/types'

import {
  CAPABILITIES,
  describeEffective,
  isCapabilityActive,
  permKey,
  toggleCapability,
} from './capabilities'

const cap = (id: string) => {
  const found = CAPABILITIES.find((c) => c.id === id)
  if (!found) throw new Error(`no such capability: ${id}`)
  return found
}

const keys = (perms: Permission[]) => new Set(perms.map(permKey))

describe('isCapabilityActive', () => {
  // A multi-grant capability is all-or-nothing: showing "View CI" as enabled
  // when only half its permissions are present would misreport real access.
  it('requires every grant, not just one', () => {
    const viewCI = cap('ci-view')
    expect(viewCI.grants.length).toBeGreaterThan(1)

    const partial = new Set(['project:read'])
    expect(isCapabilityActive(viewCI, partial)).toBe(false)

    const complete = new Set(['project:read', 'run:read'])
    expect(isCapabilityActive(viewCI, complete)).toBe(true)
  })

  it('treats the *:* wildcard as granting everything', () => {
    const wildcard = new Set(['*:*'])
    for (const c of CAPABILITIES) {
      expect(isCapabilityActive(c, wildcard), `${c.id} under wildcard`).toBe(true)
    }
  })
})

describe('toggleCapability', () => {
  it('adds every grant when turned on', () => {
    const gates = cap('ci-gates')
    const result = toggleCapability(gates, [], true)

    expect(keys(result)).toEqual(keys(gates.grants))
  })

  it('removes every grant when turned off', () => {
    const gates = cap('ci-gates')
    const result = toggleCapability(gates, [...gates.grants], false)

    expect(result).toEqual([])
  })

  // Toggling is a union/difference over the permission array, so it must not
  // disturb permissions belonging to other capabilities.
  it('leaves unrelated permissions untouched', () => {
    const unrelated: Permission = { object: 'audit', action: 'read' }
    const gates = cap('ci-gates')

    const on = toggleCapability(gates, [unrelated], true)
    expect(keys(on).has('audit:read')).toBe(true)

    const off = toggleCapability(gates, on, false)
    expect([...keys(off)]).toEqual(['audit:read'])
  })

  it('is idempotent — toggling on twice grants no duplicates', () => {
    const gates = cap('ci-gates')
    const once = toggleCapability(gates, [], true)
    const twice = toggleCapability(gates, once, true)

    expect(twice).toHaveLength(once.length)
    expect(keys(twice)).toEqual(keys(once))
  })

  // Two capabilities can grant the same permission; turning one off must not
  // silently revoke access the other still requires.
  it('does not mutate the array it was given', () => {
    const original: Permission[] = [{ object: 'run', action: 'read' }]
    const snapshot = [...original]

    toggleCapability(cap('ci-trigger'), original, true)

    expect(original).toEqual(snapshot)
  })
})

describe('describeEffective', () => {
  it('reports the wildcard as full access and nothing else', () => {
    const result = describeEffective([{ object: '*', action: '*' }])

    expect(result.wildcard).toBe(true)
    expect(result.extra).toBe(0)
    expect(result.lines).toHaveLength(1)
  })

  it('names each fully-granted capability', () => {
    const result = describeEffective(cap('ci-view').grants)

    expect(result.lines).toContain('View CI')
    expect(result.wildcard).toBe(false)
  })

  // Permissions outside every bundle still have to be surfaced, or the summary
  // would understate what a role can do.
  it('counts granular permissions no capability covers', () => {
    const result = describeEffective([
      ...cap('ci-view').grants,
      { object: 'something', action: 'exotic' },
    ])

    expect(result.lines).toContain('View CI')
    expect(result.extra).toBe(1)
  })

  it('does not count a capability grant as extra', () => {
    const result = describeEffective(cap('ci-view').grants)
    expect(result.extra).toBe(0)
  })

  it('returns no lines for an empty permission set', () => {
    expect(describeEffective([])).toEqual({ lines: [], extra: 0, wildcard: false })
  })
})

describe('CAPABILITIES registry', () => {
  it('has unique ids', () => {
    const ids = CAPABILITIES.map((c) => c.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('gives every capability at least one grant', () => {
    for (const c of CAPABILITIES) {
      expect(c.grants.length, `${c.id} must grant something`).toBeGreaterThan(0)
    }
  })
})
