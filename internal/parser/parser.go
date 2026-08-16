// Copyright (c) 2026 Bohdan Futerko
// Website: https://www.bf.com.ua
// GitHub: https://github.com/LeBohdan

package parser

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"llama-swap-pulse/internal/models"
)

var (
	rePrefix  = regexp.MustCompile(`^[0-9.]+\s+[A-Z]\s+(slot|srv)\s+(.*)$`)
	reSlotTask = regexp.MustCompile(`id\s+(\d+)\s*\|\s*task\s+(-?\d+)`)

	reIdTask = regexp.MustCompile(`id_task\s*=\s*(\d+)`)

	rePromptTokens = regexp.MustCompile(`n_tokens\s*=\s*(\d+)`)
	reProgress     = regexp.MustCompile(`progress\s*=\s*([\d.]+)`)
	rePromptTPS    = regexp.MustCompile(`t\s*=\s*[\d.]+\s+s\s*/\s*([\d.]+)\s*tokens per second`)

	reNDecoded    = regexp.MustCompile(`n_decoded\s*=\s*(\d+)`)
	reNGen        = regexp.MustCompile(`n_gen\s*=\s*(\d+)`)
	reTps         = regexp.MustCompile(`tg\s*=\s*([\d.]+)\s*t/s`)
	reTps3s       = regexp.MustCompile(`tg_3s\s*=\s*([\d.]+)\s*t/s`)

	rePromptEvalMs  = regexp.MustCompile(`prompt eval time\s*=\s*([\d.]+)\s*ms`)
	rePromptEvalTok = regexp.MustCompile(`prompt eval time\s*=\s*[\d.]+\s*ms\s*/\s*(\d+)\s*tokens`)
	rePromptEvalTPS = regexp.MustCompile(`prompt eval time.*\(([\d.]+)\s*ms per token,\s*([\d.]+)\s*tokens per second\)`)

	reEvalTok = regexp.MustCompile(`eval time\s*=\s*[\d.]+\s*ms\s*/\s*(\d+)\s*tokens`)
	reEvalTPS = regexp.MustCompile(`eval time.*\(([\d.]+)\s*ms per token,\s*([\d.]+)\s*tokens per second\)`)

	reTotalMs    = regexp.MustCompile(`total time\s*=\s*([\d.]+)\s*ms`)
	reTotalTok   = regexp.MustCompile(`total time\s*=\s*[\d.]+\s*ms\s*/\s*(\d+)\s*tokens`)

	reGraphsReused = regexp.MustCompile(`graphs reused\s*=\s*(\d+)`)

	reKeep    = regexp.MustCompile(`(?:f_)?keep\s*=\s*([\d.]+)`)
	reSimBest = regexp.MustCompile(`(?:f_)?sim_best\s*=\s*([\d.]+)`)

	reStopTokens   = regexp.MustCompile(`stop processing.*n_tokens\s*=\s*(\d+)`)
	reTruncated    = regexp.MustCompile(`truncated\s*=\s*(\d+)`)

	reCancelTask = regexp.MustCompile(`stop: cancel task, id_task = (\d+)`)
)

type Parser struct{}

func New() *Parser {
	return &Parser{}
}

func (p *Parser) ParseLine(line string) (*models.MetricEvent, bool) {
	m := rePrefix.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}

	component := m[1]
	body := m[2]
	now := time.Now()

	if component == "srv" {
		return p.parseSrv(body, now)
	}

	if component == "slot" {
		return p.parseSlot(body, now)
	}

	return nil, false
}

func (p *Parser) parseSrv(body string, now time.Time) (*models.MetricEvent, bool) {
	m := reCancelTask.FindStringSubmatch(body)
	if m != nil {
		taskId, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, false
		}
		ev := &models.MetricEvent{
			Type:      "task_cancelled",
			Task:      taskId,
			Timestamp: now,
		}
		return ev, true
	}
	return nil, false
}

func (p *Parser) parseSlot(body string, now time.Time) (*models.MetricEvent, bool) {
	if strings.Contains(body, "get_availabl") {
		return p.parseSlotSelection(body, now)
	}

	slot, task, ok := extractSlotTask(body)
	if !ok || task < 0 {
		return nil, false
	}

	if strings.Contains(body, "launch_slot_") {
		return &models.MetricEvent{
			Type:      "task_started",
			Slot:      slot,
			Task:      task,
			Phase:     "prefill",
			Timestamp: now,
		}, true
	}

	if strings.Contains(body, "release") && strings.Contains(body, "stop processing") {
		nTokens := 0
		truncated := 0
		if m := reStopTokens.FindStringSubmatch(body); m != nil {
			nTokens, _ = strconv.Atoi(m[1])
		}
		if m := reTruncated.FindStringSubmatch(body); m != nil {
			truncated, _ = strconv.Atoi(m[1])
		}
		return &models.MetricEvent{
			Type:      "task_finished",
			Slot:      slot,
			Task:      task,
			NTokens:  nTokens,
			Truncated: truncated,
			Timestamp: now,
		}, true
	}

	if strings.Contains(body, "print_timing") {
		return p.parsePrintTiming(body, slot, task, now)
	}

	return nil, false
}

