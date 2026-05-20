package protocolcodec

import "testing"

func TestTaskStatus_Valid(t *testing.T) {
	valid := []TaskStatus{TaskWorking, TaskInputRequired, TaskCompleted, TaskFailed, TaskCancelled}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []TaskStatus{"", "running", "done"} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestTaskStatus_IsTerminal(t *testing.T) {
	terminal := map[TaskStatus]bool{
		TaskWorking:       false,
		TaskInputRequired: false,
		TaskCompleted:     true,
		TaskFailed:        true,
		TaskCancelled:     true,
	}
	for s, want := range terminal {
		if s.IsTerminal() != want {
			t.Errorf("%q.IsTerminal()=%v want %v", s, s.IsTerminal(), want)
		}
	}
}

func TestTaskStatus_CanTransitionTo(t *testing.T) {
	// Legal: working -> {input_required, completed, failed, cancelled};
	// input_required -> {working, completed, failed, cancelled};
	// terminal -> nothing.
	cases := []struct {
		from, to TaskStatus
		want     bool
	}{
		{TaskWorking, TaskInputRequired, true},
		{TaskWorking, TaskCompleted, true},
		{TaskWorking, TaskFailed, true},
		{TaskWorking, TaskCancelled, true},
		{TaskWorking, TaskWorking, false},
		{TaskInputRequired, TaskWorking, true},
		{TaskInputRequired, TaskCompleted, true},
		{TaskInputRequired, TaskInputRequired, false},
		{TaskCompleted, TaskWorking, false},
		{TaskFailed, TaskCancelled, false},
		{TaskCancelled, TaskCompleted, false},
		{TaskWorking, "bogus", false},
		{"bogus", TaskWorking, false},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%q -> %q: got %v want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestToolTaskSupport_Valid(t *testing.T) {
	for _, s := range []ToolTaskSupport{TaskSupportForbidden, TaskSupportOptional, TaskSupportRequired} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []ToolTaskSupport{"", "maybe"} {
		if s.Valid() {
			t.Errorf("%q should be invalid", s)
		}
	}
}
