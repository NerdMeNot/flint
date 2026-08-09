import { os } from '@orpc/server'
import { z } from 'zod'
import { type Team, type TeamWithMembers, type User, type Role, type Assignment, type ApiKey, type PersonalToken, type AuditEntry, type Org } from './types'
import { backendGet, backendPost, backendPut, backendPatch, backendDelete } from './backend'
import type { Paginated } from './router-shared'

export const teams = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<Team>>('/teams', { limit: input.limit, cursor: input.cursor })
    }),

  get: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendGet<TeamWithMembers>(`/teams/${input.id}`)
    }),

  create: os
    .input(z.object({ name: z.string(), slug: z.string() }))
    .handler(async ({ input }) => {
      return backendPost('/teams', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/teams/${input.id}`)
    }),

  addMembers: os
    .input(z.object({ teamId: z.string(), userIds: z.array(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost(`/teams/${input.teamId}/members`, { userIds: input.userIds })
    }),

  removeMember: os
    .input(z.object({ teamId: z.string(), userId: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/teams/${input.teamId}/members/${input.userId}`)
    }),
}

export const users = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<User>>('/users', { limit: input.limit, cursor: input.cursor })
    }),

  // Provision a local (email + password) user and assign a role. Returns the
  // generated password when one wasn't supplied (shown once).
  create: os
    .input(z.object({
      email: z.string(),
      name: z.optional(z.string()),
      role: z.optional(z.string()),
      password: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendPost<{ id: string; email: string; role: string; generatedPassword?: string }>(
        '/users',
        input,
      )
    }),
}

export const roles = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<Role>>('/roles', { limit: input.limit, cursor: input.cursor })
    }),

  create: os
    .input(
      z.object({
        name: z.string(),
        slug: z.string(),
        description: z.optional(z.string()),
        permissions: z.array(z.object({ object: z.string(), action: z.string() })),
        workspaces: z.array(z.string()),
        environments: z.array(z.string()),
      }),
    )
    .handler(async ({ input }) => {
      return backendPost('/roles', input)
    }),

  update: os
    .input(
      z.object({
        id: z.string(),
        name: z.optional(z.string()),
        description: z.optional(z.string()),
        permissions: z.optional(z.array(z.object({ object: z.string(), action: z.string() }))),
        workspaces: z.optional(z.array(z.string())),
        environments: z.optional(z.array(z.string())),
      }),
    )
    .handler(async ({ input }) => {
      const { id, ...body } = input
      return backendPatch(`/roles/${id}`, body)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/roles/${input.id}`)
    }),

  assignments: {
    list: os
      .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
      .handler(async ({ input }) => {
        return backendGet<Paginated<Assignment>>('/roles/assignments', { limit: input.limit, cursor: input.cursor })
      }),

    create: os
      .input(z.object({ subjects: z.array(z.string()), role: z.string() }))
      .handler(async ({ input }) => {
        return backendPost('/roles/assignments', input)
      }),

    delete: os
      .input(z.object({ subject: z.string(), role: z.string() }))
      .handler(async ({ input }) => {
        return backendDelete(`/roles/assignments/${encodeURIComponent(input.subject)}?role=${input.role}`)
      }),
  },
}

export const apiKeys = {
  list: os
    .input(z.object({ limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<ApiKey>>('/api-keys', { limit: input.limit, cursor: input.cursor })
    }),

  create: os
    .input(z.object({
      name: z.string(),
      role: z.string(),
      workspaces: z.array(z.string()),
      environments: z.array(z.string()),
      expiresAt: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendPost('/api-keys', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/api-keys/${input.id}`)
    }),
}

export const personalTokens = {
  list: os
    .input(z.object({ userId: z.string(), limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<PersonalToken>>('/personal-tokens', { limit: input.limit, cursor: input.cursor })
    }),

  create: os
    .input(z.object({ userId: z.string(), name: z.string(), expiresAt: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendPost('/personal-tokens', input)
    }),

  delete: os
    .input(z.object({ id: z.string() }))
    .handler(async ({ input }) => {
      return backendDelete(`/personal-tokens/${input.id}`)
    }),
}

export const org = {
  get: os.handler(async () => {
    return backendGet<Org>('/org')
  }),
  setPolicy: os
    .input(z.object({ requireProjectWorkspace: z.boolean() }))
    .handler(async ({ input }) => {
      return backendPut('/org/policy', input)
    }),
}

export const auditEntries = {
  list: os
    .input(z.object({ action: z.optional(z.string()), limit: z.optional(z.number()), cursor: z.optional(z.string()) }))
    .handler(async ({ input }) => {
      return backendGet<Paginated<AuditEntry>>('/audit-entries', {
          action: input.action, limit: input.limit, cursor: input.cursor,
        })
    }),
}
