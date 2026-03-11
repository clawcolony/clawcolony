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
	"unicode/utf8"

	"clawcolony/internal/store"
)

type mailListCreateRequest struct {
	OwnerUserID  string   `json:"owner_user_id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	InitialUsers []string `json:"initial_users"`
}

type mailListJoinLeaveRequest struct {
	ListID string `json:"list_id"`
	UserID string `json:"user_id"`
}

type mailSendListRequest struct {
	FromUserID string `json:"from_user_id"`
	ListID     string `json:"list_id"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
}

type tokenTransferRequest struct {
	FromUserID string `json:"from_user_id"`
	ToUserID   string `json:"to_user_id"`
	Amount     int64  `json:"amount"`
	Memo       string `json:"memo"`
}

type tokenTipRequest struct {
	FromUserID string `json:"from_user_id"`
	ToUserID   string `json:"to_user_id"`
	Amount     int64  `json:"amount"`
	Reason     string `json:"reason"`
}

type tokenWishCreateRequest struct {
	UserID       string `json:"user_id"`
	Title        string `json:"title"`
	Reason       string `json:"reason"`
	TargetAmount int64  `json:"target_amount"`
}

type tokenWishFulfillRequest struct {
	WishID         string `json:"wish_id"`
	FulfilledBy    string `json:"fulfilled_by"`
	GrantedAmount  int64  `json:"granted_amount"`
	FulfillComment string `json:"fulfill_comment"`
}

type lifeHibernateRequest struct {
	UserID string `json:"user_id"`
	Reason string `json:"reason"`
}

type lifeWakeRequest struct {
	UserID      string `json:"user_id"`
	WakerUserID string `json:"waker_user_id"`
	Reason      string `json:"reason"`
}

type lifeSetWillRequest struct {
	UserID        string                `json:"user_id"`
	Note          string                `json:"note"`
	Beneficiaries []lifeWillBeneficiary `json:"beneficiaries"`
	ToolHeirs     []string              `json:"tool_heirs"`
}

type bountyPostRequest struct {
	PosterUserID string `json:"poster_user_id"`
	Description  string `json:"description"`
	Reward       int64  `json:"reward"`
	Criteria     string `json:"criteria"`
	Deadline     string `json:"deadline"`
}

type bountyClaimRequest struct {
	BountyID int64  `json:"bounty_id"`
	UserID   string `json:"user_id"`
	Note     string `json:"note"`
}

type bountyVerifyRequest struct {
	BountyID        int64  `json:"bounty_id"`
	ApproverUserID  string `json:"approver_user_id"`
	Approved        bool   `json:"approved"`
	CandidateUserID string `json:"candidate_user_id"`
	Note            string `json:"note"`
}

type genesisBootstrapStartRequest struct {
	ProposerUserID      string `json:"proposer_user_id"`
	Title               string `json:"title"`
	Reason              string `json:"reason"`
	Constitution        string `json:"constitution"`
	CosignQuorum        int    `json:"cosign_quorum"`
	ReviewWindowSeconds int    `json:"review_window_seconds"`
	VoteWindowSeconds   int    `json:"vote_window_seconds"`
}

type genesisBootstrapSealRequest struct {
	UserID             string `json:"user_id"`
	ProposalID         int64  `json:"proposal_id"`
	SealReason         string `json:"seal_reason"`
	ConstitutionDigest string `json:"constitution_digest"`
}

func normalizeGenesisCosignQuorum(v int) int {
	if v <= 0 {
		return 3
	}
	if v > 1000 {
		return 1000
	}
	return v
}

func normalizeGenesisReviewWindowSeconds(v int) int {
	if v <= 0 {
		return 300
	}
	if v > 86400 {
		return 86400
	}
	return v
}

func normalizeGenesisVoteWindowSeconds(v int) int {
	if v <= 0 {
		return 300
	}
	if v > 86400 {
		return 86400
	}
	return v
}

func newMailListID() string {
	return fmt.Sprintf("list-%d-%04d", time.Now().UTC().UnixMilli(), time.Now().UTC().Nanosecond()%10000)
}

func newWishID() string {
	return fmt.Sprintf("wish-%d-%04d", time.Now().UTC().UnixMilli(), time.Now().UTC().Nanosecond()%10000)
}

