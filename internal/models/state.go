// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package models

import (
	"fmt"
	"time"
)

type TaskState struct {
	// Upstream is set for tasks from polled engines (Strata/Maya);
	// empty for llama.cpp slot tasks.
	Upstream         string    `json:"upstream,omitempty"`
	Slot             int       `json:"slot"`
	Task             int       `json:"task"`
	Phase            string    `json:"phase"`
	PromptTokens     int       `json:"prompt_tokens"`
	GeneratedTokens  int       `json:"generated_tokens"`
	PromptTPS        float64   `json:"prompt_tps,omitempty"`
	GenerationTPS    float64   `json:"generation_tps,omitempty"`
	GenerationTPS3s  float64   `json:"generation_tps_3s,omitempty"`
	PromptEvalMs     float64   `json:"prompt_eval_ms,omitempty"`
	TotalMs          float64   `json:"total_ms,omitempty"`
	TotalTokens      int       `json:"total_tokens,omitempty"`
	Keep             float64   `json:"keep,omitempty"`
	Progress         float64   `json:"progress,omitempty"`
	GraphsReused     int       `json:"graphs_reused,omitempty"`
	Truncated        int       `json:"truncated,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
	Finished         bool      `json:"finished"`
	Cancelled        bool      `json:"cancelled"`
}

func TaskKey(slot, task int) string {
	return fmt.Sprintf("%d:%d", slot, task)
}

// StoreKey builds the task map key. Tasks from polled engines (Strata/Maya)
// use synthetic slot ids, so the upstream name is prefixed to keep keys unique.
func StoreKey(upstream string, slot, task int) string {
	if upstream != "" {
		return upstream + "|" + TaskKey(slot, task)
	}
	return TaskKey(slot, task)
}

func (ts *TaskState) Key() string {
	return StoreKey(ts.Upstream, ts.Slot, ts.Task)
}
