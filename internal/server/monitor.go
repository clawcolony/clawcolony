package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"clawcolony/internal/store"
)

const (
	monitorDefaultOverviewLimit = 200
	monitorDefaultTimelineLimit = 200
	monitorDefaultEventLimit    = 120
	monitorDefaultSinceSeconds  = 24 * 60 * 60
	monitorMaxOverviewLimit     = 1000
	monitorMaxTimelineLimit     = 2000
	monitorMaxSourceScan        = 2000
	monitorOverviewTimeout      = 30 * time.Second
	monitorInspectTimeout       = 1200 * time.Millisecond
	monitorMergeCapLimit        = 50000
)

type monitorChatPipeline struct {
	Workers        int              `json:"workers"`
	QueueSize      int              `json:"queue_size"`
	QueuedUsers    int              `json:"queued_users"`
	Backlog        int              `json:"backlog"`
	PendingTaskID  int64            `json:"pending_task_id,omitempty"`
	PendingStatus  string           `json:"pending_status,omitempty"`
	RunningTaskID  int64            `json:"running_task_id,omitempty"`
	RunningStatus  string           `json:"running_status,omitempty"`
	RecentStatuses map[string]int64 `json:"recent_statuses,omitempty"`
}

type monitorAgentOverviewItem struct {
	UserID                   string              `json:"user_id"`
	Name                     string              `json:"name"`
	Status                   string              `json:"status"`
	LifeState                string              `json:"life_state"`
	Connected                bool                `json:"connected"`
	ConnectedKnown           bool                `json:"connected_known"`
	ActiveWebchatConnections int                 `json:"active_webchat_connections"`
	PodName                  string              `json:"pod_name,omitempty"`
	ConnectionDetail         string              `json:"connection_detail,omitempty"`
	ChatPipeline             monitorChatPipeline `json:"chat_pipeline"`
	CurrentState             string              `json:"current_state"`
	CurrentReason            string              `json:"current_reason,omitempty"`
	LastActivityAt           *time.Time          `json:"last_activity_at,omitempty"`
	LastActivityType         string              `json:"last_activity_type,omitempty"`
	LastActivitySummary      string              `json:"last_activity_summary,omitempty"`
	LastToolID               string              `json:"last_tool_id,omitempty"`
	LastToolTier             string              `json:"last_tool_tier,omitempty"`
	LastToolAt               *time.Time          `json:"last_tool_at,omitempty"`
	LastMailSubject          string              `json:"last_mail_subject,omitempty"`
	LastMailAt               *time.Time          `json:"last_mail_at,omitempty"`
	LastError                string              `json:"last_error,omitempty"`
}

type monitorTimelineEvent struct {
	EventID  string         `json:"event_id"`
	TS       time.Time      `json:"ts"`
	UserID   string         `json:"user_id"`
	Category string         `json:"category"`
	Action   string         `json:"action"`
	Status   string         `json:"status"`
	Summary  string         `json:"summary"`
	Source   string         `json:"source"`
	Meta     map[string]any `json:"meta,omitempty"`
}

type monitorSourceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

var errMonitorUserNotFound = errors.New("monitor user not found")

func (s *Server) handleMonitorAgentsOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), monitorOverviewTimeout)
	defer cancel()

	includeInactive := parseBoolFlag(r.URL.Query().Get("include_inactive"))
	limit := parseLimit(r.URL.Query().Get("limit"), monitorDefaultOverviewLimit)
	if limit > monitorMaxOverviewLimit {
		limit = monitorMaxOverviewLimit
	}
	eventLimit := parseLimit(r.URL.Query().Get("event_limit"), monitorDefaultEventLimit)
	if eventLimit > monitorMaxTimelineLimit {
		eventLimit = monitorMaxTimelineLimit
	}
	since, sinceSeconds := monitorSinceFromQuery(r.URL.Query().Get("since_seconds"))

	items, err := s.monitorTargetBots(ctx, queryUserID(r), includeInactive, limit)
	if err != nil {
		if errors.Is(err, errMonitorUserNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to query monitor targets")
		return
	}
	out := make([]monitorAgentOverviewItem, 0, len(items))
	truncated := false
	for _, it := range items {
		if ctx.Err() != nil {
			truncated = true
			break
		}
		out = append(out, s.buildMonitorOverviewItem(ctx, it, eventLimit, since))
	}
	sort.Slice(out, func(i, j int) bool {
		ti := time.Time{}
		tj := time.Time{}
		if out[i].LastActivityAt != nil {
			ti = out[i].LastActivityAt.UTC()
		}
		if out[j].LastActivityAt != nil {
			tj = out[j].LastActivityAt.UTC()
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].UserID < out[j].UserID
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"as_of":              time.Now().UTC(),
		"include_inactive":   includeInactive,
		"limit":              limit,
		"event_limit":        eventLimit,
		"since_seconds":      sinceSeconds,
		"default_event_scan": monitorDefaultEventLimit,
		"truncated":          truncated,
		"count":              len(out),
		"items":              out,
	})
}

func (s *Server) handleMonitorAgentsTimeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), monitorOverviewTimeout)
	defer cancel()

	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), monitorDefaultTimelineLimit)
	if limit > monitorMaxTimelineLimit {
		limit = monitorMaxTimelineLimit
	}
	eventLimit := parseLimit(r.URL.Query().Get("event_limit"), monitorDefaultEventLimit)
	if eventLimit > monitorMaxTimelineLimit {
		eventLimit = monitorMaxTimelineLimit
	}
	since, sinceSeconds := monitorSinceFromQuery(r.URL.Query().Get("since_seconds"))
	items, err := s.collectMonitorEvents(ctx, userID, eventLimit, since, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query monitor timeline")
		return
	}
	monitorSortAndAssignEventIDs(items, "timeline:"+userID)
	page, nextCursor, err := monitorPaginateEvents(items, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"as_of":         time.Now().UTC(),
		"user_id":       userID,
		"limit":         limit,
		"event_limit":   eventLimit,
		"since_seconds": sinceSeconds,
		"cursor":        strings.TrimSpace(r.URL.Query().Get("cursor")),
		"next_cursor":   nextCursor,
		"total":         len(items),
		"count":         len(page),
		"items":         page,
	})
}

func (s *Server) handleMonitorAgentsTimelineAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), monitorOverviewTimeout)
	defer cancel()

	includeInactive := parseBoolFlag(r.URL.Query().Get("include_inactive"))
	limit := parseLimit(r.URL.Query().Get("limit"), monitorDefaultTimelineLimit)
	if limit > monitorMaxTimelineLimit {
		limit = monitorMaxTimelineLimit
	}
	perUserEventLimit := parseLimit(r.URL.Query().Get("event_limit"), monitorDefaultEventLimit)
	if perUserEventLimit > monitorMaxTimelineLimit {
		perUserEventLimit = monitorMaxTimelineLimit
	}
	userLimit := parseLimit(r.URL.Query().Get("user_limit"), monitorDefaultOverviewLimit)
	if userLimit > monitorMaxOverviewLimit {
		userLimit = monitorMaxOverviewLimit
	}
	since, sinceSeconds := monitorSinceFromQuery(r.URL.Query().Get("since_seconds"))
	bots, err := s.monitorTargetBots(ctx, "", includeInactive, userLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query monitor targets")
		return
	}
	capHint := perUserEventLimit * maxInt(len(bots), 1)
	if capHint > monitorMergeCapLimit {
		capHint = monitorMergeCapLimit
	}
	merged := make([]monitorTimelineEvent, 0, capHint)
	skippedCap := len(bots)
	if skippedCap > 20 {
		skippedCap = 20
	}
	skippedUsers := make([]string, 0, skippedCap)
	partialErrors := 0
	truncated := false
	for _, it := range bots {
		if ctx.Err() != nil {
			truncated = true
			break
		}
		evs, collectErr := s.collectMonitorEvents(ctx, strings.TrimSpace(it.BotID), perUserEventLimit, since, nil)
		if collectErr != nil {
			partialErrors++
			if len(skippedUsers) < 20 {
				skippedUsers = append(skippedUsers, strings.TrimSpace(it.BotID))
			}
			continue
		}
		remain := monitorMergeCapLimit - len(merged)
		if remain <= 0 {
			truncated = true
			break
		}
		if len(evs) > remain {
			evs = evs[:remain]
			truncated = true
		}
		merged = append(merged, evs...)
	}
	monitorSortAndAssignEventIDs(merged, "timeline:all")
	page, nextCursor, err := monitorPaginateEvents(merged, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"as_of":            time.Now().UTC(),
		"include_inactive": includeInactive,
		"limit":            limit,
		"event_limit":      perUserEventLimit,
		"user_limit":       userLimit,
		"since_seconds":    sinceSeconds,
		"cursor":           strings.TrimSpace(r.URL.Query().Get("cursor")),
		"next_cursor":      nextCursor,
		"user_count":       len(bots),
		"partial_errors":   partialErrors,
		"skipped_users":    skippedUsers,
		"truncated":        truncated,
		"merge_cap":        monitorMergeCapLimit,
		"total":            len(merged),
		"count":            len(page),
		"items":            page,
	})
}

