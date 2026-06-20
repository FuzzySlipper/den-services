package integration

import (
	"context"
	"errors"
	"sync"

	"den-services/shared/identity"
)

type MockRuntimeClient struct {
	mu        sync.Mutex
	aliveByID map[identity.AgentInstanceID]bool
	err       error
}

func NewMockRuntimeClient() *MockRuntimeClient {
	return &MockRuntimeClient{aliveByID: make(map[identity.AgentInstanceID]bool)}
}

func (c *MockRuntimeClient) SetAlive(instanceID identity.AgentInstanceID, alive bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.aliveByID[instanceID] = alive
}

func (c *MockRuntimeClient) SetError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.err = err
}

func (c *MockRuntimeClient) IsAlive(_ context.Context, instanceID identity.AgentInstanceID) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return false, c.err
	}
	return c.aliveByID[instanceID], nil
}

type MockDeliveryClient struct {
	mu             sync.Mutex
	createdIntents []DeliveryIntentRequest
	err            error
}

type DeliveryIntentRequest struct {
	TargetIdentity identity.AgentIdentity
	IdempotencyKey string
}

func NewMockDeliveryClient() *MockDeliveryClient {
	return &MockDeliveryClient{}
}

func (c *MockDeliveryClient) SetError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.err = err
}

func (c *MockDeliveryClient) CreateIntent(_ context.Context, request DeliveryIntentRequest) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return c.err
	}
	if !request.TargetIdentity.IsValid() {
		return ErrInvalidMockIntent
	}
	c.createdIntents = append(c.createdIntents, request)
	return nil
}

func (c *MockDeliveryClient) CreatedIntents() []DeliveryIntentRequest {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]DeliveryIntentRequest(nil), c.createdIntents...)
}

var ErrInvalidMockIntent = errors.New("invalid mock delivery intent") //nolint:gochecknoglobals
