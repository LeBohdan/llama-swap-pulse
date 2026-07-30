// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package api

// SSE event type constants used in the /api/v1/live stream.

const (
	EventSlotSelected        = "slot_selected"
	EventTaskStarted         = "task_started"
	EventPrefillProgress     = "prefill_progress"
	EventGenerationProgress  = "generation_progress"
	EventGenerationComplete  = "generation_complete"
	EventTaskFinished        = "task_finished"
	EventTaskCancelled       = "task_cancelled"
)

// Internal-only event types (not emitted over SSE).

const (
	InternalPromptEvalResult = "prompt_eval_result"
	InternalTotalResult      = "total_result"
	InternalGraphsReused     = "graphs_reused"
)
