package model

// ResourceActor is public attribution, never a credential or an authority grant.
type ResourceActor struct {
	Type    string          `json:"type" yaml:"type"`
	UserID  string          `json:"userId,omitempty" yaml:"userId,omitempty"`
	AgentID AgentIdentityID `json:"agentId,omitempty" yaml:"agentId,omitempty"`
}
