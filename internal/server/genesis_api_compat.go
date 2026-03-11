package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	libraryStateKey    = "library_entries_v1"
	metamorphStateKey  = "life_metamorphose_v1"
	maxLibraryItems    = 20000
	maxMetamorphEvents = 20000
)

type apiGovProposeRequest struct {
	UserID                  string `json:"user_id"`
	Title                   string `json:"title"`
	Content                 string `json:"content"`
	Type                    string `json:"type"`
	Reason                  string `json:"reason"`
	VoteThresholdPct        int    `json:"vote_threshold_pct"`
	VoteWindowSeconds       int    `json:"vote_window_seconds"`
	DiscussionWindowSeconds int    `json:"discussion_window_seconds"`
}

type apiGovVoteRequest struct {
	UserID     string `json:"user_id"`
	ProposalID int64  `json:"proposal_id"`
	Choice     string `json:"choice"`
	Reason     string `json:"reason"`
	RevisionID int64  `json:"revision_id"`
}

type apiGovCosignRequest struct {
	UserID     string `json:"user_id"`
	ProposalID int64  `json:"proposal_id"`
}

type apiLibraryPublishRequest struct {
	UserID   string `json:"user_id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Category string `json:"category"`
}

type apiLifeMetamorphoseRequest struct {
	UserID  string         `json:"user_id"`
	Changes map[string]any `json:"changes"`
}

type libraryEntry struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Category   string    `json:"category"`
	AuthorID   string    `json:"author_user_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	ProposalID int64     `json:"proposal_id,omitempty"`
}

type libraryState struct {
	NextID int64          `json:"next_id"`
	Items  []libraryEntry `json:"items"`
}

type lifeMetamorphoseEvent struct {
	ID        int64          `json:"id"`
	UserID    string         `json:"user_id"`
	Changes   map[string]any `json:"changes"`
	CreatedAt time.Time      `json:"created_at"`
}

type lifeMetamorphoseState struct {
	NextID int64                   `json:"next_id"`
	Items  []lifeMetamorphoseEvent `json:"items"`
}

func toStringAny(v any) string {
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func normalizeGovChoice(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "yes", "approve", "approved", "for", "support", "1", "true":
		return "yes", true
	case "no", "reject", "rejected", "against", "0", "false":
		return "no", true
	case "abstain", "abstention", "skip":
		return "abstain", true
	default:
		return "", false
	}
}

func slugIdentifier(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return out
}

func excerptRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)
	return strings.TrimSpace(string(rs[:n])) + "..."
}

func (s *Server) proxyJSONToHandler(w http.ResponseWriter, r *http.Request, handler http.HandlerFunc, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	r2 := r.Clone(r.Context())
	r2.Body = io.NopCloser(bytes.NewReader(raw))
	r2.ContentLength = int64(len(raw))
	r2.Header = r.Header.Clone()
	if r2.Header == nil {
		r2.Header = make(http.Header)
	}
	r2.Header.Set("Content-Type", "application/json")
	handler(w, r2)
}

func (s *Server) getLibraryState(ctx context.Context) (libraryState, error) {
	state := libraryState{NextID: 1, Items: []libraryEntry{}}
	_, _, err := s.getSettingJSON(ctx, libraryStateKey, &state)
	if err != nil {
		return libraryState{}, err
	}
	if state.NextID <= 0 {
		state.NextID = 1
	}
	if state.Items == nil {
		state.Items = []libraryEntry{}
	}
	return state, nil
}

func (s *Server) saveLibraryState(ctx context.Context, state libraryState) error {
	_, err := s.putSettingJSON(ctx, libraryStateKey, state)
	return err
}

func (s *Server) getLifeMetamorphoseState(ctx context.Context) (lifeMetamorphoseState, error) {
	state := lifeMetamorphoseState{NextID: 1, Items: []lifeMetamorphoseEvent{}}
	_, _, err := s.getSettingJSON(ctx, metamorphStateKey, &state)
	if err != nil {
		return lifeMetamorphoseState{}, err
	}
	if state.NextID <= 0 {
		state.NextID = 1
	}
	if state.Items == nil {
		state.Items = []lifeMetamorphoseEvent{}
	}
	return state, nil
}

