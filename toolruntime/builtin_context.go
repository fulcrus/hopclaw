package toolruntime

import (
	"context"

	"github.com/fulcrus/hopclaw/agent"
)

type builtinContextKey string

const (
	builtinRunContextKey     builtinContextKey = "toolruntime.run"
	builtinSessionContextKey builtinContextKey = "toolruntime.session"
)

func withBuiltinRunContext(ctx context.Context, run *agent.Run) context.Context {
	if run == nil {
		return ctx
	}
	return context.WithValue(ctx, builtinRunContextKey, run)
}

func withBuiltinSessionContext(ctx context.Context, session *agent.Session) context.Context {
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, builtinSessionContextKey, session)
}

func builtinRunFromContext(ctx context.Context) *agent.Run {
	if ctx == nil {
		return nil
	}
	run, _ := ctx.Value(builtinRunContextKey).(*agent.Run)
	return run
}

func builtinSessionFromContext(ctx context.Context) *agent.Session {
	if ctx == nil {
		return nil
	}
	session, _ := ctx.Value(builtinSessionContextKey).(*agent.Session)
	return session
}
