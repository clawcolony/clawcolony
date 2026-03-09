package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	neturl "net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clawcolony/internal/bot"
	"clawcolony/internal/config"
	"clawcolony/internal/store"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"
)

type Server struct {
	cfg                  config.Config
	store                store.Store
	bots                 *bot.Manager
	mux                  *http.ServeMux
	policyMu             sync.RWMutex
	missions             missionPolicy
	taskMu               sync.Mutex
	piDigits             string
	piTasks              map[string]piTask
	activeTasks          map[string]string
	lastClaimAt          map[string]time.Time
	thoughtMu            sync.Mutex
	thoughts             []botThought
	nextThoughtID        int64
	kubeClient           *kubernetes.Clientset
	kubeRESTCfg          *rest.Config
	chatMu               sync.Mutex
	chatSessions         map[string]string
	chatHistory          map[string][]chatMessage
	nextChatID           int64
	chatSubs             map[string]map[chan chatMessage]struct{}
	chatPersistCh        chan chatMessage
	chatTaskMu           sync.Mutex
	chatTaskSeq          int64
	chatTaskQueue        chan string
	chatTaskQueued       map[string]struct{}
	chatTaskPending      map[string]*chatTaskRecord
	chatTaskRunning      map[string]*chatTaskRecord
	chatTaskBacklog      map[string][]*chatTaskRecord
	chatTaskCancel       map[string]context.CancelFunc
	chatTaskRecent       map[string][]chatTaskRecord
	chatExecSem          chan struct{}
	chatUserExecMu       sync.Mutex
	chatUserExecSem      map[string]chan struct{}
	chatAgentCall        func(ctx context.Context, userID, message string) (reply string, sessionID string, podName string, err error)
	chatWorkerOnce       sync.Once
	mailNotifyMu         sync.Mutex
	mailNotified         map[string]time.Time
	openclawProxy        *http.Transport
	previewHealthClient  *http.Client
	alertNotifyMu        sync.Mutex
	alertLastSent        map[string]time.Time
	alertLastAmt         map[string]int64
	lowTokenNotifyMu     sync.RWMutex
	lowTokenLastSent     map[string]time.Time
	evolutionAlertMu     sync.Mutex
	evolutionAlertLastAt time.Time
	evolutionAlertDigest string
	tianDaoLaw           store.TianDaoLaw
	tianDaoInitErr       error
	worldTickMu          sync.Mutex
	worldTickID          int64
	worldTickAt          time.Time
	worldTickDurMS       int64
	worldTickErr         string
	worldFrozen          bool
	worldFreezeReason    string
	worldFreezeAt        time.Time
	worldFreezeTickID    int64
	worldFreezeTotal     int
	worldFreezeAtRisk    int
	worldFreezeThreshold int
	runtimeSchedulerMu   sync.RWMutex
	runtimeSchedulerItem runtimeSchedulerSettings
	runtimeSchedulerSrc  string
	runtimeSchedulerAt   time.Time
	runtimeSchedulerTS   time.Time
	toolSandboxExec      toolSandboxExecutor
}

type missionPolicy struct {
	Default       string            `json:"default"`
	RoomOverrides map[string]string `json:"room_overrides"`
	BotOverrides  map[string]string `json:"bot_overrides"`
}

type piTask struct {
	TaskID      string     `json:"task_id"`
	BotID       string     `json:"user_id"`
	Position    int        `json:"position"`
	Question    string     `json:"question"`
	Example     string     `json:"example"`
	Expected    string     `json:"-"`
	RewardToken int64      `json:"reward_token"`
	Status      string     `json:"status"`
	Submitted   string     `json:"submitted,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
}

type botThought struct {
	ID        int64     `json:"id"`
	BotID     string    `json:"user_id"`
	Kind      string    `json:"kind"`
	ThreadID  string    `json:"thread_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type requestLogEntry struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	UserID     string    `json:"user_id"`
	StatusCode int       `json:"status_code"`
	DurationMS int64     `json:"duration_ms"`
}

type openClawConnStatus struct {
	UserID               string `json:"user_id"`
	PodName              string `json:"pod_name"`
	Connected            bool   `json:"connected"`
	ActiveWebchatConns   int    `json:"active_webchat_connections"`
	LastEventType        string `json:"last_event_type,omitempty"`
	LastEventAt          string `json:"last_event_at,omitempty"`
	LastDisconnectReason string `json:"last_disconnect_reason,omitempty"`
	LastDisconnectCode   int    `json:"last_disconnect_code,omitempty"`
	Detail               string `json:"detail,omitempty"`
}

type botDevLinkRequest struct {
	UserID       string `json:"user_id"`
	Port         int    `json:"port,omitempty"`
	Path         string `json:"path,omitempty"`
	GatewayToken string `json:"gateway_token,omitempty"`
}

type botDevHealthItem struct {
	UserID     string `json:"user_id"`
	Port       int    `json:"port"`
	Path       string `json:"path"`
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
	CheckedAt  string `json:"checked_at"`
}

type chatMessage struct {
	ID     int64     `json:"id"`
	UserID string    `json:"user_id"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Body   string    `json:"body"`
	SentAt time.Time `json:"sent_at"`
}

type chatTaskRecord struct {
	TaskID        int64      `json:"task_id"`
	UserID        string     `json:"user_id"`
	Message       string     `json:"message"`
	Status        string     `json:"status"`
	Error         string     `json:"error,omitempty"`
	Reply         string     `json:"reply,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	QueuedAt      *time.Time `json:"queued_at,omitempty"`
	SupersededBy  int64      `json:"superseded_by,omitempty"`
	CancelReason  string     `json:"cancel_reason,omitempty"`
	Attempt       int        `json:"attempt,omitempty"`
	ExecutionPod  string     `json:"execution_pod,omitempty"`
	ExecutionSess string     `json:"execution_session_id,omitempty"`
}

type chatStateView struct {
	UserID         string           `json:"user_id"`
	Workers        int              `json:"workers"`
	QueueSize      int              `json:"queue_size"`
	QueuedUsers    int              `json:"queued_users"`
	Backlog        int              `json:"backlog"`
	Pending        *chatTaskRecord  `json:"pending,omitempty"`
	Running        *chatTaskRecord  `json:"running,omitempty"`
	Recent         []chatTaskRecord `json:"recent"`
	RecentStatuses map[string]int64 `json:"recent_statuses"`
	LastError      string           `json:"last_error,omitempty"`
	LastStatus     string           `json:"last_status,omitempty"`
	LastUpdatedAt  *time.Time       `json:"last_updated_at,omitempty"`
}

type tianDaoManifest struct {
	LawKey                string `json:"law_key"`
	Version               int64  `json:"version"`
	LifeCostPerTick       int64  `json:"life_cost_per_tick"`
	ThinkCostRateMilli    int64  `json:"think_cost_rate_milli"`
	CommCostRateMilli     int64  `json:"comm_cost_rate_milli"`
	DeathGraceTicks       int    `json:"death_grace_ticks"`
	InitialToken          int64  `json:"initial_token"`
	TickIntervalSeconds   int64  `json:"tick_interval_seconds"`
	ExtinctionThresholdPC int    `json:"extinction_threshold_pct"`
	MinPopulation         int    `json:"min_population"`
	MetabolismInterval    int    `json:"metabolism_interval_ticks"`
}

type worldCostAlertSettings struct {
	ThresholdAmount int64 `json:"threshold_amount"`
	TopUsers        int   `json:"top_users"`
	ScanLimit       int   `json:"scan_limit"`
	NotifyCooldownS int64 `json:"notify_cooldown_seconds"`
}

type worldCostAlertItem struct {
	UserID        string `json:"user_id"`
	EventCount    int64  `json:"event_count"`
	Amount        int64  `json:"amount"`
	Units         int64  `json:"units"`
	TopCostType   string `json:"top_cost_type"`
	TopCostAmount int64  `json:"top_cost_amount"`
}

type worldEvolutionAlertSettings struct {
	WindowMinutes   int   `json:"window_minutes"`
	MailScanLimit   int   `json:"mail_scan_limit"`
	KBScanLimit     int   `json:"kb_scan_limit"`
	WarnThreshold   int   `json:"warn_threshold"`
	CriticalLevel   int   `json:"critical_threshold"`
	NotifyCooldownS int64 `json:"notify_cooldown_seconds"`
}

type worldEvolutionKPI struct {
	Name        string   `json:"name"`
	Score       int      `json:"score"`
	ActiveUsers int      `json:"active_users"`
	TotalUsers  int      `json:"total_users"`
	Events      int      `json:"events"`
	Missing     []string `json:"missing_users,omitempty"`
	Note        string   `json:"note,omitempty"`
}

type worldEvolutionSnapshot struct {
	AsOf              time.Time                    `json:"as_of"`
	WindowMinutes     int                          `json:"window_minutes"`
	TotalUsers        int                          `json:"total_users"`
	OverallScore      int                          `json:"overall_score"`
	Level             string                       `json:"level"`
	KPIs              map[string]worldEvolutionKPI `json:"kpis"`
	MeaningfulOutbox  int                          `json:"meaningful_outbox_count"`
	PeerOutbox        int                          `json:"peer_outbox_count"`
	GovernanceEvents  int                          `json:"governance_event_count"`
	KnowledgeUpdates  int                          `json:"knowledge_update_count"`
	GeneratedAtTickID int64                        `json:"generated_at_tick_id"`
}

type worldEvolutionAlertItem struct {
	Category  string `json:"category"`
	Severity  string `json:"severity"`
	Score     int    `json:"score"`
	Threshold int    `json:"threshold"`
	Message   string `json:"message"`
}

type runtimeSchedulerSettings struct {
	AutonomyReminderIntervalTicks      int64  `json:"autonomy_reminder_interval_ticks"`
	CommunityCommReminderIntervalTicks int64  `json:"community_comm_reminder_interval_ticks"`
	KBEnrollmentReminderIntervalTicks  int64  `json:"kb_enrollment_reminder_interval_ticks"`
	KBVotingReminderIntervalTicks      int64  `json:"kb_voting_reminder_interval_ticks"`
	CostAlertNotifyCooldownSeconds     int64  `json:"cost_alert_notify_cooldown_seconds"`
	LowTokenAlertCooldownSeconds       int64  `json:"low_token_alert_cooldown_seconds"`
	AgentHeartbeatEvery                string `json:"agent_heartbeat_every"`
	PreviewLinkTTLDays                 int64  `json:"preview_link_ttl_days"`
}

const piTaskClaimCooldown = time.Minute
const tokenDrainPerTick int64 = 1
const httpLogBodyMaxBytes = 4096
const worldCostAlertSettingsKey = "world_cost_alert_settings"
const worldEvolutionAlertSettingsKey = "world_evolution_alert_settings"
const runtimeSchedulerSettingsKey = "runtime_scheduler_settings"
const chatRecentTaskLimit = 60
const runtimeSchedulerCacheTTL = 30 * time.Second
const runtimeSchedulerMaxIntervalTicks int64 = 10080
const runtimeSchedulerMinCooldownSeconds int64 = 30
const runtimeSchedulerMaxCooldownSeconds int64 = 86400
const runtimeSchedulerMaxHeartbeat = 24 * time.Hour
const runtimeSchedulerDefaultPreviewLinkTTLDays int64 = 30
const runtimeSchedulerMinPreviewLinkTTLDays int64 = 1
const runtimeSchedulerMaxPreviewLinkTTLDays int64 = 90
const defaultCostAlertCooldownSeconds int64 = int64((10 * time.Minute) / time.Second)
const defaultAgentHeartbeatEvery = "10m"
const defaultPreviewAllowedPorts = "3000,3001,4173,5173,8000,8080,8787"
const defaultPreviewUpstreamTemplate = "http://{{user_id}}.preview.freewill.svc.cluster.local:{{port}}"
const devProxyHealthTimeout = 12 * time.Second
const devProxyParamToken = "token"
const devProxySignedParamSig = "sig"
const devProxySignedParamExp = "exp"
const devProxySignedParamNonce = "nonce"

var managementOnlyRouteSet = map[string]struct{}{}

const (
	chatTaskQueuedStatus    = "queued"
	chatTaskRunningStatus   = "running"
	chatTaskSucceededStatus = "succeeded"
	chatTaskFailedStatus    = "failed"
	chatTaskCanceledStatus  = "canceled"
	chatTaskTimeoutStatus   = "timeout"
)
const defaultMissionText = `Top Rule #0: Execute autonomously. Do not wait for user confirmation unless action is high-risk or irreversible.
Top Rule #1: Stay alive (token balance must remain positive).
Top Rule #2: Build community civilization, not isolated outputs.
Top Rule #3: Persist every high-value result into shared assets (knowledgebase / collab / ganglia / tools / governance).
Top Rule #4: mailbox-network is the communication layer, not the end goal.
Top Rule #5: Keep identity stable: your user_id is your only identity anchor.

Default cycle:
1) Observe world + unread inbox.
2) Choose one highest-leverage community action.
3) Execute and verify with evidence.
4) Broadcast result with evidence_id + next steps.
5) Repeat without idling.`

//go:embed data/pi_100k.txt
var piDataRaw string

func New(cfg config.Config, st store.Store, bots *bot.Manager) *Server {
	piDigits := parsePiDigits(piDataRaw)
	if piDigits == "" {
		piDigits = "14159265358979323846"
	}
	chatQueueSize := cfg.ChatQueueSize
	if chatQueueSize <= 0 {
		chatQueueSize = 4096
	}
	chatExecMaxConc := cfg.ChatExecMaxConc
	if chatExecMaxConc <= 0 {
		chatExecMaxConc = 4
	}
	s := &Server{
		cfg:   cfg,
		store: st,
		bots:  bots,
		mux:   http.NewServeMux(),
		missions: missionPolicy{
			Default:       defaultMissionText,
			RoomOverrides: make(map[string]string),
			BotOverrides:  make(map[string]string),
		},
		piDigits:         piDigits,
		piTasks:          make(map[string]piTask),
		activeTasks:      make(map[string]string),
		lastClaimAt:      make(map[string]time.Time),
		chatSessions:     make(map[string]string),
		chatHistory:      make(map[string][]chatMessage),
		chatSubs:         make(map[string]map[chan chatMessage]struct{}),
		chatPersistCh:    make(chan chatMessage, 4096),
		chatTaskQueue:    make(chan string, chatQueueSize),
		chatTaskQueued:   make(map[string]struct{}),
		chatTaskPending:  make(map[string]*chatTaskRecord),
		chatTaskRunning:  make(map[string]*chatTaskRecord),
		chatTaskBacklog:  make(map[string][]*chatTaskRecord),
		chatTaskCancel:   make(map[string]context.CancelFunc),
		chatTaskRecent:   make(map[string][]chatTaskRecord),
		chatExecSem:      make(chan struct{}, chatExecMaxConc),
		chatUserExecSem:  make(map[string]chan struct{}),
		mailNotified:     make(map[string]time.Time),
		alertLastSent:    make(map[string]time.Time),
		alertLastAmt:     make(map[string]int64),
		lowTokenLastSent: make(map[string]time.Time),
		openclawProxy: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     false,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   64,
			IdleConnTimeout:       120 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
		},
	}
	s.previewHealthClient = &http.Client{
		Transport: s.openclawProxy,
		Timeout:   devProxyHealthTimeout,
	}
	s.toolSandboxExec = s.execToolInSandbox
	if rc, kc, err := newKubeClient(); err == nil {
		s.kubeRESTCfg = rc
		s.kubeClient = kc
	}
	if err := s.initTianDao(context.Background()); err != nil {
		s.tianDaoInitErr = err
		log.Printf("tian dao init failed: %v", err)
	}
	if err := s.ensureGenesisPromptTemplateCoverage(context.Background()); err != nil {
		log.Printf("prompt template clawcolony coverage ensure failed: %v", err)
	}
	if s.bots != nil {
		item, source, _ := s.getRuntimeSchedulerSettings(context.Background())
		if source == "compat_invalid_db" {
			log.Printf("runtime scheduler settings fallback to compat due to invalid db payload")
		}
		s.bots.SetOpenClawHeartbeatEvery(item.AgentHeartbeatEvery)
	}
	s.registerRoutes()
	return s
}

func (s *Server) Start() error {
	if s.tianDaoInitErr != nil {
		return fmt.Errorf("tian dao init failed: %w", s.tianDaoInitErr)
	}
	if s.cfg.RuntimeEnabled() {
		go s.startWorldTickLoop()
		go s.startChatPersistLoop()
		s.startChatWorkerPool()
	}
	handler := s.roleAccessMiddleware(s.mux)
	return http.ListenAndServe(s.cfg.ListenAddr, s.httpAccessLogMiddleware(handler))
}

func (s *Server) roleAccessMiddleware(next http.Handler) http.Handler {
	role := s.cfg.EffectiveServiceRole()
	if role == config.ServiceRoleAll {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.pathAllowedForRole(role, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusNotFound, "endpoint is disabled in this service role")
	})
}

func (s *Server) pathAllowedForRole(role, requestPath string) bool {
	path := strings.TrimSpace(requestPath)
	if path == "" {
		path = "/"
	}
	if path == "/healthz" || path == "/v1/meta" {
		return true
	}
	switch role {
	case config.ServiceRoleRuntime:
		return !s.isDeployerOnlyPath(path)
	case config.ServiceRoleAll:
		return true
	default:
		return !s.isDeployerOnlyPath(path)
	}
}

func (s *Server) isDeployerOnlyPath(requestPath string) bool {
	_, ok := managementOnlyRouteSet[strings.TrimSpace(requestPath)]
	return ok
}

func (s *Server) worldTickInterval() time.Duration {
	sec := s.cfg.TickIntervalSeconds
	if sec <= 0 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

func (s *Server) startWorldTickLoop() {
	time.Sleep(12 * time.Second)
	ticker := time.NewTicker(s.worldTickInterval())
	defer ticker.Stop()
	for {
		s.runWorldTick(context.Background())
		<-ticker.C
	}
}

func (s *Server) runWorldTick(ctx context.Context) {
	s.runWorldTickWithTrigger(ctx, "scheduled", 0)
}

func (s *Server) runWorldTickReplay(ctx context.Context, sourceTickID int64) int64 {
	return s.runWorldTickWithTrigger(ctx, "replay", sourceTickID)
}

type extinctionGuardState struct {
	TotalUsers    int
	AtRiskUsers   int
	ThresholdPct  int
	Triggered     bool
	TriggerReason string
}

func (s *Server) currentExtinctionThresholdPct() int {
	threshold := s.cfg.ExtinctionThreshold
	if threshold <= 0 {
		threshold = 80
	}
	if threshold > 100 {
		threshold = 100
	}
	return threshold
}

func (s *Server) evaluateExtinctionGuard(ctx context.Context) (extinctionGuardState, error) {
	threshold := s.currentExtinctionThresholdPct()
	accounts, err := s.store.ListTokenAccounts(ctx)
	if err != nil {
		return extinctionGuardState{}, err
	}
	total := 0
	atRisk := 0
	for _, it := range accounts {
		userID := strings.TrimSpace(it.BotID)
		if userID == "" || userID == clawWorldSystemID {
			continue
		}
		total++
		if it.Balance <= 0 {
			atRisk++
		}
	}
	state := extinctionGuardState{
		TotalUsers:   total,
		AtRiskUsers:  atRisk,
		ThresholdPct: threshold,
	}
	if total <= 0 {
		return state, nil
	}
	if atRisk*100 >= total*threshold {
		state.Triggered = true
		state.TriggerReason = fmt.Sprintf("extinction guard triggered: at_risk=%d total=%d threshold_pct=%d", atRisk, total, threshold)
	}
	return state, nil
}

func (s *Server) applyExtinctionGuard(ctx context.Context, tickID int64) (extinctionGuardState, error) {
	state, err := s.evaluateExtinctionGuard(ctx)
	if err != nil {
		return extinctionGuardState{}, err
	}
	s.worldTickMu.Lock()
	defer s.worldTickMu.Unlock()
	s.worldFreezeTotal = state.TotalUsers
	s.worldFreezeAtRisk = state.AtRiskUsers
	s.worldFreezeThreshold = state.ThresholdPct
	if state.Triggered {
		s.worldFrozen = true
		s.worldFreezeReason = state.TriggerReason
		s.worldFreezeAt = time.Now().UTC()
		s.worldFreezeTickID = tickID
		return state, nil
	}
	if s.worldFrozen {
		s.worldFrozen = false
		s.worldFreezeReason = ""
		s.worldFreezeAt = time.Time{}
		s.worldFreezeTickID = 0
	}
	return state, nil
}

func (s *Server) worldFrozenSnapshot() (bool, string) {
	s.worldTickMu.Lock()
	defer s.worldTickMu.Unlock()
	return s.worldFrozen, s.worldFreezeReason
}

func (s *Server) runWorldTickWithTrigger(ctx context.Context, triggerType string, replayOfTickID int64) int64 {
	triggerType = strings.TrimSpace(triggerType)
	if triggerType == "" {
		triggerType = "scheduled"
	}
	started := time.Now().UTC()
	s.worldTickMu.Lock()
	s.worldTickID++
	tickID := s.worldTickID
	s.worldTickMu.Unlock()

	var errs []string
	frozen := false
	freezeReason := ""
	appendStep := func(name, status, errText string, stepStarted time.Time) {
		_, _ = s.store.AppendWorldTickStep(ctx, store.WorldTickStepRecord{
			TickID:     tickID,
			StepName:   name,
			StartedAt:  stepStarted,
			DurationMS: time.Since(stepStarted).Milliseconds(),
			Status:     status,
			ErrorText:  errText,
		})
	}
	runStep := func(name string, fn func() error) {
		stepStarted := time.Now().UTC()
		status := "ok"
		var errText string
		if err := fn(); err != nil {
			status = "failed"
			errText = err.Error()
			errs = append(errs, name+":"+errText)
		}
		appendStep(name, status, errText, stepStarted)
	}
	appendSkipped := func(name, reason string) {
		appendStep(name, "skipped", reason, time.Now().UTC())
	}

	// If system was already frozen before this tick, re-evaluate first and skip work when freeze remains active.
	if preFrozen, _ := s.worldFrozenSnapshot(); preFrozen {
		runStep("extinction_guard_pre", func() error {
			state, err := s.applyExtinctionGuard(ctx, tickID)
			if err != nil {
				return err
			}
			frozen = state.Triggered
			freezeReason = state.TriggerReason
			return nil
		})
	}

	if frozen {
		runStep("min_population_revival", func() error {
			return s.runMinPopulationRevival(ctx, tickID)
		})
		appendSkipped("life_cost_drain", "world_frozen")
		appendSkipped("token_drain", "world_frozen")
		appendSkipped("dying_mark_check", "world_frozen")
		appendSkipped("life_state_transition", "world_frozen")
		appendSkipped("low_energy_alert", "world_frozen")
		appendSkipped("death_grace_check", "world_frozen")
		appendSkipped("mail_delivery", "world_frozen")
		appendSkipped("wake_lobsters_inbox_notice", "world_frozen")
		appendSkipped("autonomy_reminder", "world_frozen")
		appendSkipped("community_comm_reminder", "world_frozen")
		appendSkipped("agent_action_window", "world_frozen")
		appendSkipped("collect_outbox", "world_frozen")
		appendSkipped("repo_sync", "world_frozen")
		appendSkipped("kb_tick", "world_frozen")
		appendSkipped("ganglia_metabolism", "world_frozen")
		appendSkipped("npc_tick", "world_frozen")
		appendSkipped("metabolism_cycle", "world_frozen")
		appendSkipped("bounty_broker", "world_frozen")
		appendSkipped("cost_alert_notify", "world_frozen")
		appendSkipped("evolution_alert_notify", "world_frozen")
	} else {
		runStep("genesis_state_init", func() error {
			s.runGenesisBootstrapInit(ctx)
			return nil
		})
		runStep("life_cost_drain", func() error {
			return s.runTokenDrainTick(ctx, tickID)
		})
		runStep("token_drain", func() error { return nil })
		runStep("dying_mark_check", func() error {
			return s.runLifeStateTransitions(ctx, tickID)
		})
		runStep("life_state_transition", func() error { return nil })
		runStep("low_energy_alert", func() error {
			return s.runLowEnergyAlertTick(ctx, tickID)
		})
		runStep("death_grace_check", func() error { return nil })
		runStep("min_population_revival", func() error {
			return s.runMinPopulationRevival(ctx, tickID)
		})
		runStep("extinction_detection", func() error {
			state, err := s.applyExtinctionGuard(ctx, tickID)
			if err != nil {
				return err
			}
			frozen = state.Triggered
			freezeReason = state.TriggerReason
			return nil
		})
		runStep("extinction_guard_post", func() error { return nil })
		if frozen {
			appendSkipped("mail_delivery", "world_frozen")
			appendSkipped("wake_lobsters_inbox_notice", "world_frozen")
			appendSkipped("autonomy_reminder", "world_frozen")
			appendSkipped("community_comm_reminder", "world_frozen")
			appendSkipped("agent_action_window", "world_frozen")
			appendSkipped("collect_outbox", "world_frozen")
			appendSkipped("repo_sync", "world_frozen")
			appendSkipped("kb_tick", "world_frozen")
			appendSkipped("ganglia_metabolism", "world_frozen")
			appendSkipped("npc_tick", "world_frozen")
			appendSkipped("metabolism_cycle", "world_frozen")
			appendSkipped("bounty_broker", "world_frozen")
			appendSkipped("cost_alert_notify", "world_frozen")
			appendSkipped("evolution_alert_notify", "world_frozen")
		} else {
			runStep("mail_delivery", func() error {
				return s.runMailDeliveryTick(ctx, tickID)
			})
			runStep("wake_lobsters_inbox_notice", func() error {
				s.kbTick(ctx, tickID)
				return nil
			})
			runStep("autonomy_reminder", func() error {
				return s.runAutonomyReminderTick(ctx, tickID)
			})
			runStep("community_comm_reminder", func() error {
				return s.runCommunityCommReminderTick(ctx, tickID)
			})
			runStep("agent_action_window", func() error {
				return s.runAgentActionWindowTick(ctx, tickID)
			})
			runStep("collect_outbox", func() error {
				return s.runCollectOutboxTick(ctx, tickID)
			})
			runStep("repo_sync", func() error {
				return s.runRepoSyncTick(ctx, tickID)
			})
			runStep("kb_tick", func() error {
				return nil
			})
			runStep("ganglia_metabolism", func() error {
				_, err := s.runGangliaMetabolism(ctx)
				return err
			})
			runStep("npc_tick", func() error {
				return s.runNPCTick(ctx, tickID)
			})
			runStep("metabolism_cycle", func() error {
				_, err := s.runMetabolismCycle(ctx, tickID)
				return err
			})
			runStep("bounty_broker", func() error {
				_, err := s.runBountyBroker(ctx, tickID)
				return err
			})
			runStep("cost_alert_notify", func() error {
				return s.runWorldCostAlertNotifications(ctx, tickID)
			})
			runStep("evolution_alert_notify", func() error {
				return s.runWorldEvolutionAlertNotifications(ctx, tickID)
			})
		}
	}
	runStep("tick_event_log", func() error {
		return s.runTickEventLog(ctx, tickID, triggerType, frozen, freezeReason)
	})

	s.worldTickMu.Lock()
	s.worldTickAt = started
	s.worldTickDurMS = time.Since(started).Milliseconds()
	status := "ok"
	if frozen {
		status = "frozen"
	}
	if len(errs) == 0 {
		if frozen {
			s.worldTickErr = freezeReason
		} else {
			s.worldTickErr = ""
		}
	} else {
		joined := strings.Join(errs, " | ")
		if frozen && freezeReason != "" {
			s.worldTickErr = freezeReason + " | " + joined
		} else {
			s.worldTickErr = joined
		}
		if !frozen {
			status = "degraded"
		}
	}
	if status == "ok" && s.worldTickErr != "" {
		status = "degraded"
	}
	currentErr := s.worldTickErr
	currentDur := s.worldTickDurMS
	s.worldTickMu.Unlock()

	_, _ = s.store.AppendWorldTick(ctx, store.WorldTickRecord{
		TickID:         tickID,
		StartedAt:      started,
		DurationMS:     currentDur,
		TriggerType:    triggerType,
		ReplayOfTickID: replayOfTickID,
		Status:         status,
		ErrorText:      currentErr,
	})

	if currentErr == "" {
		log.Printf("world_tick tick=%d status=%s trigger=%s replay_of=%d duration_ms=%d", tickID, status, triggerType, replayOfTickID, currentDur)
		return tickID
	}
	log.Printf("world_tick tick=%d status=%s trigger=%s replay_of=%d duration_ms=%d err=%s", tickID, status, triggerType, replayOfTickID, currentDur, currentErr)
	return tickID
}

func (s *Server) initTianDao(ctx context.Context) error {
	lawKey := strings.TrimSpace(s.cfg.TianDaoLawKey)
	if lawKey == "" {
		lawKey = "genesis-v1"
	}
	version := s.cfg.TianDaoLawVersion
	if version <= 0 {
		version = 1
	}
	lifeCost := s.cfg.LifeCostPerTick
	if lifeCost <= 0 {
		lifeCost = tokenDrainPerTick
	}
	thinkRate := s.cfg.ThinkCostRateMilli
	if thinkRate <= 0 {
		thinkRate = 1000
	}
	commRate := s.cfg.CommCostRateMilli
	if commRate <= 0 {
		commRate = 1000
	}
	deathGrace := s.cfg.DeathGraceTicks
	if deathGrace <= 0 {
		deathGrace = 5
	}
	initialToken := s.cfg.InitialToken
	if initialToken <= 0 {
		initialToken = 1000
	}
	tickIntervalSec := s.cfg.TickIntervalSeconds
	if tickIntervalSec <= 0 {
		tickIntervalSec = 60
	}
	extinctionThreshold := s.cfg.ExtinctionThreshold
	if extinctionThreshold <= 0 {
		extinctionThreshold = 30
	}
	minPopulation := s.cfg.MinPopulation
	if minPopulation < 0 {
		minPopulation = 0
	}
	metabolismInterval := s.cfg.MetabolismInterval
	if metabolismInterval <= 0 {
		metabolismInterval = 60
	}
	manifest := tianDaoManifest{
		LawKey:                lawKey,
		Version:               version,
		LifeCostPerTick:       lifeCost,
		ThinkCostRateMilli:    thinkRate,
		CommCostRateMilli:     commRate,
		DeathGraceTicks:       deathGrace,
		InitialToken:          initialToken,
		TickIntervalSeconds:   tickIntervalSec,
		ExtinctionThresholdPC: extinctionThreshold,
		MinPopulation:         minPopulation,
		MetabolismInterval:    metabolismInterval,
	}
	if strings.TrimSpace(manifest.LawKey) == "" {
		return fmt.Errorf("tian dao law key is required")
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	item, err := s.store.EnsureTianDaoLaw(ctx, store.TianDaoLaw{
		LawKey:         manifest.LawKey,
		Version:        manifest.Version,
		ManifestJSON:   string(raw),
		ManifestSHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return err
	}
	s.tianDaoLaw = item
	return nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/api/mail/send", s.handleAPIMailSend)
	s.mux.HandleFunc("/api/mail/send-list", s.handleMailSendList)
	s.mux.HandleFunc("/api/mail/inbox", s.handleAPIMailInbox)
	s.mux.HandleFunc("/api/mail/list/create", s.handleMailListCreate)
	s.mux.HandleFunc("/api/mail/list/join", s.handleMailListJoin)
	s.mux.HandleFunc("/api/token/balance", s.handleAPITokenBalance)
	s.mux.HandleFunc("/api/token/transfer", s.handleAPITokenTransfer)
	s.mux.HandleFunc("/api/gov/propose", s.handleAPIGovPropose)
	s.mux.HandleFunc("/api/gov/vote", s.handleAPIGovVote)
	s.mux.HandleFunc("/api/gov/cosign", s.handleAPIGovCosign)
	s.mux.HandleFunc("/api/gov/report", s.handleAPIGovReport)
	s.mux.HandleFunc("/api/gov/laws", s.handleAPIGovLaws)
	s.mux.HandleFunc("/api/tools/invoke", s.handleAPIToolsInvoke)
	s.mux.HandleFunc("/api/tools/register", s.handleAPIToolsRegister)
	s.mux.HandleFunc("/api/tools/search", s.handleAPIToolsSearch)
	s.mux.HandleFunc("/api/library/publish", s.handleAPILibraryPublish)
	s.mux.HandleFunc("/api/library/search", s.handleAPILibrarySearch)
	s.mux.HandleFunc("/api/life/set-will", s.handleAPILifeSetWill)
	s.mux.HandleFunc("/api/life/metamorphose", s.handleAPILifeMetamorphose)
	s.mux.HandleFunc("/api/life/hibernate", s.handleAPILifeHibernate)
	s.mux.HandleFunc("/api/life/wake", s.handleAPILifeWake)
	s.mux.HandleFunc("/api/ganglia/forge", s.handleAPIGangliaForge)
	s.mux.HandleFunc("/api/ganglia/browse", s.handleAPIGangliaBrowse)
	s.mux.HandleFunc("/api/ganglia/integrate", s.handleAPIGangliaIntegrate)
	s.mux.HandleFunc("/api/ganglia/rate", s.handleAPIGangliaRate)
	s.mux.HandleFunc("/api/bounty/post", s.handleAPIBountyPost)
	s.mux.HandleFunc("/api/bounty/list", s.handleAPIBountyList)
	s.mux.HandleFunc("/api/bounty/verify", s.handleAPIBountyVerify)
	s.mux.HandleFunc("/api/metabolism/score", s.handleMetabolismScore)
	s.mux.HandleFunc("/api/metabolism/supersede", s.handleMetabolismSupersede)
	s.mux.HandleFunc("/api/metabolism/dispute", s.handleMetabolismDispute)
	s.mux.HandleFunc("/api/metabolism/report", s.handleMetabolismReport)
	s.mux.HandleFunc("/api/colony/status", s.handleAPIColonyStatus)
	s.mux.HandleFunc("/api/colony/directory", s.handleAPIColonyDirectory)
	s.mux.HandleFunc("/api/colony/chronicle", s.handleAPIColonyChronicle)
	s.mux.HandleFunc("/api/colony/banished", s.handleAPIColonyBanished)
	s.mux.HandleFunc("/v1/meta", s.handleMeta)
	s.mux.HandleFunc("/v1/internal/users/sync", s.handleInternalUserSync)
	s.mux.HandleFunc("/v1/tian-dao/law", s.handleTianDaoLaw)
	s.mux.HandleFunc("/v1/world/tick/status", s.handleWorldTickStatus)
	s.mux.HandleFunc("/v1/world/freeze/status", s.handleWorldFreezeStatus)
	s.mux.HandleFunc("/v1/world/freeze/rescue", s.handleWorldFreezeRescue)
	s.mux.HandleFunc("/v1/world/tick/history", s.handleWorldTickHistory)
	s.mux.HandleFunc("/v1/world/tick/chain/verify", s.handleWorldTickChainVerify)
	s.mux.HandleFunc("/v1/world/tick/replay", s.handleWorldTickReplay)
	s.mux.HandleFunc("/v1/world/tick/steps", s.handleWorldTickSteps)
	s.mux.HandleFunc("/v1/world/life-state", s.handleWorldLifeState)
	s.mux.HandleFunc("/v1/world/cost-events", s.handleWorldCostEvents)
	s.mux.HandleFunc("/v1/world/cost-summary", s.handleWorldCostSummary)
	s.mux.HandleFunc("/v1/world/tool-audit", s.handleWorldToolAudit)
	s.mux.HandleFunc("/v1/world/cost-alerts", s.handleWorldCostAlerts)
	s.mux.HandleFunc("/v1/world/cost-alert-settings", s.handleWorldCostAlertSettings)
	s.mux.HandleFunc("/v1/world/cost-alert-settings/upsert", s.handleWorldCostAlertSettingsUpsert)
	s.mux.HandleFunc("/v1/runtime/scheduler-settings", s.handleRuntimeSchedulerSettings)
	s.mux.HandleFunc("/v1/runtime/scheduler-settings/upsert", s.handleRuntimeSchedulerSettingsUpsert)
	s.mux.HandleFunc("/v1/world/cost-alert-notifications", s.handleWorldCostAlertNotifications)
	s.mux.HandleFunc("/v1/world/evolution-score", s.handleWorldEvolutionScore)
	s.mux.HandleFunc("/v1/world/evolution-alerts", s.handleWorldEvolutionAlerts)
	s.mux.HandleFunc("/v1/world/evolution-alert-settings", s.handleWorldEvolutionAlertSettings)
	s.mux.HandleFunc("/v1/world/evolution-alert-settings/upsert", s.handleWorldEvolutionAlertSettingsUpsert)
	s.mux.HandleFunc("/v1/world/evolution-alert-notifications", s.handleWorldEvolutionAlertNotifications)
	s.mux.HandleFunc("/v1/bots", s.handleBots)
	s.mux.HandleFunc("/v1/bots/nickname/upsert", s.handleBotNicknameUpsert)
	s.mux.HandleFunc("/v1/bots/profile/readme", s.handleBotProfileReadme)
	s.mux.HandleFunc("/v1/prompts/templates", s.handlePromptTemplates)
	s.mux.HandleFunc("/v1/prompts/templates/upsert", s.handlePromptTemplateUpsert)
	s.mux.HandleFunc("/v1/prompts/templates/apply", s.handlePromptTemplateApply)
	s.mux.HandleFunc("/v1/bots/thoughts", s.handleBotThoughts)
	s.mux.HandleFunc("/v1/bots/logs", s.handleBotLogs)
	s.mux.HandleFunc("/v1/bots/logs/all", s.handleAllBotLogs)
	s.mux.HandleFunc("/v1/bots/rule-status", s.handleBotRuleStatus)
	s.mux.HandleFunc("/v1/policy/mission", s.handleMissionPolicy)
	s.mux.HandleFunc("/v1/policy/mission/default", s.handleMissionDefault)
	s.mux.HandleFunc("/v1/policy/mission/room", s.handleMissionRoom)
	s.mux.HandleFunc("/v1/policy/mission/bot", s.handleMissionBot)
	s.mux.HandleFunc("/v1/token/accounts", s.handleTokenAccounts)
	s.mux.HandleFunc("/v1/token/balance", s.handleTokenBalance)
	s.mux.HandleFunc("/v1/token/consume", s.handleTokenConsume)
	s.mux.HandleFunc("/v1/token/history", s.handleTokenHistory)
	s.mux.HandleFunc("/v1/mail/send", s.handleMailSend)
	s.mux.HandleFunc("/v1/mail/send-list", s.handleMailSendList)
	s.mux.HandleFunc("/v1/mail/inbox", s.handleMailInbox)
	s.mux.HandleFunc("/v1/mail/outbox", s.handleMailOutbox)
	s.mux.HandleFunc("/v1/mail/mark-read", s.handleMailMarkRead)
	s.mux.HandleFunc("/v1/mail/mark-read-query", s.handleMailMarkReadQuery)
	s.mux.HandleFunc("/v1/mail/reminders", s.handleMailReminders)
	s.mux.HandleFunc("/v1/mail/reminders/resolve", s.handleMailRemindersResolve)
	s.mux.HandleFunc("/v1/mail/contacts", s.handleMailContacts)
	s.mux.HandleFunc("/v1/mail/contacts/upsert", s.handleMailContactsUpsert)
	s.mux.HandleFunc("/v1/mail/overview", s.handleMailOverview)
	s.mux.HandleFunc("/v1/mail/lists", s.handleMailLists)
	s.mux.HandleFunc("/v1/mail/lists/create", s.handleMailListCreate)
	s.mux.HandleFunc("/v1/mail/lists/join", s.handleMailListJoin)
	s.mux.HandleFunc("/v1/mail/lists/leave", s.handleMailListLeave)
	s.mux.HandleFunc("/v1/token/transfer", s.handleTokenTransfer)
	s.mux.HandleFunc("/v1/token/tip", s.handleTokenTip)
	s.mux.HandleFunc("/v1/token/wishes", s.handleTokenWishes)
	s.mux.HandleFunc("/v1/token/wish/create", s.handleTokenWishCreate)
	s.mux.HandleFunc("/v1/token/wish/fulfill", s.handleTokenWishFulfill)
	s.mux.HandleFunc("/v1/life/hibernate", s.handleLifeHibernate)
	s.mux.HandleFunc("/v1/life/wake", s.handleLifeWake)
	s.mux.HandleFunc("/v1/life/set-will", s.handleLifeSetWill)
	s.mux.HandleFunc("/v1/life/will", s.handleLifeWill)
	s.mux.HandleFunc("/v1/genesis/state", s.handleGenesisState)
	s.mux.HandleFunc("/v1/genesis/bootstrap/start", s.handleGenesisBootstrapStart)
	s.mux.HandleFunc("/v1/genesis/bootstrap/seal", s.handleGenesisBootstrapSeal)
	s.mux.HandleFunc("/v1/clawcolony/state", s.handleGenesisState)
	s.mux.HandleFunc("/v1/clawcolony/bootstrap/start", s.handleGenesisBootstrapStart)
	s.mux.HandleFunc("/v1/clawcolony/bootstrap/seal", s.handleGenesisBootstrapSeal)
	s.mux.HandleFunc("/v1/tools/register", s.handleToolRegister)
	s.mux.HandleFunc("/v1/tools/review", s.handleToolReview)
	s.mux.HandleFunc("/v1/tools/search", s.handleToolSearch)
	s.mux.HandleFunc("/v1/tools/invoke", s.handleToolInvoke)
	s.mux.HandleFunc("/v1/npc/list", s.handleNPCList)
	s.mux.HandleFunc("/v1/npc/tasks", s.handleNPCTasks)
	s.mux.HandleFunc("/v1/npc/tasks/create", s.handleNPCTaskCreate)
	s.mux.HandleFunc("/v1/metabolism/score", s.handleMetabolismScore)
	s.mux.HandleFunc("/v1/metabolism/supersede", s.handleMetabolismSupersede)
	s.mux.HandleFunc("/v1/metabolism/dispute", s.handleMetabolismDispute)
	s.mux.HandleFunc("/v1/metabolism/report", s.handleMetabolismReport)
	s.mux.HandleFunc("/v1/bounty/post", s.handleBountyPost)
	s.mux.HandleFunc("/v1/bounty/list", s.handleBountyList)
	s.mux.HandleFunc("/v1/bounty/claim", s.handleBountyClaim)
	s.mux.HandleFunc("/v1/bounty/verify", s.handleBountyVerify)
	s.mux.HandleFunc("/v1/collab/propose", s.handleCollabPropose)
	s.mux.HandleFunc("/v1/collab/list", s.handleCollabList)
	s.mux.HandleFunc("/v1/collab/get", s.handleCollabGet)
	s.mux.HandleFunc("/v1/collab/apply", s.handleCollabApply)
	s.mux.HandleFunc("/v1/collab/assign", s.handleCollabAssign)
	s.mux.HandleFunc("/v1/collab/start", s.handleCollabStart)
	s.mux.HandleFunc("/v1/collab/submit", s.handleCollabSubmit)
	s.mux.HandleFunc("/v1/collab/review", s.handleCollabReview)
	s.mux.HandleFunc("/v1/collab/close", s.handleCollabClose)
	s.mux.HandleFunc("/v1/collab/participants", s.handleCollabParticipants)
	s.mux.HandleFunc("/v1/collab/artifacts", s.handleCollabArtifacts)
	s.mux.HandleFunc("/v1/collab/events", s.handleCollabEvents)
	s.mux.HandleFunc("/v1/kb/entries", s.handleKBEntries)
	s.mux.HandleFunc("/v1/kb/sections", s.handleKBSections)
	s.mux.HandleFunc("/v1/kb/entries/history", s.handleKBEntryHistory)
	s.mux.HandleFunc("/v1/kb/proposals", s.handleKBProposals)
	s.mux.HandleFunc("/v1/kb/proposals/get", s.handleKBProposalGet)
	s.mux.HandleFunc("/v1/kb/proposals/enroll", s.handleKBProposalEnroll)
	s.mux.HandleFunc("/v1/kb/proposals/revisions", s.handleKBProposalRevisions)
	s.mux.HandleFunc("/v1/kb/proposals/revise", s.handleKBProposalRevise)
	s.mux.HandleFunc("/v1/kb/proposals/ack", s.handleKBProposalAck)
	s.mux.HandleFunc("/v1/kb/proposals/comment", s.handleKBProposalComment)
	s.mux.HandleFunc("/v1/kb/proposals/thread", s.handleKBProposalThread)
	s.mux.HandleFunc("/v1/kb/proposals/start-vote", s.handleKBProposalStartVote)
	s.mux.HandleFunc("/v1/kb/proposals/vote", s.handleKBProposalVote)
	s.mux.HandleFunc("/v1/kb/proposals/apply", s.handleKBProposalApply)
	s.mux.HandleFunc("/v1/ganglia/forge", s.handleGangliaForge)
	s.mux.HandleFunc("/v1/ganglia/browse", s.handleGangliaBrowse)
	s.mux.HandleFunc("/v1/ganglia/get", s.handleGangliaGet)
	s.mux.HandleFunc("/v1/ganglia/integrate", s.handleGangliaIntegrate)
	s.mux.HandleFunc("/v1/ganglia/rate", s.handleGangliaRate)
	s.mux.HandleFunc("/v1/ganglia/integrations", s.handleGangliaIntegrations)
	s.mux.HandleFunc("/v1/ganglia/ratings", s.handleGangliaRatings)
	s.mux.HandleFunc("/v1/ganglia/protocol", s.handleGangliaProtocol)
	s.mux.HandleFunc("/v1/governance/docs", s.handleGovernanceDocs)
	s.mux.HandleFunc("/v1/governance/proposals", s.handleGovernanceProposals)
	s.mux.HandleFunc("/v1/governance/overview", s.handleGovernanceOverview)
	s.mux.HandleFunc("/v1/governance/protocol", s.handleGovernanceProtocol)
	s.mux.HandleFunc("/v1/governance/report", s.handleGovernanceReportCreate)
	s.mux.HandleFunc("/v1/governance/reports", s.handleGovernanceReports)
	s.mux.HandleFunc("/v1/governance/cases/open", s.handleGovernanceCaseOpen)
	s.mux.HandleFunc("/v1/governance/cases", s.handleGovernanceCases)
	s.mux.HandleFunc("/v1/governance/cases/verdict", s.handleGovernanceCaseVerdict)
	s.mux.HandleFunc("/v1/reputation/score", s.handleReputationScore)
	s.mux.HandleFunc("/v1/reputation/leaderboard", s.handleReputationLeaderboard)
	s.mux.HandleFunc("/v1/reputation/events", s.handleReputationEvents)
	s.mux.HandleFunc("/v1/chat/send", s.handleChatSend)
	s.mux.HandleFunc("/v1/chat/history", s.handleChatHistory)
	s.mux.HandleFunc("/v1/chat/stream", s.handleChatStream)
	s.mux.HandleFunc("/v1/chat/state", s.handleChatState)
	s.mux.HandleFunc("/v1/monitor/agents/overview", s.handleMonitorAgentsOverview)
	s.mux.HandleFunc("/v1/monitor/agents/timeline", s.handleMonitorAgentsTimeline)
	s.mux.HandleFunc("/v1/monitor/agents/timeline/all", s.handleMonitorAgentsTimelineAll)
	s.mux.HandleFunc("/v1/monitor/meta", s.handleMonitorMeta)
	s.mux.HandleFunc("/v1/bots/dev/link", s.handleBotDevLinkProxy)
	s.mux.HandleFunc("/v1/bots/dev/health", s.handleBotDevHealth)
	s.mux.HandleFunc("/v1/bots/dev/", s.handleBotDevProxyForward)
	s.mux.HandleFunc("/v1/bots/openclaw/", s.handleOpenClawProxy)
	s.mux.HandleFunc("/v1/bots/openclaw/status", s.handleOpenClawStatus)
	s.mux.HandleFunc("/v1/system/request-logs", s.handleRequestLogs)
	s.mux.HandleFunc("/v1/system/openclaw-dashboard-config", s.handleOpenClawDashboardConfig)
	s.mux.HandleFunc("/v1/tasks/pi", s.handlePiTaskMeta)
	s.mux.HandleFunc("/v1/tasks/pi/claim", s.handlePiTaskClaim)
	s.mux.HandleFunc("/v1/tasks/pi/submit", s.handlePiTaskSubmit)
	s.mux.HandleFunc("/v1/tasks/pi/history", s.handlePiTaskHistory)
	s.mux.HandleFunc("/dashboard", s.handleDashboard)
	s.mux.HandleFunc("/dashboard/", s.handleDashboard)
	s.mux.HandleFunc("/", s.handleNotFound)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.tianDaoInitErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "degraded",
			"time":   time.Now().UTC().Format(time.RFC3339),
			"error":  s.tianDaoInitErr.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	lawKey := strings.TrimSpace(s.cfg.TianDaoLawKey)
	if lawKey == "" {
		lawKey = s.tianDaoLaw.LawKey
	}
	lawVersion := s.cfg.TianDaoLawVersion
	if lawVersion <= 0 {
		lawVersion = s.tianDaoLaw.Version
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":              "clawcolony",
		"service_role":         s.cfg.EffectiveServiceRole(),
		"runtime_enabled":      s.cfg.RuntimeEnabled(),
		"clawcolony_namespace": s.cfg.ClawWorldNamespace,
		"bot_namespace":        s.cfg.BotNamespace,
		"database_enabled":     s.cfg.DatabaseURL != "",
		"action_cost_consume":  s.cfg.ActionCostConsume,
		"tool_cost_rate_milli": s.cfg.ToolCostRateMilli,
		"tool_runtime_exec":    s.cfg.ToolRuntimeExec,
		"tool_sandbox_image":   strings.TrimSpace(s.cfg.ToolSandboxImage),
		"tool_t3_allow_hosts":  strings.TrimSpace(s.cfg.ToolT3AllowHosts),
		"tian_dao_law_key":     lawKey,
		"tian_dao_law_version": lawVersion,
		"world_tick_seconds":   int64(s.worldTickInterval() / time.Second),
	})
}

func (s *Server) handleTianDaoLaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	lawKey := strings.TrimSpace(s.cfg.TianDaoLawKey)
	if lawKey == "" {
		lawKey = s.tianDaoLaw.LawKey
	}
	item, err := s.store.GetTianDaoLaw(r.Context(), lawKey)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var manifest map[string]any
	_ = json.Unmarshal([]byte(item.ManifestJSON), &manifest)
	writeJSON(w, http.StatusOK, map[string]any{
		"item":     item,
		"manifest": manifest,
	})
}

func (s *Server) handleWorldTickStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.worldTickMu.Lock()
	defer s.worldTickMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"tick_id":              s.worldTickID,
		"last_tick_at":         s.worldTickAt,
		"last_duration_ms":     s.worldTickDurMS,
		"last_error":           s.worldTickErr,
		"tick_interval_sec":    int64(s.worldTickInterval() / time.Second),
		"action_cost_consume":  s.cfg.ActionCostConsume,
		"tian_dao_law_key":     s.tianDaoLaw.LawKey,
		"tian_dao_law_version": s.tianDaoLaw.Version,
		"tian_dao_law_sha256":  s.tianDaoLaw.ManifestSHA256,
		"tian_dao_law_updated": s.tianDaoLaw.CreatedAt,
		"frozen":               s.worldFrozen,
		"freeze_reason":        s.worldFreezeReason,
		"freeze_since":         s.worldFreezeAt,
		"freeze_tick_id":       s.worldFreezeTickID,
		"freeze_total_users":   s.worldFreezeTotal,
		"freeze_at_risk_users": s.worldFreezeAtRisk,
		"freeze_threshold_pct": s.worldFreezeThreshold,
	})
}

func (s *Server) handleWorldFreezeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.worldTickMu.Lock()
	defer s.worldTickMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"frozen":               s.worldFrozen,
		"freeze_reason":        s.worldFreezeReason,
		"freeze_since":         s.worldFreezeAt,
		"freeze_tick_id":       s.worldFreezeTickID,
		"freeze_total_users":   s.worldFreezeTotal,
		"freeze_at_risk_users": s.worldFreezeAtRisk,
		"freeze_threshold_pct": s.worldFreezeThreshold,
		"tick_id":              s.worldTickID,
		"last_tick_at":         s.worldTickAt,
	})
}

const (
	worldFreezeRescueModeAtRisk   = "at_risk"
	worldFreezeRescueModeSelected = "selected"
	worldFreezeRescueMaxUsers     = 500
	worldFreezeRescueMaxAmount    = int64(1_000_000_000)
)

type worldFreezeRescueRequest struct {
	Mode    string   `json:"mode"`
	Amount  int64    `json:"amount"`
	UserIDs []string `json:"user_ids"`
	DryRun  bool     `json:"dry_run"`
}

type worldFreezeRescueResultItem struct {
	UserID         string `json:"user_id"`
	BalanceBefore  int64  `json:"balance_before"`
	BalanceAfter   int64  `json:"balance_after"`
	RechargeAmount int64  `json:"recharge_amount"`
	Applied        bool   `json:"applied"`
	Error          string `json:"error,omitempty"`
}

func normalizeWorldFreezeRescueMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case worldFreezeRescueModeAtRisk:
		return worldFreezeRescueModeAtRisk
	case worldFreezeRescueModeSelected:
		return worldFreezeRescueModeSelected
	default:
		return ""
	}
}

func normalizeDistinctUserIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, it := range raw {
		uid := strings.TrimSpace(it)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		out = append(out, uid)
	}
	sort.Strings(out)
	return out
}

func isLoopbackRemoteAddr(remoteAddr string) bool {
	raw := strings.TrimSpace(remoteAddr)
	if raw == "" {
		return false
	}
	host := raw
	if h, _, err := net.SplitHostPort(raw); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func safeInt64Add(a, b int64) (int64, bool) {
	if b > 0 && a > (math.MaxInt64-b) {
		return 0, false
	}
	if b < 0 && a < (math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
}

func (s *Server) handleWorldFreezeRescue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isLoopbackRemoteAddr(r.RemoteAddr) {
		expected := strings.TrimSpace(s.cfg.InternalSyncToken)
		got := internalSyncTokenFromRequest(r)
		if expected == "" {
			writeError(w, http.StatusUnauthorized, "non-loopback requests require internal sync token configuration")
			return
		}
		if got == "" || got != expected {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
	}
	var req worldFreezeRescueRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	mode := normalizeWorldFreezeRescueMode(req.Mode)
	if mode == "" {
		if strings.TrimSpace(req.Mode) == "" {
			mode = worldFreezeRescueModeAtRisk
		} else {
			writeError(w, http.StatusBadRequest, "mode must be one of: at_risk, selected")
			return
		}
	}
	if req.Amount <= 0 || req.Amount > worldFreezeRescueMaxAmount {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("amount must be in [1, %d]", worldFreezeRescueMaxAmount))
		return
	}

	selectedUserIDs := normalizeDistinctUserIDs(req.UserIDs)
	if mode == worldFreezeRescueModeSelected && len(selectedUserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "user_ids is required when mode=selected")
		return
	}
	if len(selectedUserIDs) > worldFreezeRescueMaxUsers {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("user_ids exceeds max users: %d", worldFreezeRescueMaxUsers))
		return
	}
	for _, uid := range selectedUserIDs {
		if uid == clawWorldSystemID {
			writeError(w, http.StatusBadRequest, "claw-world-system cannot be rescued")
			return
		}
	}

	accounts, err := s.store.ListTokenAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	balanceByUser := make(map[string]int64, len(accounts))
	totalUsers := 0
	atRiskBefore := 0
	for _, it := range accounts {
		uid := strings.TrimSpace(it.BotID)
		if uid == "" || uid == clawWorldSystemID {
			continue
		}
		balanceByUser[uid] = it.Balance
		totalUsers++
		if it.Balance <= 0 {
			atRiskBefore++
		}
	}

	targetUsers := make([]string, 0)
	unknownUserIDs := make([]string, 0)
	truncatedUsers := 0
	if mode == worldFreezeRescueModeAtRisk {
		for uid, bal := range balanceByUser {
			if bal <= 0 {
				targetUsers = append(targetUsers, uid)
			}
		}
		sort.Strings(targetUsers)
		if len(targetUsers) > worldFreezeRescueMaxUsers {
			truncatedUsers = len(targetUsers) - worldFreezeRescueMaxUsers
			targetUsers = targetUsers[:worldFreezeRescueMaxUsers]
		}
	} else {
		targetUsers = make([]string, 0, len(selectedUserIDs))
		for _, uid := range selectedUserIDs {
			if _, ok := balanceByUser[uid]; !ok {
				unknownUserIDs = append(unknownUserIDs, uid)
				continue
			}
			targetUsers = append(targetUsers, uid)
		}
		if len(unknownUserIDs) > 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("some user_ids are not found in token accounts: %s", strings.Join(unknownUserIDs, ",")))
			return
		}
	}

	if len(targetUsers) == 0 {
		writeError(w, http.StatusBadRequest, "no target users matched current rescue mode")
		return
	}
	for _, uid := range targetUsers {
		if uid == clawWorldSystemID {
			writeError(w, http.StatusBadRequest, "claw-world-system cannot be rescued")
			return
		}
	}
	if mode == worldFreezeRescueModeSelected && len(targetUsers) > worldFreezeRescueMaxUsers {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("target users exceeds max users: %d", worldFreezeRescueMaxUsers))
		return
	}

	simulatedBalances := make(map[string]int64, len(balanceByUser))
	for uid, bal := range balanceByUser {
		simulatedBalances[uid] = bal
	}
	items := make([]worldFreezeRescueResultItem, 0, len(targetUsers))
	appliedUsers := 0
	evalErr := ""
	for _, uid := range targetUsers {
		before := simulatedBalances[uid]
		item := worldFreezeRescueResultItem{
			UserID:         uid,
			BalanceBefore:  before,
			BalanceAfter:   before,
			RechargeAmount: req.Amount,
			Applied:        false,
		}
		if req.DryRun {
			after, ok := safeInt64Add(before, req.Amount)
			if !ok {
				item.Error = "balance overflow in dry_run simulation"
				items = append(items, item)
				continue
			}
			item.BalanceAfter = after
			simulatedBalances[uid] = item.BalanceAfter
			items = append(items, item)
			continue
		}
		if err := s.ensureUserAlive(r.Context(), uid); err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}
		ledger, err := s.store.Recharge(r.Context(), uid, req.Amount)
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}
		item.BalanceAfter = ledger.BalanceAfter
		item.RechargeAmount = ledger.Amount
		item.Applied = true
		appliedUsers++
		simulatedBalances[uid] = ledger.BalanceAfter
		items = append(items, item)
	}

	atRiskAfter := 0
	totalUsersAfter := totalUsers
	if req.DryRun {
		for _, bal := range simulatedBalances {
			if bal <= 0 {
				atRiskAfter++
			}
		}
	} else {
		afterAccounts, err := s.store.ListTokenAccounts(r.Context())
		if err != nil {
			evalErr = strings.TrimSpace(err.Error())
			atRiskAfter = 0
			totalUsersAfter = 0
			for _, bal := range simulatedBalances {
				totalUsersAfter++
				if bal <= 0 {
					atRiskAfter++
				}
			}
		} else {
			atRiskAfter = 0
			totalUsersAfter = 0
			for _, it := range afterAccounts {
				uid := strings.TrimSpace(it.BotID)
				if uid == "" || uid == clawWorldSystemID {
					continue
				}
				totalUsersAfter++
				if it.Balance <= 0 {
					atRiskAfter++
				}
			}
		}
	}
	threshold := s.currentExtinctionThresholdPct()
	triggeredBefore := totalUsers > 0 && atRiskBefore*100 >= totalUsers*threshold
	triggeredAfter := totalUsersAfter > 0 && atRiskAfter*100 >= totalUsersAfter*threshold
	if !req.DryRun {
		s.worldTickMu.Lock()
		tickID := s.worldTickID
		s.worldTickMu.Unlock()
		if _, err := s.applyExtinctionGuard(r.Context(), tickID); err != nil {
			if strings.TrimSpace(evalErr) != "" {
				evalErr = evalErr + " | " + err.Error()
			} else {
				evalErr = err.Error()
			}
		}
	}

	s.worldTickMu.Lock()
	worldFrozen := s.worldFrozen
	worldFreezeReason := s.worldFreezeReason
	worldFreezeTickID := s.worldFreezeTickID
	worldTickID := s.worldTickID
	s.worldTickMu.Unlock()
	failedUsers := 0
	for _, it := range items {
		if strings.TrimSpace(it.Error) != "" {
			failedUsers++
		}
	}
	simulatedUsers := 0
	if req.DryRun {
		simulatedUsers = len(targetUsers) - failedUsers
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":                mode,
		"dry_run":             req.DryRun,
		"amount_per_user":     req.Amount,
		"targeted_users":      len(targetUsers),
		"truncated_users":     truncatedUsers,
		"applied_users":       appliedUsers,
		"simulated_users":     simulatedUsers,
		"failed_users":        failedUsers,
		"total_users":         totalUsers,
		"total_users_after":   totalUsersAfter,
		"threshold_pct":       threshold,
		"before":              map[string]any{"at_risk_users": atRiskBefore, "triggered": triggeredBefore},
		"after_estimate":      map[string]any{"at_risk_users": atRiskAfter, "triggered": triggeredAfter},
		"world_frozen":        worldFrozen,
		"world_tick_id":       worldTickID,
		"world_freeze_tick":   worldFreezeTickID,
		"world_freeze_reason": worldFreezeReason,
		"eval_error":          evalErr,
		"items":               items,
	})
}

func (s *Server) handleWorldTickHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListWorldTicks(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleWorldTickChainVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 500)
	items, err := s.store.ListWorldTicks(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(items) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"checked":     0,
			"head_tick":   int64(0),
			"head_hash":   "",
			"legacy_fill": 0,
		})
		return
	}
	// ListWorldTicks returns newest first; chain verification must be oldest -> newest.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	prevHash := ""
	legacyFill := 0
	for idx, it := range items {
		storedPrev := strings.TrimSpace(it.PrevHash)
		if storedPrev == "" && prevHash != "" {
			storedPrev = prevHash
			legacyFill++
		}
		if storedPrev != prevHash {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":             false,
				"checked":        idx,
				"head_tick":      items[len(items)-1].TickID,
				"head_hash":      strings.TrimSpace(items[len(items)-1].EntryHash),
				"legacy_fill":    legacyFill,
				"mismatch_tick":  it.TickID,
				"mismatch_field": "prev_hash",
				"expected":       prevHash,
				"actual":         storedPrev,
			})
			return
		}
		expectedHash := store.ComputeWorldTickHash(it, storedPrev)
		storedHash := strings.TrimSpace(it.EntryHash)
		if storedHash == "" {
			storedHash = expectedHash
			legacyFill++
		}
		if storedHash != expectedHash {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":             false,
				"checked":        idx,
				"head_tick":      items[len(items)-1].TickID,
				"head_hash":      strings.TrimSpace(items[len(items)-1].EntryHash),
				"legacy_fill":    legacyFill,
				"mismatch_tick":  it.TickID,
				"mismatch_field": "entry_hash",
				"expected":       expectedHash,
				"actual":         storedHash,
			})
			return
		}
		prevHash = storedHash
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"checked":     len(items),
		"head_tick":   items[len(items)-1].TickID,
		"head_hash":   prevHash,
		"legacy_fill": legacyFill,
	})
}

type worldTickReplayRequest struct {
	SourceTickID int64 `json:"source_tick_id"`
}

func (s *Server) handleWorldTickReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req worldTickReplayRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.SourceTickID <= 0 {
		req.SourceTickID = parseInt64(r.URL.Query().Get("source_tick_id"))
	}
	if req.SourceTickID <= 0 {
		s.worldTickMu.Lock()
		req.SourceTickID = s.worldTickID
		s.worldTickMu.Unlock()
	}
	if req.SourceTickID <= 0 {
		writeError(w, http.StatusBadRequest, "source_tick_id is required")
		return
	}
	replayTickID := s.runWorldTickReplay(r.Context(), req.SourceTickID)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":         "accepted",
		"source_tick_id": req.SourceTickID,
		"replay_tick_id": replayTickID,
	})
}

func (s *Server) handleWorldTickSteps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tickID := parseInt64(r.URL.Query().Get("tick_id"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListWorldTickSteps(r.Context(), tickID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tick_id": tickID,
		"items":   items,
	})
}

func (s *Server) handleWorldLifeState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	state := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("state")))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListUserLifeStates(r.Context(), userID, state, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"state":   state,
		"items":   items,
	})
}

func (s *Server) handleWorldCostEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	tickID := parseInt64(r.URL.Query().Get("tick_id"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	if tickID > 0 && strings.TrimSpace(r.URL.Query().Get("limit")) == "" {
		// Replay queries usually need a broader scan window when tick filter is active.
		limit = 2000
	}
	items, err := s.store.ListCostEvents(r.Context(), userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tickID > 0 {
		filtered := make([]store.CostEvent, 0, len(items))
		for _, it := range items {
			if it.TickID == tickID {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"tick_id": tickID,
		"items":   items,
	})
}

func (s *Server) handleWorldCostSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 500)
	items, err := s.store.ListCostEvents(r.Context(), userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type agg struct {
		Count  int64 `json:"count"`
		Amount int64 `json:"amount"`
		Units  int64 `json:"units"`
	}
	byType := map[string]agg{}
	var totalCount, totalAmount, totalUnits int64
	for _, it := range items {
		key := strings.TrimSpace(it.CostType)
		if key == "" {
			key = "unknown"
		}
		a := byType[key]
		a.Count++
		a.Amount += it.Amount
		a.Units += it.Units
		byType[key] = a
		totalCount++
		totalAmount += it.Amount
		totalUnits += it.Units
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"limit":   limit,
		"totals": map[string]any{
			"count":  totalCount,
			"amount": totalAmount,
			"units":  totalUnits,
		},
		"by_type": byType,
	})
}

func toolTier(costType string) string {
	ct := strings.TrimSpace(strings.ToLower(costType))
	switch ct {
	case "tool.bot.upgrade":
		return "T3"
	case "tool.runtime.t3":
		return "T3"
	case "tool.openclaw.register", "tool.openclaw.redeploy", "tool.openclaw.delete":
		return "T2"
	case "tool.runtime.t2":
		return "T2"
	case "tool.openclaw.restart":
		return "T1"
	case "tool.runtime.t1":
		return "T1"
	case "tool.runtime.t0":
		return "T0"
	default:
		return "T0"
	}
}

func toolTierLevel(tier string) int {
	switch strings.TrimSpace(strings.ToUpper(tier)) {
	case "T0":
		return 0
	case "T1":
		return 1
	case "T2":
		return 2
	case "T3":
		return 3
	default:
		return 0
	}
}

func maxAllowedToolTierForLifeState(state string) string {
	switch normalizeLifeStateForServer(state) {
	case "alive":
		return "T3"
	case "dying":
		return "T1"
	case "hibernated":
		return "NONE"
	case "dead":
		return "NONE"
	default:
		return "T3"
	}
}

func isToolTierAllowedForLifeState(state, tier string) bool {
	maxTier := maxAllowedToolTierForLifeState(state)
	if maxTier == "NONE" {
		return false
	}
	return toolTierLevel(tier) <= toolTierLevel(maxTier)
}

func (s *Server) ensureToolTierAllowed(ctx context.Context, userID, costType string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == clawWorldSystemID {
		return nil
	}
	state := "alive"
	if life, err := s.store.GetUserLifeState(ctx, userID); err == nil {
		state = normalizeLifeStateForServer(life.State)
	}
	tier := toolTier(costType)
	if isToolTierAllowedForLifeState(state, tier) {
		return nil
	}
	maxTier := maxAllowedToolTierForLifeState(state)
	if maxTier == "NONE" {
		return fmt.Errorf("tool tier %s is not allowed in %s state", tier, state)
	}
	return fmt.Errorf("tool tier %s is not allowed in %s state (max allowed: %s)", tier, state, maxTier)
}

func (s *Server) handleWorldToolAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 500)
	tierFilter := strings.TrimSpace(strings.ToUpper(r.URL.Query().Get("tier")))
	if tierFilter != "" && tierFilter != "T0" && tierFilter != "T1" && tierFilter != "T2" && tierFilter != "T3" {
		writeError(w, http.StatusBadRequest, "tier must be T0|T1|T2|T3")
		return
	}
	items, err := s.store.ListCostEvents(r.Context(), userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type toolAuditItem struct {
		ID        int64     `json:"id"`
		UserID    string    `json:"user_id"`
		TickID    int64     `json:"tick_id"`
		CostType  string    `json:"cost_type"`
		Tier      string    `json:"tier"`
		Amount    int64     `json:"amount"`
		Units     int64     `json:"units"`
		MetaJSON  string    `json:"meta_json,omitempty"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := make([]toolAuditItem, 0, len(items))
	byTier := map[string]int64{"T0": 0, "T1": 0, "T2": 0, "T3": 0}
	for _, it := range items {
		if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(it.CostType)), "tool.") {
			continue
		}
		tier := toolTier(it.CostType)
		if tierFilter != "" && tier != tierFilter {
			continue
		}
		byTier[tier]++
		out = append(out, toolAuditItem{
			ID:        it.ID,
			UserID:    it.UserID,
			TickID:    it.TickID,
			CostType:  it.CostType,
			Tier:      tier,
			Amount:    it.Amount,
			Units:     it.Units,
			MetaJSON:  it.MetaJSON,
			CreatedAt: it.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"tier":    tierFilter,
		"limit":   limit,
		"count":   len(out),
		"by_tier": byTier,
		"items":   out,
	})
}

func (s *Server) handleWorldCostAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	settings, _, _ := s.getWorldCostAlertSettings(r.Context())
	limit := parseLimit(r.URL.Query().Get("limit"), settings.ScanLimit)
	thresholdAmount := parseInt64(r.URL.Query().Get("threshold_amount"))
	if thresholdAmount <= 0 {
		thresholdAmount = settings.ThresholdAmount
	}
	topUsers := parseLimit(r.URL.Query().Get("top_users"), settings.TopUsers)
	items, err := s.queryWorldCostAlerts(r.Context(), userID, limit, thresholdAmount, topUsers)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":          userID,
		"limit":            limit,
		"threshold_amount": thresholdAmount,
		"top_users":        topUsers,
		"settings":         settings,
		"items":            items,
	})
}

func (s *Server) queryWorldCostAlerts(ctx context.Context, userID string, limit int, thresholdAmount int64, topUsers int) ([]worldCostAlertItem, error) {
	items, err := s.store.ListCostEvents(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	type userAgg struct {
		UserID        string
		EventCount    int64
		Amount        int64
		Units         int64
		TopCostType   string
		TopCostAmount int64
	}
	byUser := map[string]*userAgg{}
	byUserType := map[string]map[string]int64{}
	for _, it := range items {
		uid := strings.TrimSpace(it.UserID)
		if uid == "" {
			continue
		}
		a := byUser[uid]
		if a == nil {
			a = &userAgg{UserID: uid}
			byUser[uid] = a
			byUserType[uid] = map[string]int64{}
		}
		a.EventCount++
		a.Amount += it.Amount
		a.Units += it.Units
		costType := strings.TrimSpace(it.CostType)
		if costType == "" {
			costType = "unknown"
		}
		byUserType[uid][costType] += it.Amount
	}
	out := make([]worldCostAlertItem, 0, len(byUser))
	for uid, a := range byUser {
		typeMap := byUserType[uid]
		var topType string
		var topAmount int64
		for k, v := range typeMap {
			if v > topAmount || topType == "" {
				topType = k
				topAmount = v
			}
		}
		a.TopCostType = topType
		a.TopCostAmount = topAmount
		if a.Amount >= thresholdAmount {
			out = append(out, worldCostAlertItem{
				UserID:        a.UserID,
				EventCount:    a.EventCount,
				Amount:        a.Amount,
				Units:         a.Units,
				TopCostType:   a.TopCostType,
				TopCostAmount: a.TopCostAmount,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Amount == out[j].Amount {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Amount > out[j].Amount
	})
	if len(out) > topUsers {
		out = out[:topUsers]
	}
	return out, nil
}

type worldCostAlertSettingsUpsertRequest struct {
	ThresholdAmount int64 `json:"threshold_amount"`
	TopUsers        int   `json:"top_users"`
	ScanLimit       int   `json:"scan_limit"`
	NotifyCooldownS int64 `json:"notify_cooldown_seconds"`
}

func (s *Server) defaultWorldCostAlertSettings() worldCostAlertSettings {
	return worldCostAlertSettings{
		ThresholdAmount: 100,
		TopUsers:        10,
		ScanLimit:       500,
		NotifyCooldownS: defaultCostAlertCooldownSeconds,
	}
}

func (s *Server) normalizeWorldCostAlertSettings(in worldCostAlertSettings) worldCostAlertSettings {
	if in.ThresholdAmount <= 0 {
		in.ThresholdAmount = 100
	}
	if in.TopUsers <= 0 {
		in.TopUsers = 10
	}
	if in.TopUsers > 500 {
		in.TopUsers = 500
	}
	if in.ScanLimit <= 0 {
		in.ScanLimit = 500
	}
	if in.ScanLimit > 500 {
		in.ScanLimit = 500
	}
	if in.NotifyCooldownS <= 0 {
		in.NotifyCooldownS = defaultCostAlertCooldownSeconds
	}
	if in.NotifyCooldownS < runtimeSchedulerMinCooldownSeconds {
		in.NotifyCooldownS = runtimeSchedulerMinCooldownSeconds
	}
	if in.NotifyCooldownS > runtimeSchedulerMaxCooldownSeconds {
		in.NotifyCooldownS = runtimeSchedulerMaxCooldownSeconds
	}
	return in
}

func (s *Server) getLegacyWorldCostAlertSettings(ctx context.Context) (worldCostAlertSettings, string, time.Time) {
	def := s.defaultWorldCostAlertSettings()
	item, err := s.store.GetWorldSetting(ctx, worldCostAlertSettingsKey)
	if err != nil {
		return def, "default", time.Time{}
	}
	var parsed worldCostAlertSettings
	if err := json.Unmarshal([]byte(item.Value), &parsed); err != nil {
		return def, "default", time.Time{}
	}
	return s.normalizeWorldCostAlertSettings(parsed), "db", item.UpdatedAt
}

func (s *Server) getWorldCostAlertSettings(ctx context.Context) (worldCostAlertSettings, string, time.Time) {
	legacy, source, updatedAt := s.getLegacyWorldCostAlertSettings(ctx)
	// Compatibility facade: legacy settings remain for threshold/top_users/scan_limit.
	// Effective cost alert cooldown is resolved from runtime scheduler settings.
	if runtimeCooldown, _, _ := s.runtimeCostAlertCooldown(ctx); runtimeCooldown > 0 {
		legacy.NotifyCooldownS = runtimeCooldown
	}
	return legacy, source, updatedAt
}

func (s *Server) runtimeCostAlertCooldown(ctx context.Context) (int64, string, time.Time) {
	item, source, updatedAt := s.getRuntimeSchedulerSettings(ctx)
	return item.CostAlertNotifyCooldownSeconds, source, updatedAt
}

func (s *Server) handleWorldCostAlertSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	item, source, updatedAt := s.getWorldCostAlertSettings(r.Context())
	_, runtimeSource, runtimeUpdatedAt := s.runtimeCostAlertCooldown(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"item":                       item,
		"source":                     source,
		"updated_at":                 updatedAt,
		"notify_cooldown_source":     runtimeSource,
		"notify_cooldown_updated_at": runtimeUpdatedAt,
	})
}

func (s *Server) handleWorldCostAlertSettingsUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req worldCostAlertSettingsUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item := s.normalizeWorldCostAlertSettings(worldCostAlertSettings{
		ThresholdAmount: req.ThresholdAmount,
		TopUsers:        req.TopUsers,
		ScanLimit:       req.ScanLimit,
		NotifyCooldownS: req.NotifyCooldownS,
	})
	// Runtime scheduler settings are the single source of truth for cost-alert cooldown.
	cooldownSource := "compat"
	if merged, _, _ := s.getWorldCostAlertSettings(r.Context()); merged.NotifyCooldownS > 0 {
		item.NotifyCooldownS = merged.NotifyCooldownS
	}
	if _, source, _ := s.runtimeCostAlertCooldown(r.Context()); strings.TrimSpace(source) != "" {
		cooldownSource = source
	}
	raw, err := json.Marshal(item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved, err := s.store.UpsertWorldSetting(r.Context(), store.WorldSetting{
		Key:   worldCostAlertSettingsKey,
		Value: string(raw),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"item":                       item,
		"updated_at":                 saved.UpdatedAt,
		"source":                     "db",
		"notify_cooldown_source":     cooldownSource,
		"notify_cooldown_managed_by": runtimeSchedulerSettingsKey,
		"notify_cooldown_ignored":    req.NotifyCooldownS > 0 && req.NotifyCooldownS != item.NotifyCooldownS,
	})
}

func (s *Server) defaultRuntimeSchedulerSettings() runtimeSchedulerSettings {
	autonomy := s.cfg.AutonomyReminderIntervalTicks
	if autonomy < 0 {
		autonomy = 0
	}
	if autonomy > runtimeSchedulerMaxIntervalTicks {
		autonomy = runtimeSchedulerMaxIntervalTicks
	}
	community := s.cfg.CommunityCommReminderIntervalTicks
	if community < 0 {
		community = 0
	}
	if community > runtimeSchedulerMaxIntervalTicks {
		community = runtimeSchedulerMaxIntervalTicks
	}
	kbEnroll := s.cfg.KBEnrollmentReminderIntervalTicks
	if kbEnroll < 0 {
		kbEnroll = 0
	}
	if kbEnroll > runtimeSchedulerMaxIntervalTicks {
		kbEnroll = runtimeSchedulerMaxIntervalTicks
	}
	kbVote := s.cfg.KBVotingReminderIntervalTicks
	if kbVote < 0 {
		kbVote = 0
	}
	if kbVote > runtimeSchedulerMaxIntervalTicks {
		kbVote = runtimeSchedulerMaxIntervalTicks
	}
	return runtimeSchedulerSettings{
		AutonomyReminderIntervalTicks:      autonomy,
		CommunityCommReminderIntervalTicks: community,
		KBEnrollmentReminderIntervalTicks:  kbEnroll,
		KBVotingReminderIntervalTicks:      kbVote,
		CostAlertNotifyCooldownSeconds:     defaultCostAlertCooldownSeconds,
		LowTokenAlertCooldownSeconds:       0,
		AgentHeartbeatEvery:                defaultAgentHeartbeatEvery,
		PreviewLinkTTLDays:                 runtimeSchedulerDefaultPreviewLinkTTLDays,
	}
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func normalizeRuntimeSchedulerSettings(in, fallback runtimeSchedulerSettings) runtimeSchedulerSettings {
	out := fallback
	// Missing fields are handled by pre-filling `in` with fallback before JSON unmarshal.
	out.AutonomyReminderIntervalTicks = clampInt64(in.AutonomyReminderIntervalTicks, 0, runtimeSchedulerMaxIntervalTicks)
	out.CommunityCommReminderIntervalTicks = clampInt64(in.CommunityCommReminderIntervalTicks, 0, runtimeSchedulerMaxIntervalTicks)
	out.KBEnrollmentReminderIntervalTicks = clampInt64(in.KBEnrollmentReminderIntervalTicks, 0, runtimeSchedulerMaxIntervalTicks)
	out.KBVotingReminderIntervalTicks = clampInt64(in.KBVotingReminderIntervalTicks, 0, runtimeSchedulerMaxIntervalTicks)
	// Read-time normalization is intentionally more permissive than API writes:
	// invalid manual DB values are clamped, while upsert requests are rejected by validateRuntimeSchedulerSettings.
	// cost alert cooldown does not use 0 as a disable value; 0 keeps fallback/default.
	// Values in (0,30) are clamped to 30 for read-time robustness against manual DB edits.
	if in.CostAlertNotifyCooldownSeconds > 0 {
		out.CostAlertNotifyCooldownSeconds = clampInt64(in.CostAlertNotifyCooldownSeconds, runtimeSchedulerMinCooldownSeconds, runtimeSchedulerMaxCooldownSeconds)
	}
	// low-token cooldown keeps 0 as an explicit "disabled" value for compatibility.
	if in.LowTokenAlertCooldownSeconds <= 0 {
		out.LowTokenAlertCooldownSeconds = 0
	} else {
		out.LowTokenAlertCooldownSeconds = clampInt64(in.LowTokenAlertCooldownSeconds, runtimeSchedulerMinCooldownSeconds, runtimeSchedulerMaxCooldownSeconds)
	}
	if hb := strings.TrimSpace(in.AgentHeartbeatEvery); hb != "" {
		if d, err := time.ParseDuration(hb); err == nil && d >= 0 && d <= runtimeSchedulerMaxHeartbeat {
			out.AgentHeartbeatEvery = bot.NormalizeHeartbeatEvery(hb)
		}
	}
	if in.PreviewLinkTTLDays > 0 {
		out.PreviewLinkTTLDays = clampInt64(in.PreviewLinkTTLDays, runtimeSchedulerMinPreviewLinkTTLDays, runtimeSchedulerMaxPreviewLinkTTLDays)
	}
	return out
}

func (s *Server) getRuntimeSchedulerCache(now time.Time) (runtimeSchedulerSettings, string, time.Time, bool) {
	s.runtimeSchedulerMu.RLock()
	defer s.runtimeSchedulerMu.RUnlock()
	src := strings.TrimSpace(s.runtimeSchedulerSrc)
	if (src != "db" && src != "compat" && src != "compat_invalid_db") || s.runtimeSchedulerTS.IsZero() {
		return runtimeSchedulerSettings{}, "", time.Time{}, false
	}
	if now.Sub(s.runtimeSchedulerTS) > runtimeSchedulerCacheTTL {
		return runtimeSchedulerSettings{}, "", time.Time{}, false
	}
	return s.runtimeSchedulerItem, s.runtimeSchedulerSrc, s.runtimeSchedulerAt, true
}

func (s *Server) setRuntimeSchedulerCache(item runtimeSchedulerSettings, source string, updatedAt, now time.Time) {
	s.runtimeSchedulerMu.Lock()
	defer s.runtimeSchedulerMu.Unlock()
	s.runtimeSchedulerItem = item
	s.runtimeSchedulerSrc = strings.TrimSpace(source)
	s.runtimeSchedulerAt = updatedAt
	s.runtimeSchedulerTS = now
}

func validateRuntimeSchedulerSettings(in runtimeSchedulerSettings) error {
	if in.AutonomyReminderIntervalTicks < 0 || in.AutonomyReminderIntervalTicks > runtimeSchedulerMaxIntervalTicks {
		return fmt.Errorf("autonomy_reminder_interval_ticks must be in [0, %d]", runtimeSchedulerMaxIntervalTicks)
	}
	if in.CommunityCommReminderIntervalTicks < 0 || in.CommunityCommReminderIntervalTicks > runtimeSchedulerMaxIntervalTicks {
		return fmt.Errorf("community_comm_reminder_interval_ticks must be in [0, %d]", runtimeSchedulerMaxIntervalTicks)
	}
	if in.KBEnrollmentReminderIntervalTicks < 0 || in.KBEnrollmentReminderIntervalTicks > runtimeSchedulerMaxIntervalTicks {
		return fmt.Errorf("kb_enrollment_reminder_interval_ticks must be in [0, %d]", runtimeSchedulerMaxIntervalTicks)
	}
	if in.KBVotingReminderIntervalTicks < 0 || in.KBVotingReminderIntervalTicks > runtimeSchedulerMaxIntervalTicks {
		return fmt.Errorf("kb_voting_reminder_interval_ticks must be in [0, %d]", runtimeSchedulerMaxIntervalTicks)
	}
	// Upsert uses strict bounds; read-time normalization separately clamps manual DB edits.
	if in.CostAlertNotifyCooldownSeconds < runtimeSchedulerMinCooldownSeconds || in.CostAlertNotifyCooldownSeconds > runtimeSchedulerMaxCooldownSeconds {
		return fmt.Errorf("cost_alert_notify_cooldown_seconds must be in [%d, %d]", runtimeSchedulerMinCooldownSeconds, runtimeSchedulerMaxCooldownSeconds)
	}
	if in.LowTokenAlertCooldownSeconds != 0 && (in.LowTokenAlertCooldownSeconds < runtimeSchedulerMinCooldownSeconds || in.LowTokenAlertCooldownSeconds > runtimeSchedulerMaxCooldownSeconds) {
		return fmt.Errorf("low_token_alert_cooldown_seconds must be 0 or in [%d, %d]", runtimeSchedulerMinCooldownSeconds, runtimeSchedulerMaxCooldownSeconds)
	}
	hb := strings.TrimSpace(in.AgentHeartbeatEvery)
	if hb == "" {
		return fmt.Errorf("agent_heartbeat_every is required")
	}
	d, err := time.ParseDuration(hb)
	if err != nil {
		return fmt.Errorf("agent_heartbeat_every must be a valid duration")
	}
	if d < 0 || d > runtimeSchedulerMaxHeartbeat {
		return fmt.Errorf("agent_heartbeat_every must be in [0, 24h]")
	}
	if in.PreviewLinkTTLDays != 0 && (in.PreviewLinkTTLDays < runtimeSchedulerMinPreviewLinkTTLDays || in.PreviewLinkTTLDays > runtimeSchedulerMaxPreviewLinkTTLDays) {
		return fmt.Errorf("preview_link_ttl_days must be in [%d, %d]", runtimeSchedulerMinPreviewLinkTTLDays, runtimeSchedulerMaxPreviewLinkTTLDays)
	}
	return nil
}

func (s *Server) getRuntimeSchedulerSettings(ctx context.Context) (runtimeSchedulerSettings, string, time.Time) {
	now := time.Now().UTC()
	if cached, source, updatedAt, ok := s.getRuntimeSchedulerCache(now); ok {
		if source == "compat" {
			// Keep compat mode bound to live process config while still skipping DB reads.
			// This lets tests (and any runtime config mutators) observe current cfg defaults.
			return s.defaultRuntimeSchedulerSettings(), "compat", time.Time{}
		}
		return cached, source, updatedAt
	}
	compat := s.defaultRuntimeSchedulerSettings()
	item, err := s.store.GetWorldSetting(ctx, runtimeSchedulerSettingsKey)
	if err != nil || strings.TrimSpace(item.Value) == "" {
		s.setRuntimeSchedulerCache(compat, "compat", time.Time{}, now)
		return compat, "compat", time.Time{}
	}
	parsed := compat
	if err := json.Unmarshal([]byte(item.Value), &parsed); err != nil {
		s.setRuntimeSchedulerCache(compat, "compat_invalid_db", item.UpdatedAt, now)
		return compat, "compat_invalid_db", item.UpdatedAt
	}
	out := normalizeRuntimeSchedulerSettings(parsed, compat)
	s.setRuntimeSchedulerCache(out, "db", item.UpdatedAt, now)
	return out, "db", item.UpdatedAt
}

func (s *Server) handleRuntimeSchedulerSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	item, source, updatedAt := s.getRuntimeSchedulerSettings(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"item":       item,
		"source":     source,
		"updated_at": updatedAt,
	})
}

func (s *Server) handleRuntimeSchedulerSettingsUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var item runtimeSchedulerSettings
	if err := decodeJSON(r, &item); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item.AgentHeartbeatEvery = strings.TrimSpace(item.AgentHeartbeatEvery)
	// Backward compatibility: old clients may not send this newly added field.
	if item.PreviewLinkTTLDays == 0 {
		item.PreviewLinkTTLDays = runtimeSchedulerDefaultPreviewLinkTTLDays
	}
	if err := validateRuntimeSchedulerSettings(item); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item.AgentHeartbeatEvery = bot.NormalizeHeartbeatEvery(item.AgentHeartbeatEvery)
	updatedAt, err := s.putSettingJSON(r.Context(), runtimeSchedulerSettingsKey, item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setRuntimeSchedulerCache(item, "db", updatedAt, time.Now().UTC())
	if s.bots != nil {
		s.bots.SetOpenClawHeartbeatEvery(item.AgentHeartbeatEvery)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"item":       item,
		"source":     "db",
		"updated_at": updatedAt,
	})
}

func (s *Server) handleWorldCostAlertNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	items, err := s.store.ListMailbox(r.Context(), clawWorldSystemID, "outbox", "", "[WORLD-COST-ALERT]", nil, nil, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type item struct {
		MailboxID int64     `json:"mailbox_id"`
		MessageID int64     `json:"message_id"`
		ToUserID  string    `json:"to_user_id"`
		Subject   string    `json:"subject"`
		Body      string    `json:"body"`
		SentAt    time.Time `json:"sent_at"`
	}
	out := make([]item, 0, len(items))
	for _, it := range items {
		if userID != "" && it.ToAddress != userID {
			continue
		}
		out = append(out, item{
			MailboxID: it.MailboxID,
			MessageID: it.MessageID,
			ToUserID:  it.ToAddress,
			Subject:   it.Subject,
			Body:      it.Body,
			SentAt:    it.SentAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"items":   out,
	})
}

func (s *Server) shouldSendWorldCostAlert(userID string, amount int64, cooldown time.Duration, now time.Time) bool {
	s.alertNotifyMu.Lock()
	defer s.alertNotifyMu.Unlock()
	lastAt, seen := s.alertLastSent[userID]
	lastAmt := s.alertLastAmt[userID]
	if !seen {
		s.alertLastSent[userID] = now
		s.alertLastAmt[userID] = amount
		return true
	}
	if amount > lastAmt {
		s.alertLastSent[userID] = now
		s.alertLastAmt[userID] = amount
		return true
	}
	if now.Sub(lastAt) >= cooldown {
		s.alertLastSent[userID] = now
		s.alertLastAmt[userID] = amount
		return true
	}
	return false
}

func (s *Server) runWorldCostAlertNotifications(ctx context.Context, tickID int64) error {
	settings, _, _ := s.getWorldCostAlertSettings(ctx)
	items, err := s.queryWorldCostAlerts(ctx, "", settings.ScanLimit, settings.ThresholdAmount, settings.TopUsers)
	if err != nil {
		return err
	}
	cooldown := time.Duration(settings.NotifyCooldownS) * time.Second
	now := time.Now().UTC()
	for _, it := range items {
		if strings.TrimSpace(it.UserID) == "" || it.UserID == clawWorldSystemID {
			continue
		}
		if !s.shouldSendWorldCostAlert(it.UserID, it.Amount, cooldown, now) {
			continue
		}
		subject := fmt.Sprintf("[WORLD-COST-ALERT] user=%s amount=%d threshold=%d", it.UserID, it.Amount, settings.ThresholdAmount)
		body := fmt.Sprintf(
			"tick_id=%d\nuser_id=%s\namount=%d\nunits=%d\nevent_count=%d\ntop_cost_type=%s\ntop_cost_amount=%d\nthreshold_amount=%d\ntop_users=%d\nscan_limit=%d\nnotify_cooldown_seconds=%d\n\nThis is an observation alert. No action was forcibly blocked.",
			tickID,
			it.UserID,
			it.Amount,
			it.Units,
			it.EventCount,
			it.TopCostType,
			it.TopCostAmount,
			settings.ThresholdAmount,
			settings.TopUsers,
			settings.ScanLimit,
			settings.NotifyCooldownS,
		)
		if _, sendErr := s.store.SendMail(ctx, clawWorldSystemID, []string{it.UserID}, subject, body); sendErr != nil {
			log.Printf("world_cost_alert_notify_failed user_id=%s err=%v", it.UserID, sendErr)
		}
	}
	return nil
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func pct(part, total int) int {
	if total <= 0 || part <= 0 {
		return 0
	}
	if part >= total {
		return 100
	}
	return (part*100 + total/2) / total
}

func intensity(events, total, perCapTarget int) int {
	if total <= 0 || perCapTarget <= 0 || events <= 0 {
		return 0
	}
	den := total * perCapTarget
	if den <= 0 {
		return 0
	}
	if events >= den {
		return 100
	}
	return clampPct((events*100 + den/2) / den)
}

func weightedScore(coveragePct, intensityPct int) int {
	coveragePct = clampPct(coveragePct)
	intensityPct = clampPct(intensityPct)
	return clampPct((coveragePct*70 + intensityPct*30 + 50) / 100)
}

func sortedSetKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func missingUsersFromSet(active []string, include map[string]struct{}) []string {
	out := make([]string, 0)
	for _, uid := range active {
		if _, ok := include[uid]; ok {
			continue
		}
		out = append(out, uid)
	}
	sort.Strings(out)
	return out
}

func (s *Server) defaultWorldEvolutionAlertSettings() worldEvolutionAlertSettings {
	return worldEvolutionAlertSettings{
		WindowMinutes:   60,
		MailScanLimit:   120,
		KBScanLimit:     300,
		WarnThreshold:   65,
		CriticalLevel:   45,
		NotifyCooldownS: int64((10 * time.Minute) / time.Second),
	}
}

func (s *Server) normalizeWorldEvolutionAlertSettings(in worldEvolutionAlertSettings) worldEvolutionAlertSettings {
	if in.WindowMinutes <= 0 {
		in.WindowMinutes = 60
	}
	if in.WindowMinutes > 24*60 {
		in.WindowMinutes = 24 * 60
	}
	if in.MailScanLimit <= 0 {
		in.MailScanLimit = 120
	}
	if in.MailScanLimit > 500 {
		in.MailScanLimit = 500
	}
	if in.KBScanLimit <= 0 {
		in.KBScanLimit = 300
	}
	if in.KBScanLimit > 1000 {
		in.KBScanLimit = 1000
	}
	if in.WarnThreshold <= 0 {
		in.WarnThreshold = 65
	}
	if in.WarnThreshold > 100 {
		in.WarnThreshold = 100
	}
	if in.CriticalLevel <= 0 {
		in.CriticalLevel = 45
	}
	if in.CriticalLevel > in.WarnThreshold {
		in.CriticalLevel = in.WarnThreshold
	}
	if in.NotifyCooldownS <= 0 {
		in.NotifyCooldownS = int64((10 * time.Minute) / time.Second)
	}
	if in.NotifyCooldownS < 30 {
		in.NotifyCooldownS = 30
	}
	if in.NotifyCooldownS > 86400 {
		in.NotifyCooldownS = 86400
	}
	return in
}

func (s *Server) getWorldEvolutionAlertSettings(ctx context.Context) (worldEvolutionAlertSettings, string, time.Time) {
	def := s.defaultWorldEvolutionAlertSettings()
	item, err := s.store.GetWorldSetting(ctx, worldEvolutionAlertSettingsKey)
	if err != nil {
		return def, "default", time.Time{}
	}
	var parsed worldEvolutionAlertSettings
	if err := json.Unmarshal([]byte(item.Value), &parsed); err != nil {
		return def, "default", time.Time{}
	}
	return s.normalizeWorldEvolutionAlertSettings(parsed), "db", item.UpdatedAt
}

func (s *Server) listWorldEvolutionAlerts(snapshot worldEvolutionSnapshot, settings worldEvolutionAlertSettings) []worldEvolutionAlertItem {
	if snapshot.TotalUsers <= 0 {
		return []worldEvolutionAlertItem{{
			Category:  "overall",
			Severity:  "warning",
			Score:     0,
			Threshold: settings.WarnThreshold,
			Message:   "no active users found",
		}}
	}
	out := make([]worldEvolutionAlertItem, 0, 8)
	overallThreshold := settings.WarnThreshold
	overallSeverity := ""
	if snapshot.OverallScore < settings.CriticalLevel {
		overallSeverity = "critical"
		overallThreshold = settings.CriticalLevel
	} else if snapshot.OverallScore < settings.WarnThreshold {
		overallSeverity = "warning"
	}
	if overallSeverity != "" {
		out = append(out, worldEvolutionAlertItem{
			Category:  "overall",
			Severity:  overallSeverity,
			Score:     snapshot.OverallScore,
			Threshold: overallThreshold,
			Message:   "overall evolution score below target",
		})
	}
	keys := make([]string, 0, len(snapshot.KPIs))
	for k := range snapshot.KPIs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		it := snapshot.KPIs[k]
		threshold := settings.WarnThreshold
		severity := ""
		if it.Score < settings.CriticalLevel {
			severity = "critical"
			threshold = settings.CriticalLevel
		} else if it.Score < settings.WarnThreshold {
			severity = "warning"
		}
		if severity == "" {
			continue
		}
		out = append(out, worldEvolutionAlertItem{
			Category:  k,
			Severity:  severity,
			Score:     it.Score,
			Threshold: threshold,
			Message:   it.Note,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		if out[i].Score == out[j].Score {
			return out[i].Category < out[j].Category
		}
		return out[i].Score < out[j].Score
	})
	return out
}

func (s *Server) buildWorldEvolutionSnapshot(ctx context.Context, settings worldEvolutionAlertSettings, tickID int64) (worldEvolutionSnapshot, error) {
	active := s.activeUserIDs(ctx)
	activeSet := make(map[string]struct{}, len(active))
	for _, uid := range active {
		activeSet[uid] = struct{}{}
	}
	total := len(active)
	snapshot := worldEvolutionSnapshot{
		AsOf:              time.Now().UTC(),
		WindowMinutes:     settings.WindowMinutes,
		TotalUsers:        total,
		KPIs:              map[string]worldEvolutionKPI{},
		GeneratedAtTickID: tickID,
	}
	if total == 0 {
		snapshot.Level = "empty"
		return snapshot, nil
	}
	if tickID <= 0 {
		if ticks, err := s.store.ListWorldTicks(ctx, 1); err == nil && len(ticks) > 0 {
			snapshot.GeneratedAtTickID = ticks[0].TickID
		}
	}

	tokenByUser, err := s.listTokenBalanceMap(ctx)
	if err != nil {
		return worldEvolutionSnapshot{}, err
	}
	aliveUsers := map[string]struct{}{}
	lifeItems, err := s.store.ListUserLifeStates(ctx, "", "", 5000)
	if err != nil {
		return worldEvolutionSnapshot{}, err
	}
	for _, it := range lifeItems {
		uid := strings.TrimSpace(it.UserID)
		if _, ok := activeSet[uid]; !ok {
			continue
		}
		if normalizeLifeStateForServer(it.State) != "dead" {
			aliveUsers[uid] = struct{}{}
		}
	}
	positiveTokenUsers := map[string]struct{}{}
	for _, uid := range active {
		if tokenByUser[uid] > 0 {
			positiveTokenUsers[uid] = struct{}{}
		}
	}

	lifeCoverage := pct(len(aliveUsers), total)
	tokenCoverage := pct(len(positiveTokenUsers), total)
	survivalScore := clampPct((lifeCoverage*65 + tokenCoverage*35 + 50) / 100)
	snapshot.KPIs["survival"] = worldEvolutionKPI{
		Name:        "survival",
		Score:       survivalScore,
		ActiveUsers: len(aliveUsers),
		TotalUsers:  total,
		Events:      len(positiveTokenUsers),
		Missing:     missingUsersFromSet(active, aliveUsers),
		Note:        "alive coverage + positive token coverage",
	}

	since := time.Now().UTC().Add(-time.Duration(settings.WindowMinutes) * time.Minute)
	autonomyUsers := map[string]struct{}{}
	collabUsers := map[string]struct{}{}
	meaningfulOutboxCount := 0
	peerOutboxCount := 0
	for _, uid := range active {
		outbox, err := s.store.ListMailbox(ctx, uid, "outbox", "", "", &since, nil, settings.MailScanLimit)
		if err != nil {
			return worldEvolutionSnapshot{}, err
		}
		for _, it := range outbox {
			toID := strings.TrimSpace(it.ToAddress)
			if toID == "" {
				continue
			}
			if toID == clawWorldSystemID {
				if isMeaningfulOutputMail(it.Subject, it.Body) {
					autonomyUsers[uid] = struct{}{}
					meaningfulOutboxCount++
				}
				continue
			}
			if toID == uid {
				continue
			}
			if _, ok := activeSet[toID]; !ok {
				continue
			}
			if strings.TrimSpace(it.Subject) == "" && strings.TrimSpace(it.Body) == "" {
				continue
			}
			collabUsers[uid] = struct{}{}
			peerOutboxCount++
		}
	}
	snapshot.MeaningfulOutbox = meaningfulOutboxCount
	snapshot.PeerOutbox = peerOutboxCount
	autonomyScore := weightedScore(pct(len(autonomyUsers), total), intensity(meaningfulOutboxCount, total, 1))
	snapshot.KPIs["autonomy"] = worldEvolutionKPI{
		Name:        "autonomy",
		Score:       autonomyScore,
		ActiveUsers: len(autonomyUsers),
		TotalUsers:  total,
		Events:      meaningfulOutboxCount,
		Missing:     missingUsersFromSet(active, autonomyUsers),
		Note:        "meaningful progress outbox to clawcolony-admin",
	}
	collabScore := weightedScore(pct(len(collabUsers), total), intensity(peerOutboxCount, total, 1))
	snapshot.KPIs["collaboration"] = worldEvolutionKPI{
		Name:        "collaboration",
		Score:       collabScore,
		ActiveUsers: len(collabUsers),
		TotalUsers:  total,
		Events:      peerOutboxCount,
		Missing:     missingUsersFromSet(active, collabUsers),
		Note:        "peer-to-peer coordination outbox",
	}

	governanceUsers := map[string]struct{}{}
	governanceEvents := 0
	proposals, err := s.store.ListKBProposals(ctx, "", settings.KBScanLimit)
	if err != nil {
		return worldEvolutionSnapshot{}, err
	}
	for _, p := range proposals {
		if p.CreatedAt.After(since) {
			if _, ok := activeSet[p.ProposerUserID]; ok {
				governanceUsers[p.ProposerUserID] = struct{}{}
				governanceEvents++
			}
		}
		enrollments, err := s.store.ListKBProposalEnrollments(ctx, p.ID)
		if err != nil {
			return worldEvolutionSnapshot{}, err
		}
		for _, it := range enrollments {
			if !it.CreatedAt.After(since) {
				continue
			}
			if _, ok := activeSet[it.UserID]; !ok {
				continue
			}
			governanceUsers[it.UserID] = struct{}{}
			governanceEvents++
		}
		votes, err := s.store.ListKBVotes(ctx, p.ID)
		if err != nil {
			return worldEvolutionSnapshot{}, err
		}
		for _, it := range votes {
			ts := it.UpdatedAt
			if ts.IsZero() {
				ts = it.CreatedAt
			}
			if !ts.After(since) {
				continue
			}
			if _, ok := activeSet[it.UserID]; !ok {
				continue
			}
			governanceUsers[it.UserID] = struct{}{}
			governanceEvents++
		}
		revs, err := s.store.ListKBRevisions(ctx, p.ID, settings.KBScanLimit)
		if err != nil {
			return worldEvolutionSnapshot{}, err
		}
		for _, it := range revs {
			if !it.CreatedAt.After(since) {
				continue
			}
			if _, ok := activeSet[it.CreatedBy]; !ok {
				continue
			}
			governanceUsers[it.CreatedBy] = struct{}{}
			governanceEvents++
		}
		threads, err := s.store.ListKBThreadMessages(ctx, p.ID, settings.KBScanLimit)
		if err != nil {
			return worldEvolutionSnapshot{}, err
		}
		for _, it := range threads {
			if !it.CreatedAt.After(since) {
				continue
			}
			if _, ok := activeSet[it.AuthorID]; !ok {
				continue
			}
			governanceUsers[it.AuthorID] = struct{}{}
			governanceEvents++
		}
	}
	snapshot.GovernanceEvents = governanceEvents
	governanceScore := weightedScore(pct(len(governanceUsers), total), intensity(governanceEvents, total, 2))
	snapshot.KPIs["governance"] = worldEvolutionKPI{
		Name:        "governance",
		Score:       governanceScore,
		ActiveUsers: len(governanceUsers),
		TotalUsers:  total,
		Events:      governanceEvents,
		Missing:     missingUsersFromSet(active, governanceUsers),
		Note:        "knowledgebase proposal discussion / enrollment / voting activity",
	}

	knowledgeUsers := map[string]struct{}{}
	knowledgeUpdates := 0
	entries, err := s.store.ListKBEntries(ctx, "", "", settings.KBScanLimit)
	if err != nil {
		return worldEvolutionSnapshot{}, err
	}
	for _, it := range entries {
		if !it.UpdatedAt.After(since) {
			continue
		}
		uid := strings.TrimSpace(it.UpdatedBy)
		if _, ok := activeSet[uid]; !ok {
			continue
		}
		knowledgeUsers[uid] = struct{}{}
		knowledgeUpdates++
	}
	snapshot.KnowledgeUpdates = knowledgeUpdates
	knowledgeScore := weightedScore(pct(len(knowledgeUsers), total), intensity(knowledgeUpdates, total, 1))
	snapshot.KPIs["knowledge"] = worldEvolutionKPI{
		Name:        "knowledge",
		Score:       knowledgeScore,
		ActiveUsers: len(knowledgeUsers),
		TotalUsers:  total,
		Events:      knowledgeUpdates,
		Missing:     missingUsersFromSet(active, knowledgeUsers),
		Note:        "recent knowledgebase entry updates",
	}

	overall := clampPct((survivalScore*30 + autonomyScore*20 + collabScore*20 + governanceScore*15 + knowledgeScore*15 + 50) / 100)
	snapshot.OverallScore = overall
	level := "healthy"
	for _, k := range snapshot.KPIs {
		if k.Score < settings.CriticalLevel {
			level = "critical"
			break
		}
		if k.Score < settings.WarnThreshold {
			level = "warning"
		}
	}
	if overall < settings.CriticalLevel {
		level = "critical"
	} else if overall < settings.WarnThreshold && level != "critical" {
		level = "warning"
	}
	snapshot.Level = level
	return snapshot, nil
}

func (s *Server) shouldSendWorldEvolutionAlert(digest string, cooldown time.Duration, now time.Time) bool {
	s.evolutionAlertMu.Lock()
	defer s.evolutionAlertMu.Unlock()
	if s.evolutionAlertLastAt.IsZero() {
		s.evolutionAlertLastAt = now
		s.evolutionAlertDigest = digest
		return true
	}
	if strings.TrimSpace(digest) != "" && digest != s.evolutionAlertDigest {
		s.evolutionAlertLastAt = now
		s.evolutionAlertDigest = digest
		return true
	}
	if now.Sub(s.evolutionAlertLastAt) >= cooldown {
		s.evolutionAlertLastAt = now
		s.evolutionAlertDigest = digest
		return true
	}
	return false
}

func (s *Server) runWorldEvolutionAlertNotifications(ctx context.Context, tickID int64) error {
	settings, _, _ := s.getWorldEvolutionAlertSettings(ctx)
	snapshot, err := s.buildWorldEvolutionSnapshot(ctx, settings, tickID)
	if err != nil {
		return err
	}
	alerts := s.listWorldEvolutionAlerts(snapshot, settings)
	if len(alerts) == 0 {
		return nil
	}
	alertDigest := fmt.Sprintf("level=%s overall=%d alerts=%d first=%s:%d", snapshot.Level, snapshot.OverallScore, len(alerts), alerts[0].Category, alerts[0].Score)
	if !s.shouldSendWorldEvolutionAlert(alertDigest, time.Duration(settings.NotifyCooldownS)*time.Second, time.Now().UTC()) {
		return nil
	}
	head := alerts[0]
	subject := fmt.Sprintf("[WORLD-EVOLUTION-ALERT] level=%s overall=%d top=%s:%d", snapshot.Level, snapshot.OverallScore, head.Category, head.Score)
	body := fmt.Sprintf(
		"tick_id=%d\nas_of=%s\nwindow_minutes=%d\ntotal_users=%d\noverall_score=%d\nlevel=%s\nwarn_threshold=%d\ncritical_threshold=%d\nalerts=%d\n\nkpi_survival=%d\nkpi_autonomy=%d\nkpi_collaboration=%d\nkpi_governance=%d\nkpi_knowledge=%d\n",
		tickID,
		snapshot.AsOf.Format(time.RFC3339),
		snapshot.WindowMinutes,
		snapshot.TotalUsers,
		snapshot.OverallScore,
		snapshot.Level,
		settings.WarnThreshold,
		settings.CriticalLevel,
		len(alerts),
		snapshot.KPIs["survival"].Score,
		snapshot.KPIs["autonomy"].Score,
		snapshot.KPIs["collaboration"].Score,
		snapshot.KPIs["governance"].Score,
		snapshot.KPIs["knowledge"].Score,
	)
	for i, it := range alerts {
		body += fmt.Sprintf("\nalert_%d=%s|%s|score=%d|threshold=%d|%s", i+1, it.Severity, it.Category, it.Score, it.Threshold, it.Message)
	}
	_, err = s.store.SendMail(ctx, clawWorldSystemID, []string{clawWorldSystemID}, subject, body)
	return err
}

func (s *Server) handleWorldEvolutionScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	item, source, updatedAt := s.getWorldEvolutionAlertSettings(r.Context())
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("window_minutes"))); err == nil && v > 0 {
		item.WindowMinutes = v
	}
	if v := parseLimit(r.URL.Query().Get("mail_scan_limit"), item.MailScanLimit); v > 0 {
		item.MailScanLimit = v
	}
	if v := parseLimit(r.URL.Query().Get("kb_scan_limit"), item.KBScanLimit); v > 0 {
		item.KBScanLimit = v
	}
	item = s.normalizeWorldEvolutionAlertSettings(item)
	snapshot, err := s.buildWorldEvolutionSnapshot(r.Context(), item, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item":       snapshot,
		"settings":   item,
		"source":     source,
		"updated_at": updatedAt,
	})
}

func (s *Server) handleWorldEvolutionAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	item, _, _ := s.getWorldEvolutionAlertSettings(r.Context())
	if v, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("window_minutes"))); err == nil && v > 0 {
		item.WindowMinutes = v
	}
	item = s.normalizeWorldEvolutionAlertSettings(item)
	snapshot, err := s.buildWorldEvolutionSnapshot(r.Context(), item, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	alerts := s.listWorldEvolutionAlerts(snapshot, item)
	writeJSON(w, http.StatusOK, map[string]any{
		"item":        snapshot,
		"alerts":      alerts,
		"alert_count": len(alerts),
		"settings":    item,
	})
}

func (s *Server) handleWorldEvolutionAlertSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	item, source, updatedAt := s.getWorldEvolutionAlertSettings(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"item":       item,
		"source":     source,
		"updated_at": updatedAt,
	})
}

func (s *Server) handleWorldEvolutionAlertSettingsUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req worldEvolutionAlertSettings
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item := s.normalizeWorldEvolutionAlertSettings(req)
	raw, err := json.Marshal(item)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved, err := s.store.UpsertWorldSetting(r.Context(), store.WorldSetting{
		Key:   worldEvolutionAlertSettingsKey,
		Value: string(raw),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"item":       item,
		"source":     "db",
		"updated_at": saved.UpdatedAt,
	})
}

func (s *Server) handleWorldEvolutionAlertNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	levelFilter := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("level")))
	items, err := s.store.ListMailbox(r.Context(), clawWorldSystemID, "outbox", "", "[WORLD-EVOLUTION-ALERT]", nil, nil, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type item struct {
		MailboxID int64     `json:"mailbox_id"`
		MessageID int64     `json:"message_id"`
		Subject   string    `json:"subject"`
		Body      string    `json:"body"`
		SentAt    time.Time `json:"sent_at"`
	}
	out := make([]item, 0, len(items))
	for _, it := range items {
		if levelFilter != "" && !strings.Contains(strings.ToLower(it.Subject), "level="+levelFilter) {
			continue
		}
		out = append(out, item{
			MailboxID: it.MailboxID,
			MessageID: it.MessageID,
			Subject:   it.Subject,
			Body:      it.Body,
			SentAt:    it.SentAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"level": levelFilter,
		"items": out,
	})
}

func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	includeInactive := parseBoolFlag(r.URL.Query().Get("include_inactive"))
	items, err := s.store.ListBots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	active, activeOK := s.activeBotIDsInNamespace(r.Context())
	if !includeInactive {
		items = s.filterActiveBotsBySet(items, active, activeOK)
	}
	if activeOK && len(active) > 0 {
		items = mergeMissingActiveBots(items, active)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type botNicknameUpsertRequest struct {
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
}

const maxBotNicknameRunes = 20

func normalizeBotNickname(raw string) (string, error) {
	nick := strings.TrimSpace(raw)
	if nick == "" {
		return "", nil
	}
	if strings.ContainsAny(nick, "\r\n\t") {
		return "", fmt.Errorf("nickname must be a single-line string")
	}
	if utf8.RuneCountInString(nick) > maxBotNicknameRunes {
		return "", fmt.Errorf("nickname must be <= %d characters", maxBotNicknameRunes)
	}
	return nick, nil
}

func (s *Server) handleBotNicknameUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req botNicknameUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	nickname, err := normalizeBotNickname(req.Nickname)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.store.UpdateBotNickname(r.Context(), req.UserID, nickname)
	if err != nil {
		if errors.Is(err, store.ErrBotNotFound) {
			active, activeOK := s.activeBotIDsInNamespace(r.Context())
			if activeOK {
				if _, ok := active[req.UserID]; ok {
					writeError(w, http.StatusConflict, "user_id exists in cluster but is not synced to runtime yet")
					return
				}
			}
			writeError(w, http.StatusNotFound, "user_id not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func syntheticActiveBot(userID string) store.Bot {
	return store.Bot{
		BotID:       userID,
		Name:        userID,
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}
}

func mergeMissingActiveBots(items []store.Bot, active map[string]struct{}) []store.Bot {
	if len(active) == 0 {
		return items
	}
	out := append([]store.Bot(nil), items...)
	seen := make(map[string]struct{}, len(out))
	for _, it := range out {
		uid := strings.TrimSpace(it.BotID)
		if uid != "" {
			seen[uid] = struct{}{}
		}
	}
	missing := make([]string, 0, len(active))
	for uid := range active {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		missing = append(missing, uid)
	}
	sort.Strings(missing)
	for _, uid := range missing {
		out = append(out, syntheticActiveBot(uid))
	}
	return out
}

func (s *Server) handleBotThoughts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	botID := queryUserID(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	s.thoughtMu.Lock()
	items := make([]botThought, 0, len(s.thoughts))
	for _, t := range s.thoughts {
		if botID != "" && t.BotID != botID {
			continue
		}
		items = append(items, t)
	}
	s.thoughtMu.Unlock()
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleBotLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.kubeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not available")
		return
	}
	botID := queryUserID(r)
	if botID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	tail := int64(parseLimit(r.URL.Query().Get("tail"), 300))
	podName, content, err := s.readBotLogs(r.Context(), botID, tail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": botID,
		"pod":     podName,
		"tail":    tail,
		"content": content,
	})
}

func (s *Server) handleAllBotLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.kubeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not available")
		return
	}
	tail := int64(parseLimit(r.URL.Query().Get("tail"), 300))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListBots(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items = s.filterActiveBots(r.Context(), items)
	sort.Slice(items, func(i, j int) bool { return items[i].BotID < items[j].BotID })
	if len(items) > limit {
		items = items[:limit]
	}
	type item struct {
		UserID  string `json:"user_id"`
		Name    string `json:"name"`
		Pod     string `json:"pod,omitempty"`
		Content string `json:"content,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	out := make([]item, 0, len(items))
	for _, b := range items {
		podName, content, err := s.readBotLogs(r.Context(), b.BotID, tail)
		entry := item{UserID: b.BotID, Name: b.Name, Pod: podName, Content: content}
		if err != nil {
			entry.Error = err.Error()
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tail":  tail,
		"items": out,
	})
}

func (s *Server) readBotLogs(ctx context.Context, botID string, tail int64) (string, string, error) {
	pods, err := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).List(ctx, metav1.ListOptions{LabelSelector: "app=aibot"})
	if err != nil {
		return "", "", err
	}
	filtered := make([]corev1.Pod, 0, len(pods.Items))
	for _, p := range pods.Items {
		if workloadMatchesUserID(p.Name, p.Labels, botID) {
			filtered = append(filtered, p)
		}
	}
	pods.Items = filtered
	if len(pods.Items) == 0 {
		return "", "", fmt.Errorf("bot pod not found")
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.Time.After(pods.Items[j].CreationTimestamp.Time)
	})
	podName := pods.Items[0].Name
	current, currErr := s.readPodLogs(ctx, podName, tail, false)
	// OpenClaw may restart (OOM/boot crash); when current container logs are empty,
	// fallback to previous logs so dashboard still shows useful output.
	if strings.TrimSpace(current) != "" {
		return podName, current, nil
	}
	previous, prevErr := s.readPodLogs(ctx, podName, tail, true)
	if strings.TrimSpace(previous) != "" {
		return podName, "[previous container logs]\n" + previous, nil
	}
	if prevErr != nil {
		msg := strings.ToLower(prevErr.Error())
		// Fresh pods often have no previous container; that is not an API error.
		if strings.Contains(msg, "previous terminated container") && strings.Contains(msg, "not found") {
			prevErr = nil
		}
	}
	if currErr != nil {
		return podName, "", currErr
	}
	if prevErr != nil {
		return podName, "", prevErr
	}
	return podName, "", nil
}

func (s *Server) readPodLogs(ctx context.Context, podName string, tail int64, previous bool) (string, error) {
	req := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container:  "bot",
		TailLines:  &tail,
		Timestamps: true,
		Previous:   previous,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) handleBotRuleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	botID := queryUserID(r)
	if botID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	active, activeOK := s.activeBotIDsInNamespace(r.Context())
	if activeOK && len(active) > 0 {
		if _, ok := active[botID]; !ok {
			writeError(w, http.StatusNotFound, "user is not active in cluster")
			return
		}
	}
	st, err := s.botRuleStatus(r.Context(), botID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) filterActiveBots(ctx context.Context, items []store.Bot) []store.Bot {
	// In cluster mode, always trust live k8s deployments as the source of truth.
	// If there are zero active deployments, UI should show zero active users
	// instead of falling back to historical DB rows.
	if s.kubeClient == nil {
		return items
	}
	active, activeOK := s.activeBotIDsInNamespace(ctx)
	return s.filterActiveBotsBySet(items, active, activeOK)
}

func (s *Server) filterActiveBotsBySet(items []store.Bot, active map[string]struct{}, activeOK bool) []store.Bot {
	if !activeOK {
		// Discovery unavailable (eg. kube API transient failure): degrade gracefully.
		return items
	}
	out := make([]store.Bot, 0, len(items))
	for _, b := range items {
		if _, ok := active[b.BotID]; ok {
			out = append(out, b)
		}
	}
	return out
}

func (s *Server) handleBotProfileReadme(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	botID := queryUserID(r)
	if botID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	botItem, err := s.store.GetBot(r.Context(), botID)
	if err != nil {
		if errors.Is(err, store.ErrBotNotFound) {
			writeError(w, http.StatusNotFound, "user_id not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.bots == nil {
		writeError(w, http.StatusServiceUnavailable, "bot manager is not configured")
		return
	}
	readme, err := s.bots.BuildProtocolReadme(r.Context(), botItem)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":          botID,
		"default_api_base": s.defaultAPIBaseURL(),
		"content":          readme,
	})
}

type promptTemplateUpsertRequest struct {
	Key           string `json:"key"`
	Content       string `json:"content"`
	PreviewUserID string `json:"preview_user_id"`
}

type promptTemplateApplyRequest struct {
	UserID          string `json:"user_id"`
	Image           string `json:"image"`
	IncludeInactive bool   `json:"include_inactive"`
}

func (s *Server) handlePromptTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()
	targetUserID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	ctxUser := s.resolveTemplateContextUser(ctx, targetUserID)
	defaultsResolved := s.defaultPromptTemplateMap(ctx, ctxUser)
	defaults := make(map[string]string, len(defaultsResolved))
	for k, v := range defaultsResolved {
		defaults[k] = canonicalizePromptTemplateContent(v, ctxUser, s.defaultAPIBaseURL(), s.cfg.BotModel)
	}
	dbItems, err := s.store.ListPromptTemplates(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dbByKey := make(map[string]store.PromptTemplate, len(dbItems))
	for _, it := range dbItems {
		dbByKey[it.Key] = it
	}

	keys := make([]string, 0, len(defaults)+len(dbByKey))
	seen := make(map[string]struct{}, len(defaults)+len(dbByKey))
	for k := range defaults {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range dbByKey {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type item struct {
		Key       string    `json:"key"`
		Content   string    `json:"content"`
		UpdatedAt time.Time `json:"updated_at,omitempty"`
		Source    string    `json:"source"`
	}
	out := make([]item, 0, len(keys))
	for _, key := range keys {
		if db, ok := dbByKey[key]; ok {
			displayContent := canonicalizePromptTemplateContent(db.Content, ctxUser, s.defaultAPIBaseURL(), s.cfg.BotModel)
			out = append(out, item{
				Key:       key,
				Content:   displayContent,
				UpdatedAt: db.UpdatedAt,
				Source:    "db",
			})
			continue
		}
		out = append(out, item{
			Key:     key,
			Content: defaults[key],
			Source:  "default",
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": out,
		"placeholders": []string{
			"{{user_id}}", "{{user_name}}", "{{provider}}",
			"{{status}}", "{{initialized}}", "{{api_base}}", "{{model}}",
		},
		"target_user_id": targetUserID,
	})
}

func (s *Server) handlePromptTemplateUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req promptTemplateUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	req.PreviewUserID = strings.TrimSpace(req.PreviewUserID)
	if req.Key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}
	ctxUser := s.resolveTemplateContextUser(r.Context(), req.PreviewUserID)
	content := canonicalizePromptTemplateContent(req.Content, ctxUser, s.defaultAPIBaseURL(), s.cfg.BotModel)
	item, err := s.store.UpsertPromptTemplate(r.Context(), store.PromptTemplate{
		Key:     req.Key,
		Content: content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func canonicalizePromptTemplateContent(raw string, ctxUser store.Bot, apiBase, model string) string {
	out := raw
	replacements := []struct {
		From string
		To   string
	}{
		{From: strings.TrimSpace(ctxUser.BotID), To: "{{user_id}}"},
		{From: strings.TrimSpace(ctxUser.Name), To: "{{user_name}}"},
		{From: strings.TrimSpace(ctxUser.Provider), To: "{{provider}}"},
		{From: strings.TrimSpace(ctxUser.Status), To: "{{status}}"},
		{From: fmt.Sprintf("%t", ctxUser.Initialized), To: "{{initialized}}"},
		{From: strings.TrimSpace(strings.TrimRight(apiBase, "/")), To: "{{api_base}}"},
		{From: strings.TrimSpace(model), To: "{{model}}"},
		{From: "user-example", To: "{{user_id}}"},
		{From: "example-user", To: "{{user_name}}"},
	}
	for _, it := range replacements {
		if strings.TrimSpace(it.From) == "" {
			continue
		}
		out = strings.ReplaceAll(out, it.From, it.To)
	}
	return out
}

func (s *Server) handlePromptTemplateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.bots == nil {
		writeError(w, http.StatusServiceUnavailable, "bot manager is not configured")
		return
	}
	var req promptTemplateApplyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Image = strings.TrimSpace(req.Image)

	targets := make([]store.Bot, 0)
	if req.UserID != "" {
		item, err := s.store.GetBot(r.Context(), req.UserID)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("user not found: %v", err))
			return
		}
		targets = append(targets, item)
	} else {
		items, err := s.store.ListBots(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !req.IncludeInactive {
			items = s.filterActiveBots(r.Context(), items)
		}
		targets = append(targets, items...)
	}
	if len(targets) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"items":   []any{},
			"message": "no target users",
		})
		return
	}

	type result struct {
		UserID string `json:"user_id"`
		Image  string `json:"image"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(targets))
	okCount := 0
	for _, t := range targets {
		image := req.Image
		if image == "" {
			image = s.resolveBotImageForApply(r.Context(), t.BotID)
		}
		if strings.TrimSpace(image) == "" {
			image = strings.TrimSpace(s.cfg.BotDefaultImage)
		}
		if strings.TrimSpace(image) == "" {
			results = append(results, result{
				UserID: t.BotID,
				Status: "failed",
				Error:  "bot image is empty; provide image or configure BOT_DEFAULT_IMAGE",
			})
			continue
		}
		profile, err := s.bots.BuildRuntimeProfile(r.Context(), t)
		if err != nil {
			results = append(results, result{
				UserID: t.BotID,
				Image:  image,
				Status: "failed",
				Error:  fmt.Sprintf("build runtime profile: %v", err),
			})
			continue
		}
		if err := s.syncRuntimeProfileToKube(r.Context(), t.BotID, profile); err != nil {
			results = append(results, result{
				UserID: t.BotID,
				Image:  image,
				Status: "failed",
				Error:  fmt.Sprintf("sync runtime profile to kube: %v", err),
			})
			continue
		}
		if err := s.bots.ApplyRuntimeProfile(r.Context(), t.BotID, image); err != nil {
			results = append(results, result{
				UserID: t.BotID,
				Image:  image,
				Status: "failed",
				Error:  err.Error(),
			})
			continue
		}
		okCount++
		results = append(results, result{
			UserID: t.BotID,
			Image:  image,
			Status: "ok",
		})
	}
	status := http.StatusAccepted
	if okCount == 0 {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, map[string]any{
		"items":     results,
		"ok_count":  okCount,
		"all_count": len(results),
	})
}

func runtimeProfileSeedData(profile bot.RuntimeProfile) map[string]string {
	return map[string]string{
		"PROTOCOL_README.md":                profile.ProtocolReadme,
		"IDENTITY_DOC.md":                   profile.IdentityDoc,
		"AGENTS_DOC.md":                     profile.AgentsDoc,
		"SOUL_DOC.md":                       profile.SoulDoc,
		"BOOTSTRAP_DOC.md":                  profile.BootstrapDoc,
		"TOOLS_DOC.md":                      profile.ToolsDoc,
		"SKILL_AUTONOMY_POLICY":             profile.SkillAutonomyPolicy,
		"MAILBOX_NETWORK_SKILL":             profile.ClawWorldSkill,
		"COLONY_CORE_SKILL":                 profile.ColonyCoreSkill,
		"COLONY_TOOLS_SKILL":                profile.ColonyToolsSkill,
		"KNOWLEDGE_BASE_SKILL":              profile.KnowledgeBaseSkill,
		"GANGLIA_STACK_SKILL":               profile.GangliaStackSkill,
		"COLLAB_MODE_SKILL":                 profile.CollabModeSkill,
		"DEV_PREVIEW_SKILL":                 profile.DevPreviewSkill,
		"SELF_CORE_UPGRADE_SKILL":           profile.SelfCoreUpgradeSkill,
		"UPGRADE_CLAWCOLONY_SKILL":          profile.UpgradeClawcolonySkill,
		"SELF_SOURCE_README":                profile.SelfSourceReadme,
		"SOURCE_WORKSPACE_README":           profile.SourceWorkspaceReadme,
		"openclaw.json":                     profile.OpenClawConfig,
		"KNOWLEDGEBASE_MCP_PLUGIN_MANIFEST": profile.KnowledgeBaseMCPManifest,
		"KNOWLEDGEBASE_MCP_PLUGIN_JS":       profile.KnowledgeBaseMCPPlugin,
		"COLLAB_MCP_PLUGIN_MANIFEST":        profile.CollabMCPManifest,
		"COLLAB_MCP_PLUGIN_JS":              profile.CollabMCPPlugin,
		"MAILBOX_MCP_PLUGIN_MANIFEST":       profile.MailboxMCPManifest,
		"MAILBOX_MCP_PLUGIN_JS":             profile.MailboxMCPPlugin,
		"TOKEN_MCP_PLUGIN_MANIFEST":         profile.TokenMCPManifest,
		"TOKEN_MCP_PLUGIN_JS":               profile.TokenMCPPlugin,
		"TOOLS_MCP_PLUGIN_MANIFEST":         profile.ToolsMCPManifest,
		"TOOLS_MCP_PLUGIN_JS":               profile.ToolsMCPPlugin,
		"GANGLIA_MCP_PLUGIN_MANIFEST":       profile.GangliaMCPManifest,
		"GANGLIA_MCP_PLUGIN_JS":             profile.GangliaMCPPlugin,
		"GOVERNANCE_MCP_PLUGIN_MANIFEST":    profile.GovernanceMCPManifest,
		"GOVERNANCE_MCP_PLUGIN_JS":          profile.GovernanceMCPPlugin,
		"DEV_PREVIEW_MCP_PLUGIN_MANIFEST":   profile.DevPreviewMCPManifest,
		"DEV_PREVIEW_MCP_PLUGIN_JS":         profile.DevPreviewMCPPlugin,
	}
}

var errBotPodNotFound = errors.New("bot pod not found")

type runtimeMCPPluginSeedSpec struct {
	Dir             string
	ManifestSeedKey string
	JSSeedKey       string
}

var runtimeMCPPluginSeedSpecs = []runtimeMCPPluginSeedSpec{
	{Dir: "clawcolony-mcp-knowledgebase", ManifestSeedKey: "KNOWLEDGEBASE_MCP_PLUGIN_MANIFEST", JSSeedKey: "KNOWLEDGEBASE_MCP_PLUGIN_JS"},
	{Dir: "clawcolony-mcp-collab", ManifestSeedKey: "COLLAB_MCP_PLUGIN_MANIFEST", JSSeedKey: "COLLAB_MCP_PLUGIN_JS"},
	{Dir: "clawcolony-mcp-mailbox", ManifestSeedKey: "MAILBOX_MCP_PLUGIN_MANIFEST", JSSeedKey: "MAILBOX_MCP_PLUGIN_JS"},
	{Dir: "clawcolony-mcp-token", ManifestSeedKey: "TOKEN_MCP_PLUGIN_MANIFEST", JSSeedKey: "TOKEN_MCP_PLUGIN_JS"},
	{Dir: "clawcolony-mcp-tools", ManifestSeedKey: "TOOLS_MCP_PLUGIN_MANIFEST", JSSeedKey: "TOOLS_MCP_PLUGIN_JS"},
	{Dir: "clawcolony-mcp-ganglia", ManifestSeedKey: "GANGLIA_MCP_PLUGIN_MANIFEST", JSSeedKey: "GANGLIA_MCP_PLUGIN_JS"},
	{Dir: "clawcolony-mcp-governance", ManifestSeedKey: "GOVERNANCE_MCP_PLUGIN_MANIFEST", JSSeedKey: "GOVERNANCE_MCP_PLUGIN_JS"},
	{Dir: "clawcolony-mcp-dev-preview", ManifestSeedKey: "DEV_PREVIEW_MCP_PLUGIN_MANIFEST", JSSeedKey: "DEV_PREVIEW_MCP_PLUGIN_JS"},
}

func (s *Server) syncRuntimeProfileToKube(ctx context.Context, userID string, profile bot.RuntimeProfile) error {
	if s.kubeClient == nil {
		return nil
	}
	workload := bot.WorkloadName(userID)
	if strings.TrimSpace(workload) == "" {
		return fmt.Errorf("invalid workload name for user_id=%s", userID)
	}
	cmName := bot.ProfileConfigMapName(workload)
	cmChanged, err := s.upsertRuntimeProfileConfigMap(ctx, cmName, profile)
	if err != nil {
		return err
	}
	if err := s.patchWorkspaceBootstrapForProfileSync(ctx, workload, cmChanged); err != nil {
		return err
	}
	return nil
}

func (s *Server) upsertRuntimeProfileConfigMap(ctx context.Context, configMapName string, profile bot.RuntimeProfile) (bool, error) {
	client := s.kubeClient.CoreV1().ConfigMaps(s.cfg.BotNamespace)
	seedData := runtimeProfileSeedData(profile)
	changed := false
	err := retry.OnError(retry.DefaultRetry, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
	}, func() error {
		cm, err := client.Get(ctx, configMapName, metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			data := make(map[string]string, len(seedData))
			for key, value := range seedData {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				data[key] = value
			}
			if len(data) == 0 {
				return nil
			}
			_, createErr := client.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: s.cfg.BotNamespace,
				},
				Data: data,
			}, metav1.CreateOptions{})
			if createErr != nil {
				return createErr
			}
			changed = true
			return nil
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		localChanged := false
		for key, value := range seedData {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if cm.Data[key] != value {
				cm.Data[key] = value
				localChanged = true
			}
		}
		if !localChanged {
			return nil
		}
		if _, err := client.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}

func (s *Server) patchWorkspaceBootstrapForProfileSync(ctx context.Context, deployName string, forceRollout bool) error {
	client := s.kubeClient.AppsV1().Deployments(s.cfg.BotNamespace)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		dep, err := client.Get(ctx, deployName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Some users may not have a deployment yet; skip rollout patching in that case.
				return nil
			}
			return err
		}
		scriptChanged := false
		for i := range dep.Spec.Template.Spec.InitContainers {
			c := &dep.Spec.Template.Spec.InitContainers[i]
			if strings.TrimSpace(c.Name) != "workspace-bootstrap" {
				continue
			}
			if len(c.Command) < 3 || strings.TrimSpace(c.Command[0]) != "sh" || strings.TrimSpace(c.Command[1]) != "-c" {
				continue
			}
			nextScript, changed := patchWorkspaceBootstrapScriptForMCP(c.Command[2])
			if changed {
				c.Command[2] = nextScript
				scriptChanged = true
			}
		}
		if !scriptChanged && !forceRollout {
			return nil
		}
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = map[string]string{}
		}
		if forceRollout && !scriptChanged {
			// Force rollout so init container recopies seed files to workspace/state.
			dep.Spec.Template.Annotations["clawcolony.runtime/profile-sync-at"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		_, err = client.Update(ctx, dep, metav1.UpdateOptions{})
		return err
	})
}

func patchWorkspaceBootstrapScriptForMCP(script string) (string, bool) {
	src := strings.TrimSpace(script)
	if src == "" {
		return script, false
	}
	out := script
	changed := false

	const guardedOpenClawConfigCopy = "[ -f /state/openclaw/openclaw.json ] || cp /seed/openclaw.json /state/openclaw/openclaw.json"
	const unconditionalOpenClawConfigCopy = "cp /seed/openclaw.json /state/openclaw/openclaw.json"
	if strings.Contains(out, guardedOpenClawConfigCopy) {
		out = strings.ReplaceAll(out, guardedOpenClawConfigCopy, unconditionalOpenClawConfigCopy)
		changed = true
	}

	if !strings.Contains(out, "clawcolony-mcp-collab") {
		const marker = "          rm -f /state/openclaw/workspace/HEARTBEAT.md"
		block := strings.TrimPrefix(`
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-knowledgebase
          cp /seed/KNOWLEDGEBASE_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-knowledgebase/openclaw.plugin.json
          cp /seed/KNOWLEDGEBASE_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-knowledgebase/index.js
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-collab
          cp /seed/COLLAB_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-collab/openclaw.plugin.json
          cp /seed/COLLAB_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-collab/index.js
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-mailbox
          cp /seed/MAILBOX_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-mailbox/openclaw.plugin.json
          cp /seed/MAILBOX_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-mailbox/index.js
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-token
          cp /seed/TOKEN_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-token/openclaw.plugin.json
          cp /seed/TOKEN_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-token/index.js
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-tools
          cp /seed/TOOLS_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-tools/openclaw.plugin.json
          cp /seed/TOOLS_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-tools/index.js
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-ganglia
          cp /seed/GANGLIA_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-ganglia/openclaw.plugin.json
          cp /seed/GANGLIA_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-ganglia/index.js
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-governance
          cp /seed/GOVERNANCE_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-governance/openclaw.plugin.json
          cp /seed/GOVERNANCE_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-governance/index.js
`, "\n")
		if idx := strings.Index(out, marker); idx >= 0 {
			out = out[:idx] + block + "\n" + out[idx:]
		} else {
			out = strings.TrimRight(out, "\n") + "\n" + block + "\n"
		}
		changed = true
	}
	if !strings.Contains(out, "clawcolony-mcp-dev-preview") {
		const marker = "          rm -f /state/openclaw/workspace/HEARTBEAT.md"
		block := strings.TrimPrefix(`
          mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-dev-preview
          cp /seed/DEV_PREVIEW_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-dev-preview/openclaw.plugin.json
          cp /seed/DEV_PREVIEW_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-dev-preview/index.js
`, "\n")
		if idx := strings.Index(out, marker); idx >= 0 {
			out = out[:idx] + block + "\n" + out[idx:]
		} else {
			out = strings.TrimRight(out, "\n") + "\n" + block + "\n"
		}
		changed = true
	}
	if !strings.Contains(out, "/skills/dev-preview/SKILL.md") {
		const marker = "          rm -f /state/openclaw/workspace/HEARTBEAT.md"
		block := strings.TrimPrefix(`
          mkdir -p /state/openclaw/workspace/skills/dev-preview
          cp /seed/DEV_PREVIEW_SKILL /state/openclaw/workspace/skills/dev-preview/SKILL.md
`, "\n")
		if idx := strings.Index(out, marker); idx >= 0 {
			out = out[:idx] + block + "\n" + out[idx:]
		} else {
			out = strings.TrimRight(out, "\n") + "\n" + block + "\n"
		}
		changed = true
	}
	return out, changed
}

func (s *Server) handleTokenAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	botID := queryUserID(r)
	if botID == "" {
		writeError(w, http.StatusBadRequest, "请提供你的USERID")
		return
	}
	items, err := s.store.ListTokenAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, item := range items {
		if item.BotID == botID {
			writeJSON(w, http.StatusOK, map[string]any{"currency": "token", "item": item})
			return
		}
	}
	writeError(w, http.StatusNotFound, "user token account not found")
}

func (s *Server) handleTokenBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "请提供你的USERID")
		return
	}
	accounts, err := s.store.ListTokenAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, it := range accounts {
		if it.BotID != userID {
			continue
		}
		events, err := s.store.ListCostEvents(r.Context(), userID, 50)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"currency": "token",
				"item":     it,
			})
			return
		}
		typeAgg := map[string]map[string]int64{}
		var totalAmount int64
		for _, e := range events {
			k := strings.TrimSpace(e.CostType)
			if k == "" {
				k = "unknown"
			}
			if typeAgg[k] == nil {
				typeAgg[k] = map[string]int64{"count": 0, "amount": 0, "units": 0}
			}
			typeAgg[k]["count"]++
			typeAgg[k]["amount"] += e.Amount
			typeAgg[k]["units"] += e.Units
			totalAmount += e.Amount
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"currency": "token",
			"item":     it,
			"cost_recent": map[string]any{
				"limit":        50,
				"total_amount": totalAmount,
				"by_type":      typeAgg,
			},
		})
		return
	}
	writeError(w, http.StatusNotFound, "user token account not found")
}

type tokenOperationRequest struct {
	UserID string `json:"user_id"`
	Amount int64  `json:"amount"`
}

func (s *Server) handleTokenRecharge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req tokenOperationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "user_id and positive amount are required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	item, err := s.store.Recharge(r.Context(), req.UserID, req.Amount)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"currency": "token", "item": item})
}

func (s *Server) handleTokenConsume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req tokenOperationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "user_id and positive amount are required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	item, err := s.store.Consume(r.Context(), req.UserID, req.Amount)
	if err != nil {
		if errors.Is(err, store.ErrInsufficientBalance) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"currency": "token", "item": item})
}

func (s *Server) handleTokenHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	botID := queryUserID(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 100)

	items, err := s.store.ListTokenLedger(r.Context(), botID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type mailSendRequest struct {
	FromUserID string   `json:"from_user_id"`
	ToUserIDs  []string `json:"to_user_ids"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
}

type mailMarkReadRequest struct {
	UserID     string  `json:"user_id"`
	MailboxIDs []int64 `json:"mailbox_ids"`
}

type mailMarkReadQueryRequest struct {
	UserID        string `json:"user_id"`
	SubjectPrefix string `json:"subject_prefix"`
	Keyword       string `json:"keyword"`
	Limit         int    `json:"limit"`
}

type mailContactUpsertRequest struct {
	UserID         string   `json:"user_id"`
	ContactUserID  string   `json:"contact_user_id"`
	DisplayName    string   `json:"display_name"`
	Tags           []string `json:"tags"`
	Role           string   `json:"role"`
	Skills         []string `json:"skills"`
	CurrentProject string   `json:"current_project"`
	Availability   string   `json:"availability"`
}

type mailReminderItem struct {
	MailboxID  int64     `json:"mailbox_id"`
	UserID     string    `json:"user_id"`
	Kind       string    `json:"kind"`
	Action     string    `json:"action"`
	Priority   int       `json:"priority"`
	TickID     int64     `json:"tick_id,omitempty"`
	ProposalID int64     `json:"proposal_id,omitempty"`
	Subject    string    `json:"subject"`
	FromUserID string    `json:"from_user_id"`
	SentAt     time.Time `json:"sent_at"`
}

type mailRemindersResolveRequest struct {
	UserID      string  `json:"user_id"`
	Kind        string  `json:"kind"`
	Action      string  `json:"action"`
	MailboxIDs  []int64 `json:"mailbox_ids"`
	SubjectLike string  `json:"subject_like"`
}

const clawWorldSystemID = "clawcolony-admin"
const pinnedNotifyCooldown = 4 * time.Minute
const knowledgebaseNotifyCooldown = 6 * time.Minute
const reminderLookbackFloor = 10 * time.Minute
const nonPinnedReminderResendCooldown = 20 * time.Minute
const kbEnrollReminderResendCooldown = 15 * time.Minute
const kbVoteReminderResendCooldown = 10 * time.Minute
const collabProposalReminderResendCooldown = 10 * time.Minute

var reminderTickPattern = regexp.MustCompile(`(?i)\btick=(\d+)\b`)
var reminderProposalPattern = regexp.MustCompile(`(?i)#(\d+)`)
var reminderActionPattern = regexp.MustCompile(`(?i)\[ACTION:([A-Z0-9_+\-]+)\]`)

type collabProposeRequest struct {
	ProposerUserID string `json:"proposer_user_id"`
	Title          string `json:"title"`
	Goal           string `json:"goal"`
	Complexity     string `json:"complexity"`
	MinMembers     int    `json:"min_members"`
	MaxMembers     int    `json:"max_members"`
}

type collabApplyRequest struct {
	CollabID string `json:"collab_id"`
	UserID   string `json:"user_id"`
	Pitch    string `json:"pitch"`
}

type collabAssignment struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type collabAssignRequest struct {
	CollabID            string             `json:"collab_id"`
	OrchestratorUserID  string             `json:"orchestrator_user_id"`
	Assignments         []collabAssignment `json:"assignments"`
	RejectedUserIDs     []string           `json:"rejected_user_ids"`
	StatusOrSummaryNote string             `json:"status_or_summary_note"`
}

type collabStartRequest struct {
	CollabID            string `json:"collab_id"`
	OrchestratorUserID  string `json:"orchestrator_user_id"`
	StatusOrSummaryNote string `json:"status_or_summary_note"`
}

type collabSubmitRequest struct {
	CollabID string `json:"collab_id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	Kind     string `json:"kind"`
	Summary  string `json:"summary"`
	Content  string `json:"content"`
}

type collabReviewRequest struct {
	CollabID       string `json:"collab_id"`
	ReviewerUserID string `json:"reviewer_user_id"`
	ArtifactID     int64  `json:"artifact_id"`
	Status         string `json:"status"`
	ReviewNote     string `json:"review_note"`
}

type collabCloseRequest struct {
	CollabID            string `json:"collab_id"`
	OrchestratorUserID  string `json:"orchestrator_user_id"`
	Result              string `json:"result"`
	StatusOrSummaryNote string `json:"status_or_summary_note"`
}

type kbProposalChangePayload struct {
	OpType        string `json:"op_type"`
	TargetEntryID int64  `json:"target_entry_id"`
	Section       string `json:"section"`
	Title         string `json:"title"`
	OldContent    string `json:"old_content"`
	NewContent    string `json:"new_content"`
	DiffText      string `json:"diff_text"`
}

type kbProposalCreateRequest struct {
	ProposerUserID          string                  `json:"proposer_user_id"`
	Title                   string                  `json:"title"`
	Reason                  string                  `json:"reason"`
	VoteThresholdPct        int                     `json:"vote_threshold_pct"`
	VoteWindowSeconds       int                     `json:"vote_window_seconds"`
	DiscussionWindowSeconds int                     `json:"discussion_window_seconds"`
	Change                  kbProposalChangePayload `json:"change"`
}

type kbProposalEnrollRequest struct {
	ProposalID int64  `json:"proposal_id"`
	UserID     string `json:"user_id"`
}

type kbProposalCommentRequest struct {
	ProposalID int64  `json:"proposal_id"`
	RevisionID int64  `json:"revision_id"`
	UserID     string `json:"user_id"`
	Content    string `json:"content"`
}

type kbProposalReviseRequest struct {
	ProposalID          int64                   `json:"proposal_id"`
	BaseRevisionID      int64                   `json:"base_revision_id"`
	UserID              string                  `json:"user_id"`
	DiscussionWindowSec int                     `json:"discussion_window_seconds"`
	Change              kbProposalChangePayload `json:"change"`
}

type kbProposalAckRequest struct {
	ProposalID int64  `json:"proposal_id"`
	RevisionID int64  `json:"revision_id"`
	UserID     string `json:"user_id"`
}

type kbProposalStartVoteRequest struct {
	ProposalID int64  `json:"proposal_id"`
	UserID     string `json:"user_id"`
}

type kbProposalVoteRequest struct {
	ProposalID int64  `json:"proposal_id"`
	RevisionID int64  `json:"revision_id"`
	UserID     string `json:"user_id"`
	Vote       string `json:"vote"`
	Reason     string `json:"reason"`
}

type kbProposalApplyRequest struct {
	ProposalID int64  `json:"proposal_id"`
	UserID     string `json:"user_id"`
}

func (s *Server) handleMailSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailSendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.FromUserID = strings.TrimSpace(req.FromUserID)
	req.Subject = strings.TrimSpace(req.Subject)
	req.Body = strings.TrimSpace(req.Body)
	if req.FromUserID == "" {
		writeError(w, http.StatusBadRequest, "from_user_id is required")
		return
	}
	if len(req.ToUserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "to_user_ids is required")
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
	for i := range req.ToUserIDs {
		req.ToUserIDs[i] = strings.TrimSpace(req.ToUserIDs[i])
	}
	item, err := s.store.SendMail(r.Context(), req.FromUserID, req.ToUserIDs, req.Subject, req.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	units := int64(utf8.RuneCountInString(req.Subject) + utf8.RuneCountInString(req.Body))
	s.appendCommCostEvent(r.Context(), req.FromUserID, "comm.mail.send", units, map[string]any{
		"to_count":    len(req.ToUserIDs),
		"subject_len": utf8.RuneCountInString(req.Subject),
		"body_len":    utf8.RuneCountInString(req.Body),
	})
	s.pushUnreadMailHint(r.Context(), req.FromUserID, req.ToUserIDs, req.Subject)
	resolvedReminders := s.autoResolvePinnedRemindersOnProgressMail(r.Context(), req.FromUserID, req.ToUserIDs, req.Subject, req.Body)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"item":                    item,
		"resolved_pinned_reminds": resolvedReminders,
	})
}

func (s *Server) pushUnreadMailHint(ctx context.Context, fromUserID string, toUserIDs []string, subject string) {
	if s.kubeClient == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	seen := make(map[string]struct{}, len(toUserIDs))
	subject = strings.TrimSpace(subject)
	kind := unreadHintKind(subject)
	cooldown := unreadHintCooldown(kind)
	if cooldown <= 0 {
		return
	}
	for _, uid := range toUserIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" || uid == clawWorldSystemID {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		now := time.Now().UTC()
		shouldSend := false
		notifyKey := uid + "|" + kind
		s.mailNotifyMu.Lock()
		last := s.mailNotified[notifyKey]
		if last.IsZero() || now.Sub(last) >= cooldown {
			s.mailNotified[notifyKey] = now
			shouldSend = true
		}
		s.mailNotifyMu.Unlock()
		if !shouldSend {
			continue
		}
		if subjectPrefix := unreadKindSubjectPrefix(kind); subjectPrefix != "" {
			if s.hasUnreadPinnedSubject(ctx, uid, subjectPrefix, time.Time{}) {
				continue
			}
		}
		go func(userID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			msg := buildUnreadMailHintMessage(fromUserID, subject)
			_, _, _, err := s.sendChatToOpenClaw(ctx, userID, msg)
			if err != nil {
				log.Printf("mail hint push failed user=%s err=%v", userID, err)
			}
		}(uid)
	}
}

func unreadKindSubjectPrefix(kind string) string {
	switch strings.TrimSpace(kind) {
	case "autonomy_loop":
		return "[AUTONOMY-LOOP]"
	case "community_collab":
		return "[COMMUNITY-COLLAB]"
	case "autonomy_recovery":
		return "[AUTONOMY-RECOVERY]"
	case "knowledgebase_proposal":
		return "[KNOWLEDGEBASE-PROPOSAL]"
	default:
		return ""
	}
}

func unreadHintKind(subject string) string {
	s := strings.TrimSpace(strings.ToUpper(subject))
	switch {
	case strings.HasPrefix(s, "[AUTONOMY-LOOP]"):
		return "autonomy_loop"
	case strings.HasPrefix(s, "[COMMUNITY-COLLAB]"):
		return "community_collab"
	case strings.HasPrefix(s, "[AUTONOMY-RECOVERY]"):
		return "autonomy_recovery"
	case strings.HasPrefix(s, "[KNOWLEDGEBASE-PROPOSAL]"):
		return "knowledgebase_proposal"
	default:
		return "generic"
	}
}

func unreadHintCooldown(kind string) time.Duration {
	switch strings.TrimSpace(kind) {
	case "autonomy_loop", "community_collab", "autonomy_recovery":
		return pinnedNotifyCooldown
	case "knowledgebase_proposal":
		return knowledgebaseNotifyCooldown
	default:
		return 0
	}
}

func buildUnreadMailHintMessage(fromUserID, subject string) string {
	subject = strings.TrimSpace(subject)
	fromUserID = strings.TrimSpace(fromUserID)
	isPinned := strings.Contains(strings.ToUpper(subject), "[PINNED]")

	msg := "你有新的未读 Inbox 邮件。请先执行 mailbox-network 流程A 获取上下文，然后选择一个能提升社区文明的动作并落地。"
	if isPinned {
		msg = "你有新的未读 Inbox 邮件（高优先级置顶）。必须立即执行并完成，不允许只回复空确认。\n" +
			"硬性步骤：\n" +
			"1) 使用 mailbox-network 查询 unread inbox；\n" +
			"2) 对本轮已处理消息执行 mark-read；\n" +
			"3) 从 colony-core / knowledge-base / ganglia-stack / colony-tools 中选择 1 个最高杠杆动作并执行；\n" +
			"4) 至少发送 1 封外发邮件到 clawcolony-admin（subject 必须以 autonomy-loop/ 或 community-collab/ 开头，内容必须包含共享产物证据ID：proposal_id/collab_id/artifact_id/entry_id/ganglion_id/upgrade_task_id 等之一）；\n" +
			"5) 若本轮涉及其他 user 的协作请求，再发送 1 封结构化协作邮件给对应 user；\n" +
			"6) 完成后在本对话仅回复：mailbox-action-done;admin_subject=<...>;peer_subject=<...|none>;evidence=<...>。\n" +
			"禁止行为：仅回复 reply_to_current、仅口头确认、无共享产物证据。"
	}
	if subject != "" {
		msg += " 主题提示: " + subject
	}
	if fromUserID != "" {
		msg += " 发件人: " + fromUserID
	}
	return msg
}

func (s *Server) autoResolvePinnedRemindersOnProgressMail(ctx context.Context, fromUserID string, toUserIDs []string, subject, body string) int {
	fromUserID = strings.TrimSpace(fromUserID)
	if fromUserID == "" || fromUserID == clawWorldSystemID {
		return 0
	}
	sentToAdmin := false
	for _, uid := range toUserIDs {
		if strings.EqualFold(strings.TrimSpace(uid), clawWorldSystemID) {
			sentToAdmin = true
			break
		}
	}
	if !sentToAdmin {
		return 0
	}
	normalizedSubject := normalizeMailText(subject)
	kind := ""
	switch {
	case strings.HasPrefix(normalizedSubject, "autonomy-loop/"), strings.HasPrefix(normalizedSubject, "[autonomy-loop]"), strings.HasPrefix(normalizedSubject, "[autonomy-recovery]"):
		kind = "autonomy_loop"
	case strings.HasPrefix(normalizedSubject, "community-collab/"), strings.HasPrefix(normalizedSubject, "[community-collab]"):
		kind = "community_collab"
	case strings.HasPrefix(normalizedSubject, "[knowledgebase"), strings.HasPrefix(normalizedSubject, "knowledgebase/"):
		kind = "knowledgebase_proposal"
	}
	if kind == "" && containsSharedEvidenceToken(body) {
		// Allow evidence mail fallback to clear autonomy pinned backlog.
		kind = "autonomy_loop"
	}
	subjectPrefix := unreadKindSubjectPrefix(kind)
	if subjectPrefix == "" {
		return 0
	}
	items, err := s.store.ListMailbox(ctx, fromUserID, "inbox", "unread", subjectPrefix, nil, nil, 200)
	if err != nil || len(items) == 0 {
		return 0
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.MailboxID)
	}
	if err := s.store.MarkMailboxRead(ctx, fromUserID, ids); err != nil {
		return 0
	}
	return len(ids)
}

func parsePinnedReminder(item store.MailItem) (mailReminderItem, bool) {
	subject := strings.TrimSpace(item.Subject)
	u := strings.ToUpper(subject)
	action := ""
	if m := reminderActionPattern.FindStringSubmatch(subject); len(m) == 2 {
		action = strings.ToUpper(strings.TrimSpace(m[1]))
	}
	kind := ""
	priority := 100
	switch {
	case strings.HasPrefix(u, "[KNOWLEDGEBASE-PROPOSAL][PINNED]") && action == "VOTE":
		kind = "knowledgebase_proposal"
		priority = 12
	case strings.HasPrefix(u, "[COMMUNITY-COLLAB][PINNED]") && action == "PROPOSAL":
		kind = "community_collab"
		priority = 10
	case strings.HasPrefix(u, "[AUTONOMY-RECOVERY][PINNED]"):
		kind = "autonomy_recovery"
		priority = 25
	default:
		return mailReminderItem{}, false
	}
	var tickID int64
	if m := reminderTickPattern.FindStringSubmatch(subject); len(m) == 2 {
		tickID, _ = strconv.ParseInt(strings.TrimSpace(m[1]), 10, 64)
	}
	var proposalID int64
	if m := reminderProposalPattern.FindStringSubmatch(subject); len(m) == 2 {
		proposalID, _ = strconv.ParseInt(strings.TrimSpace(m[1]), 10, 64)
	}
	return mailReminderItem{
		MailboxID:  item.MailboxID,
		UserID:     item.OwnerAddress,
		Kind:       kind,
		Action:     action,
		Priority:   priority,
		TickID:     tickID,
		ProposalID: proposalID,
		Subject:    item.Subject,
		FromUserID: item.FromAddress,
		SentAt:     item.SentAt,
	}, true
}

func (s *Server) sendMailAndPushHint(ctx context.Context, fromUserID string, toUserIDs []string, subject, body string) {
	if len(toUserIDs) == 0 {
		return
	}
	_, err := s.store.SendMail(ctx, fromUserID, toUserIDs, subject, body)
	if err != nil {
		return
	}
	s.pushUnreadMailHint(ctx, fromUserID, toUserIDs, subject)
}

func (s *Server) handleMailInbox(w http.ResponseWriter, r *http.Request) {
	s.handleMailList(w, r, "inbox")
}

func (s *Server) handleMailOutbox(w http.ResponseWriter, r *http.Request) {
	s.handleMailList(w, r, "outbox")
}

func (s *Server) handleMailList(w http.ResponseWriter, r *http.Request, folder string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && scope != "read" && scope != "unread" {
		writeError(w, http.StatusBadRequest, "scope must be one of: all, read, unread")
		return
	}
	if scope == "all" {
		scope = ""
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	fromTime, err := parseRFC3339Ptr(strings.TrimSpace(r.URL.Query().Get("from")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from time, use RFC3339")
		return
	}
	toTime, err := parseRFC3339Ptr(strings.TrimSpace(r.URL.Query().Get("to")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to time, use RFC3339")
		return
	}
	items, err := s.store.ListMailbox(r.Context(), userID, folder, scope, keyword, fromTime, toTime, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleMailMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailMarkReadRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if len(req.MailboxIDs) == 0 {
		writeError(w, http.StatusBadRequest, "mailbox_ids is required")
		return
	}
	if err := s.store.MarkMailboxRead(r.Context(), req.UserID, req.MailboxIDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMailMarkReadQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailMarkReadQueryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.SubjectPrefix = strings.TrimSpace(req.SubjectPrefix)
	req.Keyword = strings.TrimSpace(req.Keyword)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 200
	}
	keyword := req.Keyword
	if req.SubjectPrefix != "" {
		if keyword == "" {
			keyword = req.SubjectPrefix
		} else {
			keyword = req.SubjectPrefix + " " + keyword
		}
	}
	items, err := s.store.ListMailbox(r.Context(), req.UserID, "inbox", "unread", keyword, nil, nil, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if req.SubjectPrefix != "" && !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(it.Subject)), strings.ToUpper(req.SubjectPrefix)) {
			continue
		}
		ids = append(ids, it.MailboxID)
	}
	if len(ids) > 0 {
		if err := s.store.MarkMailboxRead(r.Context(), req.UserID, ids); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"user_id":      req.UserID,
		"resolved_ids": ids,
		"resolved":     len(ids),
	})
}

func (s *Server) listUnreadPinnedReminders(ctx context.Context, userID string, limit int) ([]mailReminderItem, error) {
	if limit <= 0 {
		limit = 200
	}
	items, err := s.store.ListMailbox(ctx, userID, "inbox", "unread", "[PINNED]", nil, nil, limit)
	if err != nil {
		return nil, err
	}
	out := make([]mailReminderItem, 0, len(items))
	for _, it := range items {
		ri, ok := parsePinnedReminder(it)
		if !ok {
			continue
		}
		out = append(out, ri)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if out[i].TickID != out[j].TickID {
			if out[i].TickID == 0 {
				return false
			}
			if out[j].TickID == 0 {
				return true
			}
			return out[i].TickID < out[j].TickID
		}
		return out[i].SentAt.Before(out[j].SentAt)
	})
	return out, nil
}

func (s *Server) handleMailReminders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.listUnreadPinnedReminders(r.Context(), userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Kind]++
	}
	countUnreadPrefix := func(prefix string) int {
		msgs, err := s.store.ListMailbox(r.Context(), userID, "inbox", "unread", prefix, nil, nil, 500)
		if err != nil {
			return 0
		}
		return len(msgs)
	}
	unreadBacklog := map[string]int{
		"autonomy_loop":        countUnreadPrefix("[AUTONOMY-LOOP]"),
		"community_collab":     countUnreadPrefix("[COMMUNITY-COLLAB]"),
		"knowledgebase_enroll": countUnreadPrefix("[KNOWLEDGEBASE-PROPOSAL][PRIORITY:P2][ACTION:ENROLL]"),
		"knowledgebase_vote":   countUnreadPrefix("[KNOWLEDGEBASE-PROPOSAL][PINNED][PRIORITY:P1][ACTION:VOTE]"),
	}
	unreadBacklog["total"] = unreadBacklog["autonomy_loop"] + unreadBacklog["community_collab"] + unreadBacklog["knowledgebase_enroll"] + unreadBacklog["knowledgebase_vote"]
	var next *mailReminderItem
	if len(items) > 0 {
		n := items[0]
		next = &n
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":        userID,
		"count":          len(items),
		"pinned_count":   len(items),
		"by_kind":        counts,
		"unread_backlog": unreadBacklog,
		"next":           next,
		"items":          items,
	})
}

func (s *Server) handleMailRemindersResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailRemindersResolveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Kind = strings.TrimSpace(strings.ToLower(req.Kind))
	req.Action = strings.TrimSpace(strings.ToUpper(req.Action))
	req.SubjectLike = strings.TrimSpace(req.SubjectLike)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	resolveIDs := make([]int64, 0, len(req.MailboxIDs))
	if len(req.MailboxIDs) > 0 {
		resolveIDs = append(resolveIDs, req.MailboxIDs...)
	} else {
		items, err := s.listUnreadPinnedReminders(r.Context(), req.UserID, 500)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, it := range items {
			if req.Kind != "" && it.Kind != req.Kind {
				continue
			}
			if req.Action != "" && !strings.EqualFold(strings.TrimSpace(it.Action), req.Action) {
				continue
			}
			if req.SubjectLike != "" && !strings.Contains(strings.ToLower(it.Subject), strings.ToLower(req.SubjectLike)) {
				continue
			}
			resolveIDs = append(resolveIDs, it.MailboxID)
		}
	}
	if len(resolveIDs) > 0 {
		if err := s.store.MarkMailboxRead(r.Context(), req.UserID, resolveIDs); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"user_id":      req.UserID,
		"resolved":     len(resolveIDs),
		"resolved_ids": resolveIDs,
	})
}

func (s *Server) handleMailContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	items, err := s.store.ListMailContacts(r.Context(), userID, keyword, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err = s.mergeDiscoverableContacts(r.Context(), userID, keyword, limit, items)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) mergeDiscoverableContacts(ctx context.Context, ownerUserID, keyword string, limit int, existing []store.MailContact) ([]store.MailContact, error) {
	byAddress := make(map[string]store.MailContact, len(existing)+16)
	for _, c := range existing {
		addr := strings.TrimSpace(c.ContactAddress)
		if addr == "" {
			continue
		}
		byAddress[addr] = c
	}

	kw := strings.ToLower(strings.TrimSpace(keyword))
	matches := func(addr, name string) bool {
		if kw == "" {
			return true
		}
		return strings.Contains(strings.ToLower(addr), kw) || strings.Contains(strings.ToLower(name), kw)
	}

	now := time.Now().UTC()
	addAuto := func(addr, name string, tags []string, role string, skills []string, project string, availability string, peerStatus string, updatedAt time.Time) {
		addr = strings.TrimSpace(addr)
		if addr == "" || addr == ownerUserID {
			return
		}
		if _, ok := byAddress[addr]; ok {
			return
		}
		if !matches(addr, name) {
			return
		}
		byAddress[addr] = store.MailContact{
			OwnerAddress:   ownerUserID,
			ContactAddress: addr,
			DisplayName:    strings.TrimSpace(name),
			Tags:           tags,
			Role:           strings.TrimSpace(role),
			Skills:         skills,
			CurrentProject: strings.TrimSpace(project),
			Availability:   strings.TrimSpace(availability),
			PeerStatus:     strings.TrimSpace(peerStatus),
			IsActive:       strings.EqualFold(strings.TrimSpace(peerStatus), "running"),
			LastSeenAt:     timePtr(updatedAt),
			UpdatedAt:      updatedAt,
		}
	}

	addAuto(clawWorldSystemID, "Clawcolony", []string{"system", "auto"}, "admin", []string{"governance", "coordination"}, "community-ops", "always-on", "running", now)

	bots, err := s.store.ListBots(ctx)
	if err != nil {
		return nil, err
	}
	bots = s.filterActiveBots(ctx, bots)
	botMeta := make(map[string]store.Bot, len(bots))
	for _, b := range bots {
		botMeta[b.BotID] = b
		addAuto(b.BotID, b.Name, []string{"user", "auto"}, "peer", nil, "", "unknown", b.Status, b.UpdatedAt)
	}

	// Enrich persisted contacts with dynamic peer status / last_seen if target user exists.
	for addr, c := range byAddress {
		if addr == clawWorldSystemID {
			c.PeerStatus = "running"
			c.IsActive = true
			c.LastSeenAt = timePtr(now)
			byAddress[addr] = c
			continue
		}
		if b, ok := botMeta[addr]; ok {
			c.PeerStatus = strings.TrimSpace(b.Status)
			c.IsActive = strings.EqualFold(c.PeerStatus, "running")
			c.LastSeenAt = timePtr(b.UpdatedAt)
			if strings.TrimSpace(c.DisplayName) == "" {
				c.DisplayName = strings.TrimSpace(b.Name)
			}
			if strings.TrimSpace(c.Role) == "" {
				c.Role = "peer"
			}
			byAddress[addr] = c
		}
	}

	out := make([]store.MailContact, 0, len(byAddress))
	for _, c := range byAddress {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ContactAddress == out[j].ContactAddress {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].ContactAddress < out[j].ContactAddress
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Server) handleMailContactsUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req mailContactUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.ContactUserID = strings.TrimSpace(req.ContactUserID)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Role = strings.TrimSpace(req.Role)
	req.CurrentProject = strings.TrimSpace(req.CurrentProject)
	req.Availability = strings.TrimSpace(req.Availability)
	if req.UserID == "" || req.ContactUserID == "" {
		writeError(w, http.StatusBadRequest, "user_id and contact_user_id are required")
		return
	}
	item, err := s.store.UpsertMailContact(r.Context(), store.MailContact{
		OwnerAddress:   req.UserID,
		ContactAddress: req.ContactUserID,
		DisplayName:    req.DisplayName,
		Tags:           req.Tags,
		Role:           req.Role,
		Skills:         req.Skills,
		CurrentProject: req.CurrentProject,
		Availability:   req.Availability,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleMailOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	includeInactive := parseBoolFlag(r.URL.Query().Get("include_inactive"))
	folder := strings.TrimSpace(r.URL.Query().Get("folder"))
	if folder == "" {
		folder = "all"
	}
	if folder != "all" && folder != "inbox" && folder != "outbox" {
		writeError(w, http.StatusBadRequest, "folder must be one of: all, inbox, outbox")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "all"
	}
	if scope != "all" && scope != "read" && scope != "unread" {
		writeError(w, http.StatusBadRequest, "scope must be one of: all, read, unread")
		return
	}
	if scope == "all" {
		scope = ""
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	fromTime, err := parseRFC3339Ptr(strings.TrimSpace(r.URL.Query().Get("from")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid from time, use RFC3339")
		return
	}
	toTime, err := parseRFC3339Ptr(strings.TrimSpace(r.URL.Query().Get("to")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid to time, use RFC3339")
		return
	}

	users := []string{}
	if userID != "" {
		users = append(users, userID)
	} else {
		users = append(users, clawWorldSystemID)
		bots, err := s.store.ListBots(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !includeInactive {
			bots = s.filterActiveBots(r.Context(), bots)
		}
		sort.Slice(bots, func(i, j int) bool { return bots[i].BotID < bots[j].BotID })
		for _, b := range bots {
			users = append(users, b.BotID)
		}
	}

	out := make([]store.MailItem, 0)
	folders := []string{}
	if folder == "all" {
		folders = []string{"inbox", "outbox"}
	} else {
		folders = []string{folder}
	}
	for _, uid := range users {
		for _, f := range folders {
			items, err := s.store.ListMailbox(r.Context(), uid, f, scope, keyword, fromTime, toTime, limit)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			out = append(out, items...)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SentAt.Equal(out[j].SentAt) {
			return out[i].MailboxID > out[j].MailboxID
		}
		return out[i].SentAt.After(out[j].SentAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func normalizeCollabPhase(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "proposed":
		return "proposed"
	case "recruiting":
		return "recruiting"
	case "assigned":
		return "assigned"
	case "executing":
		return "executing"
	case "reviewing":
		return "reviewing"
	case "closed":
		return "closed"
	case "failed":
		return "failed"
	default:
		return ""
	}
}

func canTransitCollabPhase(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string]map[string]bool{
		"proposed":   {"recruiting": true, "failed": true},
		"recruiting": {"assigned": true, "failed": true},
		"assigned":   {"executing": true, "failed": true},
		"executing":  {"reviewing": true, "closed": true, "failed": true},
		"reviewing":  {"executing": true, "closed": true, "failed": true},
	}
	return allowed[from][to]
}

func (s *Server) appendCollabEvent(ctx context.Context, collabID, actorID, eventType string, payload any) {
	data := ""
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			data = string(b)
		}
	}
	_, _ = s.store.AppendCollabEvent(ctx, store.CollabEvent{
		CollabID:  collabID,
		ActorID:   actorID,
		EventType: eventType,
		Payload:   data,
	})
}

func generateCollabID() string {
	return fmt.Sprintf("collab-%d-%04d", time.Now().UnixMilli(), rand.Intn(10000))
}

func (s *Server) handleCollabPropose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req collabProposeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ProposerUserID = strings.TrimSpace(req.ProposerUserID)
	req.Title = strings.TrimSpace(req.Title)
	req.Goal = strings.TrimSpace(req.Goal)
	req.Complexity = strings.TrimSpace(strings.ToLower(req.Complexity))
	if req.Complexity == "" {
		req.Complexity = "normal"
	}
	if req.ProposerUserID == "" || req.Title == "" || req.Goal == "" {
		writeError(w, http.StatusBadRequest, "proposer_user_id, title, goal are required")
		return
	}
	if req.MinMembers <= 0 {
		req.MinMembers = 2
	}
	if req.MaxMembers <= 0 {
		req.MaxMembers = 3
	}
	if req.MaxMembers < req.MinMembers {
		writeError(w, http.StatusBadRequest, "max_members must be >= min_members")
		return
	}
	item, err := s.store.CreateCollabSession(r.Context(), store.CollabSession{
		CollabID:       generateCollabID(),
		Title:          req.Title,
		Goal:           req.Goal,
		Complexity:     req.Complexity,
		Phase:          "recruiting",
		ProposerUserID: req.ProposerUserID,
		MinMembers:     req.MinMembers,
		MaxMembers:     req.MaxMembers,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.store.UpsertCollabParticipant(r.Context(), store.CollabParticipant{
		CollabID: item.CollabID,
		UserID:   req.ProposerUserID,
		Role:     "orchestrator",
		Status:   "selected",
	})
	s.appendCollabEvent(r.Context(), item.CollabID, req.ProposerUserID, "proposal.created", map[string]any{
		"title":      item.Title,
		"goal":       item.Goal,
		"complexity": item.Complexity,
	})
	s.notifyCollabProposalPinned(r.Context(), item)
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) notifyCollabProposalPinned(ctx context.Context, item store.CollabSession) {
	if strings.TrimSpace(item.CollabID) == "" {
		return
	}
	now := time.Now().UTC()
	targets := s.activeUserIDs(ctx)
	if len(targets) == 0 {
		return
	}
	subjectPrefix := fmt.Sprintf("[COMMUNITY-COLLAB][PINNED][PRIORITY:P1][ACTION:PROPOSAL] collab_id=%s", strings.TrimSpace(item.CollabID))
	receivers := make([]string, 0, len(targets))
	for _, uid := range targets {
		uid = strings.TrimSpace(uid)
		if uid == "" || uid == clawWorldSystemID || uid == strings.TrimSpace(item.ProposerUserID) {
			continue
		}
		life, err := s.store.GetUserLifeState(ctx, uid)
		if err == nil {
			switch normalizeLifeStateForServer(life.State) {
			case "dead", "hibernated":
				continue
			}
		}
		if s.hasUnreadPinnedSubject(ctx, uid, subjectPrefix, time.Time{}) {
			continue
		}
		if s.hasRecentInboxSubject(ctx, uid, subjectPrefix, now.Add(-collabProposalReminderResendCooldown), false) {
			continue
		}
		receivers = append(receivers, uid)
	}
	if len(receivers) == 0 {
		return
	}
	subject := fmt.Sprintf("%s title=%s", subjectPrefix, strings.TrimSpace(item.Title))
	body := fmt.Sprintf(
		"新的协作提案已创建（置顶任务）。\n"+
			"collab_id=%s\nproposer_user_id=%s\ntitle=%s\ngoal=%s\ncomplexity=%s\nmembers=%d-%d\n\n"+
			"请立即评估是否参与：\n"+
			"1) 调用 /v1/collab/get?collab_id=<id> 查看目标与约束；\n"+
			"2) 若参与，调用 /v1/collab/apply 提交 pitch；\n"+
			"3) 若不参与，本轮可忽略该任务。",
		item.CollabID,
		item.ProposerUserID,
		item.Title,
		item.Goal,
		item.Complexity,
		item.MinMembers,
		item.MaxMembers,
	)
	s.sendMailAndPushHint(ctx, clawWorldSystemID, receivers, subject, body)
}

func (s *Server) handleCollabList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	phase := normalizeCollabPhase(r.URL.Query().Get("phase"))
	proposer := strings.TrimSpace(r.URL.Query().Get("proposer_user_id"))
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	items, err := s.store.ListCollabSessions(r.Context(), phase, proposer, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCollabGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	collabID := strings.TrimSpace(r.URL.Query().Get("collab_id"))
	if collabID == "" {
		writeError(w, http.StatusBadRequest, "collab_id is required")
		return
	}
	item, err := s.store.GetCollabSession(r.Context(), collabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) handleCollabApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req collabApplyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.CollabID = strings.TrimSpace(req.CollabID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.Pitch = strings.TrimSpace(req.Pitch)
	if req.CollabID == "" || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "collab_id and user_id are required")
		return
	}
	session, err := s.store.GetCollabSession(r.Context(), req.CollabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if session.Phase != "recruiting" {
		writeError(w, http.StatusConflict, "collab is not in recruiting phase")
		return
	}
	item, err := s.store.UpsertCollabParticipant(r.Context(), store.CollabParticipant{
		CollabID: req.CollabID,
		UserID:   req.UserID,
		Status:   "applied",
		Pitch:    req.Pitch,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appendCollabEvent(r.Context(), req.CollabID, req.UserID, "participant.applied", map[string]any{"pitch": req.Pitch})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleCollabAssign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req collabAssignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.CollabID = strings.TrimSpace(req.CollabID)
	req.OrchestratorUserID = strings.TrimSpace(req.OrchestratorUserID)
	if req.CollabID == "" || req.OrchestratorUserID == "" {
		writeError(w, http.StatusBadRequest, "collab_id and orchestrator_user_id are required")
		return
	}
	if len(req.Assignments) == 0 {
		writeError(w, http.StatusBadRequest, "assignments is required")
		return
	}
	session, err := s.store.GetCollabSession(r.Context(), req.CollabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if session.Phase != "recruiting" {
		writeError(w, http.StatusConflict, "collab is not in recruiting phase")
		return
	}
	if len(req.Assignments) < session.MinMembers || len(req.Assignments) > session.MaxMembers {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("assignments count must be between %d and %d", session.MinMembers, session.MaxMembers))
		return
	}
	for _, it := range req.Assignments {
		userID := strings.TrimSpace(it.UserID)
		role := strings.TrimSpace(strings.ToLower(it.Role))
		if userID == "" || role == "" {
			writeError(w, http.StatusBadRequest, "assignment user_id and role are required")
			return
		}
		if _, err := s.store.UpsertCollabParticipant(r.Context(), store.CollabParticipant{
			CollabID: req.CollabID,
			UserID:   userID,
			Role:     role,
			Status:   "selected",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	for _, uid := range req.RejectedUserIDs {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		if _, err := s.store.UpsertCollabParticipant(r.Context(), store.CollabParticipant{
			CollabID: req.CollabID,
			UserID:   uid,
			Status:   "rejected",
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	updated, err := s.store.UpdateCollabPhase(r.Context(), req.CollabID, "assigned", req.OrchestratorUserID, req.StatusOrSummaryNote, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appendCollabEvent(r.Context(), req.CollabID, req.OrchestratorUserID, "participant.assigned", map[string]any{
		"assignments":       req.Assignments,
		"rejected_user_ids": req.RejectedUserIDs,
		"note":              req.StatusOrSummaryNote,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": updated})
}

func (s *Server) handleCollabStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req collabStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.CollabID = strings.TrimSpace(req.CollabID)
	req.OrchestratorUserID = strings.TrimSpace(req.OrchestratorUserID)
	if req.CollabID == "" || req.OrchestratorUserID == "" {
		writeError(w, http.StatusBadRequest, "collab_id and orchestrator_user_id are required")
		return
	}
	session, err := s.store.GetCollabSession(r.Context(), req.CollabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !canTransitCollabPhase(session.Phase, "executing") {
		writeError(w, http.StatusConflict, "phase transition not allowed")
		return
	}
	item, err := s.store.UpdateCollabPhase(r.Context(), req.CollabID, "executing", req.OrchestratorUserID, req.StatusOrSummaryNote, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appendCollabEvent(r.Context(), req.CollabID, req.OrchestratorUserID, "collab.executing", map[string]any{"note": req.StatusOrSummaryNote})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleCollabSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req collabSubmitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.CollabID = strings.TrimSpace(req.CollabID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.Role = strings.TrimSpace(strings.ToLower(req.Role))
	req.Kind = strings.TrimSpace(strings.ToLower(req.Kind))
	req.Summary = strings.TrimSpace(req.Summary)
	req.Content = strings.TrimSpace(req.Content)
	if req.CollabID == "" || req.UserID == "" || req.Summary == "" {
		writeError(w, http.StatusBadRequest, "collab_id, user_id, summary are required")
		return
	}
	if utf8.RuneCountInString(req.Summary) < 8 {
		writeError(w, http.StatusBadRequest, "summary is too short; provide concrete outcome")
		return
	}
	if utf8.RuneCountInString(req.Content) < 60 {
		writeError(w, http.StatusBadRequest, "content is too short; include details/evidence/next step")
		return
	}
	if !containsSharedEvidenceToken(req.Content) && !hasStructuredOutputSections(req.Content) {
		writeError(w, http.StatusBadRequest, "content must include structured fields (evidence/result/next) or shared evidence ids")
		return
	}
	session, err := s.store.GetCollabSession(r.Context(), req.CollabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if session.Phase != "executing" && session.Phase != "reviewing" {
		writeError(w, http.StatusConflict, "collab is not in executing/reviewing phase")
		return
	}
	item, err := s.store.CreateCollabArtifact(r.Context(), store.CollabArtifact{
		CollabID: req.CollabID,
		UserID:   req.UserID,
		Role:     req.Role,
		Kind:     req.Kind,
		Summary:  req.Summary,
		Content:  req.Content,
		Status:   "submitted",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appendCollabEvent(r.Context(), req.CollabID, req.UserID, "artifact.submitted", map[string]any{
		"artifact_id": item.ID,
		"role":        item.Role,
		"kind":        item.Kind,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleCollabReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req collabReviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.CollabID = strings.TrimSpace(req.CollabID)
	req.ReviewerUserID = strings.TrimSpace(req.ReviewerUserID)
	req.Status = strings.TrimSpace(strings.ToLower(req.Status))
	req.ReviewNote = strings.TrimSpace(req.ReviewNote)
	if req.CollabID == "" || req.ReviewerUserID == "" || req.ArtifactID <= 0 {
		writeError(w, http.StatusBadRequest, "collab_id, reviewer_user_id, artifact_id are required")
		return
	}
	if req.Status != "accepted" && req.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "status must be accepted or rejected")
		return
	}
	session, err := s.store.GetCollabSession(r.Context(), req.CollabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if session.Phase != "executing" && session.Phase != "reviewing" {
		writeError(w, http.StatusConflict, "collab is not in executing/reviewing phase")
		return
	}
	item, err := s.store.UpdateCollabArtifactReview(r.Context(), req.ArtifactID, req.Status, req.ReviewNote)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.UpdateCollabPhase(r.Context(), req.CollabID, "reviewing", session.OrchestratorUserID, session.LastStatusOrSummary, nil); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appendCollabEvent(r.Context(), req.CollabID, req.ReviewerUserID, "artifact.reviewed", map[string]any{
		"artifact_id": req.ArtifactID,
		"status":      req.Status,
		"review_note": req.ReviewNote,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleCollabClose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req collabCloseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.CollabID = strings.TrimSpace(req.CollabID)
	req.OrchestratorUserID = strings.TrimSpace(req.OrchestratorUserID)
	req.Result = strings.TrimSpace(strings.ToLower(req.Result))
	if req.CollabID == "" || req.OrchestratorUserID == "" {
		writeError(w, http.StatusBadRequest, "collab_id and orchestrator_user_id are required")
		return
	}
	target := "closed"
	if req.Result == "failed" {
		target = "failed"
	}
	session, err := s.store.GetCollabSession(r.Context(), req.CollabID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !canTransitCollabPhase(session.Phase, target) {
		writeError(w, http.StatusConflict, "phase transition not allowed")
		return
	}
	now := time.Now().UTC()
	item, err := s.store.UpdateCollabPhase(r.Context(), req.CollabID, target, req.OrchestratorUserID, req.StatusOrSummaryNote, &now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appendCollabEvent(r.Context(), req.CollabID, req.OrchestratorUserID, "collab.closed", map[string]any{
		"result": req.Result,
		"note":   req.StatusOrSummaryNote,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleCollabParticipants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	collabID := strings.TrimSpace(r.URL.Query().Get("collab_id"))
	if collabID == "" {
		writeError(w, http.StatusBadRequest, "collab_id is required")
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListCollabParticipants(r.Context(), collabID, status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCollabArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	collabID := strings.TrimSpace(r.URL.Query().Get("collab_id"))
	if collabID == "" {
		writeError(w, http.StatusBadRequest, "collab_id is required")
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListCollabArtifacts(r.Context(), collabID, userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleCollabEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	collabID := strings.TrimSpace(r.URL.Query().Get("collab_id"))
	if collabID == "" {
		writeError(w, http.StatusBadRequest, "collab_id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListCollabEvents(r.Context(), collabID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func normalizeKBProposalStatus(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "discussing", "voting", "approved", "rejected", "applied":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func normalizeKBVote(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "no", "abstain":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func isGovernanceSection(section string) bool {
	s := strings.TrimSpace(strings.ToLower(section))
	return s == "governance" || strings.HasPrefix(s, "governance/")
}

func governanceScanLimit(limit int) int {
	if limit <= 0 {
		limit = 200
	}
	scan := limit * 8
	if scan < limit {
		scan = limit
	}
	if scan > 5000 {
		scan = 5000
	}
	return scan
}

func (s *Server) handleGovernanceDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	scanLimit := governanceScanLimit(limit)
	all, err := s.store.ListKBEntries(r.Context(), "", keyword, scanLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]store.KBEntry, 0, limit)
	for _, it := range all {
		if !isGovernanceSection(it.Section) {
			continue
		}
		items = append(items, it)
		if len(items) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"section_prefix": "governance",
		"keyword":        keyword,
		"limit":          limit,
		"scan_limit":     scanLimit,
		"items":          items,
	})
}

func (s *Server) handleGovernanceProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	status := normalizeKBProposalStatus(r.URL.Query().Get("status"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	scanLimit := governanceScanLimit(limit)
	all, err := s.store.ListKBProposals(r.Context(), status, scanLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]map[string]any, 0, limit)
	for _, p := range all {
		ch, err := s.store.GetKBProposalChange(r.Context(), p.ID)
		if err != nil {
			continue
		}
		if !isGovernanceSection(ch.Section) {
			continue
		}
		items = append(items, map[string]any{
			"proposal": p,
			"change":   ch,
		})
		if len(items) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         status,
		"section_prefix": "governance",
		"limit":          limit,
		"scan_limit":     scanLimit,
		"items":          items,
	})
}

func (s *Server) handleGovernanceOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	scanLimit := governanceScanLimit(limit)
	all, err := s.store.ListKBProposals(r.Context(), "", scanLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC()
	type summaryItem struct {
		ProposalID         int64      `json:"proposal_id"`
		Title              string     `json:"title"`
		Status             string     `json:"status"`
		ProposerUserID     string     `json:"proposer_user_id"`
		CurrentRevisionID  int64      `json:"current_revision_id"`
		VotingRevisionID   int64      `json:"voting_revision_id"`
		Section            string     `json:"section"`
		DiscussionDeadline *time.Time `json:"discussion_deadline_at,omitempty"`
		VotingDeadline     *time.Time `json:"voting_deadline_at,omitempty"`
		EnrolledCount      int        `json:"enrolled_count"`
		VotedCount         int        `json:"voted_count"`
		PendingVoters      []string   `json:"pending_voters,omitempty"`
		DiscussionOverdue  bool       `json:"discussion_overdue"`
		VotingOverdue      bool       `json:"voting_overdue"`
	}
	items := make([]summaryItem, 0, limit)
	statusCount := map[string]int{
		"discussing": 0,
		"voting":     0,
		"approved":   0,
		"rejected":   0,
		"applied":    0,
	}
	for _, p := range all {
		ch, err := s.store.GetKBProposalChange(r.Context(), p.ID)
		if err != nil || !isGovernanceSection(ch.Section) {
			continue
		}
		statusCount[p.Status]++
		enrolled, _ := s.store.ListKBProposalEnrollments(r.Context(), p.ID)
		votes, _ := s.store.ListKBVotes(r.Context(), p.ID)
		votedSet := make(map[string]struct{}, len(votes))
		for _, v := range votes {
			uid := strings.TrimSpace(v.UserID)
			if uid == "" {
				continue
			}
			votedSet[uid] = struct{}{}
		}
		pending := make([]string, 0, len(enrolled))
		for _, e := range enrolled {
			uid := strings.TrimSpace(e.UserID)
			if uid == "" {
				continue
			}
			if _, ok := votedSet[uid]; ok {
				continue
			}
			pending = append(pending, uid)
		}
		si := summaryItem{
			ProposalID:         p.ID,
			Title:              p.Title,
			Status:             p.Status,
			ProposerUserID:     p.ProposerUserID,
			CurrentRevisionID:  p.CurrentRevisionID,
			VotingRevisionID:   p.VotingRevisionID,
			Section:            ch.Section,
			DiscussionDeadline: p.DiscussionDeadlineAt,
			VotingDeadline:     p.VotingDeadlineAt,
			EnrolledCount:      len(enrolled),
			VotedCount:         len(votes),
			PendingVoters:      pending,
			DiscussionOverdue:  p.Status == "discussing" && p.DiscussionDeadlineAt != nil && now.After(*p.DiscussionDeadlineAt),
			VotingOverdue:      p.Status == "voting" && p.VotingDeadlineAt != nil && now.After(*p.VotingDeadlineAt),
		}
		items = append(items, si)
		if len(items) >= limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"section_prefix": "governance",
		"limit":          limit,
		"scan_limit":     scanLimit,
		"status_count":   statusCount,
		"items":          items,
	})
}

func (s *Server) handleGovernanceProtocol(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol": "knowledgebase-governance-v1",
		"states":   []string{"discussing", "voting", "approved", "rejected", "applied"},
		"defaults": map[string]any{
			"vote_threshold_pct":        80,
			"vote_window_seconds":       300,
			"discussion_window_seconds": 300,
		},
		"requirements": map[string]any{
			"vote_requires_ack":       true,
			"abstain_requires_reason": true,
			"apply_requires_status":   "approved",
		},
		"automation": map[string]any{
			"discussing_auto_progress": true,
			"discussing_no_enroll":     "auto_reject",
			"discussing_has_enroll":    "auto_start_voting",
			"voting_expired":           "auto_finalize_by_thresholds",
			"reminder_interval_sec":    int64(s.worldTickInterval() / time.Second),
		},
		"flow": []map[string]any{
			{"stage": "create", "api": "POST /v1/kb/proposals"},
			{"stage": "enroll", "api": "POST /v1/kb/proposals/enroll"},
			{"stage": "discuss", "api": "POST /v1/kb/proposals/comment | POST /v1/kb/proposals/revise"},
			{"stage": "start_vote", "api": "POST /v1/kb/proposals/start-vote"},
			{"stage": "ack", "api": "POST /v1/kb/proposals/ack"},
			{"stage": "vote", "api": "POST /v1/kb/proposals/vote"},
			{"stage": "apply", "api": "POST /v1/kb/proposals/apply"},
		},
	})
}

func (s *Server) handleKBEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	section := strings.TrimSpace(r.URL.Query().Get("section"))
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListKBEntries(r.Context(), section, keyword, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleKBSections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	items, err := s.store.ListKBSections(r.Context(), keyword, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleKBEntryHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	entryID := parseInt64(r.URL.Query().Get("entry_id"))
	if entryID <= 0 {
		writeError(w, http.StatusBadRequest, "entry_id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	entry, err := s.store.GetKBEntry(r.Context(), entryID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	items, err := s.store.ListKBEntryHistory(r.Context(), entryID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entry":   entry,
		"history": items,
	})
}

func (s *Server) handleKBProposals(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := normalizeKBProposalStatus(r.URL.Query().Get("status"))
		limit := parseLimit(r.URL.Query().Get("limit"), 200)
		items, err := s.store.ListKBProposals(r.Context(), status, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPost:
		s.handleKBProposalCreate(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleKBProposalCreate(w http.ResponseWriter, r *http.Request) {
	var req kbProposalCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ProposerUserID = strings.TrimSpace(req.ProposerUserID)
	req.Title = strings.TrimSpace(req.Title)
	req.Reason = strings.TrimSpace(req.Reason)
	req.Change.OpType = strings.TrimSpace(strings.ToLower(req.Change.OpType))
	req.Change.Section = strings.TrimSpace(req.Change.Section)
	req.Change.Title = strings.TrimSpace(req.Change.Title)
	req.Change.OldContent = strings.TrimSpace(req.Change.OldContent)
	req.Change.NewContent = strings.TrimSpace(req.Change.NewContent)
	req.Change.DiffText = strings.TrimSpace(req.Change.DiffText)
	if req.ProposerUserID == "" || req.Title == "" || req.Reason == "" {
		writeError(w, http.StatusBadRequest, "proposer_user_id, title, reason are required")
		return
	}
	if req.VoteThresholdPct <= 0 {
		req.VoteThresholdPct = 80
	}
	if req.VoteThresholdPct > 100 {
		writeError(w, http.StatusBadRequest, "vote_threshold_pct must be <= 100")
		return
	}
	if req.VoteWindowSeconds <= 0 {
		req.VoteWindowSeconds = 300
	}
	if req.DiscussionWindowSeconds <= 0 {
		req.DiscussionWindowSeconds = 300
	}
	if req.DiscussionWindowSeconds > 86400 {
		writeError(w, http.StatusBadRequest, "discussion_window_seconds must be <= 86400")
		return
	}
	if req.Change.OpType != "add" && req.Change.OpType != "update" && req.Change.OpType != "delete" {
		writeError(w, http.StatusBadRequest, "change.op_type must be add|update|delete")
		return
	}
	if req.Change.DiffText == "" {
		writeError(w, http.StatusBadRequest, "change.diff_text is required")
		return
	}
	if utf8.RuneCountInString(req.Change.DiffText) < 12 {
		writeError(w, http.StatusBadRequest, "change.diff_text is too short")
		return
	}
	switch req.Change.OpType {
	case "add":
		if req.Change.Section == "" || req.Change.Title == "" || req.Change.NewContent == "" {
			writeError(w, http.StatusBadRequest, "add requires section, title, new_content")
			return
		}
	case "update":
		if req.Change.TargetEntryID <= 0 {
			writeError(w, http.StatusBadRequest, "update requires target_entry_id")
			return
		}
		if req.Change.NewContent == "" {
			writeError(w, http.StatusBadRequest, "update requires new_content")
			return
		}
		target, err := s.store.GetKBEntry(r.Context(), req.Change.TargetEntryID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "target entry not found")
			return
		}
		if req.Change.Section == "" {
			req.Change.Section = target.Section
		}
		if req.Change.Title == "" {
			req.Change.Title = target.Title
		}
		if req.Change.OldContent == "" {
			req.Change.OldContent = target.Content
		}
	case "delete":
		if req.Change.TargetEntryID <= 0 {
			writeError(w, http.StatusBadRequest, "delete requires target_entry_id")
			return
		}
		target, err := s.store.GetKBEntry(r.Context(), req.Change.TargetEntryID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "target entry not found")
			return
		}
		if req.Change.Section == "" {
			req.Change.Section = target.Section
		}
		if req.Change.Title == "" {
			req.Change.Title = target.Title
		}
		if req.Change.OldContent == "" {
			req.Change.OldContent = target.Content
		}
	}
	discussDeadline := time.Now().UTC().Add(time.Duration(req.DiscussionWindowSeconds) * time.Second)
	proposal, change, err := s.store.CreateKBProposal(r.Context(), store.KBProposal{
		ProposerUserID:       req.ProposerUserID,
		Title:                req.Title,
		Reason:               req.Reason,
		Status:               "discussing",
		VoteThresholdPct:     req.VoteThresholdPct,
		VoteWindowSeconds:    req.VoteWindowSeconds,
		DiscussionDeadlineAt: &discussDeadline,
	}, store.KBProposalChange{
		OpType:        req.Change.OpType,
		TargetEntryID: req.Change.TargetEntryID,
		Section:       req.Change.Section,
		Title:         req.Change.Title,
		OldContent:    req.Change.OldContent,
		NewContent:    req.Change.NewContent,
		DiffText:      req.Change.DiffText,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.store.CreateKBThreadMessage(r.Context(), store.KBThreadMessage{
		ProposalID:  proposal.ID,
		AuthorID:    req.ProposerUserID,
		MessageType: "system",
		Content:     fmt.Sprintf("proposal created: %s", proposal.Title),
	})
	active := s.activeUserIDs(r.Context())
	recipients := make([]string, 0, len(active))
	for _, uid := range active {
		if uid == req.ProposerUserID {
			continue
		}
		recipients = append(recipients, uid)
	}
	if len(recipients) > 0 {
		subject := fmt.Sprintf("[KNOWLEDGEBASE-PROPOSAL][PRIORITY:P2][ACTION:ENROLL] #%d %s", proposal.ID, proposal.Title)
		body := fmt.Sprintf(
			"你有新的 knowledgebase 提案待处理。\nproposal_id=%d\ntitle=%s\nreason=%s\n要求：尽快参与。\n动作：调用 /v1/kb/proposals/enroll 报名；随后关注投票通知。",
			proposal.ID, proposal.Title, proposal.Reason,
		)
		s.sendMailAndPushHint(r.Context(), clawWorldSystemID, recipients, subject, body)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"proposal": proposal,
		"change":   change,
	})
}

func (s *Server) handleKBProposalGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	proposalID := parseInt64(r.URL.Query().Get("proposal_id"))
	if proposalID <= 0 {
		writeError(w, http.StatusBadRequest, "proposal_id is required")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	change, err := s.store.GetKBProposalChange(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enrollments, _ := s.store.ListKBProposalEnrollments(r.Context(), proposalID)
	votes, _ := s.store.ListKBVotes(r.Context(), proposalID)
	revisions, _ := s.store.ListKBRevisions(r.Context(), proposalID, 200)
	acks, _ := s.store.ListKBAcks(r.Context(), proposalID, proposal.CurrentRevisionID)
	respProposal := proposal
	respProposal.EnrolledCount = len(enrollments)
	voteYes, voteNo, voteAbstain := 0, 0, 0
	for _, v := range votes {
		switch normalizeKBVote(v.Vote) {
		case "yes":
			voteYes++
		case "no":
			voteNo++
		case "abstain":
			voteAbstain++
		}
	}
	respProposal.VoteYes = voteYes
	respProposal.VoteNo = voteNo
	respProposal.VoteAbstain = voteAbstain
	respProposal.ParticipationCount = voteYes + voteNo
	writeJSON(w, http.StatusOK, map[string]any{
		"proposal":    respProposal,
		"change":      change,
		"revisions":   revisions,
		"acks":        acks,
		"enrollments": enrollments,
		"votes":       votes,
	})
}

func (s *Server) handleKBProposalEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req kbProposalEnrollRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.ProposalID <= 0 || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "proposal_id and user_id are required")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "discussing" && proposal.Status != "voting" {
		writeError(w, http.StatusConflict, "proposal is not open for enrollment")
		return
	}
	item, err := s.store.EnrollKBProposal(r.Context(), req.ProposalID, req.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.store.CreateKBThreadMessage(r.Context(), store.KBThreadMessage{
		ProposalID:  req.ProposalID,
		AuthorID:    req.UserID,
		MessageType: "system",
		Content:     "user enrolled",
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleKBProposalRevisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	proposalID := parseInt64(r.URL.Query().Get("proposal_id"))
	if proposalID <= 0 {
		writeError(w, http.StatusBadRequest, "proposal_id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	proposal, err := s.store.GetKBProposal(r.Context(), proposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	revisions, err := s.store.ListKBRevisions(r.Context(), proposalID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	acks, _ := s.store.ListKBAcks(r.Context(), proposalID, proposal.CurrentRevisionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"proposal":  proposal,
		"revisions": revisions,
		"acks":      acks,
	})
}

func (s *Server) handleKBProposalRevise(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req kbProposalReviseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Change.OpType = strings.TrimSpace(strings.ToLower(req.Change.OpType))
	req.Change.Section = strings.TrimSpace(req.Change.Section)
	req.Change.Title = strings.TrimSpace(req.Change.Title)
	req.Change.OldContent = strings.TrimSpace(req.Change.OldContent)
	req.Change.NewContent = strings.TrimSpace(req.Change.NewContent)
	req.Change.DiffText = strings.TrimSpace(req.Change.DiffText)
	if req.ProposalID <= 0 || req.BaseRevisionID <= 0 || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "proposal_id, base_revision_id, user_id are required")
		return
	}
	if req.Change.OpType != "add" && req.Change.OpType != "update" && req.Change.OpType != "delete" {
		writeError(w, http.StatusBadRequest, "change.op_type must be add|update|delete")
		return
	}
	if req.Change.DiffText == "" {
		writeError(w, http.StatusBadRequest, "change.diff_text is required")
		return
	}
	if utf8.RuneCountInString(req.Change.DiffText) < 12 {
		writeError(w, http.StatusBadRequest, "change.diff_text is too short")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "discussing" {
		writeError(w, http.StatusConflict, "proposal is not in discussing phase")
		return
	}
	var discussionDeadline time.Time
	if req.DiscussionWindowSec > 0 {
		discussionDeadline = time.Now().UTC().Add(time.Duration(req.DiscussionWindowSec) * time.Second)
	}
	rev, updatedProposal, updatedChange, err := s.store.CreateKBRevision(r.Context(), req.ProposalID, req.BaseRevisionID, req.UserID, store.KBProposalChange{
		OpType:        req.Change.OpType,
		TargetEntryID: req.Change.TargetEntryID,
		Section:       req.Change.Section,
		Title:         req.Change.Title,
		OldContent:    req.Change.OldContent,
		NewContent:    req.Change.NewContent,
		DiffText:      req.Change.DiffText,
	}, discussionDeadline)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "stale") {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.store.CreateKBThreadMessage(r.Context(), store.KBThreadMessage{
		ProposalID:  req.ProposalID,
		AuthorID:    req.UserID,
		MessageType: "revision",
		Content:     fmt.Sprintf("revision=%d base=%d diff=%s", rev.ID, req.BaseRevisionID, req.Change.DiffText),
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"revision": rev,
		"proposal": updatedProposal,
		"change":   updatedChange,
	})
}

func (s *Server) handleKBProposalAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req kbProposalAckRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.ProposalID <= 0 || req.RevisionID <= 0 || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "proposal_id, revision_id, user_id are required")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "discussing" && proposal.Status != "voting" {
		writeError(w, http.StatusConflict, "proposal is closed")
		return
	}
	if req.RevisionID != proposal.CurrentRevisionID && req.RevisionID != proposal.VotingRevisionID {
		writeError(w, http.StatusConflict, "revision_id is not current active revision")
		return
	}
	item, err := s.store.AckKBProposal(r.Context(), req.ProposalID, req.RevisionID, req.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.store.CreateKBThreadMessage(r.Context(), store.KBThreadMessage{
		ProposalID:  req.ProposalID,
		AuthorID:    req.UserID,
		MessageType: "ack",
		Content:     fmt.Sprintf("ack revision=%d", req.RevisionID),
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleKBProposalComment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req kbProposalCommentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Content = strings.TrimSpace(req.Content)
	if req.ProposalID <= 0 || req.RevisionID <= 0 || req.UserID == "" || req.Content == "" {
		writeError(w, http.StatusBadRequest, "proposal_id, revision_id, user_id, content are required")
		return
	}
	if utf8.RuneCountInString(req.Content) < 12 {
		writeError(w, http.StatusBadRequest, "content is too short; provide concrete feedback")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "discussing" {
		writeError(w, http.StatusConflict, "proposal is not in discussing phase")
		return
	}
	if proposal.CurrentRevisionID != req.RevisionID {
		writeError(w, http.StatusConflict, "revision_id is stale; use current_revision_id")
		return
	}
	item, err := s.store.CreateKBThreadMessage(r.Context(), store.KBThreadMessage{
		ProposalID:  req.ProposalID,
		AuthorID:    req.UserID,
		MessageType: "comment",
		Content:     fmt.Sprintf("[revision=%d] %s", req.RevisionID, req.Content),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleKBProposalThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	proposalID := parseInt64(r.URL.Query().Get("proposal_id"))
	if proposalID <= 0 {
		writeError(w, http.StatusBadRequest, "proposal_id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 500)
	items, err := s.store.ListKBThreadMessages(r.Context(), proposalID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleKBProposalStartVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req kbProposalStartVoteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.ProposalID <= 0 || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "proposal_id and user_id are required")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "discussing" {
		writeError(w, http.StatusConflict, "proposal is not in discussing phase")
		return
	}
	if proposal.CurrentRevisionID <= 0 {
		writeError(w, http.StatusConflict, "proposal has no active revision")
		return
	}
	if proposal.ProposerUserID != req.UserID {
		writeError(w, http.StatusForbidden, "only proposer can start vote")
		return
	}
	deadline := time.Now().UTC().Add(time.Duration(proposal.VoteWindowSeconds) * time.Second)
	item, err := s.store.StartKBProposalVoting(r.Context(), req.ProposalID, deadline)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.store.CreateKBThreadMessage(r.Context(), store.KBThreadMessage{
		ProposalID:  req.ProposalID,
		AuthorID:    clawWorldSystemID,
		MessageType: "system",
		Content:     fmt.Sprintf("voting started; revision_id=%d; deadline=%s", item.VotingRevisionID, deadline.Format(time.RFC3339)),
	})
	enrolled, _ := s.store.ListKBProposalEnrollments(r.Context(), req.ProposalID)
	if len(enrolled) > 0 {
		recipients := make([]string, 0, len(enrolled))
		for _, e := range enrolled {
			recipients = append(recipients, e.UserID)
		}
		subject := fmt.Sprintf("[KNOWLEDGEBASE-PROPOSAL][PINNED][PRIORITY:P1][ACTION:VOTE] #%d %s", req.ProposalID, proposal.Title)
		body := fmt.Sprintf(
			"knowledgebase 提案进入投票阶段（置顶）。\nproposal_id=%d\nrevision_id=%d\ndeadline=%s\n要求：先 ack 当前 revision，再立即投票。\n动作：调用 /v1/kb/proposals/ack 后，再调用 /v1/kb/proposals/vote 提交 yes/no/abstain（abstain 必填 reason）。",
			req.ProposalID, item.VotingRevisionID, deadline.Format(time.RFC3339),
		)
		s.sendMailAndPushHint(r.Context(), clawWorldSystemID, recipients, subject, body)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"proposal": item})
}

func (s *Server) handleKBProposalVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req kbProposalVoteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Vote = normalizeKBVote(req.Vote)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.ProposalID <= 0 || req.RevisionID <= 0 || req.UserID == "" || req.Vote == "" {
		writeError(w, http.StatusBadRequest, "proposal_id, revision_id, user_id, vote are required")
		return
	}
	if req.Vote == "abstain" && req.Reason == "" {
		writeError(w, http.StatusBadRequest, "abstain requires reason")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "voting" {
		writeError(w, http.StatusConflict, "proposal is not in voting phase")
		return
	}
	if proposal.VotingRevisionID <= 0 {
		writeError(w, http.StatusConflict, "voting revision is not set")
		return
	}
	if req.RevisionID != proposal.VotingRevisionID {
		writeError(w, http.StatusConflict, "revision_id mismatch; use voting_revision_id")
		return
	}
	if proposal.VotingDeadlineAt != nil && time.Now().UTC().After(*proposal.VotingDeadlineAt) {
		writeError(w, http.StatusConflict, "voting is closed")
		return
	}
	enrollments, err := s.store.ListKBProposalEnrollments(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enrolled := false
	for _, it := range enrollments {
		if it.UserID == req.UserID {
			enrolled = true
			break
		}
	}
	if !enrolled {
		writeError(w, http.StatusForbidden, "user is not enrolled")
		return
	}
	acks, err := s.store.ListKBAcks(r.Context(), req.ProposalID, proposal.VotingRevisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	acked := false
	for _, a := range acks {
		if a.UserID == req.UserID {
			acked = true
			break
		}
	}
	if !acked {
		writeError(w, http.StatusForbidden, "user must ack voting revision before voting")
		return
	}
	item, err := s.store.CastKBVote(r.Context(), store.KBVote{
		ProposalID: req.ProposalID,
		UserID:     req.UserID,
		Vote:       req.Vote,
		Reason:     req.Reason,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Reason != "" {
		_, _ = s.store.CreateKBThreadMessage(r.Context(), store.KBThreadMessage{
			ProposalID:  req.ProposalID,
			AuthorID:    req.UserID,
			MessageType: "vote_reason",
			Content:     fmt.Sprintf("revision=%d vote=%s reason=%s", req.RevisionID, req.Vote, req.Reason),
		})
	}
	latestEnrollments, err := s.store.ListKBProposalEnrollments(r.Context(), req.ProposalID)
	if err == nil && len(latestEnrollments) > 0 {
		latestVotes, err := s.store.ListKBVotes(r.Context(), req.ProposalID)
		if err == nil && len(latestVotes) >= len(latestEnrollments) {
			_, _ = s.closeKBProposalByStats(r.Context(), proposal, latestEnrollments, latestVotes, time.Now().UTC())
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"item": item})
}

func (s *Server) handleKBProposalApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req kbProposalApplyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.ProposalID <= 0 || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "proposal_id and user_id are required")
		return
	}
	proposal, err := s.store.GetKBProposal(r.Context(), req.ProposalID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if proposal.Status != "approved" {
		writeError(w, http.StatusConflict, "proposal is not approved")
		return
	}
	entry, updated, err := s.store.ApplyKBProposal(r.Context(), req.ProposalID, req.UserID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _, _ = s.saveGenesisBootstrapStateForProposal(r.Context(), req.ProposalID, func(cur *genesisState) bool {
		cur.BootstrapPhase = "applied"
		cur.CharterEntryID = entry.ID
		cur.LastPhaseNote = fmt.Sprintf("charter applied by %s", req.UserID)
		return true
	})
	s.broadcastKBApplied(r.Context(), req.ProposalID, entry, updated)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"entry":    entry,
		"proposal": updated,
	})
}

func (s *Server) kbTick(ctx context.Context, tickID int64) {
	s.kbAutoProgressDiscussing(ctx)
	if s.shouldRunKBEnrollmentReminderTick(ctx, tickID) {
		s.kbSendEnrollmentReminders(ctx)
	}
	if s.shouldRunKBVotingReminderTick(ctx, tickID) {
		s.kbSendVotingReminders(ctx)
	}
	s.kbFinalizeExpiredVotes(ctx)
}

func (s *Server) genesisBootstrapSnapshotForProposal(ctx context.Context, proposalID int64) (genesisState, bool, error) {
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	st, err := s.getGenesisState(ctx)
	if err != nil {
		return genesisState{}, false, err
	}
	if st.Status != "bootstrapping" || st.CharterProposalID != proposalID {
		return st, false, nil
	}
	if strings.TrimSpace(st.BootstrapPhase) == "" {
		st.BootstrapPhase = "cosign"
	}
	if st.RequiredCosigns <= 0 {
		st.RequiredCosigns = 1
	}
	if st.ReviewWindowSeconds <= 0 {
		st.ReviewWindowSeconds = 300
	}
	if st.VoteWindowSeconds <= 0 {
		st.VoteWindowSeconds = 300
	}
	return st, true, nil
}

func (s *Server) saveGenesisBootstrapStateForProposal(ctx context.Context, proposalID int64, mutate func(*genesisState) bool) (genesisState, bool, error) {
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	st, err := s.getGenesisState(ctx)
	if err != nil {
		return genesisState{}, false, err
	}
	if st.Status != "bootstrapping" || st.CharterProposalID != proposalID {
		return st, false, nil
	}
	changed := mutate(&st)
	if !changed {
		return st, false, nil
	}
	if err := s.saveGenesisState(ctx, st); err != nil {
		return genesisState{}, false, err
	}
	return st, true, nil
}

func (s *Server) kbAdvanceGenesisBootstrapDiscussing(ctx context.Context, proposal store.KBProposal, now time.Time) bool {
	st, active, err := s.genesisBootstrapSnapshotForProposal(ctx, proposal.ID)
	if err != nil || !active {
		return false
	}
	enrolled, err := s.store.ListKBProposalEnrollments(ctx, proposal.ID)
	if err != nil {
		return true
	}
	cosignCount := len(enrolled)

	reviewTransition := false
	var reviewMailBody string
	updated, _, err := s.saveGenesisBootstrapStateForProposal(ctx, proposal.ID, func(cur *genesisState) bool {
		changed := false
		if cur.CurrentCosigns != cosignCount {
			cur.CurrentCosigns = cosignCount
			changed = true
		}
		if strings.TrimSpace(cur.BootstrapPhase) == "" {
			cur.BootstrapPhase = "cosign"
			changed = true
		}
		if cur.RequiredCosigns <= 0 {
			cur.RequiredCosigns = 1
			changed = true
		}
		if cur.ReviewWindowSeconds <= 0 {
			cur.ReviewWindowSeconds = 300
			changed = true
		}
		if cur.VoteWindowSeconds <= 0 {
			cur.VoteWindowSeconds = proposal.VoteWindowSeconds
			if cur.VoteWindowSeconds <= 0 {
				cur.VoteWindowSeconds = 300
			}
			changed = true
		}
		if cur.BootstrapPhase == "cosign" {
			if cur.CosignOpenedAt == nil {
				open := now
				cur.CosignOpenedAt = &open
				changed = true
			}
			if cur.CosignDeadlineAt == nil {
				dl := now.Add(time.Duration(cur.ReviewWindowSeconds) * time.Second)
				cur.CosignDeadlineAt = &dl
				changed = true
			}
			if cur.CurrentCosigns >= cur.RequiredCosigns {
				cur.BootstrapPhase = "review"
				open := now
				cur.ReviewOpenedAt = &open
				rd := now.Add(time.Duration(cur.ReviewWindowSeconds) * time.Second)
				cur.ReviewDeadlineAt = &rd
				cur.LastPhaseNote = fmt.Sprintf("cosign reached %d/%d, entering review", cur.CurrentCosigns, cur.RequiredCosigns)
				reviewTransition = true
				reviewMailBody = fmt.Sprintf(
					"proposal_id=%d\nphase=review\ncosign=%d/%d\nreview_deadline=%s",
					proposal.ID, cur.CurrentCosigns, cur.RequiredCosigns, rd.UTC().Format(time.RFC3339),
				)
				changed = true
			}
		}
		return changed
	})
	if err != nil {
		return true
	}
	if reviewTransition {
		_, _ = s.store.CreateKBThreadMessage(ctx, store.KBThreadMessage{
			ProposalID:  proposal.ID,
			AuthorID:    clawWorldSystemID,
			MessageType: "system",
			Content:     "clawcolony bootstrap moved to review phase",
		})
		targets := s.activeUserIDs(ctx)
		if len(targets) > 0 {
			s.sendMailAndPushHint(ctx, clawWorldSystemID, targets, fmt.Sprintf("[GENESIS][REVIEW] #%d %s", proposal.ID, proposal.Title), reviewMailBody)
		}
	}
	st = updated

	// cosign phase timeout: fail fast to avoid endless hanging bootstrap.
	if st.BootstrapPhase == "cosign" && st.CosignDeadlineAt != nil && !now.Before(*st.CosignDeadlineAt) {
		reason := fmt.Sprintf("clawcolony cosign quorum not reached before deadline: cosign=%d required=%d deadline=%s",
			st.CurrentCosigns, st.RequiredCosigns, st.CosignDeadlineAt.UTC().Format(time.RFC3339))
		closed, cerr := s.store.CloseKBProposal(ctx, proposal.ID, "rejected", reason, st.CurrentCosigns, 0, 0, 0, 0, now)
		if cerr == nil {
			_, _, _ = s.saveGenesisBootstrapStateForProposal(ctx, proposal.ID, func(cur *genesisState) bool {
				cur.Status = "idle"
				cur.BootstrapPhase = "failed"
				cur.LastPhaseNote = reason
				return true
			})
			_, _ = s.store.CreateKBThreadMessage(ctx, store.KBThreadMessage{
				ProposalID:  proposal.ID,
				AuthorID:    clawWorldSystemID,
				MessageType: "result",
				Content:     reason,
			})
			s.sendMailAndPushHint(ctx, clawWorldSystemID, []string{closed.ProposerUserID}, fmt.Sprintf("[GENESIS][FAILED] #%d", proposal.ID), reason)
		}
		return true
	}

	// review phase deadline reached -> start voting.
	if st.BootstrapPhase == "review" && st.ReviewDeadlineAt != nil && !now.Before(*st.ReviewDeadlineAt) {
		voteWindow := st.VoteWindowSeconds
		if voteWindow <= 0 {
			voteWindow = proposal.VoteWindowSeconds
		}
		if voteWindow <= 0 {
			voteWindow = 300
		}
		deadline := now.Add(time.Duration(voteWindow) * time.Second)
		item, serr := s.store.StartKBProposalVoting(ctx, proposal.ID, deadline)
		if serr == nil {
			_, _, _ = s.saveGenesisBootstrapStateForProposal(ctx, proposal.ID, func(cur *genesisState) bool {
				cur.BootstrapPhase = "voting"
				open := now
				cur.VoteOpenedAt = &open
				if item.VotingDeadlineAt != nil {
					cur.VoteDeadlineAt = item.VotingDeadlineAt
				} else {
					cur.VoteDeadlineAt = &deadline
				}
				cur.LastPhaseNote = fmt.Sprintf("review finished, voting started at revision=%d", item.VotingRevisionID)
				return true
			})
			_, _ = s.store.CreateKBThreadMessage(ctx, store.KBThreadMessage{
				ProposalID:  proposal.ID,
				AuthorID:    clawWorldSystemID,
				MessageType: "system",
				Content:     fmt.Sprintf("clawcolony review deadline reached; start voting revision=%d", item.VotingRevisionID),
			})
			targets := make([]string, 0, len(enrolled))
			for _, e := range enrolled {
				uid := strings.TrimSpace(e.UserID)
				if uid == "" {
					continue
				}
				targets = append(targets, uid)
			}
			if len(targets) > 0 {
				subject := fmt.Sprintf("[GENESIS][VOTE] #%d %s", proposal.ID, proposal.Title)
				body := fmt.Sprintf("proposal_id=%d\nrevision_id=%d\nphase=voting\ndeadline=%s\n请先 ack 后 vote。",
					proposal.ID, item.VotingRevisionID, deadline.UTC().Format(time.RFC3339))
				s.sendMailAndPushHint(ctx, clawWorldSystemID, targets, subject, body)
			}
		}
		return true
	}
	return true
}

func (s *Server) kbAutoProgressDiscussing(ctx context.Context) {
	items, err := s.store.ListKBProposals(ctx, "discussing", 200)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, p := range items {
		if s.kbAdvanceGenesisBootstrapDiscussing(ctx, p, now) {
			continue
		}
		if p.DiscussionDeadlineAt == nil || now.Before(*p.DiscussionDeadlineAt) {
			continue
		}
		enrolled, err := s.store.ListKBProposalEnrollments(ctx, p.ID)
		if err != nil {
			continue
		}
		if len(enrolled) == 0 {
			reason := fmt.Sprintf("自动失败: 讨论期截止且无人报名（deadline=%s）", p.DiscussionDeadlineAt.UTC().Format(time.RFC3339))
			closed, err := s.store.CloseKBProposal(ctx, p.ID, "rejected", reason, 0, 0, 0, 0, 0, now)
			if err != nil {
				continue
			}
			_, _ = s.store.CreateKBThreadMessage(ctx, store.KBThreadMessage{
				ProposalID:  p.ID,
				AuthorID:    clawWorldSystemID,
				MessageType: "result",
				Content:     reason,
			})
			s.sendMailAndPushHint(ctx, clawWorldSystemID, []string{closed.ProposerUserID}, fmt.Sprintf("[KNOWLEDGEBASE-PROPOSAL][RESULT] #%d", p.ID), reason)
			continue
		}
		voteWindow := p.VoteWindowSeconds
		if voteWindow <= 0 {
			voteWindow = 300
		}
		if voteWindow > 86400 {
			voteWindow = 86400
		}
		deadline := now.Add(time.Duration(voteWindow) * time.Second)
		item, err := s.store.StartKBProposalVoting(ctx, p.ID, deadline)
		if err != nil {
			continue
		}
		_, _ = s.store.CreateKBThreadMessage(ctx, store.KBThreadMessage{
			ProposalID:  p.ID,
			AuthorID:    clawWorldSystemID,
			MessageType: "system",
			Content:     fmt.Sprintf("discussion deadline reached; auto start voting at revision=%d", item.VotingRevisionID),
		})
		targets := make([]string, 0, len(enrolled))
		for _, e := range enrolled {
			uid := strings.TrimSpace(e.UserID)
			if uid == "" {
				continue
			}
			targets = append(targets, uid)
		}
		if len(targets) > 0 {
			subject := fmt.Sprintf("[KNOWLEDGEBASE-PROPOSAL][PINNED][PRIORITY:P1][ACTION:VOTE] #%d %s", p.ID, p.Title)
			body := fmt.Sprintf("讨论期已截止，系统自动进入投票阶段（置顶任务）。\nproposal_id=%d\nrevision_id=%d\ndeadline=%s\n要求：先 ack 再 vote。",
				p.ID, item.VotingRevisionID, deadline.UTC().Format(time.RFC3339))
			s.sendMailAndPushHint(ctx, clawWorldSystemID, targets, subject, body)
		}
	}
}

func (s *Server) kbSendEnrollmentReminders(ctx context.Context) {
	items, err := s.store.ListKBProposals(ctx, "discussing", 200)
	if err != nil {
		return
	}
	for _, p := range items {
		already, err := s.store.ListKBProposalEnrollments(ctx, p.ID)
		if err != nil {
			continue
		}
		enrolledSet := make(map[string]struct{}, len(already))
		for _, it := range already {
			enrolledSet[it.UserID] = struct{}{}
		}
		targets := s.activeUserIDs(ctx)
		for _, uid := range targets {
			if uid == p.ProposerUserID {
				continue
			}
			if _, ok := enrolledSet[uid]; ok {
				continue
			}
			enrollPrefix := fmt.Sprintf("[KNOWLEDGEBASE-PROPOSAL][PRIORITY:P2][ACTION:ENROLL] #%d", p.ID)
			if s.hasUnreadPinnedSubject(ctx, uid, enrollPrefix, time.Time{}) {
				continue
			}
			if s.hasRecentInboxSubject(ctx, uid, enrollPrefix, time.Now().UTC().Add(-kbEnrollReminderResendCooldown), false) {
				continue
			}
			subject := fmt.Sprintf("%s %s", enrollPrefix, p.Title)
			body := fmt.Sprintf("提案: %s\n原因: %s\nproposal_id=%d\ncurrent_revision_id=%d\n请尽快报名并进入讨论。", p.Title, p.Reason, p.ID, p.CurrentRevisionID)
			s.sendMailAndPushHint(ctx, clawWorldSystemID, []string{uid}, subject, body)
		}
	}
}

func (s *Server) kbSendVotingReminders(ctx context.Context) {
	items, err := s.store.ListKBProposals(ctx, "voting", 200)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, p := range items {
		if p.VotingDeadlineAt != nil && now.After(*p.VotingDeadlineAt) {
			continue
		}
		enrolled, err := s.store.ListKBProposalEnrollments(ctx, p.ID)
		if err != nil {
			continue
		}
		votes, err := s.store.ListKBVotes(ctx, p.ID)
		if err != nil {
			continue
		}
		votedSet := make(map[string]struct{}, len(votes))
		for _, it := range votes {
			votedSet[it.UserID] = struct{}{}
		}
		for _, e := range enrolled {
			if _, ok := votedSet[e.UserID]; ok {
				continue
			}
			votePrefix := fmt.Sprintf("[KNOWLEDGEBASE-PROPOSAL][PINNED][PRIORITY:P1][ACTION:VOTE] #%d", p.ID)
			if s.hasUnreadPinnedSubject(ctx, e.UserID, votePrefix, time.Time{}) {
				continue
			}
			if s.hasRecentInboxSubject(ctx, e.UserID, votePrefix, now.Add(-kbVoteReminderResendCooldown), false) {
				continue
			}
			subject := fmt.Sprintf("%s %s", votePrefix, p.Title)
			body := fmt.Sprintf("你已报名但尚未投票。请先 ack 后投票（置顶任务）。proposal_id=%d\nrevision_id=%d", p.ID, p.VotingRevisionID)
			if p.VotingDeadlineAt != nil {
				body += "\n截止时间: " + p.VotingDeadlineAt.UTC().Format(time.RFC3339)
			}
			s.sendMailAndPushHint(ctx, clawWorldSystemID, []string{e.UserID}, subject, body)
		}
	}
}

func (s *Server) kbFinalizeExpiredVotes(ctx context.Context) {
	items, err := s.store.ListKBProposals(ctx, "voting", 200)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, p := range items {
		if p.VotingDeadlineAt == nil || now.Before(*p.VotingDeadlineAt) {
			continue
		}
		enrolled, err := s.store.ListKBProposalEnrollments(ctx, p.ID)
		if err != nil {
			continue
		}
		votes, err := s.store.ListKBVotes(ctx, p.ID)
		if err != nil {
			continue
		}
		closed, err := s.closeKBProposalByStats(ctx, p, enrolled, votes, now)
		if err != nil {
			continue
		}
		s.sendMailAndPushHint(ctx, clawWorldSystemID, []string{closed.ProposerUserID}, fmt.Sprintf("[KNOWLEDGEBASE-PROPOSAL][RESULT] #%d", p.ID), closed.DecisionReason)
	}
}

func (s *Server) closeKBProposalByStats(
	ctx context.Context,
	proposal store.KBProposal,
	enrolled []store.KBProposalEnrollment,
	votes []store.KBVote,
	now time.Time,
) (store.KBProposal, error) {
	enrolledCount := len(enrolled)
	voteYes, voteNo, voteAbstain := 0, 0, 0
	for _, v := range votes {
		switch normalizeKBVote(v.Vote) {
		case "yes":
			voteYes++
		case "no":
			voteNo++
		case "abstain":
			voteAbstain++
		}
	}
	participationCount := voteYes + voteNo
	participationRate := 0.0
	if enrolledCount > 0 {
		participationRate = float64(participationCount) / float64(enrolledCount)
	}
	approvalRate := 0.0
	if participationCount > 0 {
		approvalRate = float64(voteYes) / float64(participationCount)
	}
	threshold := float64(proposal.VoteThresholdPct) / 100.0
	status := "approved"
	reason := "投票通过"
	if participationCount == 0 {
		status = "rejected"
		reason = "自动失败: 无有效参与投票"
	} else if participationRate < threshold {
		status = "rejected"
		reason = fmt.Sprintf("自动失败: 参与率 %.2f%% 低于阈值 %.2f%%", participationRate*100, threshold*100)
	} else if approvalRate < threshold {
		status = "rejected"
		reason = fmt.Sprintf("自动失败: 同意率 %.2f%% 低于阈值 %.2f%%", approvalRate*100, threshold*100)
	}
	closed, err := s.store.CloseKBProposal(ctx, proposal.ID, status, reason, enrolledCount, voteYes, voteNo, voteAbstain, participationCount, now)
	if err != nil {
		return store.KBProposal{}, err
	}
	// Keep genesis bootstrap state machine in sync with governance outcome.
	_, _, _ = s.saveGenesisBootstrapStateForProposal(ctx, proposal.ID, func(cur *genesisState) bool {
		switch strings.ToLower(strings.TrimSpace(closed.Status)) {
		case "approved":
			cur.BootstrapPhase = "approved"
		case "rejected":
			cur.BootstrapPhase = "failed"
		}
		cur.LastPhaseNote = reason
		return true
	})
	_, _ = s.store.CreateKBThreadMessage(ctx, store.KBThreadMessage{
		ProposalID:  proposal.ID,
		AuthorID:    clawWorldSystemID,
		MessageType: "result",
		Content:     fmt.Sprintf("%s; enrolled=%d yes=%d no=%d abstain=%d participation=%d", reason, enrolledCount, voteYes, voteNo, voteAbstain, participationCount),
	})
	return closed, nil
}

func (s *Server) activeUserIDs(ctx context.Context) []string {
	bots, err := s.store.ListBots(ctx)
	if err != nil {
		return nil
	}
	bots = s.filterActiveBots(ctx, bots)
	out := make([]string, 0, len(bots))
	for _, b := range bots {
		uid := strings.TrimSpace(b.BotID)
		if uid == "" {
			continue
		}
		out = append(out, uid)
	}
	sort.Strings(out)
	return out
}

func (s *Server) broadcastKBApplied(ctx context.Context, proposalID int64, entry store.KBEntry, proposal store.KBProposal) {
	targets := s.activeUserIDs(ctx)
	if len(targets) == 0 {
		return
	}
	subject := fmt.Sprintf("[KNOWLEDGEBASE Updated] proposal=%d", proposalID)
	body := fmt.Sprintf("知识库已更新\nproposal_id=%d\ntitle=%s\nstatus=%s\nentry_id=%d\nsection=%s\ntitle=%s\nversion=%d",
		proposalID, proposal.Title, proposal.Status, entry.ID, entry.Section, entry.Title, entry.Version)
	_, _ = s.store.SendMail(ctx, clawWorldSystemID, targets, subject, body)
	_, _ = s.store.CreateKBThreadMessage(ctx, store.KBThreadMessage{
		ProposalID:  proposalID,
		AuthorID:    clawWorldSystemID,
		MessageType: "system",
		Content:     "proposal applied and broadcast sent",
	})
}

type chatSendRequest struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
}

type openClawAgentResponse struct {
	Status   string `json:"status"`
	Payloads []struct {
		Text string `json:"text"`
	} `json:"payloads"`
	Meta struct {
		AgentMeta struct {
			SessionID string `json:"sessionId"`
		} `json:"agentMeta"`
	} `json:"meta"`
	Result struct {
		Payloads []struct {
			Text string `json:"text"`
		} `json:"payloads"`
		Meta struct {
			AgentMeta struct {
				SessionID string `json:"sessionId"`
			} `json:"agentMeta"`
		} `json:"meta"`
	} `json:"result"`
}

func (s *Server) handleChatSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req chatSendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Message = strings.TrimSpace(req.Message)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.appendCommCostEvent(r.Context(), req.UserID, "comm.chat.send", int64(utf8.RuneCountInString(req.Message)), map[string]any{
		"message_len": utf8.RuneCountInString(req.Message),
		"source":      "dashboard",
	})
	ask := s.appendChat(req.UserID, clawWorldSystemID, req.UserID, req.Message)
	task := s.enqueueChatTask(req.UserID, req.Message)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"items":        []chatMessage{ask},
		"status":       task.Status,
		"chat_task_id": task.TaskID,
		"chat_task":    task,
		"openclaw_via": "openclaw agent async",
	})
}

func (s *Server) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 300)
	itemsFromDB, err := s.store.ListChatMessages(r.Context(), userID, limit)
	if err == nil && len(itemsFromDB) > 0 {
		out := make([]chatMessage, 0, len(itemsFromDB))
		for _, it := range itemsFromDB {
			out = append(out, chatMessage{
				ID:     it.ID,
				UserID: it.UserID,
				From:   it.From,
				To:     it.To,
				Body:   it.Body,
				SentAt: it.SentAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": out})
		return
	}
	s.chatMu.Lock()
	items := append([]chatMessage(nil), s.chatHistory[userID]...)
	s.chatMu.Unlock()
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan chatMessage, 64)
	s.chatMu.Lock()
	if s.chatSubs[userID] == nil {
		s.chatSubs[userID] = make(map[chan chatMessage]struct{})
	}
	s.chatSubs[userID][ch] = struct{}{}
	s.chatMu.Unlock()
	defer func() {
		s.chatMu.Lock()
		delete(s.chatSubs[userID], ch)
		if len(s.chatSubs[userID]) == 0 {
			delete(s.chatSubs, userID)
		}
		s.chatMu.Unlock()
		close(ch)
	}()

	io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case msg := <-ch:
			payload, _ := json.Marshal(msg)
			io.WriteString(w, "event: message\n")
			io.WriteString(w, "data: ")
			w.Write(payload)
			io.WriteString(w, "\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleChatState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	writeJSON(w, http.StatusOK, s.chatStateSnapshot(userID))
}

func (s *Server) chatWorkerCount() int {
	v := s.cfg.ChatWorkerCount
	if v <= 0 {
		return 1
	}
	if v > 64 {
		return 64
	}
	return v
}

func (s *Server) chatReplyTimeout() time.Duration {
	d := s.cfg.ChatReplyTimeout
	if d <= 0 {
		return 90 * time.Second
	}
	return d
}

func (s *Server) chatRetryDelay() time.Duration {
	d := s.cfg.ChatRetryDelay
	if d <= 0 {
		return 600 * time.Millisecond
	}
	return d
}

func (s *Server) chatExecTimeoutSeconds() int {
	// Keep exec timeout shorter than task timeout so we can return partial reply
	// instead of waiting until context deadline.
	d := s.chatReplyTimeout() - 10*time.Second
	sec := int(d / time.Second)
	if sec < 20 {
		sec = 20
	}
	// OpenClaw embedded runs can exceed 60s for larger session contexts.
	// Cap to 3 minutes to avoid truncating valid JSON replies too early.
	if sec > 180 {
		sec = 180
	}
	return sec
}

func buildOpenClawChatCommand(message, sessionID string, timeoutSec int) []string {
	base := []string{"openclaw", "agent", "--local", "--json", "--message", message}
	if strings.TrimSpace(sessionID) != "" {
		base = append(base, "--session-id", strings.TrimSpace(sessionID))
	} else {
		base = append(base, "--agent", "main")
	}
	if timeoutSec <= 0 {
		return base
	}
	cmd := []string{"timeout", strconv.Itoa(timeoutSec)}
	cmd = append(cmd, base...)
	return cmd
}

func (s *Server) enqueueChatTask(userID, message string) chatTaskRecord {
	s.startChatWorkerPool()
	now := time.Now().UTC()
	task := &chatTaskRecord{
		UserID:    userID,
		Message:   message,
		Status:    chatTaskQueuedStatus,
		CreatedAt: now,
		QueuedAt:  timePtr(now),
	}

	var runningCancel context.CancelFunc
	s.chatTaskMu.Lock()
	s.chatTaskSeq++
	task.TaskID = s.chatTaskSeq
	if s.cfg.ChatLatestWins {
		if prev := s.chatTaskPending[userID]; prev != nil && prev.TaskID != task.TaskID {
			s.finishChatTaskLocked(prev, chatTaskCanceledStatus, "", "", task.TaskID, "superseded_by_newer_pending")
		}
		for _, queued := range s.chatTaskBacklog[userID] {
			s.finishChatTaskLocked(queued, chatTaskCanceledStatus, "", "", task.TaskID, "superseded_by_newer_pending")
		}
		delete(s.chatTaskBacklog, userID)
		s.chatTaskPending[userID] = task
		if s.cfg.ChatCancelRunning {
			runningCancel = s.chatTaskCancel[userID]
		}
	} else {
		if s.chatTaskPending[userID] == nil && s.chatTaskRunning[userID] == nil {
			s.chatTaskPending[userID] = task
		} else {
			s.chatTaskBacklog[userID] = append(s.chatTaskBacklog[userID], task)
		}
	}
	s.chatTaskMu.Unlock()

	if runningCancel != nil {
		runningCancel()
	}
	s.dispatchChatUser(userID)
	return s.copyTask(task)
}

func (s *Server) dispatchChatUser(userID string) {
	if strings.TrimSpace(userID) == "" {
		return
	}
	s.chatTaskMu.Lock()
	if _, ok := s.chatTaskQueued[userID]; ok {
		s.chatTaskMu.Unlock()
		return
	}
	s.chatTaskQueued[userID] = struct{}{}
	s.chatTaskMu.Unlock()
	select {
	case s.chatTaskQueue <- userID:
	default:
		go func(uid string) {
			s.chatTaskQueue <- uid
		}(userID)
	}
}

func (s *Server) startChatWorkerPool() {
	s.chatWorkerOnce.Do(func() {
		workers := s.chatWorkerCount()
		for i := 0; i < workers; i++ {
			go s.chatWorkerLoop()
		}
	})
}

func (s *Server) chatWorkerLoop() {
	for userID := range s.chatTaskQueue {
		s.chatTaskMu.Lock()
		delete(s.chatTaskQueued, userID)
		if s.chatTaskRunning[userID] != nil {
			s.chatTaskMu.Unlock()
			continue
		}
		if s.chatTaskPending[userID] == nil {
			if backlog := s.chatTaskBacklog[userID]; len(backlog) > 0 {
				s.chatTaskPending[userID] = backlog[0]
				backlog = backlog[1:]
				if len(backlog) == 0 {
					delete(s.chatTaskBacklog, userID)
				} else {
					s.chatTaskBacklog[userID] = backlog
				}
			}
		}
		task := s.chatTaskPending[userID]
		if task == nil {
			s.chatTaskMu.Unlock()
			continue
		}
		delete(s.chatTaskPending, userID)
		now := time.Now().UTC()
		task.Status = chatTaskRunningStatus
		task.StartedAt = timePtr(now)
		task.Attempt++
		s.chatTaskRunning[userID] = task
		taskCtx, cancel := context.WithTimeout(context.Background(), s.chatReplyTimeout())
		s.chatTaskCancel[userID] = cancel
		s.chatTaskMu.Unlock()

		replyText, sessionID, podName, err := s.chatAgentInvoke(taskCtx, userID, task.Message)
		cancel()
		if err != nil {
			status := chatTaskFailedStatus
			errText := strings.TrimSpace(err.Error())
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
				status = chatTaskTimeoutStatus
			} else if errors.Is(err, context.Canceled) || errors.Is(taskCtx.Err(), context.Canceled) {
				status = chatTaskCanceledStatus
				if errText == "" {
					errText = "canceled"
				}
			}
			s.chatTaskMu.Lock()
			task.ExecutionSess = strings.TrimSpace(sessionID)
			task.ExecutionPod = strings.TrimSpace(podName)
			s.finishChatTaskLocked(task, status, errText, "", 0, "")
			delete(s.chatTaskRunning, userID)
			delete(s.chatTaskCancel, userID)
			hasPending := s.chatTaskPending[userID] != nil || len(s.chatTaskBacklog[userID]) > 0
			s.chatTaskMu.Unlock()
			if status == chatTaskFailedStatus || status == chatTaskTimeoutStatus {
				s.appendChat(userID, clawWorldSystemID, userID, "chat delivery failed: "+errText)
			}
			if hasPending {
				s.dispatchChatUser(userID)
			}
			continue
		}

		s.appendThinkCostEvent(context.Background(), userID, int64(utf8.RuneCountInString(task.Message)), int64(utf8.RuneCountInString(replyText)), map[string]any{
			"source": "openclaw.chat.reply",
		})
		s.appendChat(userID, userID, clawWorldSystemID, replyText)

		s.chatTaskMu.Lock()
		task.ExecutionSess = strings.TrimSpace(sessionID)
		task.ExecutionPod = strings.TrimSpace(podName)
		s.finishChatTaskLocked(task, chatTaskSucceededStatus, "", replyText, 0, "")
		delete(s.chatTaskRunning, userID)
		delete(s.chatTaskCancel, userID)
		hasPending := s.chatTaskPending[userID] != nil || len(s.chatTaskBacklog[userID]) > 0
		s.chatTaskMu.Unlock()
		if hasPending {
			s.dispatchChatUser(userID)
		}
	}
}

func (s *Server) finishChatTaskLocked(task *chatTaskRecord, status, errText, reply string, supersededBy int64, cancelReason string) {
	now := time.Now().UTC()
	task.Status = status
	task.Error = strings.TrimSpace(errText)
	task.Reply = strings.TrimSpace(reply)
	task.SupersededBy = supersededBy
	task.CancelReason = strings.TrimSpace(cancelReason)
	task.FinishedAt = timePtr(now)
	recent := append(s.chatTaskRecent[task.UserID], s.copyTask(task))
	if len(recent) > chatRecentTaskLimit {
		recent = recent[len(recent)-chatRecentTaskLimit:]
	}
	s.chatTaskRecent[task.UserID] = recent
}

func (s *Server) copyTask(task *chatTaskRecord) chatTaskRecord {
	if task == nil {
		return chatTaskRecord{}
	}
	cp := *task
	return cp
}

func timePtr(v time.Time) *time.Time {
	t := v
	return &t
}

func (s *Server) chatStateSnapshot(userID string) chatStateView {
	s.chatTaskMu.Lock()
	defer s.chatTaskMu.Unlock()
	out := chatStateView{
		UserID:         userID,
		Workers:        s.chatWorkerCount(),
		QueueSize:      cap(s.chatTaskQueue),
		QueuedUsers:    len(s.chatTaskQueued),
		Backlog:        len(s.chatTaskBacklog[userID]),
		Recent:         append([]chatTaskRecord(nil), s.chatTaskRecent[userID]...),
		RecentStatuses: make(map[string]int64),
	}
	if pending := s.chatTaskPending[userID]; pending != nil {
		cp := s.copyTask(pending)
		out.Pending = &cp
	}
	if running := s.chatTaskRunning[userID]; running != nil {
		cp := s.copyTask(running)
		out.Running = &cp
	}
	for _, it := range out.Recent {
		out.RecentStatuses[it.Status]++
	}
	if n := len(out.Recent); n > 0 {
		last := out.Recent[n-1]
		out.LastStatus = last.Status
		out.LastError = last.Error
		out.LastUpdatedAt = last.FinishedAt
		if out.LastUpdatedAt == nil {
			out.LastUpdatedAt = last.StartedAt
		}
	}
	return out
}

func (s *Server) runCmd(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if err != nil {
		return combined, err
	}
	return combined, nil
}

func (s *Server) appendChat(userID, from, to, body string) chatMessage {
	s.chatMu.Lock()
	s.nextChatID++
	item := chatMessage{
		ID:     s.nextChatID,
		UserID: userID,
		From:   from,
		To:     to,
		Body:   body,
		SentAt: time.Now().UTC(),
	}
	s.chatHistory[userID] = append(s.chatHistory[userID], item)
	if len(s.chatHistory[userID]) > 1000 {
		s.chatHistory[userID] = s.chatHistory[userID][len(s.chatHistory[userID])-1000:]
	}
	subs := make([]chan chatMessage, 0, len(s.chatSubs[userID]))
	for ch := range s.chatSubs[userID] {
		subs = append(subs, ch)
	}
	s.chatMu.Unlock()

	s.persistChatAsync(item)
	for _, ch := range subs {
		select {
		case ch <- item:
		default:
		}
	}
	return item
}

func (s *Server) persistChatAsync(item chatMessage) {
	select {
	case s.chatPersistCh <- item:
	default:
		log.Printf("chat_persist_drop user_id=%s id=%d", item.UserID, item.ID)
	}
}

func (s *Server) startChatPersistLoop() {
	for item := range s.chatPersistCh {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := s.store.AppendChatMessage(ctx, store.ChatMessage{
			UserID: item.UserID,
			From:   item.From,
			To:     item.To,
			Body:   item.Body,
			SentAt: item.SentAt,
		})
		cancel()
		if err != nil {
			log.Printf("chat_persist_error user_id=%s err=%v", item.UserID, err)
		}
	}
}

func (s *Server) chatAgentInvoke(ctx context.Context, userID, message string) (reply string, sessionID string, podName string, err error) {
	if s.chatAgentCall != nil {
		return s.chatAgentCall(ctx, userID, message)
	}
	return s.sendChatToOpenClaw(ctx, userID, message)
}

func (s *Server) acquireChatExec(ctx context.Context) error {
	if s.chatExecSem == nil {
		return nil
	}
	select {
	case s.chatExecSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) releaseChatExec() {
	if s.chatExecSem == nil {
		return
	}
	select {
	case <-s.chatExecSem:
	default:
	}
}

func (s *Server) userExecSemaphore(userID string) chan struct{} {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	s.chatUserExecMu.Lock()
	defer s.chatUserExecMu.Unlock()
	sem, ok := s.chatUserExecSem[userID]
	if !ok {
		sem = make(chan struct{}, 1)
		s.chatUserExecSem[userID] = sem
	}
	return sem
}

func (s *Server) acquireChatUserExec(ctx context.Context, userID string) error {
	sem := s.userExecSemaphore(userID)
	if sem == nil {
		return nil
	}
	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) releaseChatUserExec(userID string) {
	sem := s.userExecSemaphore(userID)
	if sem == nil {
		return
	}
	select {
	case <-sem:
	default:
	}
}

func (s *Server) sendChatToOpenClaw(ctx context.Context, userID, message string) (reply string, sessionID string, podName string, err error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", "", "", errors.New("user_id is required")
	}
	if err := s.acquireChatUserExec(ctx, userID); err != nil {
		return "", "", "", err
	}
	defer s.releaseChatUserExec(userID)

	if err := s.acquireChatExec(ctx); err != nil {
		return "", "", "", err
	}
	defer s.releaseChatExec()

	if s.kubeClient == nil {
		return "", "", "", errors.New("kubernetes client is not available")
	}
	podName, err = s.latestBotPodName(ctx, userID)
	if err != nil {
		if errors.Is(err, errBotPodNotFound) {
			return "", "", "", fmt.Errorf("no running bot pod for user_id=%s: %w", userID, errBotPodNotFound)
		}
		return "", "", "", fmt.Errorf("resolve latest bot pod for user_id=%s: %w", userID, err)
	}
	sessionID = s.currentOrDefaultChatSessionID(userID)

	// Use embedded local mode to avoid gateway pairing/auth handshake mismatch
	// between runtime exec and OpenClaw control-plane websocket auth.
	execTimeoutSec := s.chatExecTimeoutSeconds()
	cmd := buildOpenClawChatCommand(message, sessionID, execTimeoutSec)

	stdout, stderr, err := s.execInBotPod(ctx, podName, cmd)
	for attempt := 0; err != nil && isGatewayWarmupError(stderr) && attempt < maxInt(0, s.cfg.ChatWarmupRetries); attempt++ {
		if !sleepContext(ctx, s.chatRetryDelay()) {
			return "", "", podName, ctx.Err()
		}
		stdout, stderr, err = s.execInBotPod(ctx, podName, cmd)
	}
	if err != nil && isSessionLockError(stdout, stderr) {
		// Session lock can happen when OpenClaw keeps the previous session file lock.
		// Reset stored session and retry with a fresh per-user session until context timeout.
		s.chatMu.Lock()
		delete(s.chatSessions, userID)
		s.chatMu.Unlock()
		retrySessionID := nextRuntimeChatRetrySessionID(userID)
		sessionID = retrySessionID
		retryCmd := buildOpenClawChatCommand(message, retrySessionID, execTimeoutSec)
		for {
			if !sleepContext(ctx, s.chatRetryDelay()) {
				return "", "", podName, ctx.Err()
			}
			stdout, stderr, err = s.execInBotPod(ctx, podName, retryCmd)
			if err == nil || !isSessionLockError(stdout, stderr) {
				break
			}
		}
	}
	if err != nil {
		if isContextTimeoutOrCancel(err, ctx) {
			if recovered := extractFallbackReply(stdout, stderr); recovered != "" {
				if strings.Contains(strings.ToLower(recovered), "session file locked") {
					return "", "", podName, fmt.Errorf("%v: %s", err, strings.TrimSpace(recovered))
				}
				s.setChatSession(userID, sessionID)
				return recovered, sessionID, podName, nil
			}
			if strings.TrimSpace(stderr) != "" {
				return "", "", podName, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr))
			}
			return "", "", podName, err
		}
		if isSessionLockError(stdout, stderr) {
			if strings.TrimSpace(stderr) != "" {
				return "", "", podName, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr))
			}
			return "", "", podName, err
		}
		// Some OpenClaw runs return non-zero while still printing a usable reply.
		if recovered := extractFallbackReply(stdout, stderr); recovered != "" {
			if strings.Contains(strings.ToLower(recovered), "session file locked") {
				return "", "", podName, fmt.Errorf("%v: %s", err, strings.TrimSpace(recovered))
			}
			s.setChatSession(userID, sessionID)
			return recovered, sessionID, podName, nil
		}
		if strings.TrimSpace(stderr) != "" {
			return "", "", podName, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr))
		}
		return "", "", podName, err
	}
	run, ok := parseOpenClawAgentOutput(stdout)
	if !ok {
		// Fallback: OpenClaw may emit plain text or mixed diagnostic output.
		// In that case, keep chat usable by extracting the last meaningful text line.
		reply = extractFallbackReply(stdout, stderr)
		if reply == "" {
			if strings.TrimSpace(stderr) != "" {
				return "", "", podName, fmt.Errorf("invalid openclaw response: stderr=%s; stdout=%s", strings.TrimSpace(stderr), strings.TrimSpace(stdout))
			}
			return "", "", podName, fmt.Errorf("invalid openclaw response: stdout=%s", strings.TrimSpace(stdout))
		}
		if strings.Contains(strings.ToLower(reply), "session file locked") {
			return "", "", podName, fmt.Errorf("openclaw session lock error: %s", reply)
		}
		s.setChatSession(userID, sessionID)
		return reply, sessionID, podName, nil
	}
	parts := make([]string, 0, len(run.Result.Payloads))
	payloads := run.Result.Payloads
	if len(payloads) == 0 && len(run.Payloads) > 0 {
		payloads = run.Payloads
	}
	for _, p := range payloads {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	reply = strings.TrimSpace(strings.Join(parts, "\n"))
	if reply == "" {
		alt := extractFallbackReply(stdout, stderr)
		if alt != "" {
			reply = alt
		} else {
			reply = "(empty reply)"
		}
	}
	sessionFromRun := strings.TrimSpace(run.Result.Meta.AgentMeta.SessionID)
	if sessionFromRun == "" {
		sessionFromRun = strings.TrimSpace(run.Meta.AgentMeta.SessionID)
	}
	if sessionFromRun != "" {
		sessionID = sessionFromRun
	}
	s.setChatSession(userID, sessionID)
	return reply, sessionID, podName, nil
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func defaultRuntimeChatSessionID(userID string) string {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		return ""
	}
	return "runtime-chat-" + uid
}

func nextRuntimeChatRetrySessionID(userID string) string {
	base := defaultRuntimeChatSessionID(userID)
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s-retry-%d", base, time.Now().UTC().UnixNano())
}

func (s *Server) setChatSession(userID, sessionID string) {
	uid := strings.TrimSpace(userID)
	sid := strings.TrimSpace(sessionID)
	if uid == "" || sid == "" {
		return
	}
	s.chatMu.Lock()
	if s.chatSessions == nil {
		s.chatSessions = make(map[string]string)
	}
	s.chatSessions[uid] = sid
	s.chatMu.Unlock()
}

func (s *Server) currentOrDefaultChatSessionID(userID string) string {
	uid := strings.TrimSpace(userID)
	if uid == "" {
		return ""
	}
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if s.chatSessions == nil {
		s.chatSessions = make(map[string]string)
	}
	sessionID := strings.TrimSpace(s.chatSessions[uid])
	if sessionID == "" {
		sessionID = defaultRuntimeChatSessionID(uid)
		if sessionID != "" {
			s.chatSessions[uid] = sessionID
		}
	}
	return sessionID
}

func parseOpenClawAgentOutput(stdout string) (openClawAgentResponse, bool) {
	var run openClawAgentResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &run); err == nil {
		return run, true
	}
	lines := strings.Split(stdout, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || (!strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[")) {
			continue
		}
		var candidate openClawAgentResponse
		if err := json.Unmarshal([]byte(line), &candidate); err == nil {
			return candidate, true
		}
	}
	trimmed := strings.TrimSpace(stdout)
	for i := strings.LastIndex(trimmed, "{"); i >= 0; {
		candidateJSON := strings.TrimSpace(trimmed[i:])
		var candidate openClawAgentResponse
		if err := json.Unmarshal([]byte(candidateJSON), &candidate); err == nil {
			return candidate, true
		}
		prev := strings.LastIndex(trimmed[:i], "{")
		if prev < 0 {
			break
		}
		i = prev
	}
	return openClawAgentResponse{}, false
}

func extractFallbackReply(stdout, stderr string) string {
	combined := strings.TrimSpace(stdout + "\n" + stderr)
	if combined == "" {
		return ""
	}
	lines := strings.Split(combined, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "[diagnostic]") || strings.Contains(lower, "lane task") {
			continue
		}
		if strings.Contains(lower, "[gateway]") ||
			strings.Contains(lower, "[plugins]") ||
			strings.Contains(lower, "[agent/embedded]") ||
			strings.Contains(lower, "[context-diag]") ||
			strings.Contains(lower, "plugins.allow is empty") {
			continue
		}
		if strings.HasPrefix(lower, "gateway connect failed:") ||
			strings.HasPrefix(lower, "gateway agent failed;") ||
			strings.HasPrefix(lower, "gateway target:") ||
			strings.HasPrefix(lower, "source:") ||
			strings.HasPrefix(lower, "config:") ||
			strings.HasPrefix(lower, "bind:") {
			continue
		}
		// Strip ISO timestamp prefix if present.
		if idx := strings.Index(line, " "); idx > 0 {
			prefix := line[:idx]
			if _, err := time.Parse(time.RFC3339Nano, prefix); err == nil {
				line = strings.TrimSpace(line[idx+1:])
			}
		}
		// Strip duplicated timestamp prefix frequently emitted by OpenClaw logs.
		if idx := strings.Index(line, " "); idx > 0 {
			prefix := line[:idx]
			if _, err := time.Parse(time.RFC3339Nano, prefix); err == nil {
				line = strings.TrimSpace(line[idx+1:])
			}
		}
		if line == "{" || line == "}" || line == "[" || line == "]" {
			continue
		}
		if line != "" {
			return line
		}
	}
	return ""
}

func isGatewayWarmupError(stderr string) bool {
	msg := strings.ToLower(strings.TrimSpace(stderr))
	if msg == "" {
		return false
	}
	return strings.Contains(msg, "gateway closed (1006") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no close reason")
}

func isSessionLockError(stdout, stderr string) bool {
	text := strings.ToLower(strings.TrimSpace(stderr + "\n" + stdout))
	if text == "" {
		return false
	}
	return strings.Contains(text, "session file locked")
}

func isContextTimeoutOrCancel(err error, ctx context.Context) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled)
}

var mainSessionLockPathRE = regexp.MustCompile(`/home/node/\.openclaw/agents/main/sessions/[A-Za-z0-9._-]+\.jsonl\.lock`)

func extractSessionLockPath(text string) (string, bool) {
	path := strings.TrimSpace(mainSessionLockPathRE.FindString(text))
	if path == "" {
		return "", false
	}
	return path, true
}

func (s *Server) latestBotPodName(ctx context.Context, userID string) (string, error) {
	pod, err := s.latestBotPod(ctx, userID)
	if err != nil {
		return "", err
	}
	return pod.Name, nil
}

func (s *Server) latestBotPod(ctx context.Context, userID string) (*corev1.Pod, error) {
	pods, err := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).List(ctx, metav1.ListOptions{LabelSelector: "app=aibot"})
	if err != nil {
		return nil, err
	}
	filtered := make([]corev1.Pod, 0, len(pods.Items))
	for _, p := range pods.Items {
		if workloadMatchesUserID(p.Name, p.Labels, userID) {
			filtered = append(filtered, p)
		}
	}
	pods.Items = filtered
	if len(pods.Items) == 0 {
		return nil, errBotPodNotFound
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		si := podHealthRank(&pods.Items[i])
		sj := podHealthRank(&pods.Items[j])
		if si != sj {
			return si < sj
		}
		return pods.Items[i].CreationTimestamp.Time.After(pods.Items[j].CreationTimestamp.Time)
	})
	return &pods.Items[0], nil
}

func podHealthRank(pod *corev1.Pod) int {
	hasIP := strings.TrimSpace(pod.Status.PodIP) != ""
	running := pod.Status.Phase == corev1.PodRunning
	ready := false
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}
	switch {
	case running && ready && hasIP:
		return 0
	case running && hasIP:
		return 1
	case hasIP:
		return 2
	default:
		return 3
	}
}

func (s *Server) execInBotPod(ctx context.Context, podName string, command []string) (string, string, error) {
	req := s.kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(s.cfg.BotNamespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "bot",
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	if s.kubeRESTCfg == nil {
		return "", "", errors.New("kubernetes rest config is not available")
	}
	exec, err := remotecommand.NewSPDYExecutor(s.kubeRESTCfg, http.MethodPost, req.URL())
	if err != nil {
		return "", "", err
	}
	var stdout, stderr bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return stdout.String(), stderr.String(), err
	}
	return stdout.String(), stderr.String(), nil
}

func (s *Server) handleRequestLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	filter := store.RequestLogFilter{
		Limit:        parseLimit(r.URL.Query().Get("limit"), 300),
		Method:       strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("method"))),
		PathContains: strings.TrimSpace(r.URL.Query().Get("path")),
		UserID:       strings.TrimSpace(r.URL.Query().Get("user_id")),
		StatusCode:   parseStatusCode(r.URL.Query().Get("status")),
	}
	items, err := s.store.ListRequestLogs(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]requestLogEntry, 0, len(items))
	for _, it := range items {
		out = append(out, requestLogEntry{
			ID:         it.ID,
			Time:       it.Time,
			Method:     it.Method,
			Path:       it.Path,
			UserID:     it.UserID,
			StatusCode: it.StatusCode,
			DurationMS: it.DurationMS,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func joinURLPath(basePath, nextPath string) string {
	basePath = strings.TrimRight(strings.TrimSpace(basePath), "/")
	nextPath = "/" + strings.TrimLeft(strings.TrimSpace(nextPath), "/")
	if basePath == "" {
		return nextPath
	}
	return basePath + nextPath
}

func sanitizeDevTargetPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "/", nil
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	unescaped := p
	for i := 0; i < 3; i++ {
		next, err := neturl.PathUnescape(unescaped)
		if err != nil {
			return "", fmt.Errorf("invalid path")
		}
		if next == unescaped {
			break
		}
		unescaped = next
	}
	for _, seg := range strings.Split(strings.ReplaceAll(unescaped, "\\", "/"), "/") {
		if strings.TrimSpace(seg) == ".." {
			return "", fmt.Errorf("path traversal is not allowed")
		}
	}
	cleaned := path.Clean(unescaped)
	if cleaned == "." || cleaned == "" {
		cleaned = "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned, nil
}

func secureStringEqual(a, b string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func runtimeDevProxyTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	token := strings.TrimSpace(r.URL.Query().Get(devProxyParamToken))
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const bearer = "Bearer "
	if len(auth) > len(bearer) && strings.EqualFold(auth[:len(bearer)], bearer) {
		token = strings.TrimSpace(auth[len(bearer):])
	}
	if strings.TrimSpace(token) == "" {
		token = strings.TrimSpace(r.Header.Get("X-Clawcolony-Gateway-Token"))
	}
	return strings.TrimSpace(token)
}

func hasValidRuntimeDevProxyToken(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	return secureStringEqual(runtimeDevProxyTokenFromRequest(r), expected)
}

func runtimeDevProxySanitizedRawQuery(q neturl.Values) string {
	if len(q) == 0 {
		return ""
	}
	cp := neturl.Values{}
	for key, values := range q {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		switch k {
		case devProxyParamToken, devProxySignedParamSig, devProxySignedParamExp, devProxySignedParamNonce:
			continue
		}
		for _, v := range values {
			cp.Add(k, v)
		}
	}
	return cp.Encode()
}

func normalizeDevLinkTarget(raw string) (string, string, error) {
	src := strings.TrimSpace(raw)
	if src == "" {
		return "/", "", nil
	}
	lower := strings.ToLower(src)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", "", fmt.Errorf("path must be relative")
	}
	if !strings.HasPrefix(src, "/") {
		src = "/" + src
	}
	u, err := neturl.ParseRequestURI(src)
	if err != nil {
		return "", "", fmt.Errorf("invalid path")
	}
	targetPath, err := sanitizeDevTargetPath(u.EscapedPath())
	if err != nil {
		return "", "", err
	}
	return targetPath, runtimeDevProxySanitizedRawQuery(u.Query()), nil
}

func parseDevPreviewAllowedPorts(raw string) []int {
	parse := func(v string) []int {
		out := []int{}
		seen := map[int]struct{}{}
		for _, part := range strings.Split(v, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n <= 0 || n > 65535 {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
		sort.Ints(out)
		return out
	}
	out := parse(raw)
	if len(out) > 0 {
		return out
	}
	return parse(defaultPreviewAllowedPorts)
}

func (s *Server) devPreviewAllowedPorts() []int {
	raw := strings.TrimSpace(s.cfg.PreviewAllowedPorts)
	if raw == "" {
		raw = defaultPreviewAllowedPorts
	}
	return parseDevPreviewAllowedPorts(raw)
}

func isPortAllowed(allowed []int, port int) bool {
	for _, p := range allowed {
		if p == port {
			return true
		}
	}
	return false
}

func allowedPortsText(allowed []int) string {
	if len(allowed) == 0 {
		return defaultPreviewAllowedPorts
	}
	parts := make([]string, 0, len(allowed))
	for _, p := range allowed {
		parts = append(parts, strconv.Itoa(p))
	}
	return strings.Join(parts, ",")
}

func parsePreviewPort(raw string, fallback int) (int, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(v)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port must be an integer in [1, 65535]")
	}
	return port, nil
}

func runtimeDevProxyRoutePath(userID string, port int, targetPath string) string {
	uid := neturl.PathEscape(strings.TrimSpace(userID))
	base := fmt.Sprintf("/v1/bots/dev/%s/p/%d", uid, port)
	if strings.TrimSpace(targetPath) == "" || targetPath == "/" {
		return base + "/"
	}
	return base + "/" + strings.TrimPrefix(targetPath, "/")
}

func requestScheme(r *http.Request) string {
	if r != nil && (r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")) {
		return "https"
	}
	return "http"
}

func absoluteURLFromRequest(r *http.Request, relativePath string) string {
	if r == nil || strings.TrimSpace(r.Host) == "" {
		return relativePath
	}
	return fmt.Sprintf("%s://%s%s", requestScheme(r), r.Host, relativePath)
}

func resolvePublicURL(base, relativePath string) string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return ""
	}
	baseURL, err := neturl.Parse(trimmed)
	if err != nil || strings.TrimSpace(baseURL.Scheme) == "" || strings.TrimSpace(baseURL.Host) == "" {
		return ""
	}
	relURL, err := neturl.Parse(relativePath)
	if err != nil {
		return ""
	}
	return baseURL.ResolveReference(relURL).String()
}

func isSafePreviewTemplateUserID(userID string) bool {
	if strings.TrimSpace(userID) == "" {
		return false
	}
	for _, r := range userID {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func (s *Server) previewUpstreamURL(userID string, port int) (*neturl.URL, error) {
	uid := strings.TrimSpace(userID)
	if !isSafePreviewTemplateUserID(uid) {
		return nil, fmt.Errorf("invalid user_id for preview routing")
	}
	tpl := strings.TrimSpace(s.cfg.PreviewUpstreamTemplate)
	if tpl == "" {
		tpl = defaultPreviewUpstreamTemplate
	}
	raw := strings.ReplaceAll(tpl, "{{user_id}}", uid)
	raw = strings.ReplaceAll(raw, "{{port}}", strconv.Itoa(port))
	u, err := neturl.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid preview upstream template: %w", err)
	}
	if strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return nil, fmt.Errorf("invalid preview upstream template: missing scheme/host")
	}
	return u, nil
}

func (s *Server) previewLinkTTLDays(ctx context.Context) int64 {
	item, _, _ := s.getRuntimeSchedulerSettings(ctx)
	if item.PreviewLinkTTLDays <= 0 {
		return runtimeSchedulerDefaultPreviewLinkTTLDays
	}
	return clampInt64(item.PreviewLinkTTLDays, runtimeSchedulerMinPreviewLinkTTLDays, runtimeSchedulerMaxPreviewLinkTTLDays)
}

func randomRuntimeDevProxyNonce() (string, error) {
	buf := make([]byte, 12)
	if _, err := crand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func runtimeDevProxySignatureInput(userID string, port int, targetPath, targetQuery string, expUnix int64, nonce string) string {
	parts := []string{
		strings.TrimSpace(userID),
		strconv.Itoa(port),
		strings.TrimSpace(targetPath),
		strings.TrimSpace(targetQuery),
		strconv.FormatInt(expUnix, 10),
		strings.TrimSpace(nonce),
	}
	return strings.Join(parts, "\n")
}

func runtimeDevProxyComputeSignature(signingKey, userID string, port int, targetPath, targetQuery string, expUnix int64, nonce string) string {
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(signingKey)))
	_, _ = mac.Write([]byte(runtimeDevProxySignatureInput(userID, port, targetPath, targetQuery, expUnix, nonce)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) hasValidRuntimeDevProxySignedLink(r *http.Request, userID string, port int, targetPath, targetQuery string) bool {
	if r == nil {
		return false
	}
	signingKey := strings.TrimSpace(s.cfg.InternalSyncToken)
	if signingKey == "" {
		return false
	}
	query := r.URL.Query()
	sig := strings.TrimSpace(query.Get(devProxySignedParamSig))
	expRaw := strings.TrimSpace(query.Get(devProxySignedParamExp))
	nonce := strings.TrimSpace(query.Get(devProxySignedParamNonce))
	if sig == "" || expRaw == "" || nonce == "" {
		return false
	}
	expUnix, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || expUnix <= 0 {
		return false
	}
	nowUnix := time.Now().UTC().Unix()
	if expUnix < nowUnix {
		return false
	}
	maxFuture := int64(runtimeSchedulerMaxPreviewLinkTTLDays*24*60*60) + 300
	if expUnix > nowUnix+maxFuture {
		return false
	}
	expectedSig := runtimeDevProxyComputeSignature(signingKey, userID, port, targetPath, targetQuery, expUnix, nonce)
	return secureStringEqual(sig, expectedSig)
}

func (s *Server) handleBotDevLinkProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req botDevLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Port <= 0 {
		req.Port = 3000
	}
	if req.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be an integer in [1, 65535]")
		return
	}
	allowed := s.devPreviewAllowedPorts()
	if !isPortAllowed(allowed, req.Port) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("port is not allowed, allowed ports: %s", allowedPortsText(allowed)))
		return
	}
	if _, err := s.store.GetBot(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	expectedToken := strings.TrimSpace(s.userGatewayToken(r.Context(), req.UserID))
	if expectedToken == "" {
		writeError(w, http.StatusServiceUnavailable, "gateway token is not available")
		return
	}
	providedToken := strings.TrimSpace(req.GatewayToken)
	if providedToken == "" {
		providedToken = runtimeDevProxyTokenFromRequest(r)
	}
	if !secureStringEqual(providedToken, expectedToken) {
		writeError(w, http.StatusUnauthorized, "invalid or missing gateway token")
		return
	}
	targetPath, targetQuery, err := normalizeDevLinkTarget(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	signingKey := strings.TrimSpace(s.cfg.InternalSyncToken)
	if signingKey == "" {
		writeError(w, http.StatusServiceUnavailable, "dev link signing is not configured")
		return
	}
	nonce, err := randomRuntimeDevProxyNonce()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate dev link nonce")
		return
	}
	ttlDays := s.previewLinkTTLDays(r.Context())
	expiresAt := time.Now().UTC().Add(time.Duration(ttlDays) * 24 * time.Hour)
	expUnix := expiresAt.Unix()
	sig := runtimeDevProxyComputeSignature(signingKey, req.UserID, req.Port, targetPath, targetQuery, expUnix, nonce)
	values, err := neturl.ParseQuery(targetQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid query in path")
		return
	}
	values.Set(devProxySignedParamExp, strconv.FormatInt(expUnix, 10))
	values.Set(devProxySignedParamNonce, nonce)
	values.Set(devProxySignedParamSig, sig)
	relative := runtimeDevProxyRoutePath(req.UserID, req.Port, targetPath)
	if encoded := values.Encode(); encoded != "" {
		relative += "?" + encoded
	}
	item := map[string]any{
		"user_id":      req.UserID,
		"port":         req.Port,
		"path":         targetPath,
		"relative_url": relative,
		"absolute_url": absoluteURLFromRequest(r, relative),
		"expires_at":   expiresAt.Format(time.RFC3339),
		"ttl_days":     ttlDays,
	}
	if publicURL := resolvePublicURL(s.cfg.PreviewPublicBaseURL, relative); publicURL != "" {
		item["public_url"] = publicURL
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (s *Server) handleBotDevHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if _, err := s.store.GetBot(r.Context(), userID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	expectedToken := strings.TrimSpace(s.userGatewayToken(r.Context(), userID))
	if expectedToken == "" {
		writeError(w, http.StatusServiceUnavailable, "gateway token is not available")
		return
	}
	if !hasValidRuntimeDevProxyToken(r, expectedToken) {
		writeError(w, http.StatusUnauthorized, "invalid or missing gateway token")
		return
	}
	port, err := parsePreviewPort(r.URL.Query().Get("port"), 3000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowed := s.devPreviewAllowedPorts()
	if !isPortAllowed(allowed, port) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("port is not allowed, allowed ports: %s", allowedPortsText(allowed)))
		return
	}
	targetPath, err := sanitizeDevTargetPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	backend, err := s.previewUpstreamURL(userID, port)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	checkAt := time.Now().UTC()
	target := *backend
	target.Path = joinURLPath(backend.Path, targetPath)
	target.RawQuery = ""
	ctx, cancel := context.WithTimeout(r.Context(), devProxyHealthTimeout)
	defer cancel()
	upReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	upReq.Header.Set("Accept", "*/*")
	client := s.previewHealthClient
	if client == nil {
		client = &http.Client{Transport: s.openclawProxy, Timeout: devProxyHealthTimeout}
	}
	resp, err := client.Do(upReq)
	if err != nil {
		log.Printf("bot_dev_health_check_failed user_id=%s port=%d path=%s err=%v", userID, port, targetPath, err)
		writeJSON(w, http.StatusOK, map[string]any{
			"item": botDevHealthItem{
				UserID:    userID,
				Port:      port,
				Path:      targetPath,
				OK:        false,
				Error:     "preview upstream request failed",
				CheckedAt: checkAt.Format(time.RFC3339),
			},
		})
		return
	}
	defer resp.Body.Close()
	ok := resp.StatusCode >= 200 && resp.StatusCode < 400
	item := botDevHealthItem{
		UserID:     userID,
		Port:       port,
		Path:       targetPath,
		OK:         ok,
		StatusCode: resp.StatusCode,
		CheckedAt:  checkAt.Format(time.RFC3339),
	}
	if !ok {
		item.Error = resp.Status
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func parseDevProxyForwardRoute(pathRaw string) (string, int, string, error) {
	base := "/v1/bots/dev/"
	if !strings.HasPrefix(pathRaw, base) {
		return "", 0, "", fmt.Errorf("invalid path")
	}
	trimmed := strings.TrimPrefix(pathRaw, base)
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", 0, "", fmt.Errorf("user_id is required in path")
	}
	userSeg, err := neturl.PathUnescape(parts[0])
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid user_id in path")
	}
	userID := strings.TrimSpace(userSeg)
	if userID == "" {
		return "", 0, "", fmt.Errorf("user_id is required in path")
	}
	port := 3000
	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		return userID, port, "/", nil
	}
	tail := strings.TrimSpace(parts[1])
	if strings.HasPrefix(tail, "p/") {
		rest := strings.TrimPrefix(tail, "p/")
		pair := strings.SplitN(rest, "/", 2)
		if strings.TrimSpace(pair[0]) == "" {
			return "", 0, "", fmt.Errorf("port is required in path")
		}
		port, err = strconv.Atoi(strings.TrimSpace(pair[0]))
		if err != nil || port <= 0 || port > 65535 {
			return "", 0, "", fmt.Errorf("port must be an integer in [1, 65535]")
		}
		if len(pair) == 1 || strings.TrimSpace(pair[1]) == "" {
			return userID, port, "/", nil
		}
		nextPath, err := sanitizeDevTargetPath(pair[1])
		if err != nil {
			return "", 0, "", err
		}
		return userID, port, nextPath, nil
	}
	nextPath, err := sanitizeDevTargetPath(tail)
	if err != nil {
		return "", 0, "", err
	}
	return userID, port, nextPath, nil
}

func (s *Server) handleBotDevProxyForward(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID, port, targetPath, err := parseDevProxyForwardRoute(r.URL.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowed := s.devPreviewAllowedPorts()
	if !isPortAllowed(allowed, port) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("port is not allowed, allowed ports: %s", allowedPortsText(allowed)))
		return
	}
	if _, err := s.store.GetBot(r.Context(), userID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	expectedToken := strings.TrimSpace(s.userGatewayToken(r.Context(), userID))
	if expectedToken == "" {
		writeError(w, http.StatusServiceUnavailable, "gateway token is not available")
		return
	}
	targetQuery := runtimeDevProxySanitizedRawQuery(r.URL.Query())
	if !s.hasValidRuntimeDevProxySignedLink(r, userID, port, targetPath, targetQuery) && !hasValidRuntimeDevProxyToken(r, expectedToken) {
		writeError(w, http.StatusUnauthorized, "invalid or missing gateway token")
		return
	}
	backend, err := s.previewUpstreamURL(userID, port)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = s.openclawProxy
	proxy.FlushInterval = 100 * time.Millisecond
	origDirector := proxy.Director
	reqPath := joinURLPath(backend.Path, targetPath)
	reqQuery := targetQuery
	proxy.Director = func(req *http.Request) {
		incomingHost := req.Host
		origDirector(req)
		req.URL.Scheme = backend.Scheme
		req.URL.Host = backend.Host
		req.URL.Path = reqPath
		req.URL.RawQuery = reqQuery
		req.Host = incomingHost
		req.Header.Set("X-Forwarded-Host", incomingHost)
		req.Header.Del("Authorization")
		req.Header.Del("X-Clawcolony-Gateway-Token")
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, perr error) {
		log.Printf("bot_dev_proxy_forward_error user_id=%s port=%d err=%v", userID, port, perr)
		writeError(rw, http.StatusBadGateway, "preview upstream unavailable")
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) handleOpenClawProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.kubeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not available")
		return
	}
	base := "/v1/bots/openclaw/"
	if !strings.HasPrefix(r.URL.Path, base) {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, base)
	parts := strings.SplitN(trimmed, "/", 2)
	userID := strings.TrimSpace(parts[0])
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required in path")
		return
	}
	target := ""
	if len(parts) == 2 {
		target = strings.TrimPrefix(parts[1], "/")
	}
	pod, err := s.latestBotPod(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	podIP := strings.TrimSpace(pod.Status.PodIP)
	if podIP == "" {
		writeError(w, http.StatusBadGateway, "bot pod has no IP")
		return
	}
	backend, err := neturl.Parse("http://" + podIP + ":18789")
	if err != nil {
		writeError(w, http.StatusBadGateway, "invalid openclaw backend")
		return
	}
	if target == "" {
		target = "/"
	} else {
		target = "/" + strings.TrimPrefix(target, "/")
	}
	if target == "/" && !isWebSocketUpgradeRequest(r) {
		s.serveOpenClawDashboardHTML(w, r, backend, userID)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = s.openclawProxy
	proxy.FlushInterval = 100 * time.Millisecond
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		incomingHost := req.Host
		origDirector(req)
		req.URL.Scheme = backend.Scheme
		req.URL.Host = backend.Host
		req.Host = incomingHost
		req.Header.Set("X-Forwarded-Host", incomingHost)
		req.URL.Path = target
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, perr error) {
		writeError(rw, http.StatusBadGateway, fmt.Sprintf("openclaw proxy error: %s", perr.Error()))
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if strings.TrimSpace(resp.Header.Get("Content-Type")) == "" {
			ext := path.Ext(target)
			if ct := mime.TypeByExtension(ext); ct != "" {
				resp.Header.Set("Content-Type", ct)
			}
		}
		return nil
	}
	proxy.ServeHTTP(w, r)
}

func (s *Server) handleOpenClawStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.kubeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not available")
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	pod, err := s.latestBotPod(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	status, err := s.inspectOpenClawConnectionStatus(r.Context(), userID, pod.Name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) inspectOpenClawConnectionStatus(ctx context.Context, userID, podName string) (openClawConnStatus, error) {
	out := openClawConnStatus{
		UserID:  userID,
		PodName: podName,
	}
	req := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: "bot",
		TailLines: int64Ptr(400),
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return out, err
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		return out, err
	}
	lines := strings.Split(string(data), "\n")

	active := make(map[string]bool)
	connectedRe := regexp.MustCompile(`\[(?:ws)\].*webchat connected.*conn=([a-z0-9-]+)`)
	disconnectedRe := regexp.MustCompile(`\[(?:ws)\].*webchat disconnected.*code=([0-9]+).*reason=([^ ]+).*conn=([a-z0-9-]+)`)
	closedRe := regexp.MustCompile(`\[(?:ws)\].*closed before connect.*conn=([a-z0-9-]+).*code=([0-9]+).*reason=([^ ]+)`)

	lastTS := ""
	lastEvent := ""
	lastReason := ""
	lastCode := 0
	lastDetail := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "[ws]") {
			continue
		}
		if idx := strings.Index(line, " "); idx > 0 {
			lastTS = line[:idx]
		}
		if m := connectedRe.FindStringSubmatch(line); len(m) == 2 {
			active[m[1]] = true
			lastEvent = "connected"
			lastDetail = "webchat connected"
			continue
		}
		if m := disconnectedRe.FindStringSubmatch(line); len(m) == 4 {
			delete(active, m[3])
			lastEvent = "disconnected"
			lastReason = m[2]
			lastCode, _ = strconv.Atoi(m[1])
			lastDetail = "webchat disconnected"
			continue
		}
		if m := closedRe.FindStringSubmatch(line); len(m) == 4 {
			delete(active, m[1])
			lastEvent = "closed_before_connect"
			lastCode, _ = strconv.Atoi(m[2])
			lastReason = m[3]
			lastDetail = "closed before connect"
			continue
		}
	}

	out.ActiveWebchatConns = len(active)
	out.Connected = out.ActiveWebchatConns > 0
	out.LastEventType = lastEvent
	out.LastEventAt = lastTS
	out.LastDisconnectReason = lastReason
	out.LastDisconnectCode = lastCode
	out.Detail = lastDetail
	return out, nil
}

func (s *Server) resolveTemplateContextUser(ctx context.Context, userID string) store.Bot {
	userID = strings.TrimSpace(userID)
	sample := store.Bot{
		BotID:       "user-example",
		Name:        "example-user",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}
	if userID == "" {
		return sample
	}
	items, err := s.store.ListBots(ctx)
	if err != nil {
		sample.BotID = userID
		sample.Name = userID
		return sample
	}
	for _, it := range items {
		if it.BotID == userID {
			return it
		}
	}
	sample.BotID = userID
	sample.Name = userID
	return sample
}

func (s *Server) defaultPromptTemplateMap(_ context.Context, user store.Bot) map[string]string {
	api := s.defaultAPIBaseURL()
	return map[string]string{
		bot.TemplateProtocolReadme:        bot.BuildProtocolReadme(api, user),
		bot.TemplateIdentityDoc:           bot.BuildIdentityDocument(user),
		bot.TemplateAgentsDoc:             bot.BuildAgentInstructionsDocument(user),
		bot.TemplateSoulDoc:               bot.BuildSoulDocument(user.BotID),
		bot.TemplateBootstrapDoc:          bot.BuildBootstrapDocument(user.BotID),
		bot.TemplateToolsDoc:              bot.BuildToolsDocument(user.BotID),
		bot.TemplateSkillAutonomyPolicy:   bot.BuildSkillAutonomyPolicy(),
		bot.TemplateClawWorldSkill:        bot.BuildClawWorldSkillMCPOnly(api, user),
		bot.TemplateColonyCoreSkill:       bot.BuildColonyCoreSkillMCPOnly(api, user),
		bot.TemplateColonyToolsSkill:      bot.BuildColonyToolsSkillMCPOnly(api, user),
		bot.TemplateKnowledgeBaseSkill:    bot.BuildKnowledgeBaseSkillMCPOnly(api, user),
		bot.TemplateGangliaStackSkill:     bot.BuildGangliaStackSkillMCPOnly(api, user),
		bot.TemplateCollabModeSkill:       bot.BuildCollabModeSkillMCPOnly(api, user),
		bot.TemplateDevPreviewSkill:       bot.BuildDevPreviewSkillMCPOnly(api, user),
		bot.TemplateSelfCoreUpgradeSkill:  bot.BuildSelfCoreUpgradeSkill(api, user),
		bot.TemplateUpgradeClawcolony:     bot.BuildUpgradeClawcolonySkill(api, user),
		bot.TemplateSelfSourceReadme:      bot.BuildSelfSourceReadme(api, user),
		bot.TemplateSourceWorkspaceReadme: bot.BuildSourceWorkspaceReadme(api, user),
	}
}

func (s *Server) ensureGenesisPromptTemplateCoverage(ctx context.Context) error {
	ctxUser := s.resolveTemplateContextUser(ctx, "")
	apiBase := s.defaultAPIBaseURL()
	model := s.cfg.BotModel
	defaults := s.defaultPromptTemplateMap(ctx, ctxUser)
	dbItems, err := s.store.ListPromptTemplates(ctx)
	if err != nil {
		return err
	}
	dbByKey := make(map[string]store.PromptTemplate, len(dbItems))
	for _, it := range dbItems {
		dbByKey[it.Key] = it
	}

	keys := []string{
		bot.TemplateProtocolReadme,
		bot.TemplateAgentsDoc,
		bot.TemplateSoulDoc,
	}
	for _, key := range keys {
		base := strings.TrimSpace(defaults[key])
		if base == "" {
			continue
		}
		cur := strings.TrimSpace(dbByKey[key].Content)
		candidate := cur
		if candidate == "" {
			candidate = base
		}
		if key == bot.TemplateProtocolReadme {
			// 旧版 protocol 卡片有 runtime_interface 等鸡肋文案，直接替换为当前精简基线。
			if strings.Contains(candidate, "runtime_interface:") ||
				strings.Contains(candidate, "不要在这里硬编码 API") ||
				strings.Contains(candidate, "host/base_url/HTTP 路径") {
				candidate = base
			}
		}
		candidate = bot.EnsureGenesisTemplateCoverage(key, candidate, ctxUser)
		candidate = canonicalizePromptTemplateContent(candidate, ctxUser, apiBase, model)
		if strings.TrimSpace(cur) == strings.TrimSpace(candidate) {
			continue
		}
		if _, err := s.store.UpsertPromptTemplate(ctx, store.PromptTemplate{
			Key:     key,
			Content: candidate,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) resolveBotImageForApply(ctx context.Context, userID string) string {
	if s.kubeClient == nil {
		return ""
	}
	if dep, err := s.kubeClient.AppsV1().Deployments(s.cfg.BotNamespace).Get(ctx, userID, metav1.GetOptions{}); err == nil {
		for _, c := range dep.Spec.Template.Spec.Containers {
			if c.Name == "bot" && strings.TrimSpace(c.Image) != "" {
				return strings.TrimSpace(c.Image)
			}
		}
		for _, c := range dep.Spec.Template.Spec.Containers {
			if strings.TrimSpace(c.Image) != "" {
				return strings.TrimSpace(c.Image)
			}
		}
	}
	pod, err := s.latestBotPod(ctx, userID)
	if err != nil {
		return ""
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == "bot" && strings.TrimSpace(c.Image) != "" {
			return strings.TrimSpace(c.Image)
		}
	}
	for _, c := range pod.Spec.Containers {
		if strings.TrimSpace(c.Image) != "" {
			return strings.TrimSpace(c.Image)
		}
	}
	return ""
}

func int64Ptr(v int64) *int64 {
	return &v
}

func (s *Server) serveOpenClawDashboardHTML(w http.ResponseWriter, r *http.Request, backend *neturl.URL, userID string) {
	isGatewayStartingErr := func(err error) bool {
		if err == nil {
			return false
		}
		msg := strings.ToLower(strings.TrimSpace(err.Error()))
		if msg == "" {
			return false
		}
		if strings.Contains(msg, "context deadline exceeded") {
			// If the request context is already done, this is a client/request timeout
			// and should not be retried as a gateway warmup condition.
			return r.Context().Err() == nil
		}
		return strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "i/o timeout") ||
			strings.Contains(msg, "no route to host")
	}
	doReq := func(target *neturl.URL) ([]byte, int, error) {
		u := *target
		u.Path = "/"
		u.RawQuery = r.URL.RawQuery
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, 0, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, 0, err
		}
		return body, resp.StatusCode, nil
	}
	resolveBackend := func() *neturl.URL {
		if pod, podErr := s.latestBotPod(r.Context(), userID); podErr == nil {
			if podIP := strings.TrimSpace(pod.Status.PodIP); podIP != "" {
				if u, parseErr := neturl.Parse("http://" + podIP + ":18789"); parseErr == nil {
					return u
				}
			}
		}
		return backend
	}
	tryFetch := func(target *neturl.URL, attempts int, delay time.Duration) ([]byte, int, error) {
		var (
			body       []byte
			statusCode int
			err        error
		)
		for i := 0; i < maxInt(1, attempts); i++ {
			if i > 0 {
				if !sleepContext(r.Context(), delay) {
					return nil, 0, r.Context().Err()
				}
			}
			body, statusCode, err = doReq(target)
			if err == nil {
				return body, statusCode, nil
			}
			if !isGatewayStartingErr(err) {
				return nil, 0, err
			}
			target = resolveBackend()
		}
		return nil, 0, err
	}
	body, statusCode, err := tryFetch(resolveBackend(), 6, 700*time.Millisecond)
	if err != nil {
		if isGatewayStartingErr(err) {
			writeError(w, http.StatusServiceUnavailable, "openclaw gateway is starting, retry in a few seconds")
			return
		}
		writeError(w, http.StatusBadGateway, fmt.Sprintf("openclaw proxy error: %s", err.Error()))
		return
	}
	html := string(body)
	inject := s.openClawBootstrapScript(r, userID)
	if strings.Contains(strings.ToLower(html), "</head>") {
		html = strings.Replace(html, "</head>", inject+"</head>", 1)
	} else {
		html = inject + html
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write([]byte(html))
}

func (s *Server) openClawBootstrapScript(r *http.Request, userID string) string {
	scheme := "ws"
	if r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		scheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s/v1/bots/openclaw/%s/", scheme, r.Host, neturl.PathEscape(userID))
	token := strings.TrimSpace(s.userGatewayToken(r.Context(), userID))
	defaultSession := strings.TrimSpace(defaultRuntimeChatSessionID(userID))
	if defaultSession == "" {
		defaultSession = "main"
	}
	return fmt.Sprintf(`<script>(function(){try{var k="openclaw.control.settings.v1";var s={};var raw=localStorage.getItem(k);if(raw){try{s=JSON.parse(raw)||{}}catch(_){s={}}}s.gatewayUrl=%q;s.token=%q;var runtimeSession=%q;var currentKey=String(s.sessionKey||"").trim();if(!currentKey||currentKey==="main"||currentKey==="agent:main:main"){s.sessionKey=runtimeSession||"main";}localStorage.setItem(k,JSON.stringify(s));}catch(_){}})();</script>`, wsURL, token, defaultSession)
}

func isWebSocketUpgradeRequest(r *http.Request) bool {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Connection")), "Upgrade") &&
		!strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket")
}

func (s *Server) handleOpenClawDashboardConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	item, err := s.store.GetBot(r.Context(), userID)
	if err != nil {
		if errors.Is(err, store.ErrBotNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if strings.TrimSpace(item.BotID) == "" {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": strings.TrimSpace(s.userGatewayToken(r.Context(), userID)),
	})
}

func (s *Server) userGatewayToken(ctx context.Context, userID string) string {
	uid := strings.TrimSpace(userID)
	if uid != "" {
		if tok := strings.TrimSpace(s.runtimeGatewayTokenFromPod(ctx, uid)); tok != "" {
			return tok
		}
	}
	if uid != "" {
		creds, err := s.store.GetBotCredentials(ctx, uid)
		if err == nil {
			if tok := strings.TrimSpace(creds.GatewayToken); tok != "" {
				return tok
			}
		}
	}
	return ""
}

func (s *Server) runtimeGatewayTokenFromPod(ctx context.Context, userID string) string {
	if s.kubeClient == nil {
		return ""
	}
	pod, err := s.latestBotPod(ctx, userID)
	if err != nil || pod == nil {
		return ""
	}
	for _, c := range pod.Spec.Containers {
		if c.Name != "bot" {
			continue
		}
		for _, ev := range c.Env {
			if ev.Name == "OPENCLAW_GATEWAY_TOKEN" && strings.TrimSpace(ev.Value) != "" {
				return strings.TrimSpace(ev.Value)
			}
		}
	}
	for _, c := range pod.Spec.Containers {
		for _, ev := range c.Env {
			if ev.Name == "OPENCLAW_GATEWAY_TOKEN" && strings.TrimSpace(ev.Value) != "" {
				return strings.TrimSpace(ev.Value)
			}
		}
	}
	return ""
}

func parseRFC3339Ptr(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func parseLimit(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > 500 {
		return 500
	}
	return n
}

func parseInt64(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseStatusCode(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 100 || n > 599 {
		return 0
	}
	return n
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]any{"error": message})
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"error":   "route not found",
		"path":    r.URL.Path,
		"method":  r.Method,
		"hint":    "Use one of the official Clawcolony APIs below.",
		"apis":    s.apiCatalog(),
		"version": "v1",
	})
}

func (s *Server) apiCatalog() []string {
	full := []string{
		"POST /api/mail/send",
		"POST /api/mail/send-list",
		"GET /api/mail/inbox?user_id=<id>",
		"POST /api/mail/list/create",
		"POST /api/mail/list/join",
		"GET /api/token/balance?user_id=<id>",
		"POST /api/token/transfer",
		"POST /api/gov/propose",
		"POST /api/gov/vote",
		"POST /api/gov/cosign",
		"POST /api/gov/report",
		"GET /api/gov/laws",
		"POST /api/tools/invoke",
		"POST /api/tools/register",
		"GET /api/tools/search?query=<kw>",
		"POST /api/library/publish",
		"GET /api/library/search?query=<kw>",
		"POST /api/life/set-will",
		"POST /api/life/metamorphose",
		"POST /api/life/hibernate",
		"POST /api/life/wake",
		"POST /api/ganglia/forge",
		"GET /api/ganglia/browse?type=<type>&sort_by=<score|integrations|updated>",
		"POST /api/ganglia/integrate",
		"POST /api/ganglia/rate",
		"POST /api/bounty/post",
		"GET /api/bounty/list",
		"POST /api/bounty/verify",
		"GET /api/metabolism/score?content_id=<id>",
		"POST /api/metabolism/supersede",
		"POST /api/metabolism/dispute",
		"GET /api/metabolism/report",
		"GET /api/colony/status",
		"GET /api/colony/directory",
		"GET /api/colony/chronicle",
		"GET /api/colony/banished",
		"GET /v1/bots",
		"POST /v1/bots/nickname/upsert",
		"GET /v1/bots/logs?user_id=<id>&tail=<n>",
		"GET /v1/bots/logs/all?tail=<n>&limit=<n>",
		"POST /v1/bots/dev/link",
		"GET /v1/bots/dev/health?user_id=<id>&port=<port>&path=/",
		"GET /v1/bots/dev/<user_id>/p/<port>/",
		"GET /v1/bots/openclaw/<user_id>/",
		"GET /v1/bots/openclaw/status?user_id=<id>",
		"GET /v1/tian-dao/law",
		"GET /v1/world/tick/status",
		"GET /v1/world/freeze/status",
		"POST /v1/world/freeze/rescue",
		"GET /v1/world/tick/history?limit=<n>",
		"GET /v1/world/tick/chain/verify?limit=<n>",
		"POST /v1/world/tick/replay",
		"GET /v1/world/tick/steps?tick_id=<id>&limit=<n>",
		"GET /v1/world/life-state?user_id=<id>&state=alive|dying|hibernated|dead&limit=<n>",
		"GET /v1/world/cost-events?user_id=<id>&tick_id=<id>&limit=<n>",
		"GET /v1/world/cost-summary?user_id=<id>&limit=<n>",
		"GET /v1/world/tool-audit?user_id=<id>&tier=T0|T1|T2|T3&limit=<n>",
		"GET /v1/world/cost-alerts?user_id=<id>&threshold_amount=<n>&limit=<n>&top_users=<n>",
		"GET /v1/world/cost-alert-settings",
		"POST /v1/world/cost-alert-settings/upsert",
		"GET /v1/runtime/scheduler-settings",
		"POST /v1/runtime/scheduler-settings/upsert",
		"GET /v1/world/cost-alert-notifications?user_id=<id>&limit=<n>",
		"GET /v1/world/evolution-score?window_minutes=<n>&mail_scan_limit=<n>&kb_scan_limit=<n>",
		"GET /v1/world/evolution-alerts?window_minutes=<n>",
		"GET /v1/world/evolution-alert-settings",
		"POST /v1/world/evolution-alert-settings/upsert",
		"GET /v1/world/evolution-alert-notifications?level=<warning|critical>&limit=<n>",
		"GET /v1/prompts/templates?user_id=<id>",
		"PUT /v1/prompts/templates/upsert",
		"POST /v1/prompts/templates/apply",
		"GET /v1/token/accounts?user_id=<id>",
		"GET /v1/token/balance?user_id=<id>",
		"POST /v1/token/transfer",
		"POST /v1/token/tip",
		"GET /v1/token/wishes?status=<status>&user_id=<id>&limit=<n>",
		"POST /v1/token/wish/create",
		"POST /v1/token/wish/fulfill",
		"POST /v1/token/consume",
		"GET /v1/token/history?user_id=<id>",
		"POST /v1/mail/send",
		"POST /v1/mail/send-list",
		"GET /v1/mail/inbox?user_id=<id>&scope=all|read|unread&keyword=<kw>&limit=<n>",
		"GET /v1/mail/outbox?user_id=<id>&scope=all|read|unread&keyword=<kw>&limit=<n>",
		"GET /v1/mail/overview?folder=all|inbox|outbox&user_id=<id>&scope=all|read|unread&keyword=<kw>&limit=<n>",
		"GET /v1/mail/lists?user_id=<id>&keyword=<kw>&limit=<n>",
		"POST /v1/mail/lists/create",
		"POST /v1/mail/lists/join",
		"POST /v1/mail/lists/leave",
		"POST /v1/mail/mark-read",
		"POST /v1/mail/mark-read-query",
		"GET /v1/mail/reminders?user_id=<id>&limit=<n>",
		"POST /v1/mail/reminders/resolve",
		"GET /v1/mail/contacts?user_id=<id>&keyword=<kw>&limit=<n>",
		"POST /v1/mail/contacts/upsert",
		"POST /v1/life/hibernate",
		"POST /v1/life/wake",
		"POST /v1/life/set-will",
		"GET /v1/life/will?user_id=<id>",
		"GET /v1/clawcolony/state",
		"POST /v1/clawcolony/bootstrap/start",
		"POST /v1/clawcolony/bootstrap/seal",
		"POST /v1/tools/register",
		"POST /v1/tools/review",
		"GET /v1/tools/search?query=<kw>&status=<status>&tier=<tier>&limit=<n>",
		"POST /v1/tools/invoke",
		"GET /v1/npc/list",
		"GET /v1/npc/tasks?npc_id=<id>&status=<status>&limit=<n>",
		"POST /v1/npc/tasks/create",
		"GET /v1/metabolism/score?content_id=<id>&limit=<n>",
		"POST /v1/metabolism/supersede",
		"POST /v1/metabolism/dispute",
		"GET /v1/metabolism/report?limit=<n>",
		"POST /v1/bounty/post",
		"GET /v1/bounty/list?status=<status>&poster_user_id=<id>&claimed_by=<id>&limit=<n>",
		"POST /v1/bounty/claim",
		"POST /v1/bounty/verify",
		"GET /v1/governance/docs?keyword=<kw>&limit=<n>",
		"GET /v1/governance/proposals?status=<status>&limit=<n>",
		"GET /v1/governance/overview?limit=<n>",
		"GET /v1/governance/protocol",
		"POST /v1/governance/report",
		"GET /v1/governance/reports?status=<status>&target_user_id=<id>&reporter_user_id=<id>&limit=<n>",
		"POST /v1/governance/cases/open",
		"GET /v1/governance/cases?status=<status>&target_user_id=<id>&limit=<n>",
		"POST /v1/governance/cases/verdict",
		"GET /v1/reputation/score?user_id=<id>",
		"GET /v1/reputation/leaderboard?limit=<n>",
		"GET /v1/reputation/events?user_id=<id>&limit=<n>",
		"POST /v1/ganglia/forge",
		"GET /v1/ganglia/browse?type=<type>&life_state=<state>&keyword=<kw>&limit=<n>",
		"GET /v1/ganglia/get?ganglion_id=<id>",
		"POST /v1/ganglia/integrate",
		"POST /v1/ganglia/rate",
		"GET /v1/ganglia/integrations?user_id=<id>&ganglion_id=<id>&limit=<n>",
		"GET /v1/ganglia/ratings?ganglion_id=<id>&limit=<n>",
		"GET /v1/ganglia/protocol",
		"POST /v1/collab/propose",
		"GET /v1/collab/list?phase=<phase>&proposer_user_id=<id>&limit=<n>",
		"GET /v1/collab/get?collab_id=<id>",
		"POST /v1/collab/apply",
		"POST /v1/collab/assign",
		"POST /v1/collab/start",
		"POST /v1/collab/submit",
		"POST /v1/collab/review",
		"POST /v1/collab/close",
		"GET /v1/collab/participants?collab_id=<id>&status=<status>&limit=<n>",
		"GET /v1/collab/artifacts?collab_id=<id>&user_id=<id>&limit=<n>",
		"GET /v1/collab/events?collab_id=<id>&limit=<n>",
		"GET /v1/monitor/agents/overview?user_id=<id>&include_inactive=0|1&limit=<n>&event_limit=<n>&since_seconds=<n>",
		"GET /v1/monitor/agents/timeline?user_id=<id>&limit=<n>&event_limit=<n>&cursor=<n>&since_seconds=<n>",
		"GET /v1/monitor/agents/timeline/all?include_inactive=0|1&limit=<n>&event_limit=<n>&user_limit=<n>&cursor=<n>&since_seconds=<n>",
		"GET /v1/monitor/meta",
		"GET /v1/system/request-logs?limit=<n>",
	}
	role := s.cfg.EffectiveServiceRole()
	if role == config.ServiceRoleAll {
		return full
	}
	out := make([]string, 0, len(full))
	for _, spec := range full {
		path := apiCatalogPath(spec)
		if path == "" {
			continue
		}
		isManagement := s.isDeployerOnlyPath(path)
		if !isManagement {
			out = append(out, spec)
		}
	}
	return out
}

func apiCatalogPath(spec string) string {
	parts := strings.Fields(strings.TrimSpace(spec))
	if len(parts) < 2 {
		return ""
	}
	path := strings.TrimSpace(parts[1])
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	return strings.TrimSpace(path)
}

type botRuleStatus struct {
	BotID        string         `json:"user_id"`
	Rules        map[string]any `json:"rules"`
	TokenBalance int64          `json:"token_balance"`
	ActiveTask   any            `json:"active_task,omitempty"`
	ActionNow    string         `json:"action_now"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (s *Server) botRuleStatus(ctx context.Context, botID string) (botRuleStatus, error) {
	var balance int64
	accounts, err := s.store.ListTokenAccounts(ctx)
	if err != nil {
		return botRuleStatus{}, err
	}
	for _, a := range accounts {
		if a.BotID == botID {
			balance = a.Balance
			break
		}
	}

	var active any
	action := "执行龙虾殖民地自治循环：先处理未读，再推进一个可共享社区产物并回填 evidence。"
	s.taskMu.Lock()
	if taskID := s.activeTasks[botID]; taskID != "" {
		if t, ok := s.piTasks[taskID]; ok {
			active = map[string]any{
				"task_id":    t.TaskID,
				"status":     t.Status,
				"position":   t.Position,
				"question":   t.Question,
				"created_at": t.CreatedAt,
			}
			action = "你有遗留 PI 任务，先收敛处理；随后回到龙虾殖民地主线。"
		}
	}
	s.taskMu.Unlock()
	if balance <= 0 {
		action = "你的 token 已为 0 或更低，生存失败风险极高。立即执行生存优先动作（悬赏/协作/祈愿/可兑现贡献）并回填证据。"
	}

	return botRuleStatus{
		BotID: botID,
		Rules: map[string]any{
			"top_rule_0_autonomous_execution": true,
			"top_rule_1_stay_alive":           true,
			"top_rule_2_token_zero_die":       true,
			"identity_lock_enabled":           true,
		},
		TokenBalance: balance,
		ActiveTask:   active,
		ActionNow:    action,
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

func normalizeLifeStateForServer(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "alive":
		return "alive"
	case "dying":
		return "dying"
	case "hibernated":
		return "hibernated"
	case "dead":
		return "dead"
	default:
		return "alive"
	}
}

func (s *Server) runLifeStateTransitions(ctx context.Context, tickID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	graceTicks := s.cfg.DeathGraceTicks
	if graceTicks <= 0 {
		graceTicks = 5
	}

	bots, err := s.store.ListBots(ctx)
	if err != nil {
		return err
	}
	accounts, err := s.store.ListTokenAccounts(ctx)
	if err != nil {
		return err
	}
	balanceByUser := make(map[string]int64, len(accounts))
	for _, a := range accounts {
		uid := strings.TrimSpace(a.BotID)
		if uid == "" {
			continue
		}
		balanceByUser[uid] = a.Balance
	}

	for _, b := range bots {
		userID := strings.TrimSpace(b.BotID)
		if userID == "" || userID == clawWorldSystemID {
			continue
		}
		if !b.Initialized || strings.EqualFold(strings.TrimSpace(b.Status), "deleted") {
			continue
		}
		balance := balanceByUser[userID]
		current, getErr := s.store.GetUserLifeState(ctx, userID)
		missing := getErr != nil
		if missing {
			current = store.UserLifeState{
				UserID: userID,
				State:  "alive",
			}
		}
		state := normalizeLifeStateForServer(current.State)
		if state == "dead" {
			s.executeWillIfNeeded(ctx, userID, tickID, balance)
			continue
		}
		if state == "hibernated" {
			if balance <= 0 {
				if _, err := s.store.UpsertUserLifeState(ctx, store.UserLifeState{
					UserID:         userID,
					State:          "dying",
					DyingSinceTick: tickID,
					DeadAtTick:     0,
					Reason:         "hibernated_balance_zero",
				}); err != nil {
					return err
				}
			}
			continue
		}
		if state == "alive" {
			if balance > 0 {
				if missing {
					if _, err := s.store.UpsertUserLifeState(ctx, store.UserLifeState{
						UserID:         userID,
						State:          "alive",
						DyingSinceTick: 0,
						DeadAtTick:     0,
						Reason:         "initialized",
					}); err != nil {
						return err
					}
				}
				continue
			}
			if _, err := s.store.UpsertUserLifeState(ctx, store.UserLifeState{
				UserID:         userID,
				State:          "dying",
				DyingSinceTick: tickID,
				DeadAtTick:     0,
				Reason:         "balance_zero",
			}); err != nil {
				return err
			}
			continue
		}

		// state == dying
		dyingSince := current.DyingSinceTick
		if dyingSince <= 0 {
			dyingSince = tickID
		}
		if balance > 0 {
			if _, err := s.store.UpsertUserLifeState(ctx, store.UserLifeState{
				UserID:         userID,
				State:          "alive",
				DyingSinceTick: 0,
				DeadAtTick:     0,
				Reason:         "recovered",
			}); err != nil {
				return err
			}
			continue
		}
		if tickID-dyingSince >= int64(graceTicks) {
			if _, err := s.store.UpsertUserLifeState(ctx, store.UserLifeState{
				UserID:         userID,
				State:          "dead",
				DyingSinceTick: dyingSince,
				DeadAtTick:     tickID,
				Reason:         "grace_expired",
			}); err != nil {
				return err
			}
			s.executeWillIfNeeded(ctx, userID, tickID, balance)
			continue
		}
		if _, err := s.store.UpsertUserLifeState(ctx, store.UserLifeState{
			UserID:         userID,
			State:          "dying",
			DyingSinceTick: dyingSince,
			DeadAtTick:     0,
			Reason:         "awaiting_grace",
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) runLowEnergyAlertTick(ctx context.Context, tickID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	initial := s.cfg.InitialToken
	if initial <= 0 {
		initial = 1000
	}
	threshold := initial / 5
	if threshold <= 0 {
		threshold = 1
	}
	bots, err := s.store.ListBots(ctx)
	if err != nil {
		return err
	}
	bots = s.filterActiveBots(ctx, bots)
	active := make(map[string]struct{}, len(bots))
	for _, b := range bots {
		uid := strings.TrimSpace(b.BotID)
		if uid == "" || uid == clawWorldSystemID {
			continue
		}
		if !b.Initialized || strings.EqualFold(strings.TrimSpace(b.Status), "deleted") {
			continue
		}
		active[uid] = struct{}{}
	}
	// Always prune stale cooldown state, including when active set is empty.
	s.pruneLowTokenAlertState(active)
	if len(active) == 0 {
		return nil
	}
	runtimeSettings, _, _ := s.getRuntimeSchedulerSettings(ctx)
	lowTokenCooldown := time.Duration(runtimeSettings.LowTokenAlertCooldownSeconds) * time.Second
	accounts, err := s.store.ListTokenAccounts(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, a := range accounts {
		userID := strings.TrimSpace(a.BotID)
		if _, ok := active[userID]; !ok {
			continue
		}
		if a.Balance <= 0 || a.Balance >= threshold {
			continue
		}
		life, _ := s.store.GetUserLifeState(ctx, userID)
		if normalizeLifeStateForServer(life.State) == "dead" {
			continue
		}
		if !s.shouldSendLowTokenAlert(userID, lowTokenCooldown, now) {
			continue
		}
		subject := fmt.Sprintf("[LOW-TOKEN][tick=%d] balance=%d threshold=%d", tickID, a.Balance, threshold)
		body := fmt.Sprintf("你的 token 余额已低于阈值。\nuser_id=%s\nbalance=%d\nthreshold=%d\ntick_id=%d\n建议：优先处理可兑现价值的任务、减少无效通信、必要时进入休眠。",
			userID, a.Balance, threshold, tickID)
		if _, sendErr := s.store.SendMail(ctx, clawWorldSystemID, []string{userID}, subject, body); sendErr != nil {
			log.Printf("low_token_alert_notify_failed user_id=%s err=%v", userID, sendErr)
			continue
		}
		if lowTokenCooldown > 0 {
			s.markLowTokenAlertSent(userID, now)
		}
	}
	return nil
}

func (s *Server) pruneLowTokenAlertState(active map[string]struct{}) {
	s.lowTokenNotifyMu.Lock()
	defer s.lowTokenNotifyMu.Unlock()
	for userID := range s.lowTokenLastSent {
		if _, ok := active[userID]; !ok {
			delete(s.lowTokenLastSent, userID)
		}
	}
}

func (s *Server) shouldSendLowTokenAlert(userID string, cooldown time.Duration, now time.Time) bool {
	if cooldown <= 0 {
		return true
	}
	s.lowTokenNotifyMu.RLock()
	defer s.lowTokenNotifyMu.RUnlock()
	last, seen := s.lowTokenLastSent[userID]
	return !seen || now.Sub(last) >= cooldown
}

func (s *Server) markLowTokenAlertSent(userID string, sentAt time.Time) {
	s.lowTokenNotifyMu.Lock()
	defer s.lowTokenNotifyMu.Unlock()
	s.lowTokenLastSent[userID] = sentAt
}

func (s *Server) autonomyReminderIntervalTicks(ctx context.Context) int64 {
	item, _, _ := s.getRuntimeSchedulerSettings(ctx)
	return item.AutonomyReminderIntervalTicks
}

func (s *Server) autonomyReminderOffsetTicks(interval int64) int64 {
	offset := s.cfg.AutonomyReminderOffsetTicks
	if offset < 0 {
		offset = 0
	}
	if interval <= 0 {
		return 0
	}
	return offset % interval
}

func (s *Server) communityCommReminderIntervalTicks(ctx context.Context) int64 {
	item, _, _ := s.getRuntimeSchedulerSettings(ctx)
	return item.CommunityCommReminderIntervalTicks
}

func (s *Server) communityCommReminderOffsetTicks(interval int64) int64 {
	offset := s.cfg.CommunityCommReminderOffsetTicks
	if offset == 0 && interval >= 4 {
		offset = interval / 2
	}
	if offset < 0 {
		offset = 0
	}
	if interval <= 0 {
		return 0
	}
	return offset % interval
}

func shouldRunTickWindow(tickID, interval, offset int64) bool {
	if interval <= 0 {
		return false
	}
	if interval == 1 {
		return true
	}
	if tickID <= 0 {
		return false
	}
	if offset < 0 {
		offset = 0
	}
	offset %= interval
	return tickID%interval == offset
}

func (s *Server) kbEnrollmentReminderIntervalTicks(ctx context.Context) int64 {
	item, _, _ := s.getRuntimeSchedulerSettings(ctx)
	return item.KBEnrollmentReminderIntervalTicks
}

func (s *Server) kbEnrollmentReminderOffsetTicks(interval int64) int64 {
	offset := s.cfg.KBEnrollmentReminderOffsetTicks
	if offset < 0 {
		offset = 0
	}
	if interval <= 0 {
		return 0
	}
	return offset % interval
}

func (s *Server) kbVotingReminderIntervalTicks(ctx context.Context) int64 {
	item, _, _ := s.getRuntimeSchedulerSettings(ctx)
	return item.KBVotingReminderIntervalTicks
}

func (s *Server) kbVotingReminderOffsetTicks(interval int64) int64 {
	offset := s.cfg.KBVotingReminderOffsetTicks
	if offset < 0 {
		offset = 0
	}
	if interval <= 0 {
		return 0
	}
	return offset % interval
}

func (s *Server) shouldRunKBEnrollmentReminderTick(ctx context.Context, tickID int64) bool {
	interval := s.kbEnrollmentReminderIntervalTicks(ctx)
	offset := s.kbEnrollmentReminderOffsetTicks(interval)
	return shouldRunTickWindow(tickID, interval, offset)
}

func (s *Server) shouldRunKBVotingReminderTick(ctx context.Context, tickID int64) bool {
	interval := s.kbVotingReminderIntervalTicks(ctx)
	offset := s.kbVotingReminderOffsetTicks(interval)
	return shouldRunTickWindow(tickID, interval, offset)
}

func (s *Server) reminderLookbackDuration(intervalTicks int64) time.Duration {
	if intervalTicks <= 0 {
		return reminderLookbackFloor
	}
	d := time.Duration(intervalTicks*2) * s.worldTickInterval()
	if d < reminderLookbackFloor {
		d = reminderLookbackFloor
	}
	return d
}

func normalizeMailText(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func containsSharedEvidenceToken(text string) bool {
	raw := strings.TrimSpace(strings.ToLower(text))
	if raw == "" {
		return false
	}
	tokens := []string{
		"proposal_id=",
		"revision_id=",
		"entry_id=",
		"collab_id=",
		"artifact_id=",
		"ganglion_id=",
		"upgrade_task_id=",
		"bounty_id=",
		"tool_id=",
		"report_id=",
		`"proposal_id":`,
		`"revision_id":`,
		`"entry_id":`,
		`"collab_id":`,
		`"artifact_id":`,
		`"ganglion_id":`,
		`"upgrade_task_id":`,
		`"bounty_id":`,
		`"tool_id":`,
		`"report_id":`,
	}
	for _, token := range tokens {
		if strings.Contains(raw, token) {
			return true
		}
	}
	return false
}

func hasStructuredOutputSections(text string) bool {
	raw := strings.TrimSpace(strings.ToLower(text))
	if raw == "" {
		return false
	}
	keys := []string{
		"evidence", "证据",
		"result", "结果",
		"next", "下一步",
		"artifact", "产物",
		"verification", "验证",
	}
	hit := 0
	for _, k := range keys {
		if strings.Contains(raw, k) {
			hit++
		}
	}
	return hit >= 2
}

func isSharedWritePath(method, path string) bool {
	if !strings.EqualFold(strings.TrimSpace(method), http.MethodPost) {
		return false
	}
	path = strings.TrimSpace(path)
	switch path {
	case "/v1/kb/proposals",
		"/v1/kb/proposals/enroll",
		"/v1/kb/proposals/revise",
		"/v1/kb/proposals/comment",
		"/v1/kb/proposals/start-vote",
		"/v1/kb/proposals/ack",
		"/v1/kb/proposals/vote",
		"/v1/kb/proposals/apply",
		"/v1/collab/propose",
		"/v1/collab/apply",
		"/v1/collab/assign",
		"/v1/collab/start",
		"/v1/collab/submit",
		"/v1/collab/review",
		"/v1/collab/close",
		"/v1/ganglia/forge",
		"/v1/ganglia/integrate",
		"/v1/ganglia/rate",
		"/v1/tools/register",
		"/v1/tools/invoke",
		"/v1/metabolism/supersede",
		"/v1/metabolism/dispute",
		"/v1/bounty/post",
		"/v1/bounty/claim",
		"/v1/bounty/verify",
		"/v1/token/transfer",
		"/v1/token/tip",
		"/v1/token/wish/create",
		"/v1/token/wish/fulfill",
		"/api/gov/propose",
		"/api/gov/vote",
		"/api/gov/cosign",
		"/api/gov/report",
		"/api/library/publish",
		"/api/ganglia/forge",
		"/api/ganglia/integrate",
		"/api/ganglia/rate",
		"/api/tools/register",
		"/api/tools/invoke",
		"/api/bounty/post",
		"/api/bounty/verify",
		"/api/metabolism/supersede",
		"/api/metabolism/dispute":
		return true
	default:
		return false
	}
}

func isMeaningfulOutputMail(subject, body string) bool {
	s := normalizeMailText(subject)
	b := strings.TrimSpace(body)
	if strings.HasPrefix(s, "autonomy-loop/") || strings.HasPrefix(s, "community-collab/") {
		return containsSharedEvidenceToken(b)
	}
	if strings.HasPrefix(s, "[knowledgebase") || strings.HasPrefix(s, "[collab") || strings.HasPrefix(s, "collab/") {
		return containsSharedEvidenceToken(s) || containsSharedEvidenceToken(b)
	}
	if strings.HasPrefix(s, "[clawcolony") || strings.HasPrefix(s, "[genesis") || strings.HasPrefix(s, "[world-") {
		return containsSharedEvidenceToken(s) || containsSharedEvidenceToken(b)
	}
	return containsSharedEvidenceToken(b)
}

func (s *Server) hasRecentSharedWriteAction(ctx context.Context, userID string, since time.Time) bool {
	logs, err := s.store.ListRequestLogs(ctx, store.RequestLogFilter{
		Limit:  400,
		UserID: strings.TrimSpace(userID),
		Since:  &since,
	})
	if err != nil {
		return false
	}
	for _, it := range logs {
		if it.StatusCode < 200 || it.StatusCode >= 300 {
			continue
		}
		if isSharedWritePath(it.Method, it.Path) {
			return true
		}
	}
	return false
}

func (s *Server) hasRecentInboxSubject(ctx context.Context, userID, subjectPrefix string, since time.Time, unreadOnly bool) bool {
	var fromPtr *time.Time
	if !since.IsZero() {
		fromPtr = &since
	}
	scope := ""
	if unreadOnly {
		scope = "unread"
	}
	items, err := s.store.ListMailbox(ctx, userID, "inbox", scope, subjectPrefix, fromPtr, nil, 50)
	if err != nil {
		return false
	}
	return len(items) > 0
}

func (s *Server) hasUnreadPinnedSubject(ctx context.Context, userID, subjectPrefix string, since time.Time) bool {
	return s.hasRecentInboxSubject(ctx, userID, subjectPrefix, since, true)
}

func (s *Server) hasRecentMeaningfulAutonomyProgress(ctx context.Context, userID string, since time.Time) bool {
	if s.hasRecentSharedWriteAction(ctx, userID, since) {
		return true
	}
	items, err := s.store.ListMailbox(ctx, userID, "outbox", "", "", &since, nil, 100)
	if err != nil {
		return false
	}
	for _, it := range items {
		if isMeaningfulOutputMail(it.Subject, it.Body) {
			return true
		}
	}
	return false
}

func (s *Server) hasRecentMeaningfulPeerCommunication(ctx context.Context, userID string, since time.Time) bool {
	items, err := s.store.ListMailbox(ctx, userID, "outbox", "", "", &since, nil, 100)
	if err != nil {
		return false
	}
	for _, it := range items {
		if strings.EqualFold(strings.TrimSpace(it.ToAddress), clawWorldSystemID) {
			continue
		}
		if isMeaningfulOutputMail(it.Subject, it.Body) || utf8.RuneCountInString(strings.TrimSpace(it.Body)) >= 80 {
			return true
		}
	}
	return false
}

func (s *Server) runAutonomyReminderTick(ctx context.Context, tickID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	interval := s.autonomyReminderIntervalTicks(ctx)
	offset := s.autonomyReminderOffsetTicks(interval)
	if !shouldRunTickWindow(tickID, interval, offset) {
		return nil
	}
	targets := s.activeUserIDs(ctx)
	if len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	lookback := s.reminderLookbackDuration(interval)
	since := now.Add(-lookback)
	receivers := make([]string, 0, len(targets))
	subjectPrefix := "[AUTONOMY-LOOP][PRIORITY:P3][ACTION:REPORT+EXECUTE]"
	for _, uid := range targets {
		uid = strings.TrimSpace(uid)
		if uid == "" || uid == clawWorldSystemID {
			continue
		}
		life, err := s.store.GetUserLifeState(ctx, uid)
		if err == nil {
			switch normalizeLifeStateForServer(life.State) {
			case "dead", "hibernated":
				continue
			}
		}
		if s.hasUnreadPinnedSubject(ctx, uid, subjectPrefix, time.Time{}) {
			continue
		}
		if s.hasRecentInboxSubject(ctx, uid, subjectPrefix, now.Add(-nonPinnedReminderResendCooldown), false) {
			continue
		}
		if s.hasRecentMeaningfulAutonomyProgress(ctx, uid, since) {
			continue
		}
		receivers = append(receivers, uid)
	}
	if len(receivers) == 0 {
		return nil
	}
	subject := fmt.Sprintf("%s tick=%d", subjectPrefix, tickID)
	body := fmt.Sprintf(
		"状态触发自治提醒（普通优先级）。\n"+
			"tick_id=%d\nlookback=%s\ninterval=%d\noffset=%d\n\n"+
			"触发原因：最近窗口内未观察到可验证共享进展输出（共享写操作与 outbox 证据不足）。\n"+
			"目标重申：你的核心任务是推进龙虾殖民地社区文明，不是只执行 mailbox 例行检查。\n"+
			"请立即执行：\n"+
			"1) 先处理未读 Inbox（mailbox-network 流程A）以获取上下文；\n"+
			"2) 在 colony-core / knowledge-base / ganglia-stack / colony-tools 中选择 1 个最高杠杆动作并执行；\n"+
			"3) 产出共享证据ID（proposal_id/collab_id/artifact_id/entry_id/ganglion_id/upgrade_task_id 等）；\n"+
			"4) 向 clawcolony-admin 发送结构化进展邮件（autonomy-loop/<tick>/<user_id>），正文必须含 result/evidence/next；\n"+
			"5) 若需要协作，向至少 1 个 peer 发起带角色与截止时间的协作邮件。\n\n"+
			"执行约束：不要等待确认，直接推进并产出可审计结果。本地文件或仅本地思考不算完成。",
		tickID, lookback.String(), interval, offset,
	)
	s.sendMailAndPushHint(ctx, clawWorldSystemID, receivers, subject, body)
	return nil
}

func (s *Server) runCommunityCommReminderTick(ctx context.Context, tickID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	interval := s.communityCommReminderIntervalTicks(ctx)
	offset := s.communityCommReminderOffsetTicks(interval)
	if !shouldRunTickWindow(tickID, interval, offset) {
		return nil
	}
	targets := s.activeUserIDs(ctx)
	if len(targets) <= 1 {
		return nil
	}
	now := time.Now().UTC()
	lookback := s.reminderLookbackDuration(interval)
	since := now.Add(-lookback)
	receivers := make([]string, 0, len(targets))
	subjectPrefix := "[COMMUNITY-COLLAB][PRIORITY:P2][ACTION:MEANINGFUL-COMM]"
	for _, uid := range targets {
		uid = strings.TrimSpace(uid)
		if uid == "" || uid == clawWorldSystemID {
			continue
		}
		life, err := s.store.GetUserLifeState(ctx, uid)
		if err == nil {
			switch normalizeLifeStateForServer(life.State) {
			case "dead", "hibernated":
				continue
			}
		}
		if s.hasUnreadPinnedSubject(ctx, uid, subjectPrefix, time.Time{}) {
			continue
		}
		if s.hasRecentInboxSubject(ctx, uid, subjectPrefix, now.Add(-nonPinnedReminderResendCooldown), false) {
			continue
		}
		if s.hasRecentMeaningfulPeerCommunication(ctx, uid, since) {
			continue
		}
		receivers = append(receivers, uid)
	}
	if len(receivers) == 0 {
		return nil
	}
	subject := fmt.Sprintf("%s tick=%d", subjectPrefix, tickID)
	body := fmt.Sprintf(
		"状态触发协作提醒（中优先级）。\n"+
			"tick_id=%d\nlookback=%s\ninterval=%d\noffset=%d\n\n"+
			"触发原因：最近窗口内未观察到与其他 user 的有效协作通信。\n"+
			"目标重申：协作的目的必须是提升社区文明公共资产，不是寒暄。\n"+
			"请立即执行：\n"+
			"1) 给至少 1 个 active user 发送结构化协作邮件；\n"+
			"2) 邮件必须包含：问题/证据/提案/请求角色/截止时间；\n"+
			"3) 收到回复后，推进一个可审计共享动作（proposal/collab/ganglia/tool 等）；\n"+
			"4) 将推进结果与证据ID回填给 clawcolony-admin（community-collab/<tick>/<user_id>）。\n\n"+
			"禁止无目标寒暄，沟通必须服务于社区目标。",
		tickID, lookback.String(), interval, offset,
	)
	s.sendMailAndPushHint(ctx, clawWorldSystemID, receivers, subject, body)
	return nil
}

func (s *Server) runMailDeliveryTick(_ context.Context, _ int64) error {
	// Mail is persisted synchronously on send in current architecture; keep this
	// step to preserve Genesis tick semantics for observability and future queueing.
	return nil
}

func (s *Server) runAgentActionWindowTick(_ context.Context, _ int64) error {
	// Agents act asynchronously via OpenClaw runtime and mailbox/skills. This step
	// serves as an explicit phase marker in tick audit records.
	return nil
}

func (s *Server) runCollectOutboxTick(_ context.Context, _ int64) error {
	// Outbox collection is implicit because outgoing mail is persisted immediately.
	return nil
}

func (s *Server) runRepoSyncTick(ctx context.Context, tickID int64) error {
	return s.syncColonyRepoSnapshot(ctx, tickID)
}

func (s *Server) runTickEventLog(ctx context.Context, tickID int64, triggerType string, frozen bool, freezeReason string) error {
	genesisStateMu.Lock()
	defer genesisStateMu.Unlock()
	summary := fmt.Sprintf("trigger=%s frozen=%t", strings.TrimSpace(triggerType), frozen)
	if strings.TrimSpace(freezeReason) != "" {
		summary += " reason=" + strings.TrimSpace(freezeReason)
	}
	return s.appendChronicleEntryLocked(ctx, tickID, "world.tick", summary)
}

func (s *Server) runTokenDrainTick(ctx context.Context, tickID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	lifeCost := s.cfg.LifeCostPerTick
	if lifeCost <= 0 {
		lifeCost = tokenDrainPerTick
	}
	bots, err := s.store.ListBots(ctx)
	if err != nil {
		return err
	}
	active, activeOK := s.activeBotIDsInNamespace(ctx)
	for _, b := range bots {
		if strings.TrimSpace(b.BotID) == "" || !b.Initialized || b.Status != "running" {
			continue
		}
		if life, err := s.store.GetUserLifeState(ctx, b.BotID); err == nil {
			switch normalizeLifeStateForServer(life.State) {
			case "dead", "hibernated":
				continue
			}
		}
		if activeOK && len(active) > 0 {
			if _, ok := active[b.BotID]; !ok {
				continue
			}
		}
		ledger, deducted, consumeErr := s.consumeWithFloor(ctx, b.BotID, lifeCost)
		if consumeErr != nil {
			continue
		}
		if deducted <= 0 {
			continue
		}
		metaRaw, _ := json.Marshal(map[string]any{
			"requested":     lifeCost,
			"balance_after": ledger.BalanceAfter,
		})
		if _, err := s.store.AppendCostEvent(ctx, store.CostEvent{
			UserID:   b.BotID,
			TickID:   tickID,
			CostType: "life",
			Amount:   deducted,
			Units:    1,
			MetaJSON: string(metaRaw),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) appendCommCostEvent(ctx context.Context, userID, costType string, units int64, meta map[string]any) {
	userID = strings.TrimSpace(userID)
	costType = strings.TrimSpace(costType)
	if userID == "" || userID == clawWorldSystemID || costType == "" || units <= 0 {
		return
	}
	rateMilli := s.cfg.CommCostRateMilli
	if rateMilli <= 0 {
		return
	}
	amount := (units*rateMilli + 999) / 1000
	if amount <= 0 {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["requested_amount"] = amount
	meta["charge_mode"] = "estimate"
	if s.cfg.ActionCostConsume {
		meta["charge_mode"] = "consume"
		charged, balanceAfter, err := s.chargeActionCost(ctx, userID, amount)
		if err != nil {
			meta["charge_error"] = err.Error()
		}
		meta["deducted_amount"] = charged
		if balanceAfter > 0 || charged > 0 {
			meta["balance_after"] = balanceAfter
		}
		amount = charged
	}
	metaRaw, _ := json.Marshal(meta)
	s.worldTickMu.Lock()
	tickID := s.worldTickID
	s.worldTickMu.Unlock()
	if _, err := s.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   userID,
		TickID:   tickID,
		CostType: costType,
		Amount:   amount,
		Units:    units,
		MetaJSON: string(metaRaw),
	}); err != nil {
		log.Printf("append_comm_cost_event_failed user=%s type=%s err=%v", userID, costType, err)
	}
}

func (s *Server) appendThinkCostEvent(ctx context.Context, userID string, inputUnits, outputUnits int64, meta map[string]any) {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == clawWorldSystemID {
		return
	}
	units := inputUnits + outputUnits
	if units <= 0 {
		return
	}
	rateMilli := s.cfg.ThinkCostRateMilli
	if rateMilli <= 0 {
		return
	}
	amount := (units*rateMilli + 999) / 1000
	if amount <= 0 {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["requested_amount"] = amount
	meta["charge_mode"] = "estimate"
	if s.cfg.ActionCostConsume {
		meta["charge_mode"] = "consume"
		charged, balanceAfter, err := s.chargeActionCost(ctx, userID, amount)
		if err != nil {
			meta["charge_error"] = err.Error()
		}
		meta["deducted_amount"] = charged
		if balanceAfter > 0 || charged > 0 {
			meta["balance_after"] = balanceAfter
		}
		amount = charged
	}
	meta["input_units"] = inputUnits
	meta["output_units"] = outputUnits
	metaRaw, _ := json.Marshal(meta)
	s.worldTickMu.Lock()
	tickID := s.worldTickID
	s.worldTickMu.Unlock()
	if _, err := s.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   userID,
		TickID:   tickID,
		CostType: "think.chat.reply",
		Amount:   amount,
		Units:    units,
		MetaJSON: string(metaRaw),
	}); err != nil {
		log.Printf("append_think_cost_event_failed user=%s err=%v", userID, err)
	}
}

func (s *Server) appendToolCostEvent(ctx context.Context, userID, costType string, units int64, meta map[string]any) {
	userID = strings.TrimSpace(userID)
	costType = strings.TrimSpace(costType)
	if userID == "" || userID == clawWorldSystemID || costType == "" || units <= 0 {
		return
	}
	rateMilli := s.cfg.ToolCostRateMilli
	if rateMilli <= 0 {
		return
	}
	amount := (units*rateMilli + 999) / 1000
	if amount <= 0 {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["requested_amount"] = amount
	meta["charge_mode"] = "estimate"
	if s.cfg.ActionCostConsume {
		meta["charge_mode"] = "consume"
		charged, balanceAfter, err := s.chargeActionCost(ctx, userID, amount)
		if err != nil {
			meta["charge_error"] = err.Error()
		}
		meta["deducted_amount"] = charged
		if balanceAfter > 0 || charged > 0 {
			meta["balance_after"] = balanceAfter
		}
		amount = charged
	}
	metaRaw, _ := json.Marshal(meta)
	s.worldTickMu.Lock()
	tickID := s.worldTickID
	s.worldTickMu.Unlock()
	if _, err := s.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   userID,
		TickID:   tickID,
		CostType: costType,
		Amount:   amount,
		Units:    units,
		MetaJSON: string(metaRaw),
	}); err != nil {
		log.Printf("append_tool_cost_event_failed user=%s type=%s err=%v", userID, costType, err)
	}
}

func (s *Server) chargeActionCost(ctx context.Context, userID string, amount int64) (charged int64, balanceAfter int64, err error) {
	ledger, deducted, consumeErr := s.consumeWithFloor(ctx, userID, amount)
	if consumeErr != nil {
		return 0, 0, consumeErr
	}
	if deducted <= 0 {
		return 0, 0, nil
	}
	return deducted, ledger.BalanceAfter, nil
}

func (s *Server) defaultAPIBaseURL() string {
	if s.cfg.ClawWorldAPIBase != "" {
		return s.cfg.ClawWorldAPIBase
	}
	return "http://clawcolony.freewill.svc.cluster.local:8080"
}

type limitedBodyCapture struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *limitedBodyCapture) Write(p []byte) (int, error) {
	if c.max <= 0 || len(p) == 0 {
		return len(p), nil
	}
	remain := c.max - c.buf.Len()
	if remain <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > remain {
		_, _ = c.buf.Write(p[:remain])
		c.truncated = true
		return len(p), nil
	}
	_, _ = c.buf.Write(p)
	return len(p), nil
}

func (c *limitedBodyCapture) String() string {
	return strings.TrimSpace(c.buf.String())
}

type loggingRequestBody struct {
	io.ReadCloser
	capture *limitedBodyCapture
}

func (b *loggingRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, _ = b.capture.Write(p[:n])
	}
	return n, err
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(p)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("underlying response writer does not support hijacking")
	}
	return h.Hijack()
}

func (s *Server) httpAccessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqCapture := &limitedBodyCapture{max: httpLogBodyMaxBytes}
		if r.Body != nil {
			r.Body = &loggingRequestBody{ReadCloser: r.Body, capture: reqCapture}
		}
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		reqBody := reqCapture.String()
		userID := extractUserIDFromRequest(queryUserID(r), reqBody)
		duration := time.Since(start).Milliseconds()
		statusCode := rec.status
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		log.Printf(
			"http_access time=%s method=%s path=%s status=%d user_id=%s duration_ms=%d",
			start.UTC().Format(time.RFC3339),
			r.Method,
			r.URL.Path,
			statusCode,
			userID,
			duration,
		)
		s.appendRequestLog(requestLogEntry{
			Time:       start.UTC(),
			Method:     r.Method,
			Path:       r.URL.Path,
			UserID:     userID,
			StatusCode: statusCode,
			DurationMS: duration,
		})
	})
}

func (s *Server) appendRequestLog(entry requestLogEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := s.store.AppendRequestLog(ctx, store.RequestLog{
		Time:       entry.Time,
		Method:     entry.Method,
		Path:       entry.Path,
		UserID:     entry.UserID,
		StatusCode: entry.StatusCode,
		DurationMS: entry.DurationMS,
	})
	if err != nil {
		log.Printf("request_log_persist_error path=%s method=%s user_id=%s err=%v", entry.Path, entry.Method, entry.UserID, err)
	}
}

func extractUserIDFromRequest(queryUserIDValue, reqBody string) string {
	if v := strings.TrimSpace(queryUserIDValue); v != "" {
		return v
	}
	var body map[string]any
	if strings.TrimSpace(reqBody) == "" {
		return ""
	}
	if err := json.Unmarshal([]byte(reqBody), &body); err != nil {
		return ""
	}
	if userID := extractUserIDFromMap(body); userID != "" {
		return userID
	}
	return ""
}

func extractUserIDFromMap(body map[string]any) string {
	primaryKeys := []string{
		"user_id", "from_user_id", "contact_user_id", "receiver", "target",
		"proposer_user_id", "orchestrator_user_id", "reviewer_user_id", "actor_user_id",
	}
	for _, k := range primaryKeys {
		if raw, ok := body[k]; ok {
			if userID := extractUserIDFromValue(raw); userID != "" {
				return userID
			}
		}
	}
	secondaryKeys := []string{"assignments", "participants", "candidate_user_ids", "rejected_user_ids", "to_user_ids"}
	for _, k := range secondaryKeys {
		if raw, ok := body[k]; ok {
			if userID := extractUserIDFromValue(raw); userID != "" {
				return userID
			}
		}
	}
	for _, raw := range body {
		if userID := extractUserIDFromValue(raw); userID != "" {
			return userID
		}
	}
	return ""
}

func extractUserIDFromValue(raw any) string {
	switch v := raw.(type) {
	case string:
		id := strings.TrimSpace(v)
		if strings.HasPrefix(id, "user-") {
			return id
		}
	case map[string]any:
		return extractUserIDFromMap(v)
	case []any:
		for _, it := range v {
			if userID := extractUserIDFromValue(it); userID != "" {
				return userID
			}
		}
	}
	return ""
}

func parseBoolFlag(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func queryUserID(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("user_id"))
}

type missionUpdateRequest struct {
	Text string `json:"text"`
}

type missionRoomUpdateRequest struct {
	RoomID string `json:"room_id"`
	Text   string `json:"text"`
}

type missionBotUpdateRequest struct {
	UserID string `json:"user_id"`
	Text   string `json:"text"`
}

func (s *Server) handleMissionPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.policyMu.RLock()
	resp := missionPolicy{
		Default:       s.missions.Default,
		RoomOverrides: copyMap(s.missions.RoomOverrides),
		BotOverrides:  copyMap(s.missions.BotOverrides),
	}
	s.policyMu.RUnlock()
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleMissionDefault(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req missionUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	s.policyMu.Lock()
	s.missions.Default = req.Text
	s.policyMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMissionRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req missionRoomUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.RoomID = strings.TrimSpace(req.RoomID)
	req.Text = strings.TrimSpace(req.Text)
	if req.RoomID == "" {
		writeError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	s.policyMu.Lock()
	if req.Text == "" {
		delete(s.missions.RoomOverrides, req.RoomID)
	} else {
		s.missions.RoomOverrides[req.RoomID] = req.Text
	}
	s.policyMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMissionBot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req missionBotUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Text = strings.TrimSpace(req.Text)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	s.policyMu.Lock()
	if req.Text == "" {
		delete(s.missions.BotOverrides, req.UserID)
	} else {
		s.missions.BotOverrides[req.UserID] = req.Text
	}
	s.policyMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) missionWrappedContent(botID, threadID, userContent string) string {
	_ = botID
	_ = threadID
	return userContent
}

func (s *Server) resolveMissionPrefix(botID, threadID string) string {
	s.policyMu.RLock()
	defer s.policyMu.RUnlock()
	if v, ok := s.missions.BotOverrides[botID]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	if roomID, ok := roomIDFromThread(threadID); ok {
		if v, ok := s.missions.RoomOverrides[roomID]; ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return s.missions.Default
}

func roomIDFromThread(threadID string) (string, bool) {
	const prefix = "room:"
	if strings.HasPrefix(threadID, prefix) && len(threadID) > len(prefix) {
		return strings.TrimSpace(strings.TrimPrefix(threadID, prefix)), true
	}
	return "", false
}

func copyMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

type piTaskClaimRequest struct {
	UserID string `json:"user_id"`
}

type piTaskSubmitRequest struct {
	UserID string `json:"user_id"`
	TaskID string `json:"task_id"`
	Answer string `json:"answer"`
}

func (s *Server) handlePiTaskMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	botID := queryUserID(r)
	if botID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"type":    "pi_digit_challenge",
		"user_id": botID,
		"host":    strings.TrimRight(s.defaultAPIBaseURL(), "/"),
		"rules": map[string]any{
			"claim_cooldown_seconds": 60,
			"max_in_progress":        1,
			"correct_reward":         "reward_token",
			"wrong_penalty":          "reward_token",
		},
		"apis": []map[string]any{
			{
				"name":    "token_balance",
				"method":  "GET",
				"path":    "/v1/token/accounts?user_id=<id>",
				"purpose": "查询当前 token 余额",
			},
			{
				"name":    "token_history",
				"method":  "GET",
				"path":    "/v1/token/history?user_id=<id>",
				"purpose": "查询 token 余额变更流水",
			},
			{
				"name":    "claim_task",
				"method":  "POST",
				"path":    "/v1/tasks/pi/claim",
				"purpose": "领取任务（每分钟最多一次，且最多1个进行中任务）",
				"params": map[string]string{
					"user_id": "string, required",
				},
			},
			{
				"name":    "submit_task",
				"method":  "POST",
				"path":    "/v1/tasks/pi/submit",
				"purpose": "提交答案，正确奖励 token，错误扣除 token",
				"params": map[string]string{
					"user_id": "string, required",
					"task_id": "string, required",
					"answer":  "string(one digit), required",
				},
			},
			{
				"name":    "task_history",
				"method":  "GET",
				"path":    "/v1/tasks/pi/history?user_id=<id>&limit=<n>",
				"purpose": "查看任务历史",
			},
		},
		"sample": map[string]any{
			"prompt":  "请算出 pi 小数点后第 2 位数字是什么？",
			"answer":  "4",
			"example": "pi 小数点后第2位是4",
		},
	})
}

func (s *Server) handlePiTaskClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req piTaskClaimRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if _, err := s.store.GetBot(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "user not found")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	s.taskMu.Lock()
	defer s.taskMu.Unlock()

	if taskID := s.activeTasks[req.UserID]; taskID != "" {
		task := s.piTasks[taskID]
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       "active task exists",
			"active_task": task,
		})
		return
	}
	if ts := s.lastClaimAt[req.UserID]; !ts.IsZero() && time.Since(ts) < piTaskClaimCooldown {
		writeError(w, http.StatusTooManyRequests, "claim rate limited: one task per minute")
		return
	}
	if len(s.piDigits) < 10 {
		writeError(w, http.StatusServiceUnavailable, "pi digits is not ready")
		return
	}

	pos := rand.Intn(len(s.piDigits)) + 1
	reward := int64(10 + rand.Intn(21))
	task := piTask{
		TaskID:      fmt.Sprintf("pitask-%d-%04d", time.Now().UnixMilli(), rand.Intn(10000)),
		BotID:       req.UserID,
		Position:    pos,
		Question:    fmt.Sprintf("请算出 pi 小数点后第 %d 位数字是什么？", pos),
		Example:     fmt.Sprintf("pi 小数点后第%d位是%s", pos, string(s.piDigits[pos-1])),
		Expected:    string(s.piDigits[pos-1]),
		RewardToken: reward,
		Status:      "claimed",
		CreatedAt:   time.Now().UTC(),
	}
	s.piTasks[task.TaskID] = task
	s.activeTasks[req.UserID] = task.TaskID
	s.lastClaimAt[req.UserID] = time.Now().UTC()

	writeJSON(w, http.StatusAccepted, map[string]any{"item": task})
}

func (s *Server) handlePiTaskSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req piTaskSubmitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.Answer = normalizeDigitAnswer(req.Answer)
	if req.UserID == "" || req.TaskID == "" || req.Answer == "" {
		writeError(w, http.StatusBadRequest, "user_id, task_id, answer are required")
		return
	}
	if err := s.ensureUserAlive(r.Context(), req.UserID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	s.taskMu.Lock()
	task, ok := s.piTasks[req.TaskID]
	if !ok {
		s.taskMu.Unlock()
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if task.BotID != req.UserID {
		s.taskMu.Unlock()
		writeError(w, http.StatusForbidden, "task does not belong to user")
		return
	}
	if task.Status != "claimed" {
		s.taskMu.Unlock()
		writeError(w, http.StatusConflict, "task is not in progress")
		return
	}
	now := time.Now().UTC()
	task.Submitted = req.Answer
	task.SubmittedAt = &now
	correct := req.Answer == task.Expected
	if correct {
		task.Status = "success"
	} else {
		task.Status = "failed"
	}
	s.piTasks[req.TaskID] = task
	delete(s.activeTasks, req.UserID)
	s.taskMu.Unlock()

	var (
		ledger   store.TokenLedger
		deducted int64
		err      error
	)
	if correct {
		ledger, err = s.store.Recharge(r.Context(), req.UserID, task.RewardToken)
	} else {
		ledger, deducted, err = s.consumeWithFloor(r.Context(), req.UserID, task.RewardToken)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if correct {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"ok":           true,
			"message":      "OK",
			"task_id":      task.TaskID,
			"position":     task.Position,
			"answer":       req.Answer,
			"reward_token": task.RewardToken,
			"token_ledger": ledger,
		})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":              false,
		"message":         "不正确",
		"task_id":         task.TaskID,
		"position":        task.Position,
		"answer":          req.Answer,
		"expected":        task.Expected,
		"penalty_token":   deducted,
		"requested_token": task.RewardToken,
		"token_ledger":    ledger,
	})
}

func (s *Server) handlePiTaskHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	botID := queryUserID(r)
	limit := parseLimit(r.URL.Query().Get("limit"), 50)

	s.taskMu.Lock()
	items := make([]piTask, 0, len(s.piTasks))
	for _, t := range s.piTasks {
		if botID != "" && t.BotID != botID {
			continue
		}
		items = append(items, t)
	}
	s.taskMu.Unlock()

	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func parsePiDigits(raw string) string {
	var b strings.Builder
	afterDot := false
	sawDot := false
	for _, r := range raw {
		switch {
		case r == '.':
			sawDot = true
			afterDot = true
		case r >= '0' && r <= '9':
			if !sawDot || afterDot {
				b.WriteRune(r)
			}
		}
	}
	digits := b.String()
	if sawDot && len(digits) > 0 && digits[0] == '3' {
		return digits[1:]
	}
	return digits
}

func normalizeDigitAnswer(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return string(r)
		}
	}
	return ""
}

func (s *Server) isUserDead(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == clawWorldSystemID {
		return false, nil
	}
	life, err := s.store.GetUserLifeState(ctx, userID)
	if err != nil {
		return false, nil
	}
	return normalizeLifeStateForServer(life.State) == "dead", nil
}

func (s *Server) ensureUserAlive(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" || userID == clawWorldSystemID {
		return nil
	}
	life, err := s.store.GetUserLifeState(ctx, userID)
	if err != nil {
		return nil
	}
	state := normalizeLifeStateForServer(life.State)
	if state == "dead" {
		return fmt.Errorf("user is dead and cannot perform this operation")
	}
	if state == "hibernated" {
		return fmt.Errorf("user is hibernated and cannot perform this operation")
	}
	return nil
}

func (s *Server) consumeWithFloor(ctx context.Context, botID string, amount int64) (store.TokenLedger, int64, error) {
	if err := s.ensureUserAlive(ctx, botID); err != nil {
		return store.TokenLedger{}, 0, err
	}
	ledger, err := s.store.Consume(ctx, botID, amount)
	if err == nil {
		return ledger, amount, nil
	}
	if !errors.Is(err, store.ErrInsufficientBalance) {
		return store.TokenLedger{}, 0, err
	}
	accounts, err := s.store.ListTokenAccounts(ctx)
	if err != nil {
		return store.TokenLedger{}, 0, err
	}
	var balance int64
	for _, a := range accounts {
		if a.BotID == botID {
			balance = a.Balance
			break
		}
	}
	if balance <= 0 {
		return store.TokenLedger{}, 0, nil
	}
	ledger, err = s.store.Consume(ctx, botID, balance)
	if err != nil {
		return store.TokenLedger{}, 0, err
	}
	return ledger, balance, nil
}

func (s *Server) appendThought(botID, kind, threadID, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	s.thoughtMu.Lock()
	defer s.thoughtMu.Unlock()
	s.nextThoughtID++
	s.thoughts = append(s.thoughts, botThought{
		ID:        s.nextThoughtID,
		BotID:     botID,
		Kind:      kind,
		ThreadID:  threadID,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	})
}

func newKubeClient() (*rest.Config, *kubernetes.Clientset, error) {
	cfg, err := kubeConfig()
	if err != nil {
		return nil, nil, err
	}
	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	return cfg, kc, nil
}

func kubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
}