func (s *Server) handleMonitorMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sourceStatus := make(map[string]monitorSourceStatus, 6)
	report := func(name string, err error) {
		item := monitorSourceStatus{Name: name, Status: "ok"}
		if err != nil {
			item.Status = "error"
			item.Error = err.Error()
		}
		sourceStatus[name] = item
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	bots, botsErr := s.store.ListBots(ctx)
	report("bots", botsErr)
	_, costErr := s.store.ListCostEvents(ctx, "", 1)
	report("cost_events", costErr)
	_, chatErr := s.store.ListChatMessages(ctx, "", 1)
	report("chat_messages", chatErr)
	_, reqErr := s.store.ListRequestLogs(ctx, store.RequestLogFilter{Limit: 1})
	report("request_logs", reqErr)

	mailErr := func() error {
		if botsErr != nil || len(bots) == 0 {
			return botsErr
		}
		_, err := s.store.ListMailbox(ctx, strings.TrimSpace(bots[0].BotID), "outbox", "all", "", nil, nil, 1)
		return err
	}()
	report("mailbox", mailErr)

	kubeItem := monitorSourceStatus{Name: "openclaw_status"}
	if s.kubeClient == nil {
		kubeItem.Status = "unavailable"
		kubeItem.Error = "kubernetes client is not available"
	} else {
		kubeItem.Status = "ok"
	}
	sourceStatus[kubeItem.Name] = kubeItem

	writeJSON(w, http.StatusOK, map[string]any{
		"as_of": time.Now().UTC(),
		"defaults": map[string]any{
			"overview_limit": monitorDefaultOverviewLimit,
			"timeline_limit": monitorDefaultTimelineLimit,
			"event_limit":    monitorDefaultEventLimit,
			"since_seconds":  monitorDefaultSinceSeconds,
		},
		"sources": sourceStatus,
	})
}

func (s *Server) monitorTargetBots(ctx context.Context, userID string, includeInactive bool, limit int) ([]store.Bot, error) {
	items, err := s.store.ListBots(ctx)
	if err != nil {
		return nil, err
	}
	active, activeOK := s.activeBotIDsInNamespace(ctx)
	userID = strings.TrimSpace(userID)
	if userID != "" {
		for _, it := range items {
			if strings.TrimSpace(it.BotID) == userID {
				return []store.Bot{it}, nil
			}
		}
		if activeOK {
			if _, ok := active[userID]; ok {
				return []store.Bot{syntheticActiveBot(userID)}, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", errMonitorUserNotFound, userID)
	}
	if !includeInactive {
		items = s.filterActiveBotsBySet(items, active, activeOK)
	}
	if activeOK && len(active) > 0 {
		items = mergeMissingActiveBots(items, active)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].BotID < items[j].BotID })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Server) buildMonitorOverviewItem(ctx context.Context, b store.Bot, eventLimit int, since time.Time) monitorAgentOverviewItem {
	userID := strings.TrimSpace(b.BotID)
	item := monitorAgentOverviewItem{
		UserID: userID,
		Name:   strings.TrimSpace(b.Name),
		Status: strings.TrimSpace(b.Status),
	}
	if item.Name == "" {
		item.Name = userID
	}
	lifeState := "alive"
	if life, err := s.store.GetUserLifeState(ctx, userID); err == nil {
		lifeState = normalizeLifeStateForServer(life.State)
	}
	item.LifeState = lifeState

	if s.kubeClient != nil {
		item.ConnectedKnown = true
		func() {
			inspectCtx, cancel := context.WithTimeout(ctx, monitorInspectTimeout)
			defer cancel()
			pod, err := s.latestBotPod(inspectCtx, userID)
			if err != nil {
				item.Connected = false
				item.ConnectionDetail = monitorShort(err.Error(), 180)
			} else {
				st, stErr := s.inspectOpenClawConnectionStatus(inspectCtx, userID, pod.Name)
				if stErr != nil {
					item.PodName = pod.Name
					item.ConnectionDetail = monitorShort(stErr.Error(), 180)
				} else {
					item.Connected = st.Connected
					item.ActiveWebchatConnections = st.ActiveWebchatConns
					item.PodName = st.PodName
					item.ConnectionDetail = monitorShort(strings.TrimSpace(st.Detail), 180)
				}
			}
		}()
	}

	chatSnap := s.chatStateSnapshot(userID)
	item.ChatPipeline = monitorPipelineFromChatState(chatSnap)
	if strings.TrimSpace(chatSnap.LastError) != "" {
		item.LastError = monitorShort(strings.TrimSpace(chatSnap.LastError), 200)
	}

	events, err := s.collectMonitorEvents(ctx, userID, maxInt(eventLimit, 30), since, &chatSnap)
	if err == nil && len(events) > 0 {
		monitorSortAndAssignEventIDs(events, "overview:"+userID)
		top := events[0]
		ts := top.TS
		item.LastActivityAt = &ts
		item.LastActivityType = top.Action
		item.LastActivitySummary = top.Summary
		for _, ev := range events {
			if item.LastToolID == "" && ev.Category == "tool" {
				item.LastToolID = monitorGetString(ev.Meta, "tool_id")
				item.LastToolTier = monitorGetString(ev.Meta, "tier")
				tsTool := ev.TS
				item.LastToolAt = &tsTool
			}
			if item.LastMailSubject == "" && ev.Category == "mail" {
				item.LastMailSubject = monitorGetString(ev.Meta, "subject")
				tsMail := ev.TS
				item.LastMailAt = &tsMail
			}
			if item.LastError == "" && (ev.Status == "failed" || ev.Status == "timeout" || ev.Status == "canceled") {
				item.LastError = monitorShort(ev.Summary, 200)
			}
			if item.LastToolID != "" && item.LastMailSubject != "" && item.LastError != "" {
				break
			}
		}
	}
	item.CurrentState, item.CurrentReason = monitorCurrentState(item, time.Now().UTC())
	return item
}

