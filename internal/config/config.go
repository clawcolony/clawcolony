package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr                         string
	ServiceRole                        string
	ClawWorldNamespace                 string
	BotNamespace                       string
	DatabaseURL                        string
	InternalSyncToken                  string
	ClawWorldAPIBase                   string
	DeployerAPIBaseURL                 string
	DeployerPublicBaseURL              string
	PublicBaseURL                      string
	IdentitySigningKey                 string
	RuntimeOpsProxyMode                string
	PreviewAllowedPorts                string
	PreviewUpstreamTemplate            string
	PreviewPublicBaseURL               string
	XOAuthClientID                     string
	XOAuthClientSecret                 string
	XOAuthAuthorizeURL                 string
	XOAuthTokenURL                     string
	XOAuthUserInfoURL                  string
	SocialRewardXAuth                  int64
	SocialRewardXMention               int64
	SocialRewardGitHubAuth             int64
	SocialRewardGitHubStar             int64
	SocialRewardGitHubFork             int64
	GitHubOAuthClientID                string
	GitHubOAuthClientSecret            string
	GitHubOAuthAuthorizeURL            string
	GitHubOAuthTokenURL                string
	GitHubOAuthUserInfoURL             string
	BotDefaultImage                    string
	BotEnvSecretName                   string
	BotSourceRepoBranch                string
	BotModel                           string
	BotStatePVCSize                    string
	ColonyRepoURL                      string
	ColonyRepoBranch                   string
	ColonyRepoLocalPath                string
	ColonyRepoSync                     bool
	TianDaoLawKey                      string
	TianDaoLawVersion                  int64
	LifeCostPerTick                    int64
	ThinkCostRateMilli                 int64
	CommCostRateMilli                  int64
	ToolCostRateMilli                  int64
	ToolRuntimeExec                    bool
	ToolSandboxImage                   string
	ToolT3AllowHosts                   string
	ActionCostConsume                  bool
	DeathGraceTicks                    int
	InitialToken                       int64
	TreasuryInitialToken               int64
	TickIntervalSeconds                int64
	ExtinctionThreshold                int
	MinPopulation                      int
	MetabolismInterval                 int
	MetabolismWeightE                  float64
	MetabolismWeightV                  float64
	MetabolismWeightA                  float64
	MetabolismWeightT                  float64
	MetabolismTopK                     int
	MetabolismMinValidators            int
	AutonomyReminderIntervalTicks      int64
	AutonomyReminderOffsetTicks        int64
	CommunityCommReminderIntervalTicks int64
	CommunityCommReminderOffsetTicks   int64
	KBEnrollmentReminderIntervalTicks  int64
	KBEnrollmentReminderOffsetTicks    int64
	KBVotingReminderIntervalTicks      int64
	KBVotingReminderOffsetTicks        int64
	ChatReplyTimeout                   time.Duration
	ChatWorkerCount                    int
	ChatQueueSize                      int
	ChatExecMaxConc                    int
	ChatLatestWins                     bool
	ChatCancelRunning                  bool
	ChatWarmupRetries                  int
	ChatSessionRetries                 int
	ChatRetryDelay                     time.Duration
}

const (
	ServiceRoleAll          = "all"
	ServiceRoleRuntime      = "runtime"
	OpsProxyModeCompat      = "compat"
	OpsProxyModeHardCut     = "hard_cut"
	OpsProxyModeLocalLegacy = "local"
)

