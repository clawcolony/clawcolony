package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawcolony/internal/bot"
	"clawcolony/internal/store"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func resolveUserIDFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	if v := strings.TrimSpace(labels["clawcolony.user_id"]); v != "" {
		return v
	}
	return ""
}

type openClawAdminOverview struct {
	Time      string                     `json:"time"`
	Namespace map[string]string          `json:"namespace"`
	Config    map[string]any             `json:"config"`
	Checks    map[string]bool            `json:"checks"`
	Pods      []openClawAdminPodOverview `json:"pods"`
}

type openClawAdminPodOverview struct {
	UserID         string `json:"user_id"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	Initialized    bool   `json:"initialized"`
	DeploymentName string `json:"deployment_name"`
	PodName        string `json:"pod_name"`
	Phase          string `json:"phase"`
	Ready          bool   `json:"ready"`
	Restarts       int32  `json:"restarts"`
	NodeName       string `json:"node_name"`
	PodIP          string `json:"pod_ip"`
	Image          string `json:"image"`
	AgeSeconds     int64  `json:"age_seconds"`
	StartedAt      string `json:"started_at,omitempty"`
}

type openClawAdminActionRequest struct {
	Action   string `json:"action"`
	UserID   string `json:"user_id"`
	Provider string `json:"provider"`
	Image    string `json:"image"`
}

func openClawAdminActionCostType(action string) string {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "restart":
		return "tool.openclaw.restart"
	case "redeploy":
		return "tool.openclaw.redeploy"
	case "delete":
		return "tool.openclaw.delete"
	default:
		return ""
	}
}

type openClawAdminGitHubHealth struct {
	SecretName  string `json:"secret_name"`
	HasToken    bool   `json:"has_token"`
	Owner       string `json:"owner"`
	MachineUser string `json:"machine_user"`
	TokenScopes string `json:"token_scopes,omitempty"`
	User        struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	} `json:"user"`
	Org struct {
		Login       string `json:"login"`
		ID          int64  `json:"id"`
		Accessible  bool   `json:"accessible"`
		StatusCode  int    `json:"status_code"`
		RoleInOrg   string `json:"role_in_org,omitempty"`
		Permissions string `json:"permissions,omitempty"`
	} `json:"org"`
	Checks map[string]bool `json:"checks"`
	Error  string          `json:"error,omitempty"`
}

type githubAdminConfig struct {
	Token       string
	Owner       string
	MachineUser string
}

type registerProvisionResult struct {
	UserID         string `json:"user_id"`
	UserName       string `json:"user_name"`
	RepoFullName   string `json:"repo_full_name"`
	RepoURLSSH     string `json:"repo_url_ssh"`
	GitSecretName  string `json:"git_secret_name"`
	SourceRef      string `json:"source_ref"`
	ReleaseTag     string `json:"release_tag,omitempty"`
	Image          string `json:"image"`
	ImageBuilt     bool   `json:"image_built"`
	ImageBuildNote string `json:"image_build_note,omitempty"`
}

type registerTaskCreateResult struct {
	TaskID int64 `json:"register_task_id"`
}

var registerNamePool = []string{
	"roy", "liam", "noah", "owen", "levi", "jude", "luca", "nolan", "ezra", "jace",
	"milo", "kai", "theo", "alex", "sam", "jay", "leo", "cole", "finn", "ryan",
}

var registerAdjPool = []string{
	"calm", "clear", "bright", "steady", "brisk", "kind", "solid", "swift", "keen", "ready",
}

func (s *Server) handleOpenClawAdminOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	out, err := s.buildOpenClawAdminOverview(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleOpenClawAdminAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req openClawAdminActionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Action = strings.TrimSpace(strings.ToLower(req.Action))
	req.UserID = strings.TrimSpace(req.UserID)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Image = strings.TrimSpace(req.Image)

	if costType := openClawAdminActionCostType(req.Action); costType != "" {
		if req.UserID == "" {
			writeError(w, http.StatusBadRequest, "user_id is required")
			return
		}
		if err := s.ensureToolTierAllowed(r.Context(), req.UserID, costType); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}

	switch req.Action {
	case "register":
		s.handleOpenClawAdminRegister(w, r, req)
	case "restart":
		s.handleOpenClawAdminRestart(w, r, req)
	case "redeploy":
		s.handleOpenClawAdminRedeploy(w, r, req)
	case "delete":
		s.handleOpenClawAdminDelete(w, r, req)
	default:
		writeError(w, http.StatusBadRequest, "unsupported action")
	}
}

func (s *Server) handleOpenClawAdminGitHubHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.cfg.GitHubMockEnabled {
		out := openClawAdminGitHubHealth{
			SecretName: "mock://github",
			HasToken:   true,
			Owner:      strings.TrimSpace(s.cfg.GitHubMockOwner),
			MachineUser: func() string {
				v := strings.TrimSpace(s.cfg.GitHubMockMachine)
				if v == "" {
					return "claw-archivist"
				}
				return v
			}(),
			Checks: map[string]bool{
				"mock_mode":            true,
				"secret_exists":        true,
				"token_present":        true,
				"owner_present":        true,
				"machine_user_present": true,
				"github_user_ok":       true,
				"github_org_ok":        true,
			},
		}
		out.User.Login = out.MachineUser
		out.User.ID = 1
		out.Org.Login = out.Owner
		out.Org.ID = 1
		out.Org.Accessible = true
		out.Org.StatusCode = http.StatusOK
		writeJSON(w, http.StatusOK, out)
		return
	}
	if s.kubeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not available")
		return
	}

	const secretName = "clawcolony-github"
	ns := s.cfg.ClawWorldNamespace
	out := openClawAdminGitHubHealth{
		SecretName: secretName,
		Checks: map[string]bool{
			"secret_exists":        false,
			"token_present":        false,
			"owner_present":        false,
			"machine_user_present": false,
			"github_user_ok":       false,
			"github_org_ok":        false,
		},
	}

	sec, err := s.kubeClient.CoreV1().Secrets(ns).Get(r.Context(), secretName, metav1.GetOptions{})
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Checks["secret_exists"] = true

	token := strings.TrimSpace(string(sec.Data["GITHUB_TOKEN"]))
	owner := strings.TrimSpace(string(sec.Data["GITHUB_OWNER"]))
	machineUser := strings.TrimSpace(string(sec.Data["GITHUB_MACHINE_USER"]))
	out.HasToken = token != ""
	out.Owner = owner
	out.MachineUser = machineUser
	out.Checks["token_present"] = token != ""
	out.Checks["owner_present"] = owner != ""
	out.Checks["machine_user_present"] = machineUser != ""
	if token == "" || owner == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	userReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+token)
	userReq.Header.Set("Accept", "application/vnd.github+json")
	userReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	userResp, err := client.Do(userReq)
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	defer userResp.Body.Close()
	out.TokenScopes = strings.TrimSpace(userResp.Header.Get("X-OAuth-Scopes"))
	if userResp.StatusCode == http.StatusOK {
		var body struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		}
		data, _ := io.ReadAll(userResp.Body)
		if err := json.Unmarshal(data, &body); err == nil {
			out.User.Login = body.Login
			out.User.ID = body.ID
			out.Checks["github_user_ok"] = body.Login != ""
		}
	}

	orgURL := fmt.Sprintf("https://api.github.com/orgs/%s", owner)
	orgReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, orgURL, nil)
	orgReq.Header.Set("Authorization", "Bearer "+token)
	orgReq.Header.Set("Accept", "application/vnd.github+json")
	orgReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	orgResp, err := client.Do(orgReq)
	if err != nil {
		out.Error = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	defer orgResp.Body.Close()
	out.Org.StatusCode = orgResp.StatusCode
	if orgResp.StatusCode == http.StatusOK {
		var body struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
		}
		data, _ := io.ReadAll(orgResp.Body)
		if err := json.Unmarshal(data, &body); err == nil {
			out.Org.Login = body.Login
			out.Org.ID = body.ID
		}
		out.Org.Accessible = true
		out.Checks["github_org_ok"] = true
	}

	if out.Checks["github_user_ok"] && machineUser != "" && !strings.EqualFold(machineUser, out.User.Login) {
		out.Error = fmt.Sprintf("machine user mismatch: secret=%s github=%s", machineUser, out.User.Login)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleOpenClawAdminRegister(w http.ResponseWriter, r *http.Request, req openClawAdminActionRequest) {
	task, err := s.startRegisterTask(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"action":           "register",
		"register_task_id": task.ID,
		"status":           "running",
	})
}

func (s *Server) startRegisterTask(ctx context.Context, req openClawAdminActionRequest) (store.RegisterTask, error) {
	if s.bots == nil {
		return store.RegisterTask{}, fmt.Errorf("bot manager is not configured")
	}
	if req.Provider == "" {
		req.Provider = "openclaw"
	}
	task, err := s.store.CreateRegisterTask(ctx, store.RegisterTask{
		Provider:  req.Provider,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		return store.RegisterTask{}, err
	}
	go s.runRegisterTask(task.ID, req)
	return task, nil
}

func (s *Server) runRegisterTask(taskID int64, req openClawAdminActionRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	_, _ = s.store.AppendRegisterTaskStep(ctx, store.RegisterTaskStep{
		TaskID:  taskID,
		Step:    "start",
		Status:  "running",
		Message: fmt.Sprintf("provider=%s", strings.TrimSpace(req.Provider)),
	})
	result, err := s.provisionAndRegisterOpenClaw(ctx, req, taskID)
	finishedAt := time.Now().UTC()
	if err != nil {
		_, _ = s.store.AppendRegisterTaskStep(context.Background(), store.RegisterTaskStep{
			TaskID:  taskID,
			Step:    "summary",
			Status:  "failed",
			Message: err.Error(),
		})
		_, _ = s.store.FinishRegisterTask(context.Background(), taskID, "failed", "", "", "", "", err.Error(), finishedAt)
		return
	}
	_, _ = s.store.AppendRegisterTaskStep(context.Background(), store.RegisterTaskStep{
		TaskID:  taskID,
		Step:    "summary",
		Status:  "ok",
		Message: fmt.Sprintf("user=%s repo=%s", result.UserID, result.RepoFullName),
	})
	_, _ = s.store.FinishRegisterTask(context.Background(), taskID, "succeeded", result.UserID, result.UserName, result.RepoFullName, result.Image, "", finishedAt)
	s.appendToolCostEvent(context.Background(), result.UserID, "tool.openclaw.register", 1, map[string]any{
		"register_task_id": taskID,
		"repo_full_name":   result.RepoFullName,
		"user_name":        result.UserName,
	})
}

func (s *Server) handleOpenClawAdminRegisterTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("register_task_id"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "register_task_id is required")
		return
	}
	taskID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || taskID <= 0 {
		writeError(w, http.StatusBadRequest, "invalid register_task_id")
		return
	}
	task, err := s.store.GetRegisterTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	steps, err := s.store.ListRegisterTaskSteps(r.Context(), taskID, 2000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var last any
	lastStepName := ""
	lastStepStatus := ""
	if n := len(steps); n > 0 {
		last = steps[n-1]
		lastStepName = strings.TrimSpace(steps[n-1].Step)
		lastStepStatus = strings.TrimSpace(steps[n-1].Status)
	}
	phase, progress := registerTaskPhaseProgress(task.Status, lastStepName, lastStepStatus)
	writeJSON(w, http.StatusOK, map[string]any{
		"register_task_id": taskID,
		"task":             task,
		"phase":            phase,
		"progress":         progress,
		"last_step":        last,
		"step_count":       len(steps),
		"steps":            steps,
	})
}

func (s *Server) handleOpenClawAdminRegisterHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	items, err := s.store.ListRegisterTasks(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleOpenClawAdminRestart(w http.ResponseWriter, r *http.Request, req openClawAdminActionRequest) {
	if s.kubeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not available")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	selector := "app=aibot"
	pods, err := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).List(r.Context(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deleted := make([]string, 0, len(pods.Items))
	for _, p := range pods.Items {
		if resolveUserIDFromLabels(p.Labels) != req.UserID {
			continue
		}
		if err := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).Delete(r.Context(), p.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		deleted = append(deleted, p.Name)
	}
	s.appendToolCostEvent(r.Context(), req.UserID, "tool.openclaw.restart", int64(len(deleted)+1), map[string]any{
		"deleted_pods": len(deleted),
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"action":       "restart",
		"user_id":      req.UserID,
		"deleted_pods": deleted,
	})
}

func (s *Server) handleOpenClawAdminRedeploy(w http.ResponseWriter, r *http.Request, req openClawAdminActionRequest) {
	if s.bots == nil {
		writeError(w, http.StatusServiceUnavailable, "bot manager is not configured")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	image := req.Image
	if image == "" {
		image = s.resolveBotImageForApply(r.Context(), req.UserID)
	}
	if err := s.bots.ApplyRuntimeProfile(r.Context(), req.UserID, image); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.appendToolCostEvent(r.Context(), req.UserID, "tool.openclaw.redeploy", 1, map[string]any{
		"image": image,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"action":  "redeploy",
		"user_id": req.UserID,
		"image":   image,
		"status":  "ok",
	})
}

func (s *Server) handleOpenClawAdminDelete(w http.ResponseWriter, r *http.Request, req openClawAdminActionRequest) {
	if s.kubeClient == nil {
		writeError(w, http.StatusServiceUnavailable, "kubernetes client is not available")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	item, err := s.store.GetBot(r.Context(), req.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("user not found: %v", err))
		return
	}
	workload := bot.WorkloadName(req.UserID)
	deploymentName := workload
	serviceName := workload
	configMapName := bot.ProfileConfigMapName(workload)
	pvcName := bot.StatePVCName(workload)

	_ = s.kubeClient.AppsV1().Deployments(s.cfg.BotNamespace).Delete(r.Context(), deploymentName, metav1.DeleteOptions{})
	_ = s.kubeClient.CoreV1().Services(s.cfg.BotNamespace).Delete(r.Context(), serviceName, metav1.DeleteOptions{})
	_ = s.kubeClient.CoreV1().ConfigMaps(s.cfg.BotNamespace).Delete(r.Context(), configMapName, metav1.DeleteOptions{})
	_ = s.kubeClient.CoreV1().PersistentVolumeClaims(s.cfg.BotNamespace).Delete(r.Context(), pvcName, metav1.DeleteOptions{})

	pods, _ := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).List(r.Context(), metav1.ListOptions{LabelSelector: "app=aibot"})
	for _, p := range pods.Items {
		if resolveUserIDFromLabels(p.Labels) != req.UserID {
			continue
		}
		_ = s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).Delete(r.Context(), p.Name, metav1.DeleteOptions{})
	}
	_, _ = s.store.UpsertBot(r.Context(), store.BotUpsertInput{
		BotID:       item.BotID,
		Name:        item.Name,
		Provider:    item.Provider,
		Status:      "deleted",
		Initialized: false,
	})
	s.appendToolCostEvent(r.Context(), req.UserID, "tool.openclaw.delete", 2, map[string]any{
		"deployment": deploymentName,
		"service":    serviceName,
		"config_map": configMapName,
		"state_pvc":  pvcName,
	})
	writeJSON(w, http.StatusAccepted, map[string]any{
		"action":  "delete",
		"user_id": req.UserID,
		"deleted": map[string]any{
			"deployment": deploymentName,
			"service":    serviceName,
			"config_map": configMapName,
			"state_pvc":  pvcName,
		},
	})
}

func registerTaskPhaseProgress(taskStatus, lastStep, lastStatus string) (string, int) {
	if taskStatus == "succeeded" {
		return "done", 100
	}
	if taskStatus == "failed" {
		return "failed", 100
	}
	order := []string{
		"start",
		"load_github_config",
		"allocate_user_name",
		"fetch_release",
		"ensure_repo",
		"sync_repo",
		"generate_git_credentials",
		"deploy_key",
		"upsert_git_secret",
		"build_image",
		"register_and_deploy",
		"summary",
	}
	idx := 0
	for i, step := range order {
		if step == lastStep {
			idx = i
			break
		}
	}
	if lastStatus == "running" && idx < len(order)-1 {
		// running shows partial completion in current stage
		return lastStep, int(float64(idx)/float64(len(order)-1)*100.0 + 0.5)
	}
	if idx < 0 {
		idx = 0
	}
	return lastStep, int(float64(idx+1)/float64(len(order)-1)*100.0 + 0.5)
}

func (s *Server) provisionAndRegisterOpenClaw(ctx context.Context, req openClawAdminActionRequest, taskID int64) (registerProvisionResult, error) {
	out := registerProvisionResult{}
	logStep := func(step, status, msg string) {
		_, _ = s.store.AppendRegisterTaskStep(ctx, store.RegisterTaskStep{
			TaskID:  taskID,
			Step:    step,
			Status:  status,
			Message: msg,
		})
	}
	gh, err := s.loadGitHubAdminConfig(ctx)
	if err != nil {
		logStep("load_github_config", "failed", err.Error())
		return out, err
	}
	logStep("load_github_config", "ok", fmt.Sprintf("owner=%s machine=%s", gh.Owner, gh.MachineUser))

	userID := fmt.Sprintf("user-%d-%04d", time.Now().UnixMilli(), rand.Intn(10000))
	userName, err := s.allocateReadableUserName(ctx, gh)
	if err != nil {
		logStep("allocate_user_name", "failed", err.Error())
		return out, err
	}
	logStep("allocate_user_name", "ok", fmt.Sprintf("user_id=%s user_name=%s", userID, userName))

	releaseTag, releaseErr := s.fetchOpenClawLatestReleaseTag(ctx, gh.Token)
	sourceRef := "main"
	if releaseTag != "" {
		sourceRef = releaseTag
	}
	if releaseErr != nil && releaseTag == "" {
		logStep("fetch_release", "warn", releaseErr.Error())
	} else {
		logStep("fetch_release", "ok", fmt.Sprintf("source_ref=%s", sourceRef))
	}

	repoName := "openclaw-" + userName
	repoFullName := gh.Owner + "/" + repoName
	repoURLSSH := fmt.Sprintf("git@github.com:%s.git", repoFullName)
	repoURLForBot := repoURLSSH
	gitSecretName := "aibot-git-" + userID
	logStep("ensure_repo", "running", repoFullName)
	if err := s.ensureGitHubRepo(ctx, gh, repoName); err != nil {
		logStep("ensure_repo", "failed", err.Error())
		return out, err
	}
	logStep("ensure_repo", "ok", repoFullName)
	logStep("sync_repo", "running", sourceRef)
	if err := s.syncRepoFromUpstreamRef(ctx, gh, repoName, sourceRef); err != nil {
		logStep("sync_repo", "failed", err.Error())
		return out, err
	}
	logStep("sync_repo", "ok", sourceRef)
	if s.cfg.GitHubMockEnabled {
		// Keep functional local register smoke under mock mode while still using per-user secrets.
		if v := strings.TrimSpace(s.cfg.UpgradeRepoURL); v != "" {
			repoURLForBot = v
		}
	}

	logStep("generate_git_credentials", "running", "ssh-keygen + ssh-keyscan")
	privateKey, publicKey, knownHosts, err := s.generateGitSSHCredentials(ctx, userID)
	if err != nil {
		logStep("generate_git_credentials", "failed", err.Error())
		return out, err
	}
	logStep("generate_git_credentials", "ok", "generated")

	logStep("deploy_key", "running", repoFullName)
	if err := s.ensureGitHubDeployKey(ctx, gh, repoFullName, userID, publicKey); err != nil {
		logStep("deploy_key", "failed", err.Error())
		return out, err
	}
	logStep("deploy_key", "ok", "created")

	logStep("upsert_git_secret", "running", gitSecretName)
	if err := s.upsertPerUserGitSecret(ctx, gitSecretName, privateKey, knownHosts); err != nil {
		logStep("upsert_git_secret", "failed", err.Error())
		return out, err
	}
	logStep("upsert_git_secret", "ok", gitSecretName)

	selectedImage := strings.TrimSpace(req.Image)
	imageBuilt := false
	imageBuildNote := ""
	if selectedImage == "" && releaseTag != "" && !s.cfg.GitHubMockEnabled {
		logStep("build_image", "running", releaseTag)
		built, berr := s.buildOpenClawImageFromRef(ctx, releaseTag)
		if berr != nil {
			imageBuildNote = "release build failed, fallback to BOT_DEFAULT_IMAGE: " + berr.Error()
			log.Printf("register image build fallback user=%s release=%s err=%v", userID, releaseTag, berr)
			logStep("build_image", "warn", berr.Error())
		} else {
			selectedImage = built
			imageBuilt = true
			logStep("build_image", "ok", built)
		}
	}
	if selectedImage == "" && s.cfg.GitHubMockEnabled {
		imageBuildNote = strings.TrimSpace(strings.TrimSpace(imageBuildNote + "; github api mock enabled, skip release image build"))
		selectedImage = strings.TrimSpace(s.cfg.BotDefaultImage)
	}
	if selectedImage == "" && releaseTag == "" {
		imageBuildNote = strings.TrimSpace(strings.TrimSpace(imageBuildNote + "; no release found, using BOT_DEFAULT_IMAGE"))
		selectedImage = strings.TrimSpace(s.cfg.BotDefaultImage)
	}
	if releaseErr != nil && releaseTag == "" {
		log.Printf("register release lookup failed, fallback to default image: %v", releaseErr)
	}

	logStep("register_and_deploy", "running", "register bot and deploy workload")
	item, err := s.bots.RegisterAndInit(ctx, bot.DeploySpec{
		Provider:         req.Provider,
		Image:            selectedImage,
		BotID:            userID,
		Name:             userName,
		SourceRepoURL:    repoURLForBot,
		SourceRepoBranch: "main",
		GitSSHSecretName: gitSecretName,
	})
	if err != nil {
		logStep("register_and_deploy", "failed", err.Error())
		return out, err
	}
	logStep("register_and_deploy", "ok", item.BotID)

	out = registerProvisionResult{
		UserID:         item.BotID,
		UserName:       item.Name,
		RepoFullName:   repoFullName,
		RepoURLSSH:     repoURLSSH,
		GitSecretName:  gitSecretName,
		SourceRef:      sourceRef,
		ReleaseTag:     releaseTag,
		Image:          selectedImage,
		ImageBuilt:     imageBuilt,
		ImageBuildNote: strings.TrimSpace(strings.Trim(imageBuildNote, "; ")),
	}
	return out, nil
}

func (s *Server) loadGitHubAdminConfig(ctx context.Context) (githubAdminConfig, error) {
	if s.cfg.GitHubMockEnabled {
		owner := strings.TrimSpace(s.cfg.GitHubMockOwner)
		if owner == "" {
			owner = "clawcolony"
		}
		machine := strings.TrimSpace(s.cfg.GitHubMockMachine)
		if machine == "" {
			machine = "claw-archivist"
		}
		return githubAdminConfig{
			Token:       "mock-token",
			Owner:       owner,
			MachineUser: machine,
		}, nil
	}
	if s.kubeClient == nil {
		return githubAdminConfig{}, fmt.Errorf("kubernetes client is not available")
	}
	const secretName = "clawcolony-github"
	sec, err := s.kubeClient.CoreV1().Secrets(s.cfg.ClawWorldNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return githubAdminConfig{}, fmt.Errorf("load github secret %s/%s: %w", s.cfg.ClawWorldNamespace, secretName, err)
	}
	cfg := githubAdminConfig{
		Token:       strings.TrimSpace(string(sec.Data["GITHUB_TOKEN"])),
		Owner:       strings.TrimSpace(string(sec.Data["GITHUB_OWNER"])),
		MachineUser: strings.TrimSpace(string(sec.Data["GITHUB_MACHINE_USER"])),
	}
	if cfg.Token == "" || cfg.Owner == "" {
		return githubAdminConfig{}, fmt.Errorf("invalid github admin secret: missing GITHUB_TOKEN or GITHUB_OWNER")
	}
	if cfg.MachineUser == "" {
		cfg.MachineUser = "x-access-token"
	}
	return cfg, nil
}

func (s *Server) allocateReadableUserName(ctx context.Context, gh githubAdminConfig) (string, error) {
	used := map[string]bool{}
	items, err := s.store.ListBots(ctx)
	if err != nil {
		return "", err
	}
	for _, it := range items {
		n := strings.ToLower(strings.TrimSpace(it.Name))
		if n != "" {
			used[n] = true
		}
	}
	for _, n := range registerNamePool {
		c := strings.ToLower(strings.TrimSpace(n))
		if c == "" || used[c] {
			continue
		}
		ok, err := s.githubRepoNameAvailable(ctx, gh, "openclaw-"+c)
		if err != nil {
			return "", err
		}
		if ok {
			return c, nil
		}
	}
	for _, n := range registerNamePool {
		base := strings.ToLower(strings.TrimSpace(n))
		if base == "" {
			continue
		}
		for _, adj := range registerAdjPool {
			c := strings.ToLower(strings.TrimSpace(adj + "-" + base))
			if c == "" || used[c] {
				continue
			}
			ok, err := s.githubRepoNameAvailable(ctx, gh, "openclaw-"+c)
			if err != nil {
				return "", err
			}
			if ok {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("username pool exhausted")
}

func (s *Server) githubRepoNameAvailable(ctx context.Context, gh githubAdminConfig, repoName string) (bool, error) {
	if s.cfg.GitHubMockEnabled {
		full := gh.Owner + "/" + strings.TrimSpace(repoName)
		s.githubMockMu.Lock()
		_, exists := s.githubMockRepo[full]
		s.githubMockMu.Unlock()
		return !exists, nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", gh.Owner, repoName)
	code, _, err := s.githubAPI(ctx, gh.Token, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	if code == http.StatusNotFound {
		return true, nil
	}
	if code == http.StatusOK {
		return false, nil
	}
	return false, fmt.Errorf("unexpected github response while checking repo name: %d", code)
}

func (s *Server) fetchOpenClawLatestReleaseTag(ctx context.Context, token string) (string, error) {
	if s.cfg.GitHubMockEnabled {
		tag := strings.TrimSpace(s.cfg.GitHubMockRelease)
		if tag == "" {
			tag = "v2026.3.1"
		}
		return tag, nil
	}
	code, body, err := s.githubAPI(ctx, token, http.MethodGet, "https://api.github.com/repos/openclaw/openclaw/releases/latest", nil)
	if err != nil {
		return "", err
	}
	if code == http.StatusNotFound {
		return "", nil
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("github latest release status=%d", code)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.TagName), nil
}

func (s *Server) ensureGitHubRepo(ctx context.Context, gh githubAdminConfig, repoName string) error {
	if s.cfg.GitHubMockEnabled {
		full := gh.Owner + "/" + strings.TrimSpace(repoName)
		s.githubMockMu.Lock()
		s.githubMockRepo[full] = time.Now().UTC()
		s.githubMockMu.Unlock()
		return nil
	}
	checkURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", gh.Owner, repoName)
	code, _, err := s.githubAPI(ctx, gh.Token, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}
	if code == http.StatusOK {
		return nil
	}
	if code != http.StatusNotFound {
		return fmt.Errorf("check github repo failed status=%d", code)
	}

	createURL := fmt.Sprintf("https://api.github.com/orgs/%s/repos", gh.Owner)
	reqBody := map[string]any{
		"name":        repoName,
		"private":     true,
		"auto_init":   false,
		"description": "OpenClaw runtime repo for " + repoName,
	}
	code, body, err := s.githubAPI(ctx, gh.Token, http.MethodPost, createURL, reqBody)
	if err != nil {
		return err
	}
	if code != http.StatusCreated && code != http.StatusUnprocessableEntity {
		return fmt.Errorf("create github repo failed status=%d body=%s", code, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *Server) syncRepoFromUpstreamRef(ctx context.Context, gh githubAdminConfig, repoName, sourceRef string) error {
	if s.cfg.GitHubMockEnabled {
		return nil
	}
	workDir, err := os.MkdirTemp("", "clawcolony-repo-sync-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	repoDir := filepath.Join(workDir, "repo")
	if _, err := s.runCmd(ctx, "", nil, "git", "clone", "--depth=1", "--single-branch", "--branch", sourceRef, "https://github.com/openclaw/openclaw.git", repoDir); err != nil {
		return fmt.Errorf("clone upstream ref=%s: %w", sourceRef, err)
	}
	// Source ref may be a release tag on detached HEAD. To avoid remote unpack failures
	// from shallow history boundaries, create an orphan bootstrap commit for target main.
	if _, err := s.runCmd(ctx, repoDir, nil, "git", "checkout", "--orphan", "bootstrap-main"); err != nil {
		return fmt.Errorf("prepare orphan branch: %w", err)
	}
	if _, err := s.runCmd(ctx, repoDir, nil, "git", "add", "-A"); err != nil {
		return fmt.Errorf("stage bootstrap snapshot: %w", err)
	}
	if _, err := s.runCmd(ctx, repoDir, nil, "git", "-c", "user.name=archivist", "-c", "user.email=archivist@clawcolony.ai", "commit", "-m", "Bootstrap from openclaw "+sourceRef); err != nil {
		return fmt.Errorf("commit bootstrap snapshot: %w", err)
	}

	targetHTTPS := fmt.Sprintf("https://github.com/%s/%s.git", gh.Owner, repoName)
	pushURL, err := withGitToken(targetHTTPS, gh.MachineUser, gh.Token)
	if err != nil {
		return err
	}
	if _, err := s.runCmd(ctx, repoDir, nil, "git", "remote", "set-url", "origin", pushURL); err != nil {
		return fmt.Errorf("set target remote: %w", err)
	}
	if out, err := s.runCmd(ctx, repoDir, nil, "git", "push", "-u", "--force", "origin", "HEAD:refs/heads/main"); err != nil {
		return fmt.Errorf("push snapshot to target main: %w; output=%s", err, strings.TrimSpace(out))
	}
	return nil
}

func (s *Server) ensureGitHubDeployKey(ctx context.Context, gh githubAdminConfig, repoFullName, userID, publicKey string) error {
	if s.cfg.GitHubMockEnabled {
		title := fmt.Sprintf("mock-clawcolony-%s-%d", sanitizeName(userID), time.Now().UTC().Unix())
		s.githubMockMu.Lock()
		s.githubMockKeys[repoFullName] = append(s.githubMockKeys[repoFullName], title)
		s.githubMockMu.Unlock()
		return nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/keys", repoFullName)
	title := fmt.Sprintf("clawcolony-%s-%d", sanitizeName(userID), time.Now().UTC().Unix())
	reqBody := map[string]any{
		"title":     title,
		"key":       strings.TrimSpace(publicKey),
		"read_only": false,
	}
	code, body, err := s.githubAPI(ctx, gh.Token, http.MethodPost, url, reqBody)
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return fmt.Errorf("create deploy key failed status=%d body=%s", code, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *Server) generateGitSSHCredentials(ctx context.Context, userID string) (privateKey, publicKey, knownHosts string, err error) {
	workDir, err := os.MkdirTemp("", "clawcolony-sshkey-*")
	if err != nil {
		return "", "", "", err
	}
	defer os.RemoveAll(workDir)
	keyPath := filepath.Join(workDir, "id_ed25519")
	if _, err := s.runCmd(ctx, "", nil, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", keyPath, "-C", "clawcolony-"+sanitizeName(userID)); err != nil {
		return "", "", "", fmt.Errorf("ssh-keygen failed: %w", err)
	}
	privateRaw, err := os.ReadFile(keyPath)
	if err != nil {
		return "", "", "", err
	}
	publicRaw, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		return "", "", "", err
	}
	scanCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	knownOut, err := s.runCmd(scanCtx, "", nil, "ssh-keyscan", "-T", "5", "-t", "rsa,ecdsa,ed25519", "github.com")
	if err != nil || strings.TrimSpace(knownOut) == "" {
		// Retry once with FQDN to avoid resolver/search-domain edge cases.
		retryOut, retryErr := s.runCmd(scanCtx, "", nil, "ssh-keyscan", "-T", "5", "-t", "rsa,ecdsa,ed25519", "github.com.")
		if retryErr != nil || strings.TrimSpace(retryOut) == "" {
			if err != nil {
				return "", "", "", fmt.Errorf("ssh-keyscan github.com failed: %w", err)
			}
			return "", "", "", fmt.Errorf("ssh-keyscan github.com failed: empty output")
		}
		knownOut = retryOut
	}
	knownOut = strings.ReplaceAll(knownOut, "github.com.", "github.com")
	return string(privateRaw), string(publicRaw), knownOut, nil
}

func (s *Server) upsertPerUserGitSecret(ctx context.Context, secretName, privateKey, knownHosts string) error {
	if s.kubeClient == nil {
		return fmt.Errorf("kubernetes client is not available")
	}
	secrets := s.kubeClient.CoreV1().Secrets(s.cfg.BotNamespace)
	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "clawcolony",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"id_ed25519":  []byte(strings.TrimSpace(privateKey) + "\n"),
			"known_hosts": []byte(strings.TrimSpace(knownHosts) + "\n"),
		},
	}
	existing, err := secrets.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, createErr := secrets.Create(ctx, obj, metav1.CreateOptions{})
			return createErr
		}
		return err
	}
	existing.Data = obj.Data
	existing.Labels = obj.Labels
	_, err = secrets.Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (s *Server) buildOpenClawImageFromRef(ctx context.Context, sourceRef string) (string, error) {
	image := fmt.Sprintf("openclaw:release-%s", sanitizeName(sourceRef))
	if _, err := s.runCmd(ctx, "", nil, "docker", "image", "inspect", image); err == nil {
		log.Printf("register image cache hit: source_ref=%s image=%s", sourceRef, image)
		return image, nil
	}

	workDir, err := os.MkdirTemp("", "clawcolony-openclaw-build-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workDir)
	repoDir := filepath.Join(workDir, "repo")
	if _, err := s.runCmd(ctx, "", nil, "git", "clone", "--depth=1", "--single-branch", "--branch", sourceRef, "https://github.com/openclaw/openclaw.git", repoDir); err != nil {
		return "", fmt.Errorf("clone openclaw release %s failed: %w", sourceRef, err)
	}

	platform := "linux/amd64"
	if p, pErr := s.detectPlatform(ctx, 0); pErr == nil {
		platform = p
	}
	dockerfile := strings.TrimSpace(s.cfg.UpgradeDockerfile)
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	dfPath := filepath.Join(repoDir, dockerfile)
	if _, err := os.Stat(dfPath); err != nil {
		return "", fmt.Errorf("dockerfile not found in release source: %s", dfPath)
	}
	buildArgs := []string{"build", "--platform", platform, "-f", dfPath, "-t", image, "--progress=plain"}
	if s.cfg.UpgradeBuildNoCache {
		buildArgs = append(buildArgs, "--no-cache")
	}
	if extra := strings.TrimSpace(s.cfg.UpgradeBuildArgs); extra != "" {
		buildArgs = append(buildArgs, strings.Fields(extra)...)
	}
	buildArgs = append(buildArgs, repoDir)
	if _, err := s.runCmd(ctx, "", []string{"DOCKER_BUILDKIT=1", "BUILDKIT_PROGRESS=plain"}, "docker", buildArgs...); err != nil {
		return "", err
	}
	if _, err := s.runCmd(ctx, "", nil, "minikube", "image", "load", image); err != nil {
		log.Printf("register build: minikube image load skipped/failed image=%s err=%v", image, err)
	}
	return image, nil
}

func (s *Server) githubAPI(ctx context.Context, token, method, rawURL string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = strings.NewReader(string(buf))
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, nil
}

func (s *Server) buildOpenClawAdminOverview(ctx context.Context) (openClawAdminOverview, error) {
	out := openClawAdminOverview{
		Time: time.Now().UTC().Format(time.RFC3339),
		Namespace: map[string]string{
			"clawcolony": s.cfg.ClawWorldNamespace,
			"user":       s.cfg.BotNamespace,
		},
		Config: map[string]any{
			"default_image":           s.cfg.BotDefaultImage,
			"model":                   s.cfg.BotModel,
			"github_api_mock_enabled": s.cfg.GitHubMockEnabled,
			"env_secret_name":         s.cfg.BotEnvSecretName,
			"git_ssh_secret_name":     s.cfg.BotGitSSHSecret,
			"git_secret_mode": func() string {
				if strings.TrimSpace(s.cfg.BotGitSSHSecret) == "" {
					return "per-user"
				}
				return "shared"
			}(),
			"source_repo_branch":        s.cfg.BotSourceRepoBranch,
			"source_repo_configured":    strings.TrimSpace(s.cfg.UpgradeRepoURL) != "",
			"upgrade_internal_auth_set": strings.TrimSpace(s.cfg.UpgradeAuthToken) != "",
		},
		Checks: map[string]bool{
			"kubernetes_client": s.kubeClient != nil,
			"database_enabled":  strings.TrimSpace(s.cfg.DatabaseURL) != "",
		},
	}
	if s.kubeClient == nil {
		return out, nil
	}
	out.Checks["env_secret_exists"] = s.checkSecretExists(ctx, s.cfg.BotNamespace, s.cfg.BotEnvSecretName)
	if strings.TrimSpace(s.cfg.BotGitSSHSecret) == "" {
		out.Checks["per_user_git_secret_mode"] = true
		out.Checks["git_ssh_secret_exists"] = true
	} else {
		out.Checks["per_user_git_secret_mode"] = false
		out.Checks["git_ssh_secret_exists"] = s.checkSecretExists(ctx, s.cfg.BotNamespace, s.cfg.BotGitSSHSecret)
	}

	botItems, err := s.store.ListBots(ctx)
	if err != nil {
		return out, err
	}
	botByID := make(map[string]store.Bot, len(botItems))
	for _, it := range botItems {
		botByID[it.BotID] = it
	}

	deps, err := s.kubeClient.AppsV1().Deployments(s.cfg.BotNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=aibot",
	})
	if err != nil {
		return out, err
	}
	pods, err := s.kubeClient.CoreV1().Pods(s.cfg.BotNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=aibot",
	})
	if err != nil {
		return out, err
	}
	podByUser := make(map[string]corev1.Pod, len(pods.Items))
	for _, p := range pods.Items {
		userID := resolveUserIDFromLabels(p.Labels)
		if userID == "" {
			// Fallback: infer from pod name prefix: <user_id>-<hash>-<suffix>
			if idx := strings.LastIndex(p.Name, "-"); idx > 0 {
				if idx2 := strings.LastIndex(p.Name[:idx], "-"); idx2 > 0 {
					userID = p.Name[:idx2]
				}
			}
		}
		if userID == "" {
			continue
		}
		prev, ok := podByUser[userID]
		if !ok || p.CreationTimestamp.Time.After(prev.CreationTimestamp.Time) {
			podByUser[userID] = p
		}
	}

	now := time.Now().UTC()
	out.Pods = make([]openClawAdminPodOverview, 0, len(deps.Items))
	for _, d := range deps.Items {
		userID := resolveUserIDFromLabels(d.Labels)
		if userID == "" {
			userID = strings.TrimSpace(d.Name)
		}
		if userID == "" {
			continue
		}
		entry := openClawAdminPodOverview{
			UserID:         userID,
			DeploymentName: d.Name,
		}
		if b, ok := botByID[userID]; ok {
			entry.Name = b.Name
			entry.Status = b.Status
			entry.Initialized = b.Initialized
		}
		if p, ok := podByUser[userID]; ok {
			entry.PodName = p.Name
			entry.Phase = string(p.Status.Phase)
			entry.NodeName = p.Spec.NodeName
			entry.PodIP = p.Status.PodIP
			if !p.Status.StartTime.IsZero() {
				entry.StartedAt = p.Status.StartTime.UTC().Format(time.RFC3339)
				entry.AgeSeconds = int64(now.Sub(p.Status.StartTime.Time).Seconds())
			}
			for _, c := range p.Status.ContainerStatuses {
				if c.Name == "bot" {
					entry.Restarts = c.RestartCount
					entry.Ready = c.Ready
					entry.Image = c.Image
					break
				}
			}
			if entry.Image == "" {
				for _, c := range p.Spec.Containers {
					if c.Name == "bot" {
						entry.Image = c.Image
						break
					}
				}
			}
		}
		out.Pods = append(out.Pods, entry)
	}
	return out, nil
}

func (s *Server) checkSecretExists(ctx context.Context, namespace, name string) bool {
	name = strings.TrimSpace(name)
	if s.kubeClient == nil || name == "" {
		return false
	}
	_, err := s.kubeClient.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	return err == nil
}