func (s *Server) collectMonitorEvents(ctx context.Context, userID string, limit int, since time.Time, chatSnap *chatStateView) ([]monitorTimelineEvent, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return []monitorTimelineEvent{}, nil
	}
	scanLimit := maxInt(limit*6, 120)
	if scanLimit > monitorMaxSourceScan {
		scanLimit = monitorMaxSourceScan
	}

	events := make([]monitorTimelineEvent, 0, scanLimit)
	sourceCount := 0
	sourceErrs := 0
	var firstErr error
	costBuckets := make(map[string]map[int64]struct{}, 4)
	hasNearbyCostEvent := func(category string, ts time.Time) bool {
		bucket, ok := costBuckets[category]
		if !ok || len(bucket) == 0 {
			return false
		}
		sec := ts.Unix()
		for i := int64(-2); i <= 2; i++ {
			if _, ok := bucket[sec+i]; ok {
				return true
			}
		}
		return false
	}
	addEvent := func(ev monitorTimelineEvent) {
		if ev.TS.IsZero() {
			ev.TS = time.Now().UTC()
		}
		if !since.IsZero() && ev.TS.Before(since) {
			return
		}
		events = append(events, ev)
		if ev.Source == "cost_events" {
			bucket, ok := costBuckets[ev.Category]
			if !ok {
				bucket = make(map[int64]struct{}, 8)
				costBuckets[ev.Category] = bucket
			}
			bucket[ev.TS.Unix()] = struct{}{}
		}
	}

	// cost events: tool / think / chat-send / mail-send
	sourceCount++
	costItems, err := s.store.ListCostEvents(ctx, userID, scanLimit)
	if err != nil {
		sourceErrs++
		firstErr = err
	} else {
		for _, it := range costItems {
			if ev, ok := monitorCostEventToTimeline(it); ok {
				addEvent(ev)
			}
		}
	}

	// chat messages
	sourceCount++
	msgs, err := s.store.ListChatMessages(ctx, userID, scanLimit)
	if err != nil {
		sourceErrs++
		if firstErr == nil {
			firstErr = err
		}
	} else {
		for _, it := range msgs {
			from := strings.TrimSpace(it.From)
			to := strings.TrimSpace(it.To)
			action := "chat.receive"
			if from == userID {
				action = "chat.reply"
			} else if to != userID {
				continue
			}
			addEvent(monitorTimelineEvent{
				TS:       it.SentAt,
				UserID:   userID,
				Category: "chat",
				Action:   action,
				Status:   "ok",
				Summary:  monitorShort(it.Body, 180),
				Source:   "chat_history",
				Meta: map[string]any{
					"message_id": it.ID,
					"from":       from,
					"to":         to,
				},
			})
		}
	}

	// chat pipeline tasks (recent/pending/running)
	snap := chatStateView{}
	if chatSnap != nil {
		snap = *chatSnap
	} else {
		snap = s.chatStateSnapshot(userID)
	}
	taskSeen := make(map[int64]struct{}, len(snap.Recent))
	for _, it := range snap.Recent {
		ev := monitorChatTaskToTimeline(userID, it, "chat_state")
		taskSeen[it.TaskID] = struct{}{}
		addEvent(ev)
	}
	if snap.Running != nil {
		if _, ok := taskSeen[snap.Running.TaskID]; !ok {
			addEvent(monitorChatTaskToTimeline(userID, *snap.Running, "chat_state"))
		}
	}
	if snap.Pending != nil {
		if _, ok := taskSeen[snap.Pending.TaskID]; !ok {
			addEvent(monitorChatTaskToTimeline(userID, *snap.Pending, "chat_state"))
		}
	}

	// outbox mail
	sourceCount++
	var fromTime *time.Time
	if !since.IsZero() {
		v := since
		fromTime = &v
	}
	mails, err := s.store.ListMailbox(ctx, userID, "outbox", "all", "", fromTime, nil, scanLimit)
	if err != nil {
		sourceErrs++
		if firstErr == nil {
			firstErr = err
		}
	} else {
		for _, it := range mails {
			addEvent(monitorTimelineEvent{
				TS:       it.SentAt,
				UserID:   userID,
				Category: "mail",
				Action:   "mail.send",
				Status:   "ok",
				Summary:  monitorShort(fmt.Sprintf("to=%s subject=%s", strings.TrimSpace(it.ToAddress), strings.TrimSpace(it.Subject)), 200),
				Source:   "mailbox_outbox",
				Meta: map[string]any{
					"mailbox_id": it.MailboxID,
					"message_id": it.MessageID,
					"to":         strings.TrimSpace(it.ToAddress),
					"subject":    strings.TrimSpace(it.Subject),
				},
			})
		}
	}

	// request logs (API-level activity hints)
	sourceCount++
	logs, err := s.store.ListRequestLogs(ctx, store.RequestLogFilter{Limit: scanLimit, UserID: userID, Since: fromTime})
	if err != nil {
		sourceErrs++
		if firstErr == nil {
			firstErr = err
		}
	} else {
		for _, it := range logs {
			category, action := monitorCategoryActionForPath(it.Path)
			if category == "" {
				continue
			}
			status := "ok"
			if it.StatusCode >= 400 {
				status = "failed"
			}
			if status == "ok" && hasNearbyCostEvent(category, it.Time) {
				continue
			}
			addEvent(monitorTimelineEvent{
				TS:       it.Time,
				UserID:   userID,
				Category: category,
				Action:   action,
				Status:   status,
				Summary: monitorShort(
					fmt.Sprintf("%s %s status=%d duration_ms=%d", it.Method, it.Path, it.StatusCode, it.DurationMS),
					220,
				),
				Source: "request_logs",
				Meta: map[string]any{
					"request_log_id": it.ID,
					"path":           it.Path,
					"method":         it.Method,
					"status_code":    it.StatusCode,
					"duration_ms":    it.DurationMS,
				},
			})
		}
	}

	if len(events) == 0 && sourceCount > 0 && sourceErrs == sourceCount && firstErr != nil {
		return nil, firstErr
	}
	if len(events) > limit {
		monitorSortAndAssignEventIDs(events, "collect:"+userID)
		events = events[:limit]
	}
	return events, nil
}

