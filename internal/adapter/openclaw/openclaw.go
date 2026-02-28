package openclaw

import (
	"context"
	"sync"
	"time"

	"github.com/crabwise-ai/crabwise/internal/audit"
	"github.com/crabwise-ai/crabwise/internal/openclawstate"
)

type Adapter struct {
	cfg   Config
	state *openclawstate.Store

	client *GatewayClient
	cache  *SessionCache

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewAdapter(cfg Config, state *openclawstate.Store) *Adapter {
	client := NewGatewayClient(cfg)
	return &Adapter{
		cfg:    cfg,
		state:  state,
		client: client,
		cache:  NewSessionCache(client, cfg.SessionRefreshInterval),
	}
}

func (a *Adapter) Start(ctx context.Context, events chan<- *audit.AuditEvent) error {
	ctx, a.cancel = context.WithCancel(ctx)

	a.client.OnEvent(func(evt *EventFrame) {
		a.handleEvent(evt, events)
	})

	if _, err := a.client.Connect(ctx); err != nil {
		return err
	}

	if err := a.refreshSessions(ctx); err != nil {
		return err
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.refreshLoop(ctx)
	}()

	return nil
}

func (a *Adapter) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.client.Close()
	a.wg.Wait()
	return nil
}

func (a *Adapter) CanEnforce() bool {
	return false
}

func (a *Adapter) SessionCacheSize() int {
	return len(a.cache.Snapshot())
}

func (a *Adapter) Connected() bool {
	return a.client.Connected()
}

func (a *Adapter) refreshLoop(ctx context.Context) {
	if a.cfg.SessionRefreshInterval <= 0 {
		return
	}

	ticker := time.NewTicker(a.cfg.SessionRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = a.refreshSessions(ctx)
		}
	}
}

func (a *Adapter) refreshSessions(ctx context.Context) error {
	if err := a.cache.Refresh(ctx); err != nil {
		return err
	}

	for _, session := range a.cache.Snapshot() {
		a.state.RecordSession(openclawstate.SessionMeta{
			SessionKey:    session.Key,
			AgentID:       session.AgentID,
			ParentSession: session.SpawnedBy,
			Model:         session.Model,
			ThinkingLevel: session.ThinkingLevel,
			LastActivity:  time.UnixMilli(session.LastActivityAt),
		})
	}

	return nil
}

func (a *Adapter) handleEvent(evt *EventFrame, events chan<- *audit.AuditEvent) {
	switch payload := evt.Payload.(type) {
	case *ChatEvent:
		a.handleChatEvent(payload, events)
	case *AgentEvent:
		a.handleAgentEvent(payload, events)
	}
}

func (a *Adapter) handleChatEvent(payload *ChatEvent, events chan<- *audit.AuditEvent) {
	session, _ := a.cache.Get(payload.SessionKey)
	now := time.Now()
	model := session.Model
	provider := inferProviderFromModel(model)

	a.state.RecordRun(payload.RunID, payload.SessionKey)
	a.state.RecordChat(payload.RunID, payload.SessionKey, provider, model, now)

	emitNonBlocking(events, mapChatEvent(now, payload, session, provider, model))
}

func (a *Adapter) handleAgentEvent(payload *AgentEvent, events chan<- *audit.AuditEvent) {
	if payload.SessionKey != "" {
		a.state.RecordRun(payload.RunID, payload.SessionKey)
	}

	emitNonBlocking(events, mapAgentEvent(payload))
}

func emitNonBlocking(events chan<- *audit.AuditEvent, evt *audit.AuditEvent) {
	if evt == nil {
		return
	}
	select {
	case events <- evt:
	default:
	}
}
