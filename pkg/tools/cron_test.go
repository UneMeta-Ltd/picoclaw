package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	picocron "github.com/sipeed/picoclaw/pkg/cron"
)

type mockCronExecutor struct{}

func (m *mockCronExecutor) ProcessDirectWithChannel(
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ string,
) (string, error) {
	return "ok", nil
}

func newCronToolForTest(t *testing.T) *CronTool {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "cron", "jobs.json")
	cs := picocron.NewCronService(storePath, nil)
	mb := bus.NewMessageBus()
	t.Cleanup(mb.Close)

	tool, err := NewCronTool(cs, &mockCronExecutor{}, mb, t.TempDir(), false, 5*time.Second, nil)
	if err != nil {
		t.Fatalf("failed to create cron tool: %v", err)
	}
	return tool
}

func TestCronToolAddJobUsesCurrentContext(t *testing.T) {
	tool := newCronToolForTest(t)
	tool.SetContext("web", "chat-1")

	result := tool.Execute(context.Background(), map[string]any{
		"action":     "add",
		"message":    "test reminder",
		"at_seconds": float64(60),
	})
	if result.IsError {
		t.Fatalf("expected add success, got error: %s", result.ForLLM)
	}

	jobs := tool.cronService.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Payload.Channel != "web" || jobs[0].Payload.To != "chat-1" {
		t.Fatalf("unexpected payload route: channel=%s to=%s", jobs[0].Payload.Channel, jobs[0].Payload.To)
	}
}

func TestCronToolAddJobWithTargetChannelRequiresChatID(t *testing.T) {
	tool := newCronToolForTest(t)
	tool.SetContext("web", "chat-1")

	result := tool.Execute(context.Background(), map[string]any{
		"action":         "add",
		"message":        "notify telegram",
		"at_seconds":     float64(60),
		"target_channel": "telegram",
	})
	if !result.IsError {
		t.Fatalf("expected error when target_chat_id missing")
	}
	if !strings.Contains(strings.ToLower(result.ForLLM), "target_chat_id") {
		t.Fatalf("expected target_chat_id hint, got: %s", result.ForLLM)
	}
}

func TestCronToolAddJobWithTargetChannelAlias(t *testing.T) {
	tool := newCronToolForTest(t)
	tool.SetContext("web", "chat-1")

	result := tool.Execute(context.Background(), map[string]any{
		"action":         "add",
		"message":        "notify lark",
		"at_seconds":     float64(60),
		"target_channel": "lark",
		"target_chat_id": "ou_xxx",
	})
	if result.IsError {
		t.Fatalf("expected add success for alias channel, got error: %s", result.ForLLM)
	}

	jobs := tool.cronService.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].Payload.Channel != "feishu" {
		t.Fatalf("expected alias lark -> feishu, got %s", jobs[0].Payload.Channel)
	}
	if jobs[0].Payload.To != "ou_xxx" {
		t.Fatalf("expected target chat id ou_xxx, got %s", jobs[0].Payload.To)
	}
}
