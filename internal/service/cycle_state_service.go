package service

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/repository"
)

// CycleStateService 提供基于 user_cycle_states 的周期生命周期管理能力。
type CycleStateService interface {
	// CreateCycle 发放一个新的周期记录，默认以 PENDING 状态入库。
	CreateCycle(ctx context.Context, req CycleGrantRequest) (*model.UserCycleState, error)

	// FindSystemFreeCycle 返回用户当前的 SYSTEM/FREE 周期（若存在）。
	FindSystemFreeCycle(ctx context.Context, userID int) (*model.UserCycleState, error)

	// ResetSystemFreeCycle 重置 SYSTEM/FREE 周期到指定窗口，并将 used_seconds / summary_used 清零。
	ResetSystemFreeCycle(ctx context.Context, cycleID int64, periodStart int64, periodEnd int64, totalSeconds int, summaryTotal int) error

	// ActivateCycle 将周期状态从 PENDING 切换为 ACTIVE，并可更新周期起止时间。
	ActivateCycle(ctx context.Context, cycleID int64, periodStart, periodEnd *int64) error

	// IncrementUsage 累计某周期的消耗秒数，用于扣费。
	IncrementUsage(ctx context.Context, cycleID int64, seconds int) error

	// IncrementSummaryUsage 累计某周期的摘要额度消耗次数。
	IncrementSummaryUsage(ctx context.Context, cycleID int64, usedDelta int) error

	// Transition 将周期状态从 from 迁移到 to，用于完成/取消等动作。
	Transition(ctx context.Context, cycleID int64, from, to model.CycleState) error

	// ListConsumable 返回用户当前可用于扣费的周期列表（ACTIVE 状态）。
	ListConsumable(ctx context.Context, userID int) ([]*model.UserCycleState, error)

	// ListActiveByTier 返回指定权益层级的 ACTIVE 周期。
	ListActiveByTier(ctx context.Context, userID int, tier model.AccountRole) ([]*model.UserCycleState, error)

	// ListEffectiveByUserAndStates 返回用户在“当前时间窗口内有效”的周期列表（跨 tier）。
	// 统计口径允许 ACTIVE + COMPLETED，但会过滤掉未开始或已过期的周期。
	ListEffectiveByUserAndStates(ctx context.Context, userID int, states []model.CycleState, now int64) ([]*model.UserCycleState, error)

	// ListEffectiveByTier 返回指定权益层级在“当前时间窗口内有效”的周期列表，用于统计展示。
	// 统计口径允许 ACTIVE + COMPLETED，但会过滤掉未开始或已过期的周期。
	ListEffectiveByTier(ctx context.Context, userID int, tier model.AccountRole) ([]*model.UserCycleState, error)

	// ListActiveByPremium 返回指定订阅对应的 Premium 周期，支持状态过滤。
	ListActiveByPremium(ctx context.Context, userID int, subscriptionID int64, states []model.CycleState) ([]*model.UserCycleState, error)

	// TransitionActiveByTier 将指定权益层级的所有 ACTIVE 周期批量迁移到目标状态。
	TransitionActiveByTier(ctx context.Context, userID int, tier model.AccountRole, to model.CycleState) error

	// ListAllActiveExpired 返回所有需要过期处理的 ACTIVE 周期（period_end <= now）。
	ListAllActiveExpired(ctx context.Context, now int64) ([]*model.UserCycleState, error)

	// FindLatestEndedPaidCycle 返回用户最近一次结束的付费周期（PAYG/Premium/Pro），用于角色回退展示。
	FindLatestEndedPaidCycle(ctx context.Context, userID int) (*model.UserCycleState, error)
}

// CycleGrantRequest 描述创建周期时需要的参数。
type CycleGrantRequest struct {
	UserID       int
	Tier         model.AccountRole
	OriginType   model.CycleOriginType
	OriginID     *int64
	CycleNo      int
	PeriodStart  *int64
	PeriodEnd    *int64
	TotalSeconds int
	SummaryTotal int
	InitialState model.CycleState // 可选，默认为 PENDING
}

type cycleStateService struct {
	repo   repository.UserCycleRepository
	logger *zap.Logger
}

// NewCycleStateService 创建周期状态机服务实现。
func NewCycleStateService(repo repository.UserCycleRepository, logger *zap.Logger) CycleStateService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &cycleStateService{
		repo:   repo,
		logger: logger,
	}
}

func (s *cycleStateService) CreateCycle(ctx context.Context, req CycleGrantRequest) (*model.UserCycleState, error) {
	if req.UserID == 0 {
		return nil, fmt.Errorf("user_id is required")
	}
	if req.TotalSeconds < 0 {
		return nil, fmt.Errorf("total_seconds cannot be negative")
	}
	if req.SummaryTotal < 0 {
		return nil, fmt.Errorf("summary_total cannot be negative")
	}
	cycle := model.NewUserCycleState(
		req.UserID,
		req.Tier,
		req.OriginType,
		req.OriginID,
		req.CycleNo,
		req.PeriodStart,
		req.PeriodEnd,
		req.TotalSeconds,
	)
	cycle.SummaryTotal = req.SummaryTotal
	cycle.SummaryUsed = 0
	if req.InitialState != "" {
		cycle.State = req.InitialState
	}
	if err := s.repo.Create(ctx, cycle); err != nil {
		return nil, err
	}
	return cycle, nil
}

