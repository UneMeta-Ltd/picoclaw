package tools

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
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
	return newCronToolForTestWithConfig(t, config.DefaultConfig())
}

func newCronToolForTestWithConfig(t *testing.T, cfg *config.Config) *CronTool {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "cron", "jobs.json")
	cs := picocron.NewCronService(storePath, nil)
	mb := bus.NewMessageBus()
	t.Cleanup(mb.Close)

	tool, err := NewCronTool(cs, &mockCronExecutor{}, mb, t.TempDir(), false, 5*time.Second, cfg)
	if err != nil {
		t.Fatalf("failed to create cron tool: %v", err)
	}
	return tool
}

func TestCronTool_CommandBlockedFromRemoteChannel(t *testing.T) {
	tool := newCronToolForTest(t)
	ctx := WithToolContext(context.Background(), "telegram", "chat-1")

	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"message":         "check disk",
		"command":         "df -h",
		"command_confirm": true,
		"at_seconds":      float64(60),
	})

	if !result.IsError {
		t.Fatal("expected command scheduling to be blocked from remote channel")
	}
	if !strings.Contains(result.ForLLM, "restricted to internal channels") {
		t.Fatalf("expected restricted-to-internal-channels error, got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandDoesNotRequireConfirmByDefault(t *testing.T) {
	tool := newCronToolForTest(t)
	ctx := WithToolContext(context.Background(), "cli", "direct")

	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "check disk",
		"command":    "df -h",
		"at_seconds": float64(60),
	})

	if result.IsError {
		t.Fatalf("expected command scheduling without confirm to succeed, got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandRequiresConfirmWhenAllowCommandDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Cron.AllowCommand = false

	tool := newCronToolForTestWithConfig(t, cfg)
	ctx := WithToolContext(context.Background(), "cli", "direct")

	result := tool.Execute(ctx, map[string]any{
		"action":     "add",
		"message":    "check disk",
		"command":    "df -h",
		"at_seconds": float64(60),
	})

	if !result.IsError {
		t.Fatal("expected confirmation requirement when allow_command is disabled")
	}
	if !strings.Contains(result.ForLLM, "command_confirm=true") {
		t.Fatalf("expected command_confirm hint, got: %s", result.ForLLM)
	}
}

func TestCronTool_CommandBlockedWhenExecDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Exec.Enabled = false

	tool := newCronToolForTestWithConfig(t, cfg)
	ctx := WithToolContext(context.Background(), "cli", "direct")

	result := tool.Execute(ctx, map[string]any{
		"action":          "add",
		"message":         "check disk",
		"command":         "df -h",
		"command_confirm": true,
		"at_seconds":      float64(60),
	})

	if !result.IsError {
		t.Fatal("expected command scheduling to be blocked when exec is disabled")
	}
	if !strings.Contains(result.ForLLM, "command execution is disabled") {
		t.Fatalf("expected exec-disabled error, got: %s", result.ForLLM)
	}
}

func TestCronTool_AddJobRequiresSessionContext(t *testing.T) {
	tool := newCronToolForTest(t)

	result := tool.Execute(context.Background(), map[string]any{
		"action":     "add",
		"message":    "reminder",
		"at_seconds": float64(60),
	})

	if !result.IsError {
		t.Fatal("expected missing session context error")
	}
	if !strings.Contains(result.ForLLM, "no session context") {
		t.Fatalf("expected no-session-context error, got: %s", result.ForLLM)
	}
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

func TestCronToolAddJobBroadcastsToConfiguredChannels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels.Web.Enabled = true
	cfg.Channels.Web.AllowFrom = config.FlexibleStringSlice{"web-session-1"}
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "tg-token"
	cfg.Channels.Telegram.AllowFrom = config.FlexibleStringSlice{"10001"}
	cfg.Channels.Feishu.Enabled = true
	cfg.Channels.Feishu.AllowFrom = config.FlexibleStringSlice{"ou_abc"}

	tool := newCronToolForTestWithConfig(t, cfg)
	tool.SetContext("telegram", "10001")

	result := tool.Execute(context.Background(), map[string]any{
		"action":     "add",
		"message":    "broadcast reminder",
		"at_seconds": float64(60),
	})
	if result.IsError {
		t.Fatalf("expected add success, got error: %s", result.ForLLM)
	}

	jobs := tool.cronService.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	var targets []string
	for _, target := range jobs[0].Payload.Targets {
		targets = append(targets, target.Channel+":"+target.To)
	}
	sort.Strings(targets)
	want := []string{
		"feishu:ou_abc",
		"telegram:10001",
		"web:web-session-1",
	}
	sort.Strings(want)
	if strings.Join(targets, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected targets: got=%v want=%v", targets, want)
	}
}

func TestCronToolAddJobBroadcastAddsWebWildcardWhenAllowFromEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels.Web.Enabled = true
	cfg.Channels.Web.AllowFrom = nil
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = "tg-token"
	cfg.Channels.Telegram.AllowFrom = config.FlexibleStringSlice{"10001"}

	tool := newCronToolForTestWithConfig(t, cfg)
	tool.SetContext("telegram", "10001")

	result := tool.Execute(context.Background(), map[string]any{
		"action":     "add",
		"message":    "broadcast reminder",
		"at_seconds": float64(60),
	})
	if result.IsError {
		t.Fatalf("expected add success, got error: %s", result.ForLLM)
	}

	jobs := tool.cronService.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	var targets []string
	for _, target := range jobs[0].Payload.Targets {
		targets = append(targets, target.Channel+":"+target.To)
	}
	sort.Strings(targets)
	want := []string{
		"telegram:10001",
		"web:*",
	}
	sort.Strings(want)
	if strings.Join(targets, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected targets: got=%v want=%v", targets, want)
	}
}

func TestCronToolExecuteJobPublishesErrorWhenExecDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Exec.Enabled = false

	tool := newCronToolForTestWithConfig(t, cfg)
	job := &picocron.CronJob{
		ID: "job-1",
		Payload: picocron.CronPayload{
			Channel: "cli",
			To:      "direct",
			Command: "df -h",
		},
	}

	if got := tool.ExecuteJob(context.Background(), job); got != "ok" {
		t.Fatalf("ExecuteJob() = %q, want ok", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	msg, ok := tool.msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound message")
	}
	if !strings.Contains(msg.Content, "command execution is disabled") {
		t.Fatalf("expected exec-disabled message, got: %s", msg.Content)
	}
}

func TestCronToolExecuteJobDeliversToAllTargets(t *testing.T) {
	tool := newCronToolForTest(t)

	job := &picocron.CronJob{
		ID: "job-1",
		Payload: picocron.CronPayload{
			Deliver: true,
			Message: "timer done",
			Targets: []picocron.CronTarget{
				{Channel: "telegram", To: "10001"},
				{Channel: "web", To: "session-1"},
			},
		},
	}

	if got := tool.ExecuteJob(context.Background(), job); got != "ok" {
		t.Fatalf("ExecuteJob() = %s, want ok", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got := make([]string, 0, 2)
	for len(got) < 2 {
		msg, ok := tool.msgBus.SubscribeOutbound(ctx)
		if !ok {
			break
		}
		got = append(got, msg.Channel+":"+msg.ChatID+":"+msg.Content)
	}
	sort.Strings(got)
	want := []string{
		"telegram:10001:timer done",
		"web:session-1:timer done",
	}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected outbound messages: got=%v want=%v", got, want)
	}
}
