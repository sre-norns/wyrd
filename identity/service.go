package identity

import (
	"context"
	"sync"

	e "github.com/sre-norns/wyrd/identity/model"
	"gorm.io/gorm"
)

// Service owns identity and tenancy in one product database.
type Service struct {
	db             *gorm.DB
	config         *Config
	providers      map[string]upstreamProvider
	providerStatus sync.Map
}

func NewService(db *gorm.DB) *Service                      { c := DefaultConfig(); return NewServiceWithConfig(db, &c) }
func NewServiceWithConfig(db *gorm.DB, c *Config) *Service { return &Service{db: db, config: c} }
func (s *Service) Sessions() e.SessionsService             { return &sessionsService{db: s.db} }
func (s *Service) Principal() e.PrincipalService           { return &principalService{db: s.db} }
func (s *Service) SignInMethods() e.SignInMethodService    { return &signInMethodService{db: s.db} }
func (s *Service) PersonalProfile() e.PersonalProfileService {
	return &personalProfileService{db: s.db}
}
func (s *Service) Accounts() e.AccountsService { return &accountsService{db: s.db, config: s.config} }
func (s *Service) AccountMemberships() e.AccountMembershipsService {
	return &accountMembershipsService{db: s.db}
}
func (s *Service) AccountInvitations() e.AccountInvitationsService {
	return &accountInvitationsService{db: s.db, config: s.config}
}
func (s *Service) AgentIdentities() e.AgentIdentitiesService {
	return &agentIdentitiesService{db: s.db}
}
func (s *Service) AgentIdentityTokens() e.AgentIdentityTokensService {
	return &agentIdentityTokensService{db: s.db}
}
func (s *Service) AgentAuthorizations() e.AgentAuthorizationsService {
	return &agentAuthorizationsService{db: s.db}
}
func (s *Service) ProjectMemberships() e.ProjectMembershipsService {
	return &projectMembershipsService{db: s.db, config: s.config}
}
func (s *Service) Projects() e.ProjectsService { return &projectsService{db: s.db} }

// Directory is the read models behind project access management.
func (s *Service) Directory() e.DirectoryService { return &directoryService{db: s.db} }
func (s *Service) ServiceConfig() e.ServiceConfigService {
	return &serviceConfigService{db: s.db, config: s.config}
}
func (s *Service) OAuth() e.OAuthService                       { return &oauthService{db: s.db, config: s.config} }
func (s *Service) MachineIdentities() e.AgentIdentitiesService { return s.AgentIdentities() }
func (s *Service) MachineTokens() e.AgentIdentityTokensService { return s.AgentIdentityTokens() }
func (s *Service) MachineGrants() e.AgentAuthorizationsService { return s.AgentAuthorizations() }
func (s *Service) RequestContext(ctx context.Context, r Request) context.Context {
	return WithRequest(ctx, r)
}

func (s *Service) Providers() map[string]StorageUpstreamProvider { return s.providers }

func (s *Service) WebClientID() string { return s.config.WebClientID }
