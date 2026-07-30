// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package parser

import (
	"testing"
)

func TestTaskStart(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("1.11.544.915 I slot launch_slot_: id  0 | task 0 | processing task, is_child = 0")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.Type != "task_started" {
		t.Errorf("expected type task_started, got %s", ev.Type)
	}
	if ev.Slot != 0 {
		t.Errorf("expected slot=0, got %d", ev.Slot)
	}
	if ev.Task != 0 {
		t.Errorf("expected task=0, got %d", ev.Task)
	}
}

func TestPrefillProgress(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("1.21.151.553 I slot print_timing: id  0 | task 0 | prompt processing, n_tokens = 2048, progress = 0.03, t = 9.61 s / 213.19 tokens per second")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.Type != "prefill_progress" {
		t.Errorf("expected type prefill_progress, got %s", ev.Type)
	}
	if ev.Slot != 0 {
		t.Errorf("expected slot=0, got %d", ev.Slot)
	}
	if ev.Task != 0 {
		t.Errorf("expected task=0, got %d", ev.Task)
	}
	if ev.PromptTokens != 2048 {
		t.Errorf("expected prompt_tokens=2048, got %d", ev.PromptTokens)
	}
	if ev.Progress != 0.03 {
		t.Errorf("expected progress=0.03, got %f", ev.Progress)
	}
	if ev.PromptTPS != 213.19 {
		t.Errorf("expected prompt_tps=213.19, got %f", ev.PromptTPS)
	}
}

func TestGenerationProgress(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.04.003.651 I slot print_timing: id  0 | task 0 | n_decoded = 100, tg = 12.17 t/s, tg_3s = 12.16 t/s")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.Type != "generation_progress" {
		t.Errorf("expected type generation_progress, got %s", ev.Type)
	}
	if ev.Slot != 0 {
		t.Errorf("expected slot=0, got %d", ev.Slot)
	}
	if ev.Task != 0 {
		t.Errorf("expected task=0, got %d", ev.Task)
	}
	if ev.GeneratedTokens != 100 {
		t.Errorf("expected generated_tokens=100, got %d", ev.GeneratedTokens)
	}
	if ev.GenerationTPS != 12.17 {
		t.Errorf("expected generation_tps=12.17, got %f", ev.GenerationTPS)
	}
	if ev.GenerationTPS3s != 12.16 {
		t.Errorf("expected generation_tps_3s=12.16, got %f", ev.GenerationTPS3s)
	}
}

func TestGenerationProgressNoTps3s(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("0.38.537.798 I slot print_timing: id  0 | task 0 | n_decoded = 100, tg = 32.54 t/s")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.Type != "generation_progress" {
		t.Errorf("expected type generation_progress, got %s", ev.Type)
	}
	if ev.GeneratedTokens != 100 {
		t.Errorf("expected generated_tokens=100, got %d", ev.GeneratedTokens)
	}
	if ev.GenerationTPS != 32.54 {
		t.Errorf("expected generation_tps=32.54, got %f", ev.GenerationTPS)
	}
	if ev.GenerationTPS3s != 0 {
		t.Errorf("expected generation_tps_3s=0 when not present, got %f", ev.GenerationTPS3s)
	}
}

func TestPromptEvalTiming(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.40.082.210 I slot print_timing: id  0 | task 0 | prompt eval time = 464238.39 ms / 80651 tokens (5.76 ms per token, 173.73 tokens per second)")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.PromptEvalMs != 464238.39 {
		t.Errorf("expected prompt_eval_ms=464238.39, got %f", ev.PromptEvalMs)
	}
	if ev.PromptTokens != 80651 {
		t.Errorf("expected prompt_tokens=80651, got %d", ev.PromptTokens)
	}
	if ev.PromptTPS != 173.73 {
		t.Errorf("expected prompt_tps=173.73, got %f", ev.PromptTPS)
	}
}

func TestGenerationCompletion(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.40.082.219 I slot print_timing: id  0 | task 0 | eval time = 44298.85 ms / 530 tokens (83.58 ms per token, 11.96 tokens per second)")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.Type != "generation_complete" {
		t.Errorf("expected type generation_complete, got %s", ev.Type)
	}
	if ev.Slot != 0 {
		t.Errorf("expected slot=0, got %d", ev.Slot)
	}
	if ev.Task != 0 {
		t.Errorf("expected task=0, got %d", ev.Task)
	}
	if ev.GeneratedTokens != 530 {
		t.Errorf("expected generation_tokens=530, got %d", ev.GeneratedTokens)
	}
	if ev.GenerationTPS != 11.96 {
		t.Errorf("expected generation_tps=11.96, got %f", ev.GenerationTPS)
	}
}

