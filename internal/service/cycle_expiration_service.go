package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/model"
)

// CycleExpirationService 处理周期过期逻辑
type CycleExpirationService interface {
	// CheckAndExpireCycles 检查并处理过期的周期
	CheckAndExpireCycles(ctx context.Context, userID int) error

	// ExpireProCycles 过期Pro周期并清空所有时长
	ExpireProCycles(ctx context.Context, userID int) error

	// ExpireProMiniCycles 过期Pro Mini周期并清空所有时长
	ExpireProMiniCycles(ctx context.Context, userID int) error

	// ExpireAllEndedCycles 扫描所有已结束（period_end <= now）的周期并标记为 EXPIRED
	ExpireAllEndedCycles(ctx context.Context) (int, error)

	// IsProCycleExpired 检查指定Pro周期是否已过期
	IsProCycleExpired(cycle *model.UserCycleState, now int64) bool

	// CheckExpiredProCyclesForUser 检查指定用户的Pro周期是否过期
	CheckExpiredProCyclesForUser(ctx context.Context, userID int) (bool, error)
}

type cycleExpirationService struct {
	cycleSvc        CycleStateService
	accountStateSvc AccountStateService
	logger          *zap.Logger
}

// NewCycleExpirationService 创建周期过期服务
func NewCycleExpirationService(
	cycleSvc CycleStateService,
	accountStateSvc AccountStateService,
	logger *zap.Logger,
) CycleExpirationService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &cycleExpirationService{
		cycleSvc:        cycleSvc,
		accountStateSvc: accountStateSvc,
		logger:          logger,
	}
}

func (s *cycleExpirationService) CheckAndExpireCycles(ctx context.Context, userID int) error {
	now := time.Now().Unix()

	// 检查Pro周期是否过期
	proCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRolePro)
	if err != nil {
		return err
	}

	for _, cycle := range proCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
			s.logger.Info("检测到过期的Pro周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("expired_at", *cycle.PeriodEnd),
				zap.Int64("current_time", now))

			if err := s.ExpireProCycles(ctx, userID); err != nil {
				return err
			}
			break // 只需要处理一次
		}
	}

	// 检查Pro Mini周期是否过期
	proMiniCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRoleProMini)
	if err != nil {
		return err
	}

	for _, cycle := range proMiniCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
			s.logger.Info("检测到过期的Pro Mini周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("expired_at", *cycle.PeriodEnd),
				zap.Int64("current_time", now))

			if err := s.ExpireProMiniCycles(ctx, userID); err != nil {
				return err
			}
			break // 只需要处理一次
		}
	}

	// 检查其他类型的过期周期
	if err := s.expireOtherCycles(ctx, userID, now); err != nil {
		return err
	}

	return nil
}

func (s *cycleExpirationService) ExpireProCycles(ctx context.Context, userID int) error {
	s.logger.Info("开始过期Pro周期", zap.Int("user_id", userID))
	// 只修改 user_cycle_states 的 state，不做账户字段清空/降级副作用。
	return s.cycleSvc.TransitionActiveByTier(ctx, userID, model.AccountRolePro, model.CycleStateExpired)
}

func (s *cycleExpirationService) ExpireProMiniCycles(ctx context.Context, userID int) error {
	s.logger.Info("开始过期Pro Mini周期", zap.Int("user_id", userID))
	// 只修改 user_cycle_states 的 state，不做账户字段清空/降级副作用。
	return s.cycleSvc.TransitionActiveByTier(ctx, userID, model.AccountRoleProMini, model.CycleStateExpired)
}

// ExpireAllEndedCycles 扫描所有已结束（period_end <= now）的周期并标记为 EXPIRED
func (s *cycleExpirationService) ExpireAllEndedCycles(ctx context.Context) (int, error) {
	now := time.Now().Unix()
	expiredCount := 0

	// 全量扫描：找出所有 ACTIVE 且 period_end <= now 的周期（不限 tier）。
	cycles, err := s.cycleSvc.ListAllActiveExpired(ctx, now)
	if err != nil {
		return 0, err
	}
	if len(cycles) == 0 {
		return 0, nil
	}

	for _, cycle := range cycles {
		if cycle == nil {
			continue
		}
		from := cycle.State
		if from == "" {
			from = model.CycleStateActive
		}
		if from != model.CycleStateActive && from != model.CycleStateCompleted {
			continue
		}
		if err := s.cycleSvc.Transition(ctx, cycle.ID, from, model.CycleStateExpired); err != nil {
			// 幂等：周期可能已被其他流程处理，忽略无影响的“未命中”。
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return expiredCount, err
		}
		expiredCount++
	}
	return expiredCount, nil
}