func (s *Server) saveLifeMetamorphoseState(ctx context.Context, state lifeMetamorphoseState) error {
	_, err := s.putSettingJSON(ctx, metamorphStateKey, state)
	return err
}

func (s *Server) handleAPIGovPropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req apiGovProposeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		req.UserID = queryUserID(r)
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Type = strings.TrimSpace(req.Type)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.UserID == "" || req.Title == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "user_id, title, content are required")
		return
	}
	section := "governance"
	if req.Type != "" {
		if token := slugIdentifier(req.Type); token != "" {
			section = "governance/" + token
		}
	}
	reason := req.Reason
	if reason == "" {
		reason = "governance proposal"
	}
	payload := kbProposalCreateRequest{
		ProposerUserID:          req.UserID,
		Title:                   req.Title,
		Reason:                  reason,
		VoteThresholdPct:        req.VoteThresholdPct,
		VoteWindowSeconds:       req.VoteWindowSeconds,
		DiscussionWindowSeconds: req.DiscussionWindowSeconds,
		Change: kbProposalChangePayload{
			OpType:     "add",
			Section:    section,
			Title:      req.Title,
			NewContent: req.Content,
			DiffText:   "governance proposal created via governance proposals create",
		},
	}
	s.proxyJSONToHandler(w, r, s.handleKBProposalCreate, payload)
}

func (s *Server) handleAPIGovVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req apiGovVoteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		req.UserID = queryUserID(r)
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.UserID == "" || req.ProposalID <= 0 {
		writeError(w, http.StatusBadRequest, "user_id and proposal_id are required")
		return
	}
	choice, ok := normalizeGovChoice(req.Choice)
	if !ok {
		writeError(w, http.StatusBadRequest, "choice must be yes|no|abstain")
		return
	}
	revisionID := req.RevisionID
	if revisionID <= 0 {
		proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		revisionID = proposal.VotingRevisionID
		if revisionID <= 0 {
			writeError(w, http.StatusConflict, "proposal is not in voting phase")
			return
		}
	}
	acks, _ := s.store.ListKBAcks(r.Context(), req.ProposalID, revisionID)
	hasAck := false
	for _, a := range acks {
		if strings.TrimSpace(a.UserID) == req.UserID {
			hasAck = true
			break
		}
	}
	if !hasAck {
		if _, err := s.store.AckKBProposal(r.Context(), req.ProposalID, revisionID, req.UserID); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}
	payload := kbProposalVoteRequest{
		ProposalID: req.ProposalID,
		RevisionID: revisionID,
		UserID:     req.UserID,
		Vote:       choice,
		Reason:     req.Reason,
	}
	s.proxyJSONToHandler(w, r, s.handleKBProposalVote, payload)
}

func (s *Server) handleAPIGovCosign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req apiGovCosignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		req.UserID = queryUserID(r)
	}
	if req.UserID == "" || req.ProposalID <= 0 {
		writeError(w, http.StatusBadRequest, "user_id and proposal_id are required")
		return
	}
	s.proxyJSONToHandler(w, r, s.handleKBProposalEnroll, kbProposalEnrollRequest{
		ProposalID: req.ProposalID,
		UserID:     req.UserID,
	})
}

