package lifecycle

import (
	"context"
	"errors"
	"testing"
)

type mockComponent struct {
	startCalled bool
	stopCalled  bool
	startErr    error
	stopErr     error
}

func (m *mockComponent) Start(_ context.Context) error {
	m.startCalled = true
	return m.startErr
}

func (m *mockComponent) Stop(_ context.Context) error {
	m.stopCalled = true
	return m.stopErr
}

func TestManager_StartStop(t *testing.T) {
	mgr := NewManager()
	c1 := &mockComponent{}
	c2 := &mockComponent{}

	mgr.Register(c1)
	mgr.Register(c2)

	if err := mgr.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !c1.startCalled || !c2.startCalled {
		t.Error("Start not called on all components")
	}

	if err := mgr.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if !c1.stopCalled || !c2.stopCalled {
		t.Error("Stop not called on all components")
	}
}

func TestManager_StartFailure(t *testing.T) {
	mgr := NewManager()
	c1 := &mockComponent{}
	c2 := &mockComponent{startErr: errors.New("boom")}
	c3 := &mockComponent{}

	mgr.Register(c1)
	mgr.Register(c2)
	mgr.Register(c3)

	err := mgr.Start(t.Context())
	if err == nil {
		t.Fatal("expected error from Start")
	}

	if !c1.startCalled || !c2.startCalled {
		t.Error("Start not called on c1 or c2")
	}
	if c3.startCalled {
		t.Error("Start should not be called on c3 after c2 fails")
	}
	if !c1.stopCalled {
		t.Error("c1 should be stopped on rollback")
	}
}

func TestManager_StopCollectsErrors(t *testing.T) {
	mgr := NewManager()
	c1 := &mockComponent{stopErr: errors.New("err1")}
	c2 := &mockComponent{stopErr: errors.New("err2")}

	mgr.Register(c1)
	mgr.Register(c2)

	_ = mgr.Start(t.Context())
	err := mgr.Stop(t.Context())

	if err == nil {
		t.Fatal("expected error from Stop")
	}
	if !c1.stopCalled || !c2.stopCalled {
		t.Error("both components should be stopped even with errors")
	}
}
