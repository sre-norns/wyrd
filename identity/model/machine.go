package model

// MachineIdentity is an account-owned identity. Legacy Agent names remain wire
// and table aliases until the coordinated envelope release.
type MachineIdentity = AgentIdentity
type MachineIdentityID = AgentIdentityID
type MachineToken = AgentIdentityToken
type MachineTokenID = AgentIdentityTokenID
type MachineGrant = AgentAuthorization
type MachineGrantID = AgentAuthorizationID