func (s *cycleStateService) FindSystemFreeCycle(ctx context.Context, userID int) (*model.UserCycleState, error) {
	return s.repo.FindSystemFreeCycle(ctx, userID)
}

func (s *cycleStateService) ResetSystemFreeCycle(ctx context.Context, cycleID int64, periodStart int64, periodEnd int64, totalSeconds int, summaryTotal int) error {
	return s.repo.ResetSystemFreeCycle(ctx, cycleID, periodStart, periodEnd, totalSeconds, summaryTotal)
}

func (s *cycleStateService) ActivateCycle(ctx context.Context, cycleID int64, periodStart, periodEnd *int64) error {
	if cycleID == 0 {
		return fmt.Errorf("cycle_id is required")
	}
	if err := s.repo.UpdatePeriod(ctx, cycleID, periodStart, periodEnd); err != nil {
		return err
	}
	return s.repo.UpdateState(ctx, cycleID, model.CycleStatePending, model.CycleStateActive)
}

func (s *cycleStateService) IncrementUsage(ctx context.Context, cycleID int64, seconds int) error {
	if cycleID == 0 {
		return fmt.Errorf("cycle_id is required")
	}
	if seconds <= 0 {
		return nil
	}
	return s.repo.IncrementUsage(ctx, cycleID, seconds)
}

func (s *cycleStateService) IncrementSummaryUsage(ctx context.Context, cycleID int64, usedDelta int) error {
	if cycleID == 0 {
		return fmt.Errorf("cycle_id is required")
	}
	if usedDelta <= 0 {
		return nil
	}
	return s.repo.IncrementSummaryUsage(ctx, cycleID, usedDelta)
}

func (s *cycleStateService) Transition(ctx context.Context, cycleID int64, from, to model.CycleState) error {
	if cycleID == 0 {
		return fmt.Errorf("cycle_id is required")
	}
	if from == "" || to == "" {
		return fmt.Errorf("from/to state is required")
	}
	return s.repo.UpdateState(ctx, cycleID, from, to)
}

func (s *cycleStateService) ListConsumable(ctx context.Context, userID int) ([]*model.UserCycleState, error) {
	if userID == 0 {
		return []*model.UserCycleState{}, nil
	}
	return s.repo.ListConsumable(ctx, userID)
}

func (s *cycleStateService) ListActiveByPremium(ctx context.Context, userID int, subscriptionID int64, states []model.CycleState) ([]*model.UserCycleState, error) {
	if userID == 0 || subscriptionID == 0 {
		return []*model.UserCycleState{}, nil
	}
	if len(states) == 0 {
		return []*model.UserCycleState{}, nil
	}
	cycles, err := s.repo.ListActiveByPremium(ctx, userID, states, subscriptionID)
	if err != nil {
		return nil, err
	}
	return cycles, nil
}

func (s *cycleStateService) ListActiveByTier(ctx context.Context, userID int, tier model.AccountRole) ([]*model.UserCycleState, error) {
	if userID == 0 {
		return []*model.UserCycleState{}, nil
	}
	cycles, err := s.repo.ListByUserAndStates(ctx, userID, []model.CycleState{model.CycleStateActive})
	if err != nil {
		return nil, err
	}
	filtered := make([]*model.UserCycleState, 0, len(cycles))
	for _, cycle := range cycles {
		if cycle.Tier == tier {
			filtered = append(filtered, cycle)
		}
	}
	return filtered, nil
}

func (s *cycleStateService) ListEffectiveByUserAndStates(ctx context.Context, userID int, states []model.CycleState, now int64) ([]*model.UserCycleState, error) {
	return s.repo.ListEffectiveByUserAndStates(ctx, userID, states, now)
}

func (s *cycleStateService) ListEffectiveByTier(ctx context.Context, userID int, tier model.AccountRole) ([]*model.UserCycleState, error) {
	if userID == 0 {
		return []*model.UserCycleState{}, nil
	}
	cycles, err := s.repo.ListByTierAndStates(ctx, userID, tier, []model.CycleState{model.CycleStateActive, model.CycleStateCompleted})
	if err != nil {
		return nil, err
	}
	filtered := make([]*model.UserCycleState, 0, len(cycles))
	now := time.Now().Unix()
	for _, cycle := range cycles {
		if cycle == nil {
			continue
		}
		if cycle.PeriodStart != nil && *cycle.PeriodStart > now {
			continue
		}
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd <= now {
			continue
		}
		filtered = append(filtered, cycle)
	}
	return filtered, nil
}

func (s *cycleStateService) ListAllActiveExpired(ctx context.Context, now int64) ([]*model.UserCycleState, error) {
	return s.repo.ListAllActiveExpired(ctx, now)
}

func (s *cycleStateService) FindLatestEndedPaidCycle(ctx context.Context, userID int) (*model.UserCycleState, error) {
	return s.repo.FindLatestEndedPaidCycle(ctx, userID)
}

func (s *cycleStateService) TransitionActiveByTier(ctx context.Context, userID int, tier model.AccountRole, to model.CycleState) error {
	if userID == 0 {
		return nil
	}
	if to == "" {
		return fmt.Errorf("target state is required")
	}
	cycles, err := s.ListActiveByTier(ctx, userID, tier)
	if err != nil {
		return err
	}
	for _, cycle := range cycles {
		if err := s.repo.UpdateState(ctx, cycle.ID, model.CycleStateActive, to); err != nil {
			return err
		}
	}
	return nil
}
