import { os } from '@orpc/server'
import { z } from 'zod'
import { type AuthUser, type Session, type MfaSetup, type AuthProviders, type ProviderTestResult, type TestLoginStart, type TestLoginResult, type GroupMappings, type ScimStatus, type SignInLog, providerConfigSchema } from './types'
import { backendGet, backendPost, backendPut, backendDelete, backendGetRoot, backendPostRoot, backendPutRoot, backendDeleteRoot } from './backend'

export const auth = {
  // The session/auth endpoints live at the server root (not /api/v1), so they use
  // the *Root backend helpers.
  me: os.handler(async () => {
    return backendGetRoot<AuthUser>('/auth/me')
  }),

  updateProfile: os
    .input(z.object({
      name: z.optional(z.string()),
      avatarUrl: z.optional(z.string()),
      themeMode: z.optional(z.enum(['light', 'dark', 'auto'])),
      colorTheme: z.optional(z.string()),
    }))
    .handler(async ({ input }) => {
      return backendPutRoot('/auth/profile', input)
    }),

  changePassword: os
    .input(z.object({ currentPassword: z.string(), newPassword: z.string() }))
    .handler(async ({ input }) => {
      return backendPostRoot('/auth/change-password', input)
    }),

  sessions: {
    list: os.handler(async () => {
      return backendGetRoot<{ items: Session[] }>('/auth/sessions').then((r) => r.items)
    }),
    revoke: os
      .input(z.object({ id: z.string() }))
      .handler(async ({ input }) => {
        return backendDeleteRoot(`/auth/sessions/${input.id}`)
      }),
  },

  mfa: {
    setup: os.handler(async () => {
      return backendPostRoot<MfaSetup>('/auth/mfa/setup')
    }),
    verifySetup: os
      .input(z.object({ code: z.string() }))
      .handler(async ({ input }) => {
        return backendPostRoot('/auth/mfa/setup/verify', input)
      }),
    disable: os
      .input(z.object({ code: z.string() }))
      .handler(async () => {
        return backendDeleteRoot('/auth/mfa')
      }),
    regenerateRecoveryCodes: os
      .input(z.object({ code: z.optional(z.string()), recoveryCode: z.optional(z.string()) }))
      .handler(async ({ input }) => {
        return backendPostRoot<{ recoveryCodes: string[] }>('/auth/mfa/recovery-codes', input)
      }),
  },

  providers: {
    list: os.handler(async () => {
      return backendGet<AuthProviders>('/auth/providers')
    }),
    setRequireSso: os
      .input(z.object({ enabled: z.boolean() }))
      .handler(async ({ input }) => {
        return backendPut<{ requireSso: boolean }>('/auth/require-sso', input)
      }),
    save: os
      .input(z.object({
        providerType: z.enum(['oidc', 'saml']),
        displayName: z.optional(z.string()),
        config: providerConfigSchema,
      }))
      .handler(async ({ input }) => {
        return backendPut('/auth/provider', input)
      }),
    test: os
      .input(z.object({
        providerType: z.enum(['oidc', 'saml']),
        config: providerConfigSchema,
      }))
      .handler(async ({ input }) => {
        return backendPost<ProviderTestResult>('/auth/provider/test', input)
      }),
    testLoginStart: os
      .input(z.object({
        providerType: z.enum(['oidc', 'saml']),
        config: providerConfigSchema,
      }))
      .handler(async ({ input }) => {
        return backendPost<TestLoginStart>('/auth/provider/test-login', input)
      }),
    testLoginResult: os
      .input(z.object({ id: z.string() }))
      .handler(async ({ input }) => {
        return backendGet<TestLoginResult>(`/auth/provider/test-login/${input.id}`)
      }),
    delete: os
      .input(z.object({ providerType: z.string() }))
      .handler(async ({ input }) => {
        return backendDelete(`/auth/provider/${input.providerType}`)
      }),
    groupMappings: {
      get: os.handler(async () => {
        return backendGet<GroupMappings>('/auth/group-mappings')
      }),
      save: os
        .input(z.object({
          strict: z.boolean(),
          mappings: z.array(z.object({ groupName: z.string(), roleId: z.string() })),
        }))
        .handler(async ({ input }) => {
          return backendPut('/auth/group-mappings', input)
        }),
    },
    signInLog: os.handler(async () => {
      return backendGet<SignInLog>('/auth/sign-in-log')
    }),
    scim: {
      get: os.handler(async () => {
        return backendGet<ScimStatus>('/auth/scim')
      }),
      generate: os.handler(async () => {
        return backendPost<{ token: string; baseUrl: string }>('/auth/scim/token')
      }),
      revoke: os.handler(async () => {
        return backendDelete('/auth/scim/token')
      }),
    },
  },
}
