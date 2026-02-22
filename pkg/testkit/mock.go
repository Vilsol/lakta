package testkit

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/Vilsol/lakta/pkg/lakta"
)

var (
	_ lakta.Module      = (*MockModule)(nil)
	_ lakta.SyncModule  = (*MockSyncModule)(nil)
	_ lakta.AsyncModule = (*MockAsyncModule)(nil)
	_ lakta.Provider    = (*MockProviderModule)(nil)
	_ lakta.Dependent   = (*MockProviderModule)(nil)
)

// MockModule implements lakta.Module and tracks call counts.
type MockModule struct {
	InitErr       error
	ShutdownErr   error
	InitCalls     atomic.Int32
	ShutdownCalls atomic.Int32
	OnInit        func(ctx context.Context) error
	OnShutdown    func(ctx context.Context) error
}

// NewMockModule creates a new MockModule.
func NewMockModule() *MockModule {
	return &MockModule{}
}

func (m *MockModule) Init(ctx context.Context) error {
	m.InitCalls.Add(1)
	if m.OnInit != nil {
		return m.OnInit(ctx)
	}
	return m.InitErr
}

func (m *MockModule) Shutdown(ctx context.Context) error {
	m.ShutdownCalls.Add(1)
	if m.OnShutdown != nil {
		return m.OnShutdown(ctx)
	}
	return m.ShutdownErr
}

// MockSyncModule implements lakta.SyncModule and tracks call counts.
type MockSyncModule struct {
	MockModule

	StartErr   error
	StartCalls atomic.Int32
	OnStart    func(ctx context.Context) error
	// BlockStart blocks Start until the channel is closed or ctx is cancelled.
	BlockStart chan struct{}
}

// NewMockSyncModule creates a new MockSyncModule.
func NewMockSyncModule() *MockSyncModule {
	return &MockSyncModule{}
}

func (m *MockSyncModule) Start(ctx context.Context) error {
	m.StartCalls.Add(1)
	if m.BlockStart != nil {
		select {
		case <-m.BlockStart:
		case <-ctx.Done():
			return fmt.Errorf("%w", ctx.Err())
		}
	}
	if m.OnStart != nil {
		return m.OnStart(ctx)
	}
	return m.StartErr
}

// MockAsyncModule implements lakta.AsyncModule and tracks call counts.
type MockAsyncModule struct {
	MockModule

	StartAsyncErr   error
	StartAsyncCalls atomic.Int32
	OnStartAsync    func(ctx context.Context) error
}

// NewMockAsyncModule creates a new MockAsyncModule.
func NewMockAsyncModule() *MockAsyncModule {
	return &MockAsyncModule{}
}

func (m *MockAsyncModule) StartAsync(ctx context.Context) error {
	m.StartAsyncCalls.Add(1)
	if m.OnStartAsync != nil {
		return m.OnStartAsync(ctx)
	}
	return m.StartAsyncErr
}

// MockProviderModule implements lakta.Module, lakta.Provider, and lakta.Dependent
// with configurable type declarations for use in dependency graph tests.
type MockProviderModule struct {
	MockModule

	ProvidesTypes []reflect.Type
	RequiredDeps  []reflect.Type
	OptionalDeps  []reflect.Type
}

// NewMockProviderModule creates a new MockProviderModule.
func NewMockProviderModule() *MockProviderModule {
	return &MockProviderModule{}
}

func (m *MockProviderModule) Provides() []reflect.Type {
	return m.ProvidesTypes
}

func (m *MockProviderModule) Dependencies() ([]reflect.Type, []reflect.Type) {
	return m.RequiredDeps, m.OptionalDeps
}
