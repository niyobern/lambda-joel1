package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/berniyo/paypack-lambda/internal/paypack"
)

type fakeClient struct {
	cashInFn          func(ctx context.Context, number string, amount float64) (*paypack.Transaction, error)
	findTransactionFn func(ctx context.Context, ref string) (*paypack.Transaction, error)
}

func (f *fakeClient) CashIn(ctx context.Context, number string, amount float64) (*paypack.Transaction, error) {
	return f.cashInFn(ctx, number, amount)
}

func (f *fakeClient) FindTransaction(ctx context.Context, ref string) (*paypack.Transaction, error) {
	return f.findTransactionFn(ctx, ref)
}

type fakeCallback struct {
	calls []SubscriptionResponse
	err   error
}

func (f *fakeCallback) Send(ctx context.Context, payload SubscriptionResponse) error {
	f.calls = append(f.calls, payload)
	return f.err
}

func TestProcessorHandleSuccess(t *testing.T) {
	client := &fakeClient{
		cashInFn: func(ctx context.Context, number string, amount float64) (*paypack.Transaction, error) {
			return &paypack.Transaction{Ref: "abc", Status: "pending"}, nil
		},
		findTransactionFn: func(ctx context.Context, ref string) (*paypack.Transaction, error) {
			return &paypack.Transaction{Ref: ref, Status: "success"}, nil
		},
	}

	cb := &fakeCallback{}
	processor := NewProcessor(
		client,
		WithPollInterval(5*time.Millisecond),
		WithTimeout(200*time.Millisecond),
		WithCallbackSender(cb),
	)

	event := SubscriptionEvent{Number: "2507", Amount: 1000}
	resp, err := processor.Handle(context.Background(), event)
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Equal(t, "success", resp.Status)
	require.Equal(t, "abc", resp.Reference)
	require.Equal(t, event.Number, resp.Request.Number)
	require.Len(t, cb.calls, 1)
	require.Equal(t, resp, cb.calls[0])
}

func TestProcessorHandlePollsUntilFound(t *testing.T) {
	calls := 0
	client := &fakeClient{
		cashInFn: func(ctx context.Context, number string, amount float64) (*paypack.Transaction, error) {
			return &paypack.Transaction{Ref: "abc"}, nil
		},
		findTransactionFn: func(ctx context.Context, ref string) (*paypack.Transaction, error) {
			calls++
			if calls < 3 {
				return nil, paypack.ErrTransactionNotFound
			}
			return &paypack.Transaction{Ref: ref, Status: "success"}, nil
		},
	}

	cb := &fakeCallback{}
	processor := NewProcessor(
		client,
		WithPollInterval(5*time.Millisecond),
		WithTimeout(200*time.Millisecond),
		WithCallbackSender(cb),
	)

	resp, err := processor.Handle(context.Background(), SubscriptionEvent{Number: "2507", Amount: 1000})
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Equal(t, 3, calls)
	require.Len(t, cb.calls, 1)
}

func TestProcessorHandleTimeout(t *testing.T) {
	client := &fakeClient{
		cashInFn: func(ctx context.Context, number string, amount float64) (*paypack.Transaction, error) {
			return &paypack.Transaction{Ref: "abc"}, nil
		},
		findTransactionFn: func(ctx context.Context, ref string) (*paypack.Transaction, error) {
			return nil, paypack.ErrTransactionNotFound
		},
	}

	cb := &fakeCallback{}
	processor := NewProcessor(
		client,
		WithPollInterval(5*time.Millisecond),
		WithTimeout(20*time.Millisecond),
		WithCallbackSender(cb),
	)

	resp, err := processor.Handle(context.Background(), SubscriptionEvent{Number: "2507", Amount: 1000})
	require.NoError(t, err)
	require.False(t, resp.Found)
	require.Equal(t, "failed", resp.Status)
	require.Equal(t, "transaction not confirmed within 5 minutes", resp.Message)
	require.Len(t, cb.calls, 1)
}

func TestProcessorHandleValidatesInput(t *testing.T) {
	client := &fakeClient{}
	processor := NewProcessor(client)

	_, err := processor.Handle(context.Background(), SubscriptionEvent{})
	require.EqualError(t, err, "number is required")
}
