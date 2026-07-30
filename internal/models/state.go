// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package models

import (
	"fmt"
	"time"
)

type TaskState struct {
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

func (ts *TaskState) Key() string {
	return TaskKey(ts.Slot, ts.Task)
}
