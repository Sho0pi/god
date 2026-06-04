package agent

import (
	"context"
	"fmt"

	"github.com/sho0pi/god/internal/command"
	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector"
)

// cmdSession implements command.Runtime for a single command invocation,
// binding the agent and the request context that the handler operates on.
type cmdSession struct {
	a        *Agent
	ctx      context.Context
	msg      connector.Message
	userKey  string
	roleName string
	soulName string
	roleCfg  config.RoleConfig
}

var _ command.Runtime = (*cmdSession)(nil)

func (s *cmdSession) ClearHistory() error {
	s.a.clearUserHistory(s.userKey)
	return nil
}

func (s *cmdSession) IsAdmin() bool {
	if s.roleName == "admin" {
		return true
	}
	// Bootstrap admins (config.Admin) keep admin powers even if the store has
	// assigned them a lower role.
	for _, id := range s.a.liveConfig().Admin {
		if id == s.msg.UserID {
			return true
		}
	}
	return false
}

func (s *cmdSession) FactoryReset() error {
	s.a.clearUserHistory(s.userKey)
	if s.a.store == nil {
		return nil
	}
	if err := s.a.store.DeleteSoul(s.ctx, s.msg.Connector, s.msg.UserID); err != nil {
		return fmt.Errorf("delete soul: %w", err)
	}
	if err := s.a.store.DeleteRole(s.ctx, s.msg.Connector, s.msg.UserID); err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if err := s.a.store.DeleteMemories(s.ctx, s.msg.Connector, s.msg.UserID); err != nil {
		return fmt.Errorf("delete memories: %w", err)
	}
	return nil
}

func (s *cmdSession) Info() command.UserInfo {
	return command.UserInfo{
		Connector: s.msg.Connector,
		UserID:    s.msg.UserID,
		Soul:      s.soulName,
		Role:      s.roleName,
		LLMModel:  s.roleCfg.LLM.Model,
		Provider:  s.roleCfg.LLM.Provider,
	}
}

func (s *cmdSession) AllowAdd(number string) error {
	if s.a.store == nil {
		return command.ErrUnsupported
	}
	return s.a.store.AddAllow(s.ctx, s.msg.Connector, number)
}

func (s *cmdSession) AllowRemove(number string) error {
	if s.a.store == nil {
		return command.ErrUnsupported
	}
	return s.a.store.RemoveAllow(s.ctx, s.msg.Connector, number)
}

func (s *cmdSession) AllowList() ([]string, error) {
	if s.a.store == nil {
		return nil, command.ErrUnsupported
	}
	return s.a.store.ListAllow(s.ctx, s.msg.Connector)
}

func (s *cmdSession) ResolveApproval(approve bool, id string) {
	s.a.resumeApproval(s.ctx, s.userKey, s.msg.ChatID, approve, id)
}

func (s *cmdSession) GenerateLinkCode() (string, error) {
	if s.a.store == nil {
		return "", command.ErrUnsupported
	}
	return s.a.generateLinkCode(s.msg.Connector, s.msg.UserID), nil
}

func (s *cmdSession) RedeemLinkCode(code string) (string, error) {
	if s.a.store == nil {
		return "", command.ErrUnsupported
	}
	return s.a.redeemLinkCode(s.ctx, code, s.msg.Connector, s.msg.UserID)
}

func (s *cmdSession) Unlink() error {
	if s.a.store == nil {
		return command.ErrUnsupported
	}
	s.a.clearUserHistory(s.userKey)
	return s.a.store.Unlink(s.ctx, s.msg.Connector, s.msg.UserID)
}

func (s *cmdSession) LinkStatus() (bool, string) {
	if s.a.store == nil {
		return false, ""
	}
	cc, cu, err := s.a.store.ResolveIdentity(s.ctx, s.msg.Connector, s.msg.UserID)
	if err != nil || (cc == s.msg.Connector && cu == s.msg.UserID) {
		return false, ""
	}
	return true, cc + ":" + cu
}
