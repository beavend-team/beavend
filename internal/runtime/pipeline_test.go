package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

type dummyStage struct {
	name     string
	err      error
	executed bool
	mutateFn func(s *core.ReqState)
}

func (d *dummyStage) Handle(_ context.Context, state *core.ReqState) error {
	d.executed = true
	if d.mutateFn != nil {
		d.mutateFn(state)
	}
	return d.err
}

func TestPipeline_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("runs all stages successfully in order", func(t *testing.T) {
		s1 := &dummyStage{name: "s1", mutateFn: func(s *core.ReqState) { s.Metadata["s1"] = true }}
		s2 := &dummyStage{name: "s2", mutateFn: func(s *core.ReqState) { s.Metadata["s2"] = true }}

		pipeline := NewPipeline(s1, s2)
		state := &core.ReqState{
			RequestID: "req_pipe_1",
			Metadata:  make(map[string]any),
		}

		err := pipeline.Execute(ctx, state)
		require.NoError(t, err)
		assert.True(t, s1.executed)
		assert.True(t, s2.executed)
		assert.Equal(t, true, state.Metadata["s1"])
		assert.Equal(t, true, state.Metadata["s2"])
	})

	t.Run("halts pipeline on stage failure", func(t *testing.T) {
		stageErr := errors.New("something exploded")
		s1 := &dummyStage{name: "s1", err: stageErr}
		s2 := &dummyStage{name: "s2"}

		pipeline := NewPipeline(s1, s2)
		state := &core.ReqState{
			RequestID: "req_pipe_2",
		}

		err := pipeline.Execute(ctx, state)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrPipelineAborted)
		assert.True(t, s1.executed)
		assert.False(t, s2.executed)
		require.NotNil(t, state.Error)
		assert.Equal(t, "internal_error", state.Error.Code)
	})

	t.Run("halts on context cancellation", func(t *testing.T) {
		cancCtx, cancel := context.WithCancel(context.Background())
		cancel()

		s1 := &dummyStage{name: "s1"}
		pipeline := NewPipeline(s1)
		state := &core.ReqState{
			RequestID: "req_pipe_3",
		}

		err := pipeline.Execute(cancCtx, state)
		require.ErrorIs(t, err, context.Canceled)
		assert.False(t, s1.executed)
		require.NotNil(t, state.Error)
		assert.Equal(t, "cancelled", state.Error.Code)
	})
}