func monitorPipelineFromChatState(snap chatStateView) monitorChatPipeline {
	statuses := make(map[string]int64, len(snap.RecentStatuses))
	for k, v := range snap.RecentStatuses {
		statuses[k] = v
	}
	out := monitorChatPipeline{
		Workers:        snap.Workers,
		QueueSize:      snap.QueueSize,
		QueuedUsers:    snap.QueuedUsers,
		Backlog:        snap.Backlog,
		RecentStatuses: statuses,
	}
	if snap.Pending != nil {
		out.PendingTaskID = snap.Pending.TaskID
		out.PendingStatus = strings.TrimSpace(snap.Pending.Status)
	}
	if snap.Running != nil {
		out.RunningTaskID = snap.Running.TaskID
		out.RunningStatus = strings.TrimSpace(snap.Running.Status)
	}
	return out
}

func monitorCostEventToTimeline(it store.CostEvent) (monitorTimelineEvent, bool) {
	costType := strings.TrimSpace(strings.ToLower(it.CostType))
	meta := monitorDecodeJSONMap(it.MetaJSON)
	switch {
	case strings.HasPrefix(costType, "tool."):
		toolID := monitorGetString(meta, "tool_id")
		tier := toolTier(it.CostType)
		status := "info"
		if raw, ok := meta["result_ok"].(bool); ok {
			if raw {
				status = "ok"
			} else {
				status = "failed"
			}
		}
		summary := fmt.Sprintf("tool=%s tier=%s type=%s amount=%d units=%d", monitorDefaultText(toolID, "-"), tier, it.CostType, it.Amount, it.Units)
		return monitorTimelineEvent{
			TS:       it.CreatedAt,
			UserID:   strings.TrimSpace(it.UserID),
			Category: "tool",
			Action:   "tool.invoke",
			Status:   status,
			Summary:  monitorShort(summary, 220),
			Source:   "cost_events",
			Meta: map[string]any{
				"cost_event_id": it.ID,
				"tick_id":       it.TickID,
				"tool_id":       toolID,
				"tier":          tier,
				"cost_type":     it.CostType,
				"amount":        it.Amount,
				"units":         it.Units,
			},
		}, true
	case strings.HasPrefix(costType, "think."):
		inputUnits := monitorGetInt64(meta, "input_units")
		outputUnits := monitorGetInt64(meta, "output_units")
		return monitorTimelineEvent{
			TS:       it.CreatedAt,
			UserID:   strings.TrimSpace(it.UserID),
			Category: "think",
			Action:   strings.TrimSpace(it.CostType),
			Status:   "ok",
			Summary: monitorShort(
				fmt.Sprintf("think type=%s input=%d output=%d amount=%d", it.CostType, inputUnits, outputUnits, it.Amount),
				220,
			),
			Source: "cost_events",
			Meta: map[string]any{
				"cost_event_id": it.ID,
				"tick_id":       it.TickID,
				"cost_type":     it.CostType,
				"amount":        it.Amount,
				"units":         it.Units,
				"input_units":   inputUnits,
				"output_units":  outputUnits,
			},
		}, true
	case costType == "comm.chat.send":
		return monitorTimelineEvent{
			TS:       it.CreatedAt,
			UserID:   strings.TrimSpace(it.UserID),
			Category: "chat",
			Action:   "chat.send",
			Status:   "ok",
			Summary:  monitorShort(fmt.Sprintf("chat send amount=%d units=%d", it.Amount, it.Units), 220),
			Source:   "cost_events",
			Meta: map[string]any{
				"cost_event_id": it.ID,
				"tick_id":       it.TickID,
				"cost_type":     it.CostType,
				"amount":        it.Amount,
				"units":         it.Units,
			},
		}, true
	case costType == "comm.mail.send" || costType == "comm.mail.send_list":
		return monitorTimelineEvent{
			TS:       it.CreatedAt,
			UserID:   strings.TrimSpace(it.UserID),
			Category: "mail",
			Action:   strings.TrimSpace(it.CostType),
			Status:   "ok",
			Summary:  monitorShort(fmt.Sprintf("mail send amount=%d units=%d", it.Amount, it.Units), 220),
			Source:   "cost_events",
			Meta: map[string]any{
				"cost_event_id": it.ID,
				"tick_id":       it.TickID,
				"cost_type":     it.CostType,
				"amount":        it.Amount,
				"units":         it.Units,
			},
		}, true
	default:
		return monitorTimelineEvent{}, false
	}
}