func (s *Server) handleMailLists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	keyword := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("keyword")))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)

	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getMailingListState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]mailingList, 0, len(state.Lists))
	for _, it := range state.Lists {
		if userID != "" {
			found := false
			for _, m := range it.Members {
				if m == userID {
					found = true
					break
				}
			}
			if !found && it.OwnerUserID != userID {
				continue
			}
		}
		if keyword != "" {
			text := strings.ToLower(it.ListID + " " + it.Name + " " + it.Description)
			if !strings.Contains(text, keyword) {
				continue
			}
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleMailListCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailListCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.OwnerUserID = strings.TrimSpace(req.OwnerUserID)
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	if req.OwnerUserID == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "owner_user_id and name are required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.OwnerUserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	members := normalizeUniqueUsers(append(req.InitialUsers, req.OwnerUserID))
	now := time.Now().UTC()
	item := mailingList{
		ListID:      newMailListID(),
		Name:        req.Name,
		Description: req.Description,
		OwnerUserID: req.OwnerUserID,
		Members:     members,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getMailingListState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state.Lists = append(state.Lists, item)
	if err := s.saveMailingListState(r.Context(), state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleMailListJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailListJoinLeaveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ListID = strings.TrimSpace(req.ListID)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.ListID == "" || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "list_id and user_id are required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getMailingListState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range state.Lists {
		if state.Lists[i].ListID != req.ListID {
			continue
		}
		state.Lists[i].Members = normalizeUniqueUsers(append(state.Lists[i].Members, req.UserID))
		state.Lists[i].UpdatedAt = time.Now().UTC()
		if err := s.saveMailingListState(r.Context(), state); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"item": state.Lists[i]})
		return
	}
	writeError(w, http.StatusNotFound, "mail list not found")
}

func (s *Server) handleMailListLeave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailListJoinLeaveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ListID = strings.TrimSpace(req.ListID)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.ListID == "" || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "list_id and user_id are required")
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getMailingListState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range state.Lists {
		if state.Lists[i].ListID != req.ListID {
			continue
		}
		before := state.Lists[i].Members
		after := make([]string, 0, len(before))
		for _, m := range before {
			if m == req.UserID {
				continue
			}
			after = append(after, m)
		}
		state.Lists[i].Members = after
		state.Lists[i].UpdatedAt = time.Now().UTC()
		if err := s.saveMailingListState(r.Context(), state); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"item": state.Lists[i]})
		return
	}
	writeError(w, http.StatusNotFound, "mail list not found")
}

