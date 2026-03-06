package server

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Server) desiredMinPopulation() int {
	v := s.cfg.MinPopulation
	if v <= 0 {
		v = 1
	}
	return v
}

func (s *Server) listLivingUserIDs(ctx context.Context) ([]string, error) {
	ids, err := s.listActiveUserIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || id == clawWorldSystemID {
			continue
		}
		life, err := s.store.GetUserLifeState(ctx, id)
		if err != nil {
			// No persisted life-state means this user is considered alive by default.
			out = append(out, id)
			continue
		}
		if normalizeLifeStateForServer(life.State) == "dead" {
			continue
		}
		out = append(out, id)
	}
	return normalizeUniqueUsers(out), nil
}

func (s *Server) countRunningRegisterTasks(ctx context.Context) (int, error) {
	items, err := s.store.ListRegisterTasks(ctx, 200)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, it := range items {
		if strings.EqualFold(strings.TrimSpace(it.Status), "running") {
			count++
		}
	}
	return count, nil
}

func (s *Server) runMinPopulationRevival(ctx context.Context, tickID int64) error {
	minPopulation := s.desiredMinPopulation()
	living, err := s.listLivingUserIDs(ctx)
	if err != nil {
		return err
	}
	current := len(living)
	if current >= minPopulation {
		return nil
	}

	runningTasks, err := s.countRunningRegisterTasks(ctx)
	if err != nil {
		return err
	}
	needed := minPopulation - current
	toCreate := needed - runningTasks
	if toCreate <= 0 {
		genesisStateMu.Lock()
		state, stateErr := s.getAutoRevivalState(ctx)
		if stateErr == nil {
			state.LastTriggerTick = tickID
			state.LastTriggerAt = time.Now().UTC()
			state.LastReason = fmt.Sprintf("gap already covered by running tasks: min=%d current=%d running=%d", minPopulation, current, runningTasks)
			state.LastRequested = 0
			state.LastTaskIDs = []int64{}
			_ = s.saveAutoRevivalState(ctx, state)
		}
		genesisStateMu.Unlock()
		return nil
	}
	// Keep each tick bounded to avoid burst registration on unstable loops.
	if toCreate > 3 {
		toCreate = 3
	}

	taskIDs := make([]int64, 0, toCreate)
	for i := 0; i < toCreate; i++ {
		task, err := s.startRegisterTask(ctx, openClawAdminActionRequest{
			Action:   "register",
			Provider: "openclaw",
		})
		if err != nil {
			return err
		}
		taskIDs = append(taskIDs, task.ID)
	}

	genesisStateMu.Lock()
	state, stateErr := s.getAutoRevivalState(ctx)
	if stateErr == nil {
		state.LastTriggerTick = tickID
		state.LastTriggerAt = time.Now().UTC()
		state.LastReason = fmt.Sprintf("auto revival triggered: min=%d current=%d running_before=%d requested=%d", minPopulation, current, runningTasks, toCreate)
		state.LastRequested = toCreate
		state.LastTaskIDs = taskIDs
		_ = s.saveAutoRevivalState(ctx, state)
	}
	genesisStateMu.Unlock()

	subject := "[WORLD-REVIVAL] min population recovery triggered"
	body := fmt.Sprintf(
		"tick_id=%d\nmin_population=%d\ncurrent_population=%d\nrunning_register_tasks=%d\nnew_register_requests=%d\nregister_task_ids=%v",
		tickID, minPopulation, current, runningTasks, toCreate, taskIDs,
	)
	s.sendMailAndPushHint(ctx, clawWorldSystemID, []string{clawWorldSystemID}, subject, body)
	return nil
}