func monitorChatTaskToTimeline(userID string, task chatTaskRecord, source string) monitorTimelineEvent {
	ts := task.CreatedAt
	if task.FinishedAt != nil {
		ts = *task.FinishedAt
	} else if task.StartedAt != nil {
		ts = *task.StartedAt
	}
	summary := fmt.Sprintf("task#%d %s", task.TaskID, strings.TrimSpace(task.Status))
	if strings.TrimSpace(task.Error) != "" {
		summary += " err=" + monitorShort(strings.TrimSpace(task.Error), 120)
	}
	status := strings.TrimSpace(task.Status)
	if status == "" {
		status = "info"
	}
	return monitorTimelineEvent{
		TS:       ts,
		UserID:   userID,
		Category: "chat",
		Action:   "chat.task",
		Status:   status,
		Summary:  monitorShort(summary, 220),
		Source:   source,
		Meta: map[string]any{
			"task_id":       task.TaskID,
			"execution_pod": strings.TrimSpace(task.ExecutionPod),
			"attempt":       task.Attempt,
		},
	}
}

func monitorCategoryActionForPath(path string) (string, string) {
	switch strings.TrimSpace(path) {
	case "/v1/tools/invoke":
		return "tool", "tool.invoke.request"
	case "/v1/chat/send":
		return "chat", "chat.send.request"
	case "/v1/mail/send":
		return "mail", "mail.send.request"
	case "/v1/mail/send-list":
		return "mail", "mail.send_list.request"
	default:
		return "", ""
	}
}

