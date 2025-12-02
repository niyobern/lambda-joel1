package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/berniyo/paypack-lambda/internal/paypack"
)

// PaymentClient defines the subset of the Paypack client used by the processor.
type PaymentClient interface {
	CashIn(ctx context.Context, number string, amount float64) (*paypack.Transaction, error)
	FindTransaction(ctx context.Context, ref string) (*paypack.Transaction, error)
}

// SubscriptionEvent represents the payload sent to the Lambda function.
type SubscriptionEvent struct {
	Number   string         `json:"number"`
	Amount   float64        `json:"amount"`
	Client   string         `json:"client,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SubscriptionResponse is emitted after processing completes.
type SubscriptionResponse struct {
	Reference   string               `json:"ref"`
	Status      string               `json:"status"`
	Found       bool                 `json:"found"`
	Transaction *paypack.Transaction `json:"transaction,omitempty"`
	Message     string               `json:"message,omitempty"`
	Request     SubscriptionEvent    `json:"request"`
}

// CallbackSender delivers subscription outcomes to downstream systems.
type CallbackSender interface {
	Send(ctx context.Context, payload SubscriptionResponse) error
}

// Processor coordinates cash-in and transaction polling.
type Processor struct {
	client       PaymentClient
	pollInterval time.Duration
	timeout      time.Duration
	logger       *log.Logger
	callback     CallbackSender
}

// Option customizes the processor.
type Option func(*Processor)

// WithPollInterval adjusts the delay between find calls.
func WithPollInterval(d time.Duration) Option {
	return func(p *Processor) {
		if d > 0 {
			p.pollInterval = d
		}
	}
}

// WithTimeout overrides the total polling timeout.
func WithTimeout(d time.Duration) Option {
	return func(p *Processor) {
		if d > 0 {
			p.timeout = d
		}
	}
}

// WithLogger lets callers supply a custom logger.
func WithLogger(l *log.Logger) Option {
	return func(p *Processor) {
		if l != nil {
			p.logger = l
		}
	}
}

// WithCallbackSender wires a callback destination invoked after processing concludes.
func WithCallbackSender(sender CallbackSender) Option {
	return func(p *Processor) {
		p.callback = sender
	}
}

// NewProcessor builds a Processor with sane defaults.
func NewProcessor(client PaymentClient, opts ...Option) *Processor {
	p := &Processor{
		client:       client,
		pollInterval: 5 * time.Second,
		timeout:      5 * time.Minute,
		logger:       log.New(os.Stdout, "paypack-lambda ", log.LstdFlags),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Handle implements the AWS Lambda handler entry point.
func (p *Processor) Handle(ctx context.Context, event SubscriptionEvent) (SubscriptionResponse, error) {
	if err := validateEvent(event); err != nil {
		return SubscriptionResponse{}, err
	}

	p.logger.Printf("initiating cashin for number=%s amount=%.2f", event.Number, event.Amount)
	cashTxn, err := p.client.CashIn(ctx, event.Number, event.Amount)
	if err != nil {
		return SubscriptionResponse{}, fmt.Errorf("cashin failed: %w", err)
	}

	ref := cashTxn.Ref
	p.logger.Printf("cashin accepted ref=%s; starting polling", ref)

	polledTxn, err := p.pollTransaction(ctx, ref)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			resp := SubscriptionResponse{
				Reference: ref,
				Status:    "failed",
				Found:     false,
				Message:   "transaction not confirmed within 5 minutes",
				Request:   event,
			}
			p.emitCallback(ctx, resp)
			return resp, nil
		}
		return SubscriptionResponse{}, err
	}

	resp := SubscriptionResponse{
		Reference:   ref,
		Status:      polledTxn.Status,
		Found:       true,
		Transaction: polledTxn,
		Request:     event,
	}
	p.emitCallback(ctx, resp)
	return resp, nil
}

func (p *Processor) pollTransaction(ctx context.Context, ref string) (*paypack.Transaction, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		transaction, err := p.client.FindTransaction(ctx, ref)
		if err == nil {
			p.logger.Printf("transaction %s confirmed", ref)
			return transaction, nil
		}

		if !errors.Is(err, paypack.ErrTransactionNotFound) {
			return nil, err
		}

		p.logger.Printf("transaction %s not ready; waiting %s", ref, p.pollInterval)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func validateEvent(event SubscriptionEvent) error {
	if strings.TrimSpace(event.Number) == "" {
		return errors.New("number is required")
	}
	if event.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}

func (p *Processor) emitCallback(ctx context.Context, resp SubscriptionResponse) {
	if p.callback == nil {
		return
	}
	if err := p.callback.Send(ctx, resp); err != nil {
		p.logger.Printf("callback delivery failed: %v", err)
	}
}