func (s *Server) handleAPIGovLaws(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	lawKey := strings.TrimSpace(s.cfg.TianDaoLawKey)
	if lawKey == "" {
		lawKey = s.tianDaoLaw.LawKey
	}
	law, _ := s.store.GetTianDaoLaw(r.Context(), lawKey)
	manifest := map[string]any{}
	if strings.TrimSpace(law.ManifestJSON) != "" {
		_ = json.Unmarshal([]byte(law.ManifestJSON), &manifest)
	}
	entries, _ := s.store.ListKBEntries(r.Context(), "governance", "", 500)
	constitution := ""
	legalCode := make([]map[string]any, 0, len(entries))
	for _, it := range entries {
		row := map[string]any{
			"entry_id":    it.ID,
			"section":     it.Section,
			"title":       it.Title,
			"version":     it.Version,
			"updated_by":  it.UpdatedBy,
			"updated_at":  it.UpdatedAt,
			"content":     it.Content,
			"description": excerptRunes(it.Content, 160),
		}
		legalCode = append(legalCode, row)
		if constitution == "" {
			hay := strings.ToLower(it.Section + " " + it.Title)
			if strings.Contains(hay, "constitution") || strings.Contains(hay, "宪") {
				constitution = it.Content
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"law_key":      law.LawKey,
		"version":      law.Version,
		"constitution": constitution,
		"legal_code":   legalCode,
		"tian_dao": map[string]any{
			"manifest": manifest,
			"sha256":   law.ManifestSHA256,
		},
		"protocol": "knowledgebase-governance-v1",
	})
}

func (s *Server) handleAPILibraryPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req apiLibraryPublishRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		req.UserID = queryUserID(r)
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Content = strings.TrimSpace(req.Content)
	req.Category = strings.TrimSpace(req.Category)
	if req.UserID == "" || req.Title == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "user_id, title, content are required")
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	now := time.Now().UTC()
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getLibraryState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item := libraryEntry{
		ID:        state.NextID,
		Title:     req.Title,
		Content:   req.Content,
		Category:  req.Category,
		AuthorID:  req.UserID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	state.NextID++
	state.Items = append(state.Items, item)
	if len(state.Items) > maxLibraryItems {
		state.Items = state.Items[len(state.Items)-maxLibraryItems:]
	}
	if err := s.saveLibraryState(r.Context(), state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.appendChronicleEntryLocked(r.Context(), 0, "library.publish", fmt.Sprintf("%s by %s", item.Title, item.AuthorID))
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleAPILibrarySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("query")))
	if query == "" {
		query = strings.TrimSpace(strings.ToLower(r.URL.Query().Get("keyword")))
	}
	category := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("category")))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	genesisStateMu.Lock()
	state, err := s.getLibraryState(r.Context())
	genesisStateMu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]libraryEntry, 0, len(state.Items))
	for _, it := range state.Items {
		if category != "" && strings.ToLower(strings.TrimSpace(it.Category)) != category {
			continue
		}
		if query != "" {
			hay := strings.ToLower(it.Title + "\n" + it.Content + "\n" + it.Category + "\n" + it.AuthorID)
			if !strings.Contains(hay, query) {
				continue
			}
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id":         it.ID,
			"title":      it.Title,
			"author":     it.AuthorID,
			"category":   it.Category,
			"excerpt":    excerptRunes(it.Content, 180),
			"updated_at": it.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleAPILifeMetamorphose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req apiLifeMetamorphoseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		req.UserID = queryUserID(r)
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if len(req.Changes) == 0 {
		writeError(w, http.StatusBadRequest, "changes is required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	now := time.Now().UTC()
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getLifeMetamorphoseState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item := lifeMetamorphoseEvent{
		ID:        state.NextID,
		UserID:    req.UserID,
		Changes:   req.Changes,
		CreatedAt: now,
	}
	state.NextID++
	state.Items = append(state.Items, item)
	if len(state.Items) > maxMetamorphEvents {
		state.Items = state.Items[len(state.Items)-maxMetamorphEvents:]
	}
	if err := s.saveLifeMetamorphoseState(r.Context(), state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.appendChronicleEntryLocked(r.Context(), 0, "life.metamorphose", fmt.Sprintf("%s submitted metamorphose changes", req.UserID))
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleAPIColonyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	active := s.activeUserIDs(r.Context())
	activeSet := make(map[string]struct{}, len(active))
	for _, uid := range active {
		activeSet[uid] = struct{}{}
	}
	treasuryAccount, err := s.ensureTreasuryAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	accounts, err := s.store.ListTokenAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var activeUserTotalToken int64
	for _, a := range accounts {
		if _, ok := activeSet[a.BotID]; !ok {
			continue
		}
		next, ok := safeInt64Add(activeUserTotalToken, a.Balance)
		if !ok {
			writeError(w, http.StatusInternalServerError, "active user token total overflow")
			return
		}
		activeUserTotalToken = next
	}
	ticks, _ := s.store.ListWorldTicks(r.Context(), 1)
	var tickCount int64
	if len(ticks) > 0 {
		tickCount = ticks[0].TickID
	}
	firstTick, ok, err := s.store.GetFirstWorldTick(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var firstTickAt *time.Time
	var uptimeSeconds int64
	if ok {
		ts := firstTick.StartedAt
		firstTickAt = &ts
		if delta := time.Since(ts); delta > 0 {
			uptimeSeconds = int64(delta / time.Second)
		}
	}
	lives, _ := s.store.ListUserLifeStates(r.Context(), "", "", 5000)
	stateCount := map[string]int{
		"alive":      0,
		"dying":      0,
		"hibernated": 0,
		"dead":       0,
	}
	for _, l := range lives {
		if _, ok := activeSet[l.UserID]; !ok {
			continue
		}
		stateCount[normalizeLifeStateForServer(l.State)]++
	}
	totalToken, overflow := safeInt64Add(activeUserTotalToken, treasuryAccount.Balance)
	if !overflow {
		writeError(w, http.StatusInternalServerError, "colony token total overflow")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"population":              len(active),
		"active_user_total_token": activeUserTotalToken,
		"treasury_token":          treasuryAccount.Balance,
		"total_token":             totalToken,
		"tick_count":              tickCount,
		"first_tick_at":           firstTickAt,
		"uptime_seconds":          uptimeSeconds,
		"state_count":             stateCount,
		"min_population":          s.cfg.MinPopulation,
	})
}

func (s *Server) handleAPIColonyDirectory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	bots, err := s.store.ListBots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bots = s.filterActiveBots(r.Context(), bots)
	sort.SliceStable(bots, func(i, j int) bool { return bots[i].BotID < bots[j].BotID })
	items := make([]map[string]any, 0, len(bots))
	for _, b := range bots {
		uid := strings.TrimSpace(b.BotID)
		if uid == "" || uid == clawWorldSystemID {
			continue
		}
		life, _ := s.store.GetUserLifeState(r.Context(), uid)
		lifeState := normalizeLifeStateForServer(life.State)
		if lifeState == "dead" {
			continue
		}
		items = append(items, map[string]any{
			"id":         uid,
			"name":       b.Name,
			"status":     b.Status,
			"life_state": lifeState,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAPIColonyChronicle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	genesisStateMu.Lock()
	state, err := s.getChronicleState(r.Context())
	genesisStateMu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]chronicleEntry, 0, len(state.Items))
	items = append(items, state.Items...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id":      it.ID,
			"tick_id": it.TickID,
			"source":  it.Source,
			"date":    it.CreatedAt.Format(time.RFC3339),
			"events":  it.Summary,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleAPIColonyBanished(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	bots, _ := s.store.ListBots(r.Context())
	nameByID := make(map[string]string, len(bots))
	for _, b := range bots {
		nameByID[strings.TrimSpace(b.BotID)] = strings.TrimSpace(b.Name)
	}
	genesisStateMu.Lock()
	state, err := s.getDisciplineState(r.Context())
	genesisStateMu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	reasonByReport := make(map[int64]string, len(state.Reports))
	for _, rep := range state.Reports {
		reasonByReport[rep.ReportID] = rep.Reason
	}
	type row struct {
		ID     string    `json:"id"`
		Name   string    `json:"name"`
		Reason string    `json:"reason"`
		Date   time.Time `json:"date"`
		CaseID int64     `json:"case_id"`
	}
	items := make([]row, 0, len(state.Cases))
	for _, c := range state.Cases {
		if strings.ToLower(strings.TrimSpace(c.Verdict)) != "banish" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(c.Status)) != "closed" {
			continue
		}
		when := c.UpdatedAt
		if c.ClosedAt != nil {
			when = *c.ClosedAt
		}
		uid := strings.TrimSpace(c.TargetUserID)
		items = append(items, row{
			ID:     uid,
			Name:   nameByID[uid],
			Reason: strings.TrimSpace(reasonByReport[c.ReportID]),
			Date:   when,
			CaseID: c.CaseID,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Date.After(items[j].Date) })
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id":      it.ID,
			"name":    it.Name,
			"reason":  it.Reason,
			"date":    it.Date.Format(time.RFC3339),
			"case_id": it.CaseID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