func TestTaskRelease(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.40.092.986 I slot      release: id  0 | task 0 | stop processing: n_tokens = 81180, truncated = 0")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.Type != "task_finished" {
		t.Errorf("expected type task_finished, got %s", ev.Type)
	}
	if ev.Slot != 0 {
		t.Errorf("expected slot=0, got %d", ev.Slot)
	}
	if ev.Task != 0 {
		t.Errorf("expected task=0, got %d", ev.Task)
	}
	if ev.NTokens != 81180 {
		t.Errorf("expected n_tokens=81180, got %d", ev.NTokens)
	}
	if ev.Truncated != 0 {
		t.Errorf("expected truncated=0, got %d", ev.Truncated)
	}
}

func TestTaskCancellation(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("4.51.754.462 W srv          stop: cancel task, id_task = 24")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.Type != "task_cancelled" {
		t.Errorf("expected type task_cancelled, got %s", ev.Type)
	}
	if ev.Task != 24 {
		t.Errorf("expected task=24, got %d", ev.Task)
	}
}

func TestTotalTime(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.40.082.220 I slot print_timing: id  0 | task 0 | total time = 508537.23 ms / 81181 tokens")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.TotalMs != 508537.23 {
		t.Errorf("expected total_ms=508537.23, got %f", ev.TotalMs)
	}
	if ev.TotalTokens != 81181 {
		t.Errorf("expected total_tokens=81181, got %d", ev.TotalTokens)
	}
}

func TestUnrelatedLogLine(t *testing.T) {
	p := New()
	_, ok := p.ParseLine("0.00.154.698 I cmn  common_param: common_params_print_info: verbosity = 3")
	if ok {
		t.Fatal("expected ok=false for unrelated line")
	}
}

func TestSlotSelectionLRU(t *testing.T) {
	p := New()
	_, ok := p.ParseLine("1.11.536.798 I slot get_availabl: id  0 | task -1 | selected slot by LRU, t_last = -1")
	if ok {
		t.Fatal("expected ok=false for slot selection with LRU")
	}
}

func TestSlotSelectionLCP(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.42.536.231 I slot get_availabl: id  0 | task -1 | selected slot by LCP similarity, sim_best = 1.000 (> 0.100 thold), f_keep = 1.000")
	if !ok {
		t.Fatal("expected ok=true for slot selection with LCP")
	}
	if ev.Type != "slot_selected" {
		t.Errorf("expected type slot_selected, got %s", ev.Type)
	}
	if ev.Slot != 0 {
		t.Errorf("expected slot=0, got %d", ev.Slot)
	}
	if ev.Keep != 1.000 {
		t.Errorf("expected keep=1.000, got %f", ev.Keep)
	}
}

func TestSlotSelectionLCPSimBest(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.42.536.231 I slot get_availabl: id  0 | task -1 | selected slot by LCP similarity, sim_best = 0.965 (> 0.100 thold), f_keep = 0.960")
	if !ok {
		t.Fatal("expected ok=true for slot selection with LCP")
	}
	if ev.Type != "slot_selected" {
		t.Errorf("expected type slot_selected, got %s", ev.Type)
	}
	if ev.Keep != 0.960 {
		t.Errorf("expected keep=0.960, got %f", ev.Keep)
	}
}

func TestSlotSelectionLCPNewFormat(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("249.05.151.259 I slot get_availabl: id  0 | task -1 | selected slot by LCP similarity, f_sim_best = 0.406 (> 0.100 thold), f_keep = 0.384")
	if !ok {
		t.Fatal("expected ok=true for slot selection with LCP (new format)")
	}
	if ev.Type != "slot_selected" {
		t.Errorf("expected type slot_selected, got %s", ev.Type)
	}
	if ev.Keep != 0.384 {
		t.Errorf("expected keep=0.384, got %f", ev.Keep)
	}
}

func TestGraphsReused(t *testing.T) {
	p := New()
	ev, ok := p.ParseLine("9.40.082.226 I slot print_timing: id  0 | task 0 | graphs reused = 527")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if ev.GraphsReused != 527 {
		t.Errorf("expected graphs_reused=527, got %d", ev.GraphsReused)
	}
}
