// SSO provider presets. These are PURE DATA: each preset pre-fills the generic
// OIDC/SAML engine with a provider's conventional scopes, claim/attribute names,
// issuer-URL hints, and setup steps. There is deliberately no per-provider code
// path — selecting a preset just seeds the same config the generic forms edit.
//
// The values and caveats below come from each IdP's own documentation (claim
// names, group-claim quirks, issuer formats). When a provider isn't listed, the
// "Generic" presets are the escape hatch.

import type { ProviderConfig } from '#/lib/api/types'

export type SSOProtocol = 'oidc' | 'saml'

export interface ProtocolPreset {
  /** Partial config seeded into the form when this preset+protocol is chosen. */
  defaults: Partial<ProviderConfig>
  /** Placeholder shown in the issuer-URL / metadata-URL field. */
  hint?: string
  /** Numbered setup instructions shown inline during the Configure step. */
  setupSteps: string[]
  /** A caveat worth surfacing prominently (e.g. Azure group GUIDs). */
  note?: string
}

export interface SSOPreset {
  id: string
  name: string
  blurb: string
  docUrl?: string
  oidc?: ProtocolPreset
  saml?: ProtocolPreset
}

const STD_OIDC_SCOPES = ['openid', 'profile', 'email', 'groups']

export const SSO_PRESETS: SSOPreset[] = [
  {
    id: 'okta',
    name: 'Okta',
    blurb: 'Okta Workforce Identity',
    docUrl: 'https://developer.okta.com/docs/guides/customize-tokens-groups-claim/',
    oidc: {
      hint: 'https://your-org.okta.com/oauth2/default',
      defaults: { scopes: STD_OIDC_SCOPES, groupsClaim: 'groups', emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'In Okta, create an app integration → OIDC → Web Application.',
        'Set the Sign-in redirect URI to the Redirect URI shown below.',
        'On your authorization server, add a "groups" claim (filter Matches regex .*) and include the groups scope.',
        'Assign the app to the users/groups who should have access, then paste the Client ID and secret here.',
      ],
      note: 'Okta only emits groups when you add a groups claim AND request the groups scope.',
    },
    saml: {
      hint: 'https://your-org.okta.com/app/.../sso/saml/metadata',
      defaults: { emailAttributes: ['email'], nameAttributes: ['displayName'], groupsAttributes: ['groups'], nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress' },
      setupSteps: [
        'In Okta, create a SAML 2.0 app integration.',
        'Set the Single sign-on URL to the ACS URL and the Audience URI to the SP Entity ID shown below.',
        'Add attribute statements: email, displayName; and a group attribute statement named "groups".',
        'Paste the Okta metadata URL here.',
      ],
    },
  },
  {
    id: 'entra',
    name: 'Microsoft Entra ID',
    blurb: 'Azure AD / Entra ID',
    docUrl: 'https://learn.microsoft.com/en-us/entra/identity-platform/optional-claims',
    oidc: {
      hint: 'https://login.microsoftonline.com/<tenant-id>/v2.0',
      defaults: { scopes: ['openid', 'profile', 'email'], groupsClaim: 'groups', emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'In Entra, register an application and add the Redirect URI shown below.',
        'Create a client secret and note the Application (client) ID.',
        'Under Token configuration, add the groups claim if you need group sync.',
        'Use the tenant-specific v2.0 issuer above (pin your tenant GUID).',
      ],
      note: 'Entra emits group object GUIDs (not names) and omits groups above ~200 members (overage). Map roles by GUID, or use SCIM for authoritative names.',
    },
    saml: {
      hint: 'https://login.microsoftonline.com/<tenant-id>/federationmetadata/.../federationmetadata.xml',
      defaults: {
        emailAttributes: ['http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress'],
        nameAttributes: ['http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name'],
        groupsAttributes: ['http://schemas.microsoft.com/ws/2008/06/identity/claims/groups'],
        nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress',
      },
      setupSteps: [
        'In Entra, create an Enterprise Application → set up single sign-on → SAML.',
        'Set the Reply URL to the ACS URL and the Identifier to the SP Entity ID shown below.',
        'Under Attributes & Claims, add a groups claim if you need group sync.',
        'Paste the App Federation Metadata URL here.',
      ],
      note: 'Entra SAML groups arrive as object GUIDs under the Microsoft groups attribute (pre-filled below).',
    },
  },
  {
    id: 'google',
    name: 'Google Workspace',
    blurb: 'Google Workspace / Cloud Identity',
    docUrl: 'https://developers.google.com/identity/openid-connect/openid-connect',
    oidc: {
      hint: 'https://accounts.google.com',
      defaults: { scopes: ['openid', 'profile', 'email'], emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'In Google Cloud, create an OAuth 2.0 Client ID (Web application).',
        'Add the Redirect URI shown below to the authorized redirect URIs.',
        'Paste the Client ID and secret here; the issuer is accounts.google.com.',
      ],
      note: 'Google Workspace does not send group claims over OIDC. Use SAML group mapping or SCIM for group sync.',
    },
    saml: {
      hint: 'https://accounts.google.com/o/saml2/idp?idpid=...',
      defaults: { emailAttributes: ['email'], nameAttributes: ['name'], groupsAttributes: ['groups'], nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress' },
      setupSteps: [
        'In the Google Admin console, add a custom SAML app.',
        'Set the ACS URL and Entity ID to the values shown below.',
        'Map Directory attributes (email, name) and add a Group membership attribute named "groups" (max 75 groups).',
        'Paste the IdP metadata URL here.',
      ],
    },
  },
  {
    id: 'auth0',
    name: 'Auth0',
    blurb: 'Auth0 by Okta',
    docUrl: 'https://auth0.com/docs/secure/tokens/json-web-tokens/create-custom-claims',
    oidc: {
      hint: 'https://your-tenant.us.auth0.com/',
      defaults: { scopes: STD_OIDC_SCOPES, groupsClaim: 'https://your-namespace/groups', emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'In Auth0, create a Regular Web Application.',
        'Add the Redirect URI shown below to the Allowed Callback URLs.',
        'Add an Action that sets a NAMESPACED groups claim, then put that exact claim name in the Groups claim field.',
        'The issuer must include its trailing slash.',
      ],
      note: 'Auth0 silently drops non-namespaced custom claims — groups MUST be delivered under a namespaced claim (e.g. https://your-app/groups).',
    },
    saml: {
      hint: 'https://your-tenant.us.auth0.com/samlp/metadata/YOUR_CLIENT_ID',
      defaults: {
        emailAttributes: ['http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress'],
        nameAttributes: ['http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name'],
        groupsAttributes: ['http://schemas.xmlsoap.org/claims/Group'],
        nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress',
      },
      setupSteps: [
        'In Auth0, open your application → Addons → enable the SAML2 Web App addon.',
        'Set the Application Callback URL to the ACS URL shown below.',
        'In the addon Settings, map email/name (and a groups claim if needed) and set the NameID to the email.',
        'Paste the SAML metadata URL above (samlp/metadata/YOUR_CLIENT_ID).',
      ],
      note: 'Auth0 SAML defaults to a user_id NameID — map it to email so Flint can key users correctly.',
    },
  },
  {
    id: 'onelogin',
    name: 'OneLogin',
    blurb: 'OneLogin by One Identity',
    docUrl: 'https://developers.onelogin.com/openid-connect',
    oidc: {
      hint: 'https://your-subdomain.onelogin.com/oidc/2',
      defaults: { scopes: STD_OIDC_SCOPES, groupsClaim: 'groups', emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'In OneLogin, add an OpenID Connect (OIDC) app.',
        'Set the Redirect URI to the value shown below.',
        'On the Parameters tab, add a Groups parameter (e.g. mapped to Roles) and request the groups scope.',
        'Paste the Client ID and secret here.',
      ],
      note: 'OneLogin returns groups only when BOTH the groups scope is requested and a Groups parameter is configured.',
    },
    saml: {
      hint: 'https://your-subdomain.onelogin.com/saml/metadata/...',
      defaults: { emailAttributes: ['email'], nameAttributes: ['name'], groupsAttributes: ['memberOf'], nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress' },
      setupSteps: [
        'In OneLogin, add a SAML Custom Connector (Advanced).',
        'Set the ACS URL and Audience to the values shown below.',
        'On the Parameters tab map email, name and a memberOf (roles) attribute.',
        'Paste the issuer/metadata URL here.',
      ],
    },
  },
  {
    id: 'keycloak',
    name: 'Keycloak',
    blurb: 'Keycloak / Red Hat SSO',
    docUrl: 'https://www.keycloak.org/docs/latest/server_admin/',
    oidc: {
      hint: 'https://your-host/realms/your-realm',
      defaults: { scopes: STD_OIDC_SCOPES, groupsClaim: 'groups', emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'In Keycloak, create a confidential OpenID Connect client.',
        'Add the Redirect URI shown below to the Valid redirect URIs.',
        'Add a Group Membership mapper with token claim name "groups".',
        'Use the realm issuer above (older Keycloak versions include /auth before /realms).',
      ],
      note: 'Keycloak emits no group claim until you add a Group Membership mapper.',
    },
    saml: {
      hint: 'https://your-host/realms/your-realm/protocol/saml/descriptor',
      defaults: {
        emailAttributes: ['email'],
        nameAttributes: ['displayName'],
        groupsAttributes: ['groups', 'member'],
        nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress',
      },
      setupSteps: [
        'In Keycloak, create a SAML client.',
        'Set Valid redirect URIs and the Assertion Consumer Service POST Binding URL to the ACS URL shown below.',
        'Add user-property mappers for email/name and a Group list mapper for groups.',
        'Paste the realm SAML descriptor URL above (older versions include /auth before /realms).',
      ],
    },
  },
  {
    id: 'jumpcloud',
    name: 'JumpCloud',
    blurb: 'JumpCloud Directory',
    docUrl: 'https://jumpcloud.com/support/sso-with-oidc',
    oidc: {
      hint: 'https://oauth.id.jumpcloud.com/',
      defaults: { scopes: ['openid', 'profile', 'email'], groupsClaim: 'groups', emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'In JumpCloud, create an SSO application → OIDC.',
        'Set the Redirect URI to the value shown below.',
        'Enable "Include group attribute" and set the attribute name to "groups" for group sync.',
        'Use the regional issuer above (EU: oauth.id.eu.jumpcloud.com, IN: oauth.id.in.jumpcloud.com).',
      ],
      note: 'JumpCloud issuer is region-specific, and groups require the "Include group attribute" toggle.',
    },
    saml: {
      hint: 'https://sso.jumpcloud.com/saml2/your-app',
      defaults: {
        emailAttributes: ['email'],
        nameAttributes: ['fullname', 'displayName'],
        groupsAttributes: ['memberOf', 'groups'],
        nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress',
      },
      setupSteps: [
        'In JumpCloud, create an SSO application → SAML (Custom or the Flint connector).',
        'Set the ACS URL and SP Entity ID to the values shown below.',
        'Map email, firstname/lastname/fullname; enable "Include group attribute" (default name memberOf) for groups.',
        'Paste the JumpCloud IdP metadata URL here.',
      ],
    },
  },
  {
    id: 'generic-oidc',
    name: 'Generic OIDC',
    blurb: 'Any OpenID Connect provider',
    oidc: {
      hint: 'https://idp.example.com',
      defaults: { scopes: STD_OIDC_SCOPES, groupsClaim: 'groups', emailClaim: 'email', nameClaim: 'name' },
      setupSteps: [
        'Register an OIDC client with your provider.',
        'Set the redirect URI to the value shown below.',
        'Ensure the issuer supports discovery (.well-known/openid-configuration).',
        'Adjust the scopes and claim names below to match what your IdP emits.',
      ],
    },
  },
  {
    id: 'generic-saml',
    name: 'Generic SAML',
    blurb: 'Any SAML 2.0 provider',
    saml: {
      hint: 'https://idp.example.com/metadata',
      defaults: { nameIdFormat: 'urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress' },
      setupSteps: [
        'In your IdP, create a SAML 2.0 application.',
        'Set the ACS URL and SP Entity ID to the values shown below.',
        'Configure attribute statements for email, name and groups.',
        'Provide the IdP metadata as a URL or paste the XML.',
      ],
    },
  },
]

export function presetById(id: string): SSOPreset | undefined {
  return SSO_PRESETS.find((p) => p.id === id)
}

export function presetProtocols(p: SSOPreset): SSOProtocol[] {
  const out: SSOProtocol[] = []
  if (p.oidc) out.push('oidc')
  if (p.saml) out.push('saml')
  return out
}