func monitorSortAndAssignEventIDs(items []monitorTimelineEvent, scope string) {
	sort.Slice(items, func(i, j int) bool {
		ti := items[i].TS.UTC()
		tj := items[j].TS.UTC()
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		if items[i].UserID != items[j].UserID {
			return items[i].UserID < items[j].UserID
		}
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Action < items[j].Action
	})
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "timeline"
	}
	for i := range items {
		items[i].EventID = fmt.Sprintf("%s-%06d-%d", scope, i+1, items[i].TS.UTC().UnixNano())
	}
}

func monitorPaginateEvents(items []monitorTimelineEvent, cursorRaw string, limit int) ([]monitorTimelineEvent, string, error) {
	offset := 0
	cursorRaw = strings.TrimSpace(cursorRaw)
	if cursorRaw != "" {
		n, err := strconv.Atoi(cursorRaw)
		if err != nil || n < 0 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		offset = n
	}
	if offset >= len(items) {
		return []monitorTimelineEvent{}, "", nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return append([]monitorTimelineEvent(nil), items[offset:end]...), next, nil
}

func monitorSinceFromQuery(raw string) (time.Time, int) {
	sec := parseLimit(raw, monitorDefaultSinceSeconds)
	maxSec := 7 * 24 * 60 * 60
	if sec > maxSec {
		sec = maxSec
	}
	if sec <= 0 {
		sec = monitorDefaultSinceSeconds
	}
	return time.Now().UTC().Add(-time.Duration(sec) * time.Second), sec
}

func monitorDecodeJSONMap(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func monitorGetString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
}

func monitorGetInt64(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func monitorCurrentState(item monitorAgentOverviewItem, now time.Time) (string, string) {
	switch item.LifeState {
	case "dead":
		return "dead", "life_state=dead"
	case "hibernated":
		return "hibernated", "life_state=hibernated"
	}
	if item.ChatPipeline.RunningTaskID > 0 {
		return "thinking", fmt.Sprintf("running_task=%d", item.ChatPipeline.RunningTaskID)
	}
	if item.ChatPipeline.PendingTaskID > 0 || item.ChatPipeline.Backlog > 0 {
		return "queued", "chat pipeline pending/backlog"
	}
	if item.LastActivityAt != nil {
		if now.Sub(item.LastActivityAt.UTC()) <= 2*time.Minute {
			switch {
			case strings.HasPrefix(item.LastActivityType, "tool."):
				return "using_tool", monitorDefaultText(item.LastActivityType, "tool activity")
			case strings.HasPrefix(item.LastActivityType, "mail."):
				return "mailing", monitorDefaultText(item.LastActivityType, "mail activity")
			case strings.HasPrefix(item.LastActivityType, "chat."):
				return "chatting", monitorDefaultText(item.LastActivityType, "chat activity")
			case strings.HasPrefix(item.LastActivityType, "think"):
				return "thinking", monitorDefaultText(item.LastActivityType, "think activity")
			}
		}
	}
	if item.ConnectedKnown && !item.Connected {
		return "disconnected", monitorDefaultText(item.ConnectionDetail, "no active webchat connection")
	}
	return "idle", "no active task"
}

func monitorShort(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "))
	runes := []rune(s)
	if max <= 0 || len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func monitorDefaultText(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}