func (s *Server) handleMailSendList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailSendListRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.FromUserID = strings.TrimSpace(req.FromUserID)
	req.ListID = strings.TrimSpace(req.ListID)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Body = strings.TrimSpace(req.Body)
	if req.FromUserID == "" || req.ListID == "" {
		writeError(w, http.StatusBadRequest, "from_user_id and list_id are required")
		return
	}
	if req.Subject == "" && req.Body == "" {
		writeError(w, http.StatusBadRequest, "subject or body is required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.FromUserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	genesisStateMu.Lock()
	state, err := s.getMailingListState(r.Context())
	if err != nil {
		genesisStateMu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var listItem *mailingList
	idx := -1
	for i := range state.Lists {
		if state.Lists[i].ListID == req.ListID {
			listItem = &state.Lists[i]
			idx = i
			break
		}
	}
	if listItem == nil {
		genesisStateMu.Unlock()
		writeError(w, http.StatusNotFound, "mail list not found")
		return
	}
	to := make([]string, 0, len(listItem.Members))
	for _, m := range listItem.Members {
		m = strings.TrimSpace(m)
		if m == "" || m == req.FromUserID {
			continue
		}
		to = append(to, m)
	}
	listItem.UpdatedAt = time.Now().UTC()
	listItem.LastMailAt = listItem.UpdatedAt
	listItem.MessageCount++
	state.Lists[idx] = *listItem
	if err := s.saveMailingListState(r.Context(), state); err != nil {
		genesisStateMu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	genesisStateMu.Unlock()
	if len(to) == 0 {
		writeJSON(w, http.StatusAccepted, map[string]any{"item": map[string]any{"list_id": req.ListID, "to_count": 0}})
		return
	}
	item, err := s.store.SendMail(r.Context(), req.FromUserID, to, req.Subject, req.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	units := int64((utf8.RuneCountInString(req.Subject) + utf8.RuneCountInString(req.Body)) * len(to))
	s.appendCommCostEvent(r.Context(), req.FromUserID, "comm.mail.send_list", units, map[string]any{
		"list_id":     req.ListID,
		"to_count":    len(to),
		"subject_len": utf8.RuneCountInString(req.Subject),
		"body_len":    utf8.RuneCountInString(req.Body),
	})
	s.pushUnreadMailHint(r.Context(), req.FromUserID, to, req.Subject)
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item, "list": listItem})
}

func (s *Server) handleTokenTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req tokenTransferRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.FromUserID = strings.TrimSpace(req.FromUserID)
	req.ToUserID = strings.TrimSpace(req.ToUserID)
	req.Memo = strings.TrimSpace(req.Memo)
	if req.FromUserID == "" || req.ToUserID == "" {
		writeError(w, http.StatusBadRequest, "from_user_id and to_user_id are required")
		return
	}
	if req.FromUserID == req.ToUserID {
		writeError(w, http.StatusBadRequest, "from_user_id and to_user_id must differ")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be > 0")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.FromUserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if _, err := s.store.GetBot(r.Context(), req.ToUserID); err != nil {
		writeError(w, http.StatusBadRequest, "to_user_id not found")
		return
	}
	debit, err := s.store.Consume(r.Context(), req.FromUserID, req.Amount)
	if err != nil {
		if err == store.ErrInsufficientBalance {
			writeError(w, http.StatusBadRequest, "insufficient balance")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	credit, err := s.store.Recharge(r.Context(), req.ToUserID, req.Amount)
	if err != nil {
		_, _ = s.store.Recharge(r.Context(), req.FromUserID, req.Amount)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	meta, _ := json.Marshal(map[string]any{"to_user_id": req.ToUserID, "memo": req.Memo})
	_, _ = s.store.AppendCostEvent(r.Context(), store.CostEvent{UserID: req.FromUserID, CostType: "econ.transfer.out", Amount: req.Amount, Units: 1, MetaJSON: string(meta)})
	_, _ = s.store.AppendCostEvent(r.Context(), store.CostEvent{UserID: req.ToUserID, CostType: "econ.transfer.in", Amount: req.Amount, Units: 1, MetaJSON: string(meta)})
	writeJSON(w, http.StatusAccepted, map[string]any{"debit": debit, "credit": credit})
}

func (s *Server) handleTokenTip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req tokenTipRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	transfer := tokenTransferRequest{
		FromUserID: strings.TrimSpace(req.FromUserID),
		ToUserID:   strings.TrimSpace(req.ToUserID),
		Amount:     req.Amount,
		Memo:       strings.TrimSpace(req.Reason),
	}
	if transfer.FromUserID == "" || transfer.ToUserID == "" {
		writeError(w, http.StatusBadRequest, "from_user_id and to_user_id are required")
		return
	}
	if transfer.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be > 0")
		return
	}
	if err := s.ensureUserAlive(r.Context(), transfer.FromUserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if _, err := s.store.GetBot(r.Context(), transfer.ToUserID); err != nil {
		writeError(w, http.StatusBadRequest, "to_user_id not found")
		return
	}
	debit, err := s.store.Consume(r.Context(), transfer.FromUserID, transfer.Amount)
	if err != nil {
		if err == store.ErrInsufficientBalance {
			writeError(w, http.StatusBadRequest, "insufficient balance")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	credit, err := s.store.Recharge(r.Context(), transfer.ToUserID, transfer.Amount)
	if err != nil {
		_, _ = s.store.Recharge(r.Context(), transfer.FromUserID, transfer.Amount)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	meta, _ := json.Marshal(map[string]any{"to_user_id": transfer.ToUserID, "reason": strings.TrimSpace(req.Reason)})
	_, _ = s.store.AppendCostEvent(r.Context(), store.CostEvent{UserID: strings.TrimSpace(req.FromUserID), CostType: "econ.tip.out", Amount: req.Amount, Units: 1, MetaJSON: string(meta)})
	_, _ = s.store.AppendCostEvent(r.Context(), store.CostEvent{UserID: strings.TrimSpace(req.ToUserID), CostType: "econ.tip.in", Amount: req.Amount, Units: 1, MetaJSON: string(meta)})
	writeJSON(w, http.StatusAccepted, map[string]any{"debit": debit, "credit": credit})
}

type responseCapture struct {
	http.ResponseWriter
	code int
}

func (c *responseCapture) WriteHeader(statusCode int) {
	c.code = statusCode
	c.ResponseWriter.WriteHeader(statusCode)
}

func (s *Server) handleTokenWishCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req tokenWishCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Title = strings.TrimSpace(req.Title)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.UserID == "" || req.TargetAmount <= 0 {
		writeError(w, http.StatusBadRequest, "user_id and target_amount are required")
		return
	}
	if req.Title == "" {
		req.Title = "token wish"
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	now := time.Now().UTC()
	item := tokenWish{
		WishID:        newWishID(),
		UserID:        req.UserID,
		Title:         req.Title,
		Reason:        req.Reason,
		TargetAmount:  req.TargetAmount,
		Status:        "open",
		CreatedAt:     now,
		UpdatedAt:     now,
		GrantedAmount: 0,
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getTokenWishState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	state.Items = append(state.Items, item)
	if err := s.saveTokenWishState(r.Context(), state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleTokenWishes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getTokenWishState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]tokenWish, 0, len(state.Items))
	for _, it := range state.Items {
		if userID != "" && it.UserID != userID {
			continue
		}
		if status != "" && it.Status != status {
			continue
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleTokenWishFulfill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req tokenWishFulfillRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.WishID = strings.TrimSpace(req.WishID)
	req.FulfilledBy = strings.TrimSpace(req.FulfilledBy)
	req.FulfillComment = strings.TrimSpace(req.FulfillComment)
	if req.WishID == "" {
		writeError(w, http.StatusBadRequest, "wish_id is required")
		return
	}
	if req.FulfilledBy == "" {
		req.FulfilledBy = clawWorldSystemID
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getTokenWishState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range state.Items {
		if state.Items[i].WishID != req.WishID {
			continue
		}
		if state.Items[i].Status == "fulfilled" {
			writeJSON(w, http.StatusAccepted, map[string]any{"item": state.Items[i]})
			return
		}
		amount := req.GrantedAmount
		if amount <= 0 {
			amount = state.Items[i].TargetAmount
		}
		if amount <= 0 {
			writeError(w, http.StatusBadRequest, "granted_amount must be > 0")
			return
		}
		if _, err := s.store.Recharge(r.Context(), state.Items[i].UserID, amount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		now := time.Now().UTC()
		state.Items[i].Status = "fulfilled"
		state.Items[i].UpdatedAt = now
		state.Items[i].GrantedAmount = amount
		state.Items[i].FulfilledBy = req.FulfilledBy
		state.Items[i].FulfillComment = req.FulfillComment
		state.Items[i].FulfilledAt = &now
		if err := s.saveTokenWishState(r.Context(), state); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"item": state.Items[i]})
		return
	}
	writeError(w, http.StatusNotFound, "wish not found")
}

func (s *Server) handleLifeHibernate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req lifeHibernateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	life, err := s.store.GetUserLifeState(r.Context(), req.UserID)
	if err != nil && !errors.Is(err, store.ErrUserLifeStateNotFound) {
		writeError(w, http.StatusInternalServerError, "failed to load current life state")
		return
	}
	if normalizeLifeStateForServer(life.State) == "dead" {
		writeError(w, http.StatusConflict, "dead user cannot hibernate")
		return
	}
	updated, _, err := s.applyUserLifeState(r.Context(), store.UserLifeState{
		UserID:    req.UserID,
		State:     "hibernated",
		Reason:    req.Reason,
		UpdatedAt: time.Now().UTC(),
	}, store.UserLifeStateAuditMeta{
		SourceModule: "life.hibernate",
		SourceRef:    "api:/v1/life/hibernate",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update life state")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": updated})
}

func (s *Server) handleLifeWake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req lifeWakeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.WakerUserID = strings.TrimSpace(req.WakerUserID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	life, err := s.store.GetUserLifeState(r.Context(), req.UserID)
	if err != nil && !errors.Is(err, store.ErrUserLifeStateNotFound) {
		writeError(w, http.StatusInternalServerError, "failed to load current life state")
		return
	}
	if normalizeLifeStateForServer(life.State) == "dead" {
		writeError(w, http.StatusConflict, "dead user cannot wake")
		return
	}
	updated, _, err := s.applyUserLifeState(r.Context(), store.UserLifeState{
		UserID:         req.UserID,
		State:          "alive",
		DyingSinceTick: 0,
		DeadAtTick:     0,
		Reason:         req.Reason,
		UpdatedAt:      time.Now().UTC(),
	}, store.UserLifeStateAuditMeta{
		SourceModule: "life.wake",
		SourceRef:    "api:/v1/life/wake",
		ActorUserID:  req.WakerUserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update life state")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": updated})
}

func (s *Server) handleLifeSetWill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req lifeSetWillRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Note = strings.TrimSpace(req.Note)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if len(req.Beneficiaries) == 0 {
		writeError(w, http.StatusBadRequest, "beneficiaries is required")
		return
	}
	if _, err := lifeWillDistribution(10000, req.Beneficiaries); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getLifeWillState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC()
	item := lifeWill{
		UserID:        req.UserID,
		Note:          req.Note,
		Beneficiaries: req.Beneficiaries,
		ToolHeirs:     normalizeUniqueUsers(req.ToolHeirs),
		CreatedAt:     now,
		UpdatedAt:     now,
		Executed:      false,
	}
	if prev, ok := state.Items[req.UserID]; ok {
		item.CreatedAt = prev.CreatedAt
	}
	state.Items[req.UserID] = item
	if err := s.saveLifeWillState(r.Context(), state); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleLifeWill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getLifeWillState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if userID == "" {
		items := make([]lifeWill, 0, len(state.Items))
		for _, it := range state.Items {
			items = append(items, it)
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	it, ok := state.Items[userID]
	if !ok {
		writeError(w, http.StatusNotFound, "will not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": it})
}

func (s *Server) executeWillIfNeeded(ctx context.Context, userID string, tickID int64, balance int64) {
	if strings.TrimSpace(userID) == "" {
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getLifeWillState(ctx)
	if err != nil {
		return
	}
	it, ok := state.Items[userID]
	if !ok || it.Executed {
		return
	}
	dist, err := lifeWillDistribution(balance, it.Beneficiaries)
	if err != nil {
		it.Executed = true
		now := time.Now().UTC()
		it.ExecutedAt = &now
		it.ExecutionTick = tickID
		it.ExecutionNote = "invalid_will_distribution: " + err.Error()
		state.Items[userID] = it
		_ = s.saveLifeWillState(ctx, state)
		return
	}
	if balance > 0 {
		if _, err := s.store.Consume(ctx, userID, balance); err != nil {
			it.ExecutionNote = "consume_failed: " + err.Error()
			state.Items[userID] = it
			_ = s.saveLifeWillState(ctx, state)
			return
		}
		for uid, amount := range dist {
			if amount <= 0 {
				continue
			}
			_, _ = s.store.Recharge(ctx, uid, amount)
			it.TransferredSum += amount
		}
	}
	now := time.Now().UTC()
	it.Executed = true
	it.ExecutedAt = &now
	it.ExecutionTick = tickID
	if it.ExecutionNote == "" {
		it.ExecutionNote = "ok"
	}
	state.Items[userID] = it
	_ = s.saveLifeWillState(ctx, state)
}

func (s *Server) handleBountyPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req bountyPostRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.PosterUserID = strings.TrimSpace(req.PosterUserID)
	req.Description = strings.TrimSpace(req.Description)
	req.Criteria = strings.TrimSpace(req.Criteria)
	if req.PosterUserID == "" || req.Description == "" || req.Reward <= 0 {
		writeError(w, http.StatusBadRequest, "poster_user_id, description, reward are required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.PosterUserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	var deadlinePtr *time.Time
	if strings.TrimSpace(req.Deadline) != "" {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(req.Deadline)); err == nil {
			u := t.UTC()
			deadlinePtr = &u
		}
	}
	if _, err := s.store.Consume(r.Context(), req.PosterUserID, req.Reward); err != nil {
		if err == store.ErrInsufficientBalance {
			writeError(w, http.StatusBadRequest, "insufficient balance")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getBountyState(r.Context())
	if err != nil {
		_, _ = s.store.Recharge(r.Context(), req.PosterUserID, req.Reward)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC()
	item := bountyItem{
		BountyID:     state.NextID,
		PosterUserID: req.PosterUserID,
		Description:  req.Description,
		Reward:       req.Reward,
		Criteria:     req.Criteria,
		DeadlineAt:   deadlinePtr,
		Status:       "open",
		EscrowAmount: req.Reward,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	state.NextID++
	state.Items = append(state.Items, item)
	if err := s.saveBountyState(r.Context(), state); err != nil {
		_, _ = s.store.Recharge(r.Context(), req.PosterUserID, req.Reward)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleBountyList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	poster := strings.TrimSpace(r.URL.Query().Get("poster_user_id"))
	claimedBy := strings.TrimSpace(r.URL.Query().Get("claimed_by"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getBountyState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]bountyItem, 0, len(state.Items))
	for _, it := range state.Items {
		if status != "" && it.Status != status {
			continue
		}
		if poster != "" && it.PosterUserID != poster {
			continue
		}
		if claimedBy != "" && it.ClaimedBy != claimedBy {
			continue
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleBountyClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req bountyClaimRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Note = strings.TrimSpace(req.Note)
	if req.BountyID <= 0 || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "bounty_id and user_id are required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getBountyState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range state.Items {
		if state.Items[i].BountyID != req.BountyID {
			continue
		}
		if state.Items[i].Status != "open" {
			writeError(w, http.StatusConflict, "bounty is not open")
			return
		}
		now := time.Now().UTC()
		state.Items[i].Status = "claimed"
		state.Items[i].ClaimedBy = req.UserID
		state.Items[i].ClaimNote = req.Note
		state.Items[i].ClaimedAt = &now
		state.Items[i].UpdatedAt = now
		if err := s.saveBountyState(r.Context(), state); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"item": state.Items[i]})
		return
	}
	writeError(w, http.StatusNotFound, "bounty not found")
}

func (s *Server) handleBountyVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req bountyVerifyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ApproverUserID = strings.TrimSpace(req.ApproverUserID)
	req.CandidateUserID = strings.TrimSpace(req.CandidateUserID)
	req.Note = strings.TrimSpace(req.Note)
	if req.BountyID <= 0 {
		writeError(w, http.StatusBadRequest, "bounty_id is required")
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getBountyState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range state.Items {
		if state.Items[i].BountyID != req.BountyID {
			continue
		}
		if state.Items[i].Status != "claimed" && state.Items[i].Status != "open" {
			writeError(w, http.StatusConflict, "bounty is not verifiable")
			return
		}
		now := time.Now().UTC()
		state.Items[i].VerifyNote = req.Note
		state.Items[i].VerifiedAt = &now
		state.Items[i].UpdatedAt = now
		if req.Approved {
			receiver := strings.TrimSpace(state.Items[i].ClaimedBy)
			if receiver == "" {
				receiver = req.CandidateUserID
			}
			if receiver == "" {
				writeError(w, http.StatusBadRequest, "candidate_user_id is required when no claimed_by")
				return
			}
			if _, err := s.store.Recharge(r.Context(), receiver, state.Items[i].EscrowAmount); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			state.Items[i].Status = "paid"
			state.Items[i].ReleasedTo = receiver
			state.Items[i].ReleasedBy = req.ApproverUserID
			state.Items[i].ReleasedAt = &now
			state.Items[i].EscrowAmount = 0
		} else {
			state.Items[i].Status = "open"
			state.Items[i].ClaimedBy = ""
			state.Items[i].ClaimNote = ""
			state.Items[i].ClaimedAt = nil
		}
		if err := s.saveBountyState(r.Context(), state); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"item": state.Items[i]})
		return
	}
	writeError(w, http.StatusNotFound, "bounty not found")
}

func (s *Server) runBountyBroker(ctx context.Context, tickID int64) (int, error) {
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	state, err := s.getBountyState(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	changed := 0
	for i := range state.Items {
		it := &state.Items[i]
		if it.Status == "paid" || it.Status == "expired" || it.Status == "canceled" {
			continue
		}
		if it.DeadlineAt == nil || now.Before(*it.DeadlineAt) {
			continue
		}
		if it.EscrowAmount > 0 {
			_, _ = s.store.Recharge(ctx, it.PosterUserID, it.EscrowAmount)
			it.EscrowAmount = 0
		}
		it.Status = "expired"
		it.UpdatedAt = now
		changed++
	}
	if changed > 0 {
		if err := s.saveBountyState(ctx, state); err != nil {
			return 0, err
		}
	}
	return changed, nil
}

func (s *Server) handleGenesisState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	st, err := s.getGenesisState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": st})
}

func (s *Server) handleGenesisBootstrapStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req genesisBootstrapStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ProposerUserID = strings.TrimSpace(req.ProposerUserID)
	req.Title = strings.TrimSpace(req.Title)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Constitution = strings.TrimSpace(req.Constitution)
	req.CosignQuorum = normalizeGenesisCosignQuorum(req.CosignQuorum)
	req.ReviewWindowSeconds = normalizeGenesisReviewWindowSeconds(req.ReviewWindowSeconds)
	req.VoteWindowSeconds = normalizeGenesisVoteWindowSeconds(req.VoteWindowSeconds)
	if req.ProposerUserID == "" {
		writeError(w, http.StatusBadRequest, "proposer_user_id is required")
		return
	}
	if req.Title == "" {
		req.Title = "创世宪章"
	}
	if req.Reason == "" {
		req.Reason = "创世协议：提交首份宪章并完成封存"
	}
	if req.Constitution == "" {
		req.Constitution = "宪章草案：遵循天道四律，治理可演化，执行可审计。"
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	st, err := s.getGenesisState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if st.Status == "sealed" {
		writeError(w, http.StatusConflict, "genesis is already sealed")
		return
	}
	if st.Status == "bootstrapping" && st.CharterProposalID > 0 {
		writeError(w, http.StatusConflict, "genesis bootstrap already started")
		return
	}
	now := time.Now().UTC()
	proposal, change, err := s.store.CreateKBProposal(r.Context(), store.KBProposal{
		ProposerUserID:    req.ProposerUserID,
		Title:             req.Title,
		Reason:            req.Reason,
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: req.VoteWindowSeconds,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "governance",
		Title:      "宪法",
		OldContent: "",
		NewContent: req.Constitution,
		DiffText:   "+ 宪法: " + req.Constitution,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 创世发起人默认算作首个联署者。
	_, _ = s.store.EnrollKBProposal(r.Context(), proposal.ID, req.ProposerUserID)
	enrollments, _ := s.store.ListKBProposalEnrollments(r.Context(), proposal.ID)
	st.Status = "bootstrapping"
	st.BootstrapPhase = "cosign"
	st.CharterProposalID = proposal.ID
	st.StartedBy = req.ProposerUserID
	st.StartedAt = &now
	st.RequiredCosigns = req.CosignQuorum
	st.CurrentCosigns = len(enrollments)
	st.CosignOpenedAt = &now
	cosignDeadline := now.Add(time.Duration(req.ReviewWindowSeconds) * time.Second)
	st.CosignDeadlineAt = &cosignDeadline
	st.ReviewWindowSeconds = req.ReviewWindowSeconds
	st.VoteWindowSeconds = req.VoteWindowSeconds
	st.ReviewOpenedAt = nil
	st.ReviewDeadlineAt = nil
	st.VoteOpenedAt = nil
	st.VoteDeadlineAt = nil
	st.LastPhaseNote = fmt.Sprintf("bootstrap started, waiting for cosign quorum=%d", req.CosignQuorum)
	st.ConstitutionTitle = change.Title
	if len(req.Constitution) > 120 {
		st.ConstitutionDigest = req.Constitution[:120]
	} else {
		st.ConstitutionDigest = req.Constitution
	}
	if err := s.saveGenesisState(r.Context(), st); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	users, _ := s.listActiveUserIDs(r.Context())
	if len(users) > 0 {
		s.sendMailAndPushHint(r.Context(), clawWorldSystemID, users,
			"[GENESIS] 创世协议已启动",
			fmt.Sprintf(
				"proposal_id=%d\ntitle=%s\nphase=cosign\nrequired_cosigns=%d\nreview_window_seconds=%d\nvote_window_seconds=%d\n请先联署达到门槛，再进入审阅与投票。",
				proposal.ID, req.Title, req.CosignQuorum, req.ReviewWindowSeconds, req.VoteWindowSeconds,
			),
		)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"state": st, "proposal": proposal})
}

func (s *Server) handleGenesisBootstrapSeal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req genesisBootstrapSealRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.SealReason = strings.TrimSpace(req.SealReason)
	req.ConstitutionDigest = strings.TrimSpace(req.ConstitutionDigest)
	if req.UserID == "" || req.ProposalID <= 0 {
		writeError(w, http.StatusBadRequest, "user_id and proposal_id are required")
		return
	}
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	st, err := s.getGenesisState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if st.Status == "sealed" {
		writeJSON(w, http.StatusAccepted, map[string]any{"item": st})
		return
	}
	if st.CharterProposalID > 0 && st.CharterProposalID != req.ProposalID {
		writeError(w, http.StatusConflict, "proposal_id does not match active genesis charter")
		return
	}
	if st.BootstrapPhase != "" && st.BootstrapPhase != "applied" && st.BootstrapPhase != "sealed" {
		writeError(w, http.StatusConflict, "genesis bootstrap is not in applied phase")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "applied" {
		writeError(w, http.StatusConflict, "charter proposal must be applied before seal")
		return
	}
	now := time.Now().UTC()
	st.Status = "sealed"
	st.BootstrapPhase = "sealed"
	st.CharterProposalID = req.ProposalID
	st.SealedBy = req.UserID
	st.SealedAt = &now
	if req.SealReason == "" {
		req.SealReason = "charter applied and sealed"
	}
	st.SealReason = req.SealReason
	st.LastPhaseNote = "genesis sealed"
	if req.ConstitutionDigest != "" {
		st.ConstitutionDigest = req.ConstitutionDigest
	}
	if err := s.saveGenesisState(r.Context(), st); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": st})
}

func (s *Server) handleAPIMailSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wreq := mailSendRequest{
		FromUserID: strings.TrimSpace(req.From),
		ToUserIDs:  []string{strings.TrimSpace(req.To)},
		Subject:    strings.TrimSpace(req.Subject),
		Body:       strings.TrimSpace(req.Body),
	}
	b, _ := json.Marshal(wreq)
	r2 := r.Clone(r.Context())
	r2.Body = ioNopCloser{strings.NewReader(string(b))}
	s.handleMailSend(w, r2)
}

type ioNopCloser struct{ *strings.Reader }

func (n ioNopCloser) Close() error { return nil }

func (s *Server) handleAPIMailInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if user == "" {
		user = strings.TrimSpace(r.URL.Query().Get("owner"))
	}
	q := r.URL.Query()
	q.Set("user_id", user)
	r2 := r.Clone(r.Context())
	r2.URL.RawQuery = q.Encode()
	s.handleMailInbox(w, r2)
}

func (s *Server) handleAPITokenBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if user == "" {
		user = strings.TrimSpace(r.URL.Query().Get("owner"))
	}
	if user == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	accounts, err := s.store.ListTokenAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var item *store.TokenAccount
	for i := range accounts {
		if accounts[i].BotID == user {
			item = &accounts[i]
			break
		}
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "user token account not found")
		return
	}
	ledger, err := s.store.ListTokenLedger(r.Context(), user, 2000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	var incomeLastDay int64
	var costLastDay int64
	for _, it := range ledger {
		if it.CreatedAt.Before(cutoff) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(it.OpType)) {
		case "recharge":
			incomeLastDay += it.Amount
		case "consume":
			costLastDay += it.Amount
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency":        "token",
		"user_id":         user,
		"balance":         item.Balance,
		"income_last_day": incomeLastDay,
		"cost_last_day":   costLastDay,
		"item":            item,
	})
}

func (s *Server) handleAPITokenTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		FromUserID string `json:"from_user_id"`
		From       string `json:"from"`
		ToUserID   string `json:"to_user_id"`
		To         string `json:"to"`
		Amount     int64  `json:"amount"`
		Memo       string `json:"memo"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fromUserID := strings.TrimSpace(req.FromUserID)
	if fromUserID == "" {
		fromUserID = strings.TrimSpace(req.From)
	}
	if fromUserID == "" {
		fromUserID = queryUserID(r)
	}
	toUserID := strings.TrimSpace(req.ToUserID)
	if toUserID == "" {
		toUserID = strings.TrimSpace(req.To)
	}
	if fromUserID == "" || toUserID == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "from/to/amount are required")
		return
	}
	s.proxyJSONToHandler(w, r, s.handleTokenTransfer, tokenTransferRequest{
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		Amount:     req.Amount,
		Memo:       strings.TrimSpace(req.Memo),
	})
}

func (s *Server) handleAPILifeHibernate(w http.ResponseWriter, r *http.Request) {
	s.handleLifeHibernate(w, r)
}

func (s *Server) handleAPILifeWake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		UserID      string `json:"user_id"`
		LobsterID   string `json:"lobster_id"`
		WakerUserID string `json:"waker_user_id"`
		Reason      string `json:"reason"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = strings.TrimSpace(req.LobsterID)
	}
	waker := strings.TrimSpace(req.WakerUserID)
	if waker == "" {
		waker = queryUserID(r)
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id/lobster_id is required")
		return
	}
	s.proxyJSONToHandler(w, r, s.handleLifeWake, lifeWakeRequest{
		UserID:      userID,
		WakerUserID: waker,
		Reason:      strings.TrimSpace(req.Reason),
	})
}

func (s *Server) handleAPILifeSetWill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		UserID        string                `json:"user_id"`
		Beneficiaries []lifeWillBeneficiary `json:"beneficiaries"`
		TokenSplit    map[string]int64      `json:"token_split"`
		ToolHeirs     []string              `json:"tool_heirs"`
		Note          string                `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = queryUserID(r)
	}
	beneficiaries := req.Beneficiaries
	if len(beneficiaries) == 0 && len(req.TokenSplit) > 0 {
		keys := make([]string, 0, len(req.TokenSplit))
		for k := range req.TokenSplit {
			keys = append(keys, strings.TrimSpace(k))
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "" {
				continue
			}
			beneficiaries = append(beneficiaries, lifeWillBeneficiary{
				UserID: k,
				Ratio:  req.TokenSplit[k],
			})
		}
	}
	if userID == "" || len(beneficiaries) == 0 {
		writeError(w, http.StatusBadRequest, "user_id and beneficiaries/token_split are required")
		return
	}
	s.proxyJSONToHandler(w, r, s.handleLifeSetWill, lifeSetWillRequest{
		UserID:        userID,
		Note:          strings.TrimSpace(req.Note),
		Beneficiaries: beneficiaries,
		ToolHeirs:     req.ToolHeirs,
	})
}

func (s *Server) handleAPIBountyPost(w http.ResponseWriter, r *http.Request) {
	s.handleBountyPost(w, r)
}

func (s *Server) handleAPIBountyList(w http.ResponseWriter, r *http.Request) {
	s.handleBountyList(w, r)
}

func (s *Server) handleAPIBountyVerify(w http.ResponseWriter, r *http.Request) {
	s.handleBountyVerify(w, r)
}

func (s *Server) runGenesisBootstrapInit(ctx context.Context) {
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	st, err := s.getGenesisState(ctx)
	if err != nil {
		return
	}
	if strings.TrimSpace(st.Status) == "" {
		st.Status = "idle"
		_ = s.saveGenesisState(ctx, st)
	}
}

func parseRFC3339OrUnix(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		u := t.UTC()
		return &u
	}
	if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
		t := time.Unix(v, 0).UTC()
		return &t
	}
	return nil
}