// IsProCycleExpired 检查指定Pro周期是否已过期
func (s *cycleExpirationService) IsProCycleExpired(cycle *model.UserCycleState, now int64) bool {
	if cycle.Tier != model.AccountRolePro {
		return false
	}

	if cycle.State != model.CycleStateActive {
		return false
	}

	// Pro周期有365天的有效期
	if cycle.PeriodEnd != nil && *cycle.PeriodEnd <= now {
		return true
	}

	return false
}

// CheckExpiredProCyclesForUser 检查指定用户的Pro周期是否过期
func (s *cycleExpirationService) CheckExpiredProCyclesForUser(ctx context.Context, userID int) (bool, error) {
	now := time.Now().Unix()

	// 获取用户的活跃Pro周期
	proCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRolePro)
	if err != nil {
		return false, err
	}

	hasExpired := false
	for _, cycle := range proCycles {
		if s.IsProCycleExpired(cycle, now) {
			s.logger.Info("发现过期的Pro周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("period_end", *cycle.PeriodEnd),
				zap.Int64("now", now))

			// 执行过期处理
			if err := s.ExpireProCycles(ctx, userID); err != nil {
				s.logger.Error("Pro周期过期处理失败",
					zap.Int("user_id", userID),
					zap.Error(err))
				return false, err
			}

			hasExpired = true
			break // 一旦处理了过期，就不需要继续检查其他周期
		}
	}

	return hasExpired, nil
}

func (s *cycleExpirationService) expireOtherCycles(ctx context.Context, userID int, now int64) error {
	// 检查PAYG周期过期
	paygCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRolePayg)
	if err != nil {
		return err
	}

	for _, cycle := range paygCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
			s.logger.Info("过期PAYG周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("expired_at", *cycle.PeriodEnd))

			if err := s.cycleSvc.Transition(ctx, cycle.ID, model.CycleStateActive, model.CycleStateExpired); err != nil {
				return err
			}
		}
	}

	// 检查SPECIAL_OFFER周期过期
	specialOfferCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRoleSpecialOffer)
	if err != nil {
		return err
	}

	for _, cycle := range specialOfferCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
			s.logger.Info("过期SPECIAL_OFFER周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("expired_at", *cycle.PeriodEnd))

			if err := s.cycleSvc.Transition(ctx, cycle.ID, model.CycleStateActive, model.CycleStateExpired); err != nil {
				return err
			}
		}
	}

	// 检查Premium周期过期
	premiumCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRolePremium)
	if err != nil {
		return err
	}

	for _, cycle := range premiumCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
			s.logger.Info("过期Premium周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("expired_at", *cycle.PeriodEnd))

			if err := s.cycleSvc.Transition(ctx, cycle.ID, model.CycleStateActive, model.CycleStateExpired); err != nil {
				return err
			}
		}
	}

	// 检查Free周期过期
	freeCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRoleFree)
	if err != nil {
		return err
	}

	for _, cycle := range freeCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
			s.logger.Info("过期Free周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("expired_at", *cycle.PeriodEnd))

			if err := s.cycleSvc.Transition(ctx, cycle.ID, model.CycleStateActive, model.CycleStateExpired); err != nil {
				return err
			}
		}
	}

	// 检查ProMini周期过期
	proMiniCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRoleProMini)
	if err != nil {
		return err
	}

	for _, cycle := range proMiniCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
			s.logger.Info("过期ProMini周期",
				zap.Int("user_id", userID),
				zap.Int64("cycle_id", cycle.ID),
				zap.Int64("expired_at", *cycle.PeriodEnd))

			if err := s.cycleSvc.Transition(ctx, cycle.ID, model.CycleStateActive, model.CycleStateExpired); err != nil {
				return err
			}
		}
	}

	return nil
}