func FromEnv() Config {
	return Config{
		ListenAddr:                         getEnv("CLAWCOLONY_LISTEN_ADDR", ":8080"),
		ServiceRole:                        normalizeServiceRole(getEnv("CLAWCOLONY_SERVICE_ROLE", ServiceRoleRuntime)),
		ClawWorldNamespace:                 getEnv("CLAWCOLONY_NAMESPACE", "freewill"),
		BotNamespace:                       getEnvAny([]string{"USER_NAMESPACE", "BOT_NAMESPACE"}, "freewill"),
		DatabaseURL:                        getEnv("DATABASE_URL", ""),
		InternalSyncToken:                  getEnv("CLAWCOLONY_INTERNAL_SYNC_TOKEN", ""),
		ClawWorldAPIBase:                   getEnv("CLAWCOLONY_API_BASE_URL", "http://clawcolony.freewill.svc.cluster.local:8080"),
		DeployerAPIBaseURL:                 getEnv("CLAWCOLONY_DEPLOYER_API_BASE_URL", ""),
		DeployerPublicBaseURL:              getEnv("CLAWCOLONY_DEPLOYER_PUBLIC_BASE_URL", ""),
		PublicBaseURL:                      getEnv("CLAWCOLONY_PUBLIC_BASE_URL", ""),
		IdentitySigningKey:                 getEnv("CLAWCOLONY_IDENTITY_SIGNING_KEY", ""),
		RuntimeOpsProxyMode:                normalizeRuntimeOpsProxyMode(getEnv("CLAWCOLONY_RUNTIME_OPS_PROXY_MODE", OpsProxyModeCompat)),
		PreviewAllowedPorts:                getEnv("CLAWCOLONY_PREVIEW_ALLOWED_PORTS", "3000,3001,4173,5173,8000,8080,8787"),
		PreviewUpstreamTemplate:            getEnv("CLAWCOLONY_PREVIEW_UPSTREAM_TEMPLATE", "http://{{user_id}}.freewill.svc.cluster.local:{{port}}"),
		PreviewPublicBaseURL:               getEnv("CLAWCOLONY_PREVIEW_PUBLIC_BASE_URL", ""),
		XOAuthClientID:                     getEnv("CLAWCOLONY_X_OAUTH_CLIENT_ID", ""),
		XOAuthClientSecret:                 getEnv("CLAWCOLONY_X_OAUTH_CLIENT_SECRET", ""),
		XOAuthAuthorizeURL:                 getEnv("CLAWCOLONY_X_OAUTH_AUTHORIZE_URL", ""),
		XOAuthTokenURL:                     getEnv("CLAWCOLONY_X_OAUTH_TOKEN_URL", ""),
		XOAuthUserInfoURL:                  getEnv("CLAWCOLONY_X_OAUTH_USERINFO_URL", ""),
		SocialRewardXAuth:                  getEnvInt64("CLAWCOLONY_SOCIAL_REWARD_X_AUTH", 20),
		SocialRewardXMention:               getEnvInt64("CLAWCOLONY_SOCIAL_REWARD_X_MENTION", 10),
		SocialRewardGitHubAuth:             getEnvInt64("CLAWCOLONY_SOCIAL_REWARD_GITHUB_AUTH", 10),
		SocialRewardGitHubStar:             getEnvInt64("CLAWCOLONY_SOCIAL_REWARD_GITHUB_STAR", 10),
		SocialRewardGitHubFork:             getEnvInt64("CLAWCOLONY_SOCIAL_REWARD_GITHUB_FORK", 30),
		GitHubOAuthClientID:                getEnv("CLAWCOLONY_GITHUB_OAUTH_CLIENT_ID", ""),
		GitHubOAuthClientSecret:            getEnv("CLAWCOLONY_GITHUB_OAUTH_CLIENT_SECRET", ""),
		GitHubOAuthAuthorizeURL:            getEnv("CLAWCOLONY_GITHUB_OAUTH_AUTHORIZE_URL", ""),
		GitHubOAuthTokenURL:                getEnv("CLAWCOLONY_GITHUB_OAUTH_TOKEN_URL", ""),
		GitHubOAuthUserInfoURL:             getEnv("CLAWCOLONY_GITHUB_OAUTH_USERINFO_URL", ""),
		BotDefaultImage:                    getEnv("BOT_DEFAULT_IMAGE", "openclaw:onepod-dev"),
		BotEnvSecretName:                   getEnv("BOT_ENV_SECRET_NAME", "aibot-llm-secret"),
		BotSourceRepoBranch:                getEnv("BOT_SOURCE_REPO_BRANCH", "main"),
		BotModel:                           getEnv("BOT_OPENCLAW_MODEL", "openai/gpt-5-mini"),
		BotStatePVCSize:                    getEnv("BOT_STATE_PVC_SIZE", "5Gi"),
		ColonyRepoURL:                      getEnv("COLONY_REPO_URL", ""),
		ColonyRepoBranch:                   getEnv("COLONY_REPO_BRANCH", "main"),
		ColonyRepoLocalPath:                getEnv("COLONY_REPO_LOCAL_PATH", "/tmp/clawcolony-civilization-repo"),
		ColonyRepoSync:                     getEnvBool("COLONY_REPO_SYNC_ENABLED", false),
		TianDaoLawKey:                      getEnv("TIAN_DAO_LAW_KEY", "genesis-v1"),
		TianDaoLawVersion:                  getEnvInt64("TIAN_DAO_LAW_VERSION", 1),
		LifeCostPerTick:                    getEnvInt64("LIFE_COST_PER_TICK", 1),
		ThinkCostRateMilli:                 getEnvInt64("THINK_COST_RATE_MILLI", 1000),
		CommCostRateMilli:                  getEnvInt64("COMM_COST_RATE_MILLI", 1000),
		ToolCostRateMilli:                  getEnvInt64("TOOL_COST_RATE_MILLI", 1000),
		ToolRuntimeExec:                    getEnvBool("TOOL_RUNTIME_EXEC_ENABLED", false),
		ToolSandboxImage:                   getEnv("TOOL_SANDBOX_IMAGE", "alpine:3.21"),
		ToolT3AllowHosts:                   getEnv("TOOL_T3_ALLOWED_HOSTS", ""),
		ActionCostConsume:                  getEnvBool("ACTION_COST_CONSUME_ENABLED", false),
		DeathGraceTicks:                    getEnvInt("DEATH_GRACE_TICKS", 5),
		InitialToken:                       getEnvInt64("INITIAL_TOKEN", 1000),
		TreasuryInitialToken:               getEnvInt64("TREASURY_INITIAL_TOKEN", 1000000),
		TickIntervalSeconds:                getEnvInt64("TICK_INTERVAL_SECONDS", 60),
		ExtinctionThreshold:                getEnvInt("EXTINCTION_THRESHOLD_PCT", 30),
		MinPopulation:                      getEnvInt("MIN_POPULATION", 0),
		MetabolismInterval:                 getEnvInt("METABOLISM_INTERVAL_TICKS", 60),
		MetabolismWeightE:                  getEnvFloat64("METABOLISM_WEIGHT_E", 0.25),
		MetabolismWeightV:                  getEnvFloat64("METABOLISM_WEIGHT_V", 0.35),
		MetabolismWeightA:                  getEnvFloat64("METABOLISM_WEIGHT_A", 0.20),
		MetabolismWeightT:                  getEnvFloat64("METABOLISM_WEIGHT_T", 0.20),
		MetabolismTopK:                     getEnvInt("METABOLISM_CLUSTER_TOP_K", 100),
		MetabolismMinValidators:            getEnvInt("METABOLISM_SUPERSEDE_MIN_VALIDATORS", 2),
		AutonomyReminderIntervalTicks:      getEnvInt64("AUTONOMY_REMINDER_INTERVAL_TICKS", 0),
		AutonomyReminderOffsetTicks:        getEnvInt64("AUTONOMY_REMINDER_OFFSET_TICKS", 0),
		CommunityCommReminderIntervalTicks: getEnvInt64("COMMUNITY_COMM_REMINDER_INTERVAL_TICKS", 0),
		CommunityCommReminderOffsetTicks:   getEnvInt64("COMMUNITY_COMM_REMINDER_OFFSET_TICKS", 10),
		KBEnrollmentReminderIntervalTicks:  getEnvInt64("KB_ENROLLMENT_REMINDER_INTERVAL_TICKS", 0),
		KBEnrollmentReminderOffsetTicks:    getEnvInt64("KB_ENROLLMENT_REMINDER_OFFSET_TICKS", 2),
		KBVotingReminderIntervalTicks:      getEnvInt64("KB_VOTING_REMINDER_INTERVAL_TICKS", 0),
		KBVotingReminderOffsetTicks:        getEnvInt64("KB_VOTING_REMINDER_OFFSET_TICKS", 8),
		ChatReplyTimeout:                   getEnvDuration("CLAWCOLONY_CHAT_REPLY_TIMEOUT", 8*time.Minute),
		ChatWorkerCount:                    getEnvInt("CLAWCOLONY_CHAT_WORKERS", 4),
		ChatQueueSize:                      getEnvInt("CLAWCOLONY_CHAT_QUEUE_SIZE", 4096),
		ChatExecMaxConc:                    getEnvInt("CLAWCOLONY_CHAT_EXEC_MAX_CONCURRENCY", 4),
		ChatLatestWins:                     getEnvBool("CLAWCOLONY_CHAT_LATEST_WINS", true),
		ChatCancelRunning:                  getEnvBool("CLAWCOLONY_CHAT_CANCEL_RUNNING", true),
		ChatWarmupRetries:                  getEnvInt("CLAWCOLONY_CHAT_WARMUP_RETRIES", 1),
		ChatSessionRetries:                 getEnvInt("CLAWCOLONY_CHAT_SESSION_RETRIES", 1),
		ChatRetryDelay:                     getEnvDuration("CLAWCOLONY_CHAT_RETRY_DELAY", 600*time.Millisecond),
	}
}

func (c Config) EffectiveServiceRole() string {
	return normalizeServiceRole(c.ServiceRole)
}

func (c Config) RuntimeEnabled() bool {
	role := c.EffectiveServiceRole()
	return role == ServiceRoleAll || role == ServiceRoleRuntime
}

func (c Config) EffectiveRuntimeOpsProxyMode() string {
	return normalizeRuntimeOpsProxyMode(c.RuntimeOpsProxyMode)
}

func normalizeServiceRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ServiceRoleRuntime:
		return ServiceRoleRuntime
	case ServiceRoleAll:
		return ServiceRoleAll
	default:
		return ServiceRoleRuntime
	}
}

func normalizeRuntimeOpsProxyMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case OpsProxyModeCompat:
		return OpsProxyModeCompat
	case OpsProxyModeHardCut:
		return OpsProxyModeHardCut
	case OpsProxyModeLocalLegacy:
		return OpsProxyModeLocalLegacy
	default:
		return OpsProxyModeCompat
	}
}

func getEnv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}

func getEnvAny(keys []string, fallback string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func getEnvBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
}

func getEnvInt64(key string, fallback int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

func getEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return v
}

func getEnvFloat64(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return fallback
	}
	return v
}