func (p *Parser) parsePrintTiming(body string, slot, task int, now time.Time) (*models.MetricEvent, bool) {
	if strings.Contains(body, "prompt processing") {
		promptTokens := 0
		progress := 0.0
		promptTPS := 0.0

		if m := rePromptTokens.FindStringSubmatch(body); m != nil {
			promptTokens, _ = strconv.Atoi(m[1])
		}
		if m := reProgress.FindStringSubmatch(body); m != nil {
			progress, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := rePromptTPS.FindStringSubmatch(body); m != nil {
			promptTPS, _ = strconv.ParseFloat(m[1], 64)
		}

		ev := &models.MetricEvent{
			Type:         "prefill_progress",
			Slot:         slot,
			Task:         task,
			Phase:        "prefill",
			PromptTokens: promptTokens,
			Progress:     progress,
			PromptTPS:    promptTPS,
			Timestamp:    now,
		}
		return ev, true
	}

	if strings.Contains(body, "n_decoded") || strings.Contains(body, "n_gen") {
		genTok := 0
		genTPS := 0.0
		genTPS3s := 0.0

		if m := reNGen.FindStringSubmatch(body); m != nil {
			genTok, _ = strconv.Atoi(m[1])
		} else if m := reNDecoded.FindStringSubmatch(body); m != nil {
			genTok, _ = strconv.Atoi(m[1])
		}
		if m := reTps.FindStringSubmatch(body); m != nil {
			genTPS, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := reTps3s.FindStringSubmatch(body); m != nil {
			genTPS3s, _ = strconv.ParseFloat(m[1], 64)
		}

		ev := &models.MetricEvent{
			Type:            "generation_progress",
			Slot:            slot,
			Task:            task,
			Phase:           "generation",
			GeneratedTokens: genTok,
			GenerationTPS:   genTPS,
			GenerationTPS3s: genTPS3s,
			Timestamp:       now,
		}
		return ev, true
	}

	if strings.Contains(body, "eval time") && !strings.Contains(body, "prompt eval time") {
		evalTok := 0
		evalTPS := 0.0

		if m := reEvalTok.FindStringSubmatch(body); m != nil {
			evalTok, _ = strconv.Atoi(m[1])
		}
		if m := reEvalTPS.FindStringSubmatch(body); m != nil {
			evalTPS, _ = strconv.ParseFloat(m[2], 64)
		}

		ev := &models.MetricEvent{
			Type:            "generation_complete",
			Slot:            slot,
			Task:            task,
			Phase:           "generation",
			GeneratedTokens: evalTok,
			GenerationTPS:   evalTPS,
			Timestamp:       now,
		}
		return ev, true
	}

	if strings.Contains(body, "prompt eval time") {
		ev := &models.MetricEvent{
			Type:      "prompt_eval_result",
			Slot:      slot,
			Task:      task,
			Timestamp: now,
		}
		if m := rePromptEvalMs.FindStringSubmatch(body); m != nil {
			ev.PromptEvalMs, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := rePromptEvalTok.FindStringSubmatch(body); m != nil {
			ev.PromptTokens, _ = strconv.Atoi(m[1])
		}
		if m := rePromptEvalTPS.FindStringSubmatch(body); m != nil {
			ev.PromptTPS, _ = strconv.ParseFloat(m[2], 64)
		}
		return ev, true
	}

	if strings.Contains(body, "total time") {
		ev := &models.MetricEvent{
			Type:      "total_result",
			Slot:      slot,
			Task:      task,
			Timestamp: now,
		}
		if m := reTotalMs.FindStringSubmatch(body); m != nil {
			ev.TotalMs, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := reTotalTok.FindStringSubmatch(body); m != nil {
			ev.TotalTokens, _ = strconv.Atoi(m[1])
		}
		return ev, true
	}

	if strings.Contains(body, "graphs reused") {
		ev := &models.MetricEvent{
			Type:         "graphs_reused",
			Slot:         slot,
			Task:         task,
			Timestamp:    now,
		}
		if m := reGraphsReused.FindStringSubmatch(body); m != nil {
			ev.GraphsReused, _ = strconv.Atoi(m[1])
		}
		return ev, true
	}

	return nil, false
}

func (p *Parser) parseSlotSelection(body string, now time.Time) (*models.MetricEvent, bool) {
	if !strings.Contains(body, "selected slot by LCP similarity") {
		return nil, false
	}

	slot, _, ok := extractSlotTask(body)
	if !ok {
		return nil, false
	}

	keep := 0.0
	if m := reKeep.FindStringSubmatch(body); m != nil {
		keep, _ = strconv.ParseFloat(m[1], 64)
	}

	sim := 0.0
	if m := reSimBest.FindStringSubmatch(body); m != nil {
		sim, _ = strconv.ParseFloat(m[1], 64)
	}

	return &models.MetricEvent{
		Type:      "slot_selected",
		Slot:      slot,
		Task:      -1,
		Keep:      keep,
		Sim:       sim,
		Timestamp: now,
	}, true
}

func extractSlotTask(body string) (slot, task int, ok bool) {
	m := reSlotTask.FindStringSubmatch(body)
	if m == nil {
		return 0, 0, false
	}
	s, errS := strconv.Atoi(m[1])
	t, errT := strconv.Atoi(m[2])
	if errS != nil || errT != nil {
		return 0, 0, false
	}
	return s, t, true
}
