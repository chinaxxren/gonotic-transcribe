package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/pkg/database"
	"github.com/chinaxxren/gonotic/internal/repository"
)

// AccountStateService 对外提供用户账本的读写能力。
type AccountStateService interface {
	// GetSnapshot 获取用户账本快照，若不存在则自动初始化。
	GetSnapshot(ctx context.Context, userID int) (*model.UserAccountState, error)

	// GetCycleSnapshot 读取基于 user_cycle_states 聚合的账本快照。
	GetCycleSnapshot(ctx context.Context, userID int) (*AccountCycleSnapshot, error)

	// UpdateWithTx 以事务方式更新账本，调用方提供变更函数。
	UpdateWithTx(ctx context.Context, userID int, fn func(context.Context, *model.UserAccountState) error) (*model.UserAccountState, error)

	// ResetFreeCycle 将免费周期重置到给定时间点，用于每月发放。
	ResetFreeCycle(ctx context.Context, userID int, cycleStart int64) (*model.UserAccountState, error)

	// RecordPaygPurchase 记录新的 Payg 购买并返回最新账本。
	RecordPaygPurchase(ctx context.Context, userID int, seconds int, expiresAt int64, txnID int64) (*model.UserAccountState, error)

	// RecordPaygPurchaseWithGrantTime 记录新的 Payg 购买，支持指定发放时间和 tier。
	RecordPaygPurchaseWithGrantTime(ctx context.Context, userID int, seconds int, expiresAt int64, txnID int64, grantAt int64, tier model.AccountRole) (*model.UserAccountState, error)

	// ConsumeSummaryQuota 扣减摘要额度次数并写入摘要审计。
	ConsumeSummaryQuota(ctx context.Context, userID int, usedDelta int, businessID int, templateID *int64, source string) (*model.UserAccountState, error)

	// ConsumeBalances 根据周期到期时间扣减秒数，返回扣减明细。
	ConsumeBalances(ctx context.Context, userID int, seconds int) (*BalanceConsumeDetail, error)

	// ConsumeBalancesWithAudit 扣费并记录审计日志，用于可追溯的消费场景。
	ConsumeBalancesWithAudit(ctx context.Context, userID int, seconds int, businessID int, source string) (*BalanceConsumeDetail, error)

	// CancelPaygCycles 精确取消指定的 PAYG 周期（例如退款或主动退订）。
	// 注意：此方法应按具体的交易ID/周期ID精确调用，不建议用于"清空所有PAYG"的批量操作。
	// 参数 cycleIDs 应该对应具体需要退款的交易，而不是用户的所有PAYG周期。
	CancelPaygCycles(ctx context.Context, userID int, cycleIDs []int64, to model.CycleState) (*model.UserAccountState, error)

	// CancelSpecialOfferCycles 精确取消指定的 Special Offer 周期（例如退款或主动撤销）。
	// 注意：Special Offer 与 PAYG 一样属于可消费型权益，但 tier 不同，需单独校验与取消。
	CancelSpecialOfferCycles(ctx context.Context, userID int, cycleIDs []int64, to model.CycleState) (*model.UserAccountState, error)

	// StartPremiumSubscription 启动 Premium 订阅周期，保留已有 PAYG 周期。
	StartPremiumSubscription(ctx context.Context, userID int, periodStart int64, initialSeconds int, subscriptionID int64) (*model.UserAccountState, error)

	// StartProSubscription 启动 Pro 订阅年度额度，保留已有 Premium/PAYG 周期。
	StartProSubscription(ctx context.Context, userID int, periodStart int64, totalSeconds int, subscriptionID int64) (*model.UserAccountState, error)

	// StartProMiniSubscription 启动 Pro Mini 订阅年度额度，保留已有 Premium/PAYG 周期。
	StartProMiniSubscription(ctx context.Context, userID int, periodStart int64, totalSeconds int, subscriptionID int64) (*model.UserAccountState, error)

	// CancelProSubscription 取消 Pro 订阅，按优先级降级用户角色。
	CancelProSubscription(ctx context.Context, userID int, subscriptionID int64, cancelReason model.CycleState) (*model.UserAccountState, error)

	// CancelPremiumSubscription 取消Premium订阅，恢复PAYG余额或降级为Free
	CancelPremiumSubscription(ctx context.Context, userID int, subscriptionID int64, cancelReason model.CycleState) (*model.UserAccountState, error)

	// GrantSubscriptionCycle 统一的订阅周期发放逻辑，供LifecycleWorker和Scheduler共用
	GrantSubscriptionCycle(ctx context.Context, sub *model.Subscription, grantAt int64, billing BillingParams) error
}

func (s *accountStateService) listSummaryEligibleCompletedCycles(ctx context.Context, userID int, existing map[int64]struct{}) ([]*model.UserCycleState, error) {
	now := time.Now().Unix()
	cycles, err := s.cycleSvc.ListEffectiveByUserAndStates(ctx, userID, []model.CycleState{model.CycleStateActive, model.CycleStateCompleted}, now)
	if err != nil {
		return nil, err
	}

	var eligible []*model.UserCycleState
	for _, cycle := range cycles {
		if cycle == nil || cycle.State != model.CycleStateCompleted {
			continue
		}
		if cycle.SummaryTotal <= cycle.SummaryUsed {
			continue
		}
		if existing != nil {
			if _, ok := existing[cycle.ID]; ok {
				continue
			}
			existing[cycle.ID] = struct{}{}
		}
		eligible = append(eligible, cycle)
	}
	return eligible, nil
}

var ErrPremiumCycleLimit = errors.New("premium subscription cycle limit reached")

// CancelProSubscription 取消 Pro 订阅并根据存量周期重新确定角色。
func (s *accountStateService) CancelProSubscription(ctx context.Context, userID int, subscriptionID int64, cancelReason model.CycleState) (*model.UserAccountState, error) {
	if cancelReason != model.CycleStateRefunded && cancelReason != model.CycleStateCancelled && cancelReason != model.CycleStateExpired {
		return nil, fmt.Errorf("invalid cancel reason: %s, must be REFUNDED or CANCELLED", cancelReason)
	}

	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		proCycles, err := s.cycleSvc.ListActiveByTier(txCtx, userID, model.AccountRolePro)
		if err != nil {
			return err
		}

		proMiniCycles, err := s.cycleSvc.ListActiveByTier(txCtx, userID, model.AccountRoleProMini)
		if err != nil {
			return err
		}

		var targetPro []*model.UserCycleState
		for _, cycle := range proCycles {
			if cycle.OriginType == model.CycleOriginSubscription && cycle.OriginID != nil && *cycle.OriginID == subscriptionID {
				targetPro = append(targetPro, cycle)
			}
		}

		for _, cycle := range proMiniCycles {
			if cycle.OriginType == model.CycleOriginSubscription && cycle.OriginID != nil && *cycle.OriginID == subscriptionID {
				targetPro = append(targetPro, cycle)
			}
		}

		if len(targetPro) == 0 {
			s.logger.Info("Pro/ProMini订阅已无ACTIVE周期，可能已处理过取消请求",
				zap.Int("user_id", userID),
				zap.Int64("subscription_id", subscriptionID))
			// 幂等成功：不变更周期，但仍刷新账本快照，确保 ProTotal 等字段正确。
			_, err := s.refreshStateFromCycles(txCtx, userID, state)
			return err
		}

		for _, cycle := range targetPro {
			if err := s.cycleSvc.Transition(txCtx, cycle.ID, model.CycleStateActive, cancelReason); err != nil {
				return err
			}
		}

		// 只有在找到并处理了周期后才清空状态
		state.ProTotal = 0
		state.ProExpireAt = nil

		agg, err := s.refreshStateFromCycles(txCtx, userID, state)
		if err != nil {
			return err
		}
		s.applyProDowngradeRole(state, agg)
		if state.Role == model.AccountRoleFree {
			clearFreeQuota(state)
			s.ensurePaidState(state)
		}

		s.logger.Info("Pro/ProMini订阅取消完成",
			zap.Int("user_id", userID),
			zap.Int64("subscription_id", subscriptionID),
			zap.String("cancel_reason", string(cancelReason)),
			zap.String("new_role", string(state.Role)))

		return nil
	})
}

func (s *accountStateService) CancelSpecialOfferCycles(ctx context.Context, userID int, cycleIDs []int64, to model.CycleState) (*model.UserAccountState, error) {
	if to != model.CycleStateRefunded && to != model.CycleStateCancelled {
		return nil, fmt.Errorf("invalid target state: %s, must be REFUNDED or CANCELLED", to)
	}

	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		if len(cycleIDs) == 0 {
			s.logger.Info("没有指定要取消的SPECIAL_OFFER周期", zap.Int("user_id", userID))
			return nil
		}

		// 验证所有周期ID都是有效的ACTIVE SPECIAL_OFFER周期
		activeCycles, err := s.cycleSvc.ListActiveByTier(txCtx, userID, model.AccountRoleSpecialOffer)
		if err != nil {
			return err
		}

		activeCycleMap := make(map[int64]*model.UserCycleState)
		for _, cycle := range activeCycles {
			activeCycleMap[cycle.ID] = cycle
		}

		var validCycles []*model.UserCycleState
		for _, id := range cycleIDs {
			if id == 0 {
				return fmt.Errorf("invalid cycle id: %d", id)
			}
			if cycle, exists := activeCycleMap[id]; exists {
				validCycles = append(validCycles, cycle)
			} else {
				s.logger.Warn("尝试取消不存在或非ACTIVE的SPECIAL_OFFER周期",
					zap.Int("user_id", userID),
					zap.Int64("cycle_id", id))
				return fmt.Errorf("cycle %d is not an active SPECIAL_OFFER cycle for user %d", id, userID)
			}
		}

		// 执行取消操作
		var cancelledSeconds int
		for _, cycle := range validCycles {
			if err := s.cycleSvc.Transition(txCtx, cycle.ID, model.CycleStateActive, to); err != nil {
				return err
			}
			remaining := cycle.TotalSeconds - cycle.UsedSeconds
			if remaining > 0 {
				cancelledSeconds += remaining
			}
		}

		// 刷新账本状态
		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}

		// 如果降级为Free，确保不发放免费时长
		if state.Role == model.AccountRoleFree {
			clearFreeQuota(state)
			s.ensurePaidState(state)
		}

		s.logger.Info("SPECIAL_OFFER周期取消完成",
			zap.Int("user_id", userID),
			zap.Int("cancelled_cycles", len(validCycles)),
			zap.Int("cancelled_seconds", cancelledSeconds),
			zap.String("cancel_reason", string(to)),
			zap.String("new_role", string(state.Role)))

		return nil
	})
}

func (s *accountStateService) StartProMiniSubscription(ctx context.Context, userID int, periodStart int64, totalSeconds int, subscriptionID int64) (*model.UserAccountState, error) {
	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		// 确保标记为付费用户（清理免费额度）
		s.ensurePaidState(state)
		if err := s.cycleSvc.TransitionActiveByTier(txCtx, userID, model.AccountRoleFree, model.CycleStateCompleted); err != nil {
			return err
		}
		clearFreeQuota(state)
		// Pro Mini 使用独立的角色
		state.Role = model.AccountRoleProMini

		existingProMiniCycles, err := s.cycleSvc.ListActiveByTier(txCtx, userID, model.AccountRoleProMini)
		if err != nil {
			return err
		}

		state.PremiumBacklog = 0

		expireAt := periodStart + s.billing.ProMiniCycleSeconds
		grant := CycleGrantRequest{
			UserID:       userID,
			Tier:         model.AccountRoleProMini,
			OriginType:   model.CycleOriginSubscription,
			OriginID:     pointerInt64(subscriptionID),
			CycleNo:      len(existingProMiniCycles) + 1,
			PeriodStart:  pointerInt64(periodStart),
			PeriodEnd:    pointerInt64(expireAt),
			TotalSeconds: totalSeconds,
			SummaryTotal: s.billing.SummaryProMiniQuota,
			InitialState: model.CycleStateActive,
		}
		if _, err := s.cycleSvc.CreateCycle(txCtx, grant); err != nil {
			return err
		}

		state.PremiumBacklog = 0
		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}

		// P0 修复: 恢复用户角色同步，使用 txCtx 确保在同一事务中执行
		if s.userRepo != nil {
			// 转换 AccountRole 到 UserRole
			var targetRole model.UserRole
			switch state.Role {
			case model.AccountRoleFree:
				targetRole = model.RoleFree
			case model.AccountRolePayg, model.AccountRoleSpecialOffer, model.AccountRolePremium, model.AccountRolePro, model.AccountRoleProMini:
				targetRole = model.RoleVip
			default:
				targetRole = model.RoleFree
			}

			if err := s.userRepo.UpgradeUserRole(txCtx, userID, targetRole); err != nil {
				// 如果角色相同或已经是VIP，不算错误
				if !strings.Contains(err.Error(), "cannot downgrade") && !strings.Contains(err.Error(), "already VIP") {
					s.logger.Warn("Failed to sync user role to users table",
						zap.Error(err),
						zap.Int("user_id", userID),
						zap.String("account_role", string(state.Role)),
						zap.String("user_role", string(targetRole)))
				}
			}
		}

		return nil
	})
}

func (s *accountStateService) ConsumeSummaryQuota(ctx context.Context, userID int, usedDelta int, businessID int, templateID *int64, source string) (*model.UserAccountState, error) {
	if userID == 0 {
		return nil, fmt.Errorf("user_id is required")
	}
	if usedDelta <= 0 {
		return s.GetSnapshot(ctx, userID)
	}
	if businessID == 0 {
		businessID = model.BusinessIDSummary
	}
	if strings.TrimSpace(source) == "" {
		source = "summary"
	}

	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}

		cycles, err := s.cycleSvc.ListConsumable(txCtx, userID)
		if err != nil {
			return fmt.Errorf("list consumable cycles failed: %w", err)
		}

		cycleMap := make(map[int64]struct{}, len(cycles))
		for _, c := range cycles {
			if c != nil {
				cycleMap[c.ID] = struct{}{}
			}
		}

		if extra, err := s.listSummaryEligibleCompletedCycles(txCtx, userID, cycleMap); err == nil && len(extra) > 0 {
			cycles = append(cycles, extra...)
		} else if err != nil {
			return err
		}

		if len(cycles) == 0 {
			return fmt.Errorf("insufficient summary balance, need %d more", usedDelta)
		}

		sort.Slice(cycles, func(i, j int) bool {
			iExp := getExpirationTime(cycles[i])
			jExp := getExpirationTime(cycles[j])
			if iExp == jExp {
				if cycles[i].CreatedAt == cycles[j].CreatedAt {
					return cycles[i].ID < cycles[j].ID
				}
				return cycles[i].CreatedAt < cycles[j].CreatedAt
			}
			return iExp < jExp
		})

		type summaryDeduction struct {
			cycleID int64
			used    int
		}
		var deductions []summaryDeduction
		remaining := usedDelta
		for _, cycle := range cycles {
			if remaining <= 0 {
				break
			}
			available := cycle.SummaryTotal - cycle.SummaryUsed
			if available <= 0 {
				continue
			}
			consume := minInt(available, remaining)
			if consume <= 0 {
				continue
			}
			if err := s.cycleSvc.IncrementSummaryUsage(txCtx, cycle.ID, consume); err != nil {
				if strings.Contains(err.Error(), "would exceed cycle capacity") {
					continue
				}
				return err
			}
			deductions = append(deductions, summaryDeduction{cycleID: cycle.ID, used: consume})
			remaining -= consume
		}
		if remaining > 0 {
			return fmt.Errorf("insufficient summary balance, need %d more", remaining)
		}

		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}

		if s.summaryLedgerRepo != nil && len(deductions) > 0 {
			for _, d := range deductions {
				entry := &model.SummaryLedger{
					UserID:       userID,
					BusinessID:   businessID,
					CycleID:      d.cycleID,
					TemplateID:   templateID,
					SummaryDelta: d.used,
					Source:       source,
				}
				if err := s.summaryLedgerRepo.Create(txCtx, entry); err != nil {
					return fmt.Errorf("create summary ledger entry failed: %w", err)
				}
			}
		}
		return nil
	})
}

func (s *accountStateService) applyProDowngradeRole(state *model.UserAccountState, agg *AccountCycleAggregate) {
	if agg == nil {
		state.Role = model.AccountRoleFree
		return
	}
	if agg.Pro != nil {
		state.Role = model.AccountRolePro
		return
	}
	if agg.ProMini != nil {
		state.Role = model.AccountRoleProMini
		return
	}
	if agg.Premium != nil {
		state.Role = model.AccountRolePremium
		return
	}
	if agg.Payg != nil {
		state.Role = model.AccountRolePayg
		return
	}
	if agg.SpecialOffer != nil {
		state.Role = model.AccountRoleSpecialOffer
		return
	}
	state.Role = model.AccountRoleFree
}

func (s *accountStateService) applyPremiumDowngradeRole(state *model.UserAccountState, agg *AccountCycleAggregate) {
	if agg == nil {
		state.Role = model.AccountRoleFree
		return
	}
	if agg.Pro != nil {
		state.Role = model.AccountRolePro
		return
	}
	if agg.ProMini != nil {
		state.Role = model.AccountRoleProMini
		return
	}
	if agg.Premium != nil {
		state.Role = model.AccountRolePremium
		return
	}
	if agg.Payg != nil {
		state.Role = model.AccountRolePayg
		return
	}
	if agg.SpecialOffer != nil {
		state.Role = model.AccountRoleSpecialOffer
		return
	}
	state.Role = model.AccountRoleFree
}

func aggregateEffectiveCycleQuota(cycles []*model.UserCycleState) *CycleQuota {
	return aggregateCycleQuota(cycles)
}

type accountStateService struct {
	repo              repository.AccountStateRepository
	txRunner          transactionRunner
	logger            *zap.Logger
	billing           BillingParams
	cycleSvc          CycleStateService
	cycleConsumption  CycleConsumptionService
	usageLedger       repository.UsageLedgerRepository
	summaryLedgerRepo repository.SummaryLedgerRepository
	subscriptionRepo  repository.SubscriptionRepository
	userRepo          repository.UserRepository
	cycleExpiration   CycleExpirationService
}

type transactionRunner interface {
	WithTransaction(ctx context.Context, fn func(*sqlx.Tx) error) error
}

type noopTxRunner struct{}

func (noopTxRunner) WithTransaction(ctx context.Context, fn func(*sqlx.Tx) error) error {
	return fn(nil)
}

// NewAccountStateService 创建账本服务实例。
func NewAccountStateService(
	db *database.DB,
	repo repository.AccountStateRepository,
	cycleSvc CycleStateService,
	usageLedger repository.UsageLedgerRepository,
	summaryLedger repository.SummaryLedgerRepository,
	subscriptionRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	billing BillingParams,
	logger *zap.Logger,
) AccountStateService {
	return newAccountStateService(db, repo, cycleSvc, usageLedger, summaryLedger, subscriptionRepo, userRepo, billing, logger, nil, nil)
}

// NewAccountStateServiceWithExpiration 创建账本服务实例，支持可选的过期服务
func NewAccountStateServiceWithExpiration(
	db *database.DB,
	repo repository.AccountStateRepository,
	cycleSvc CycleStateService,
	usageLedger repository.UsageLedgerRepository,
	summaryLedger repository.SummaryLedgerRepository,
	subscriptionRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	billing BillingParams,
	logger *zap.Logger,
	cycleExpiration CycleExpirationService,
) AccountStateService {
	return newAccountStateService(db, repo, cycleSvc, usageLedger, summaryLedger, subscriptionRepo, userRepo, billing, logger, cycleExpiration, nil)
}

func NewAccountStateServiceWithTransactionRepo(
	db *database.DB,
	repo repository.AccountStateRepository,
	cycleSvc CycleStateService,
	usageLedger repository.UsageLedgerRepository,
	summaryLedger repository.SummaryLedgerRepository,
	subscriptionRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	transactionRepo repository.TransactionRepository,
	billing BillingParams,
	logger *zap.Logger,
) AccountStateService {
	return newAccountStateService(db, repo, cycleSvc, usageLedger, summaryLedger, subscriptionRepo, userRepo, billing, logger, nil, transactionRepo)
}

func NewAccountStateServiceWithExpirationAndTransactionRepo(
	db *database.DB,
	repo repository.AccountStateRepository,
	cycleSvc CycleStateService,
	usageLedger repository.UsageLedgerRepository,
	summaryLedger repository.SummaryLedgerRepository,
	subscriptionRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	transactionRepo repository.TransactionRepository,
	billing BillingParams,
	logger *zap.Logger,
	cycleExpiration CycleExpirationService,
) AccountStateService {
	return newAccountStateService(db, repo, cycleSvc, usageLedger, summaryLedger, subscriptionRepo, userRepo, billing, logger, cycleExpiration, transactionRepo)
}

func newAccountStateService(
	db *database.DB,
	repo repository.AccountStateRepository,
	cycleSvc CycleStateService,
	usageLedger repository.UsageLedgerRepository,
	summaryLedger repository.SummaryLedgerRepository,
	subscriptionRepo repository.SubscriptionRepository,
	userRepo repository.UserRepository,
	billing BillingParams,
	logger *zap.Logger,
	cycleExpiration CycleExpirationService,
	transactionRepo repository.TransactionRepository,
) AccountStateService {
	if logger == nil {
		logger = zap.NewNop()
	}
	billing = billing.WithDefaults()
	var runner transactionRunner
	if db != nil {
		runner = db
	} else {
		runner = noopTxRunner{}
	}

	cycleConsumption := NewCycleConsumptionServiceWithTransactionRepo(cycleSvc, transactionRepo, logger)
	if usageLedger != nil {
		cycleConsumption = NewCycleConsumptionServiceWithAuditAndTransactionRepo(cycleSvc, usageLedger, transactionRepo, logger)
	}

	return &accountStateService{
		repo:              repo,
		txRunner:          runner,
		logger:            logger,
		billing:           billing,
		cycleSvc:          cycleSvc,
		cycleConsumption:  cycleConsumption,
		usageLedger:       usageLedger,
		summaryLedgerRepo: summaryLedger,
		subscriptionRepo:  subscriptionRepo,
		userRepo:          userRepo,
		cycleExpiration:   cycleExpiration,
	}
}

func (s *accountStateService) GetSnapshot(ctx context.Context, userID int) (*model.UserAccountState, error) {
	// 基于周期数据构建账户状态快照
	snapshot, err := s.GetCycleSnapshot(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 将周期快照转换为传统的UserAccountState格式
	return snapshotToLegacyState(snapshot), nil
}

func (s *accountStateService) GetCycleSnapshot(ctx context.Context, userID int) (*AccountCycleSnapshot, error) {
	// 在获取快照前检查并处理过期的周期
	if err := s.checkAndExpireCycles(ctx, userID); err != nil {
		s.logger.Error("检查周期过期失败", zap.Int("user_id", userID), zap.Error(err))
		// 不阻断获取快照的流程，只记录警告
	}

	buckets, err := s.loadCycleBuckets(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 创建一个基础状态用于聚合构建
	baseState := s.newDefaultState(userID)

	// 检查用户是否曾经付费
	if s.userRepo != nil {
		hasPaid, err := s.userRepo.HasPaidBefore(ctx, userID)
		if err != nil {
			s.logger.Error("检查用户付费历史失败", zap.Int("user_id", userID), zap.Error(err))
		} else {
			baseState.HasEverPaid = hasPaid
		}
	}
	if baseState.HasEverPaid {
		clearFreeQuota(baseState)
	}

	agg, err := s.buildCycleAggregate(ctx, userID, buckets, baseState)
	if err != nil {
		return nil, err
	}

	// 角色回退：当当前没有任何有效付费周期时（聚合为 Free），但用户存在历史购买记录，
	// 使用最近一次结束的付费周期作为展示角色，便于前端展示用户最近的购买类型。
	if agg != nil && agg.Role == model.AccountRoleFree {
		latest, err := s.cycleSvc.FindLatestEndedPaidCycle(ctx, userID)
		if err != nil {
			return nil, err
		}
		if latest != nil {
			agg.Role = latest.Tier
			agg.HasEverPaid = true
			agg.Free = nil
		}
	}

	return aggregateToSnapshot(agg), nil
}

func (s *accountStateService) UpdateWithTx(ctx context.Context, userID int, fn func(context.Context, *model.UserAccountState) error) (*model.UserAccountState, error) {
	if _, ok := database.GetTx(ctx); ok {
		return s.updateState(ctx, userID, fn)
	}

	var latest *model.UserAccountState
	err := s.txRunner.WithTransaction(ctx, func(tx *sqlx.Tx) error {
		txCtx := database.ContextWithTx(ctx, tx)
		var err error
		latest, err = s.updateState(txCtx, userID, fn)
		return err
	})
	if err != nil {
		return nil, err
	}
	return latest, nil
}

func (s *accountStateService) updateState(ctx context.Context, userID int, fn func(context.Context, *model.UserAccountState) error) (*model.UserAccountState, error) {
	// 基于周期数据获取当前状态
	snapshot, err := s.GetCycleSnapshot(ctx, userID)
	if err != nil {
		return nil, err
	}

	state := snapshotToLegacyState(snapshot)
	if state == nil {
		state = s.newDefaultState(userID)
	}

	if fn != nil {
		if err := fn(ctx, state); err != nil {
			return nil, err
		}
	}

	state.UpdatedAt = time.Now().Unix()

	// 注意：这里我们不再保存到user_account_states表
	// 状态的持久化通过周期操作来实现
	// 这个方法主要用于状态计算和临时修改

	return state, nil
}

// ResetFreeCycle 重置免费的周期窗口，常用于每月发放 30 分钟。
// 注意：降级用户（曾经付费的用户）不会获得免费时长。
func (s *accountStateService) ResetFreeCycle(ctx context.Context, userID int, cycleStart int64) (*model.UserAccountState, error) {
	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		clearFreeQuota(state)

		// 检查用户是否曾经付费，如果是降级用户则不发放免费时长
		hasPaidBefore := state.HasEverPaid
		if !hasPaidBefore && s.userRepo != nil {
			paidBefore, err := s.userRepo.HasPaidBefore(txCtx, userID)
			if err != nil {
				return err
			}
			hasPaidBefore = paidBefore
			if paidBefore {
				state.HasEverPaid = true
			}
		}

		cycleEnd := cycleStart + s.billing.FreeCycleSeconds
		state.FreeCycleStart = pointerInt64(cycleStart)
		state.FreeCycleEnd = pointerInt64(cycleEnd)
		state.Role = model.AccountRoleFree

		// 降级用户不获得免费时长
		if hasPaidBefore {
			state.FreeTotal = 0
			state.FreeUsed = 0
			s.logger.Info("降级用户不发放免费时长",
				zap.Int("user_id", userID),
				zap.Bool("has_ever_paid", hasPaidBefore))
			_, err := s.refreshStateFromCycles(txCtx, userID, state)
			return err
		}

		// 只有从未付费的用户才获得免费时长
		state.FreeTotal = s.billing.FreeAllowanceSeconds
		state.FreeUsed = 0

		// FREE 只维护一条 SYSTEM 周期：先将所有 active free 标记完成，再 reset 该 SYSTEM/FREE 周期到新窗口。
		if err := s.cycleSvc.TransitionActiveByTier(txCtx, userID, model.AccountRoleFree, model.CycleStateCompleted); err != nil {
			return err
		}

		freeCycle, err := s.cycleSvc.FindSystemFreeCycle(txCtx, userID)
		if err != nil {
			return err
		}
		if freeCycle != nil {
			if err := s.cycleSvc.ResetSystemFreeCycle(txCtx, freeCycle.ID, cycleStart, cycleEnd, s.billing.FreeAllowanceSeconds, s.billing.SummaryFreeQuota); err != nil {
				return err
			}
		} else {
			grant := CycleGrantRequest{
				UserID:       userID,
				Tier:         model.AccountRoleFree,
				OriginType:   model.CycleOriginSystem,
				CycleNo:      1,
				PeriodStart:  pointerInt64(cycleStart),
				PeriodEnd:    pointerInt64(cycleEnd),
				TotalSeconds: s.billing.FreeAllowanceSeconds,
				SummaryTotal: s.billing.SummaryFreeQuota,
				InitialState: model.CycleStateActive,
			}
			if _, err := s.cycleSvc.CreateCycle(txCtx, grant); err != nil {
				return err
			}
		}

		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}
		// 由于统计查询会按当前时间过滤有效期，历史/指定窗口可能不会进入聚合。
		// ResetFreeCycle 的语义是“重置到指定窗口”，因此这里强制写回本次窗口信息。
		state.FreeCycleStart = pointerInt64(cycleStart)
		state.FreeCycleEnd = pointerInt64(cycleEnd)
		state.FreeTotal = s.billing.FreeAllowanceSeconds
		state.FreeUsed = 0
		return nil
	})
}

// RecordPaygPurchase 将新的 Payg 时长包写入账本，同时记录明细。
func (s *accountStateService) RecordPaygPurchase(ctx context.Context, userID int, seconds int, expiresAt int64, txnID int64) (*model.UserAccountState, error) {
	return s.RecordPaygPurchaseWithGrantTime(ctx, userID, seconds, expiresAt, txnID, time.Now().Unix(), model.AccountRolePayg)
}

// RecordPaygPurchaseWithGrantTime 将新的 Payg 时长包写入账本，支持指定发放时间和 tier。
func (s *accountStateService) RecordPaygPurchaseWithGrantTime(ctx context.Context, userID int, seconds int, expiresAt int64, txnID int64, grantAt int64, tier model.AccountRole) (*model.UserAccountState, error) {
	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		// 付费购买时需要标记用户已付费，停发免费额度
		s.ensurePaidState(state)
		if err := s.cycleSvc.TransitionActiveByTier(txCtx, userID, model.AccountRoleFree, model.CycleStateCompleted); err != nil {
			return err
		}
		clearFreeQuota(state)
		// 非 Premium/Pro/ProMini 用户才需要切换为指定 tier；否则保持当前角色
		if state.Role != model.AccountRolePremium && state.Role != model.AccountRolePro && state.Role != model.AccountRoleProMini {
			state.Role = tier
		}

		paygCycles, err := s.cycleSvc.ListActiveByTier(txCtx, userID, tier)
		if err != nil {
			return err
		}
		req := CycleGrantRequest{
			UserID:       userID,
			Tier:         tier,
			OriginType:   model.CycleOriginTransaction,
			OriginID:     pointerInt64(txnID),
			CycleNo:      len(paygCycles) + 1,
			PeriodStart:  pointerInt64(grantAt),
			PeriodEnd:    pointerInt64(expiresAt),
			TotalSeconds: seconds,
			SummaryTotal: func() int {
				switch tier {
				case model.AccountRoleSpecialOffer:
					return s.billing.SummarySpecialOfferQuota
				default:
					return s.billing.SummaryPaygQuota
				}
			}(),
			InitialState: model.CycleStateActive,
		}
		if _, err := s.cycleSvc.CreateCycle(txCtx, req); err != nil {
			return err
		}

		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}
		return nil
	})
}

// ConsumeBalances 按周期到期时间扣减额度。
func (s *accountStateService) ConsumeBalances(ctx context.Context, userID int, seconds int) (*BalanceConsumeDetail, error) {
	if seconds <= 0 {
		return &BalanceConsumeDetail{}, nil
	}

	var resultDetail *BalanceConsumeDetail
	_, err := s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		consumeResult, err := s.cycleConsumption.Consume(txCtx, userID, seconds)
		if err != nil {
			return err
		}
		if consumeResult == nil {
			resultDetail = &BalanceConsumeDetail{}
		} else {
			detailCopy := consumeResult.Detail
			detailCopy.Deductions = append([]CycleDeduction(nil), consumeResult.Deductions...)
			resultDetail = &detailCopy
			state.ProUsed += consumeResult.Detail.FromPro
			state.ProMiniUsed += consumeResult.Detail.FromProMini
			state.PremiumUsed += consumeResult.Detail.FromPremium
			state.PaygUsed += consumeResult.Detail.FromPayg
			state.SpecialOfferUsed += consumeResult.Detail.FromSpecialOffer
			state.FreeUsed += consumeResult.Detail.FromFree
		}

		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}
		if state.Role == model.AccountRoleFree {
			clearFreeQuota(state)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resultDetail, nil
}

// ConsumeBalancesWithAudit 扣费并记录审计日志，用于可追溯的消费场景。
func (s *accountStateService) ConsumeBalancesWithAudit(ctx context.Context, userID int, seconds int, businessID int, source string) (*BalanceConsumeDetail, error) {
	if seconds <= 0 {
		return &BalanceConsumeDetail{}, nil
	}

	var resultDetail *BalanceConsumeDetail
	_, err := s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		// 使用支持审计的扣费方法
		consumeResult, err := s.cycleConsumption.ConsumeWithAudit(txCtx, userID, seconds, businessID, source)
		if err != nil {
			return err
		}
		if consumeResult == nil {
			resultDetail = &BalanceConsumeDetail{}
		} else {
			detailCopy := consumeResult.Detail
			detailCopy.Deductions = append([]CycleDeduction(nil), consumeResult.Deductions...)
			resultDetail = &detailCopy
			state.ProUsed += consumeResult.Detail.FromPro
			state.ProMiniUsed += consumeResult.Detail.FromProMini
			state.PremiumUsed += consumeResult.Detail.FromPremium
			state.PaygUsed += consumeResult.Detail.FromPayg
			state.SpecialOfferUsed += consumeResult.Detail.FromSpecialOffer
			state.FreeUsed += consumeResult.Detail.FromFree
		}

		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}
		if state.Role == model.AccountRoleFree {
			clearFreeQuota(state)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resultDetail, nil
}

func (s *accountStateService) newDefaultState(userID int) *model.UserAccountState {
	now := time.Now().Unix()

	return &model.UserAccountState{
		UserID:            userID,
		Role:              model.AccountRoleFree,
		HasEverPaid:       false,
		FreeCycleStart:    nil,
		FreeCycleEnd:      nil,
		FreeTotal:         0,
		FreeUsed:          0,
		PaygTotal:         0,
		PaygUsed:          0,
		SpecialOfferTotal: 0,
		SpecialOfferUsed:  0,
		PremiumCycleIndex: 0,
		PremiumTotal:      0,
		PremiumUsed:       0,
		PremiumBacklog:    0,
		ProTotal:          0,
		ProUsed:           0,
		ProMiniTotal:      0,
		ProMiniUsed:       0,
		UpdatedAt:         now,
	}
}

func (s *accountStateService) CancelPaygCycles(ctx context.Context, userID int, cycleIDs []int64, to model.CycleState) (*model.UserAccountState, error) {
	if to != model.CycleStateRefunded && to != model.CycleStateCancelled {
		return nil, fmt.Errorf("invalid target state: %s, must be REFUNDED or CANCELLED", to)
	}

	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		if len(cycleIDs) == 0 {
			s.logger.Info("没有指定要取消的PAYG周期", zap.Int("user_id", userID))
			return nil
		}

		// 验证所有周期ID都是有效的ACTIVE PAYG周期
		activeCycles, err := s.cycleSvc.ListActiveByTier(txCtx, userID, model.AccountRolePayg)
		if err != nil {
			return err
		}

		activeCycleMap := make(map[int64]*model.UserCycleState)
		for _, cycle := range activeCycles {
			activeCycleMap[cycle.ID] = cycle
		}

		var validCycles []*model.UserCycleState
		for _, id := range cycleIDs {
			if id == 0 {
				return fmt.Errorf("invalid cycle id: %d", id)
			}
			if cycle, exists := activeCycleMap[id]; exists {
				validCycles = append(validCycles, cycle)
			} else {
				s.logger.Warn("尝试取消不存在或非ACTIVE的PAYG周期",
					zap.Int("user_id", userID),
					zap.Int64("cycle_id", id))
				return fmt.Errorf("cycle %d is not an active PAYG cycle for user %d", id, userID)
			}
		}

		// 执行取消操作
		var cancelledSeconds int
		for _, cycle := range validCycles {
			if err := s.cycleSvc.Transition(txCtx, cycle.ID, model.CycleStateActive, to); err != nil {
				return err
			}
			// 计算被取消的有效时长
			remaining := cycle.TotalSeconds - cycle.UsedSeconds
			if remaining > 0 {
				cancelledSeconds += remaining
			}
		}

		// 刷新账本状态
		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}

		// 如果降级为Free，确保不发放免费时长
		if state.Role == model.AccountRoleFree {
			clearFreeQuota(state)
			s.ensurePaidState(state)
		}

		s.logger.Info("PAYG周期取消完成",
			zap.Int("user_id", userID),
			zap.Int("cancelled_cycles", len(validCycles)),
			zap.Int("cancelled_seconds", cancelledSeconds),
			zap.String("cancel_reason", string(to)),
			zap.String("new_role", string(state.Role)))

		return nil
	})
}

// StartPremiumSubscription 启动 Premium 订阅周期，保留已有 PAYG 周期。
func (s *accountStateService) StartPremiumSubscription(ctx context.Context, userID int, periodStart int64, initialSeconds int, subscriptionID int64) (*model.UserAccountState, error) {
	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		// 确保标记为已付费用户，并清理任何免费额度
		s.ensurePaidState(state)
		if err := s.cycleSvc.TransitionActiveByTier(txCtx, userID, model.AccountRoleFree, model.CycleStateCompleted); err != nil {
			return err
		}
		clearFreeQuota(state)
		// Premium 订阅启动后，账户角色切换为 Premium
		state.Role = model.AccountRolePremium

		sub, err := s.subscriptionRepo.GetByID(txCtx, subscriptionID)
		if err != nil {
			return err
		}
		if sub == nil {
			return fmt.Errorf("subscription %d not found", subscriptionID)
		}

		// 如果仓库未携带过期时间，尝试刷新以兼容旧测试数据
		// 段代码不是简单重复。第一次用 txCtx 读取订阅是事务上下文；
		// 但如果旧数据里没有 expires_at，就再用顶层 ctx 读一次，
		// 兼容那些还没写入过期时间的测试数据，避免返回 nil 而导致校验失败。因此两次
		// GetByID 的语义不同，一个是正常读取，一个是兜底刷新。
		if sub.ExpiresAt == nil {
			if refreshed, refreshErr := s.subscriptionRepo.GetByID(ctx, subscriptionID); refreshErr == nil && refreshed != nil {
				sub = refreshed
			}
		}

		premiumCycleSeconds := s.billing.PremiumCycleSeconds
		if premiumCycleSeconds <= 0 {
			premiumCycleSeconds = int64(model.PremiumCycleDurationSeconds)
		}

		if sub.ExpiresAt != nil && periodStart >= *sub.ExpiresAt {
			s.logger.Info("Premium subscription expired before grant",
				zap.Int("user_id", userID),
				zap.Int64("subscription_id", subscriptionID),
				zap.Int64("period_start", periodStart),
				zap.Int64("expires_at", *sub.ExpiresAt))
			return ErrPremiumCycleLimit
		}

		allPremiumCycles, err := s.cycleSvc.ListActiveByPremium(txCtx, userID, subscriptionID, []model.CycleState{model.CycleStateActive, model.CycleStateCompleted, model.CycleStateExpired})
		if err != nil {
			return err
		}

		var maxCycles int
		if sub.ExpiresAt != nil {
			windowSeconds := float64(*sub.ExpiresAt - sub.CreatedAt)
			if windowSeconds <= 0 {
				s.logger.Info("Premium subscription has non-positive validity window",
					zap.Int("user_id", userID),
					zap.Int64("subscription_id", subscriptionID),
					zap.Int64("expires_at", *sub.ExpiresAt),
					zap.Int64("created_at", sub.CreatedAt))
				return ErrPremiumCycleLimit
			}

			// 在开发环境中，使用标准的30天周期来计算最大周期数，避免因短周期产生过多周期
			cycleSecondsForCalculation := premiumCycleSeconds
			if premiumCycleSeconds < 86400 { // 小于1天视为开发环境
				cycleSecondsForCalculation = 30 * 24 * 3600 // 使用标准30天
				s.logger.Info("开发环境检测：使用标准30天周期计算最大周期数",
					zap.Int("user_id", userID),
					zap.Int64("subscription_id", subscriptionID),
					zap.Int64("actual_cycle_seconds", premiumCycleSeconds),
					zap.Int64("calculation_cycle_seconds", cycleSecondsForCalculation))
			}

			maxCycles = int(math.Ceil(windowSeconds / float64(cycleSecondsForCalculation)))
			if maxCycles <= 0 {
				maxCycles = 12
			}
		} else {
			// 如果没有expires_at，使用默认的12周期限制作为安全保护
			maxCycles = 12
			s.logger.Warn("Premium subscription missing expires_at, using default cycle limit",
				zap.Int("user_id", userID),
				zap.Int64("subscription_id", subscriptionID),
				zap.Int("default_max_cycles", maxCycles))
		}

		if len(allPremiumCycles) >= maxCycles {
			s.logger.Info("Premium subscription reached cycle limit",
				zap.Int("user_id", userID),
				zap.Int64("subscription_id", subscriptionID),
				zap.Int("existing_cycles", len(allPremiumCycles)),
				zap.Int("max_cycles", maxCycles),
				zap.Bool("has_expires_at", sub.ExpiresAt != nil))
			return ErrPremiumCycleLimit
		}

		cycleEnd := periodStart + premiumCycleSeconds
		grant := CycleGrantRequest{
			UserID:       userID,
			Tier:         model.AccountRolePremium,
			OriginType:   model.CycleOriginSubscription,
			OriginID:     pointerInt64(subscriptionID),
			CycleNo:      len(allPremiumCycles) + 1,
			PeriodStart:  pointerInt64(periodStart),
			PeriodEnd:    pointerInt64(cycleEnd),
			TotalSeconds: initialSeconds,
			SummaryTotal: s.billing.SummaryPremiumQuota,
			InitialState: model.CycleStateActive,
		}
		if _, err := s.cycleSvc.CreateCycle(txCtx, grant); err != nil {
			return err
		}

		state.PremiumBacklog = 0
		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}

		// P0 修复: 恢复用户角色同步，使用 txCtx 确保在同一事务中执行
		if s.userRepo != nil {
			// 转换 AccountRole 到 UserRole
			var targetRole model.UserRole
			switch state.Role {
			case model.AccountRoleFree:
				targetRole = model.RoleFree
			case model.AccountRolePayg, model.AccountRoleSpecialOffer, model.AccountRolePremium, model.AccountRolePro, model.AccountRoleProMini:
				targetRole = model.RoleVip
			default:
				targetRole = model.RoleFree
			}

			if err := s.userRepo.UpgradeUserRole(txCtx, userID, targetRole); err != nil {
				// 如果角色相同或已经是VIP，不算错误
				if !strings.Contains(err.Error(), "cannot downgrade") && !strings.Contains(err.Error(), "already VIP") {
					s.logger.Warn("Failed to sync user role to users table",
						zap.Error(err),
						zap.Int("user_id", userID),
						zap.String("account_role", string(state.Role)),
						zap.String("user_role", string(targetRole)))
				}
			}
		}

		return nil
	})
}

// StartProSubscription 启动 Pro 订阅年度额度，保留已有 Premium/PAYG 周期。
func (s *accountStateService) StartProSubscription(ctx context.Context, userID int, periodStart int64, totalSeconds int, subscriptionID int64) (*model.UserAccountState, error) {
	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		// 确保标记为付费用户（清理免费额度）
		s.ensurePaidState(state)
		if err := s.cycleSvc.TransitionActiveByTier(txCtx, userID, model.AccountRoleFree, model.CycleStateCompleted); err != nil {
			return err
		}
		clearFreeQuota(state)
		// Pro 订阅激活，账户角色切换为 Pro
		state.Role = model.AccountRolePro

		existingProCycles, err := s.cycleSvc.ListActiveByTier(txCtx, userID, model.AccountRolePro)
		if err != nil {
			return err
		}

		state.PremiumBacklog = 0

		expireAt := periodStart + s.billing.ProCycleSeconds
		grant := CycleGrantRequest{
			UserID:       userID,
			Tier:         model.AccountRolePro,
			OriginType:   model.CycleOriginSubscription,
			OriginID:     pointerInt64(subscriptionID),
			CycleNo:      len(existingProCycles) + 1,
			PeriodStart:  pointerInt64(periodStart),
			PeriodEnd:    pointerInt64(expireAt),
			TotalSeconds: totalSeconds,
			SummaryTotal: s.billing.SummaryProQuota,
			InitialState: model.CycleStateActive,
		}
		if _, err := s.cycleSvc.CreateCycle(txCtx, grant); err != nil {
			return err
		}

		state.PremiumBacklog = 0
		if _, err := s.refreshStateFromCycles(txCtx, userID, state); err != nil {
			return err
		}

		// P0 修复: 恢复用户角色同步，使用 txCtx 确保在同一事务中执行
		if s.userRepo != nil {
			// 转换 AccountRole 到 UserRole
			var targetRole model.UserRole
			switch state.Role {
			case model.AccountRoleFree:
				targetRole = model.RoleFree
			case model.AccountRolePayg, model.AccountRoleSpecialOffer, model.AccountRolePremium, model.AccountRolePro, model.AccountRoleProMini:
				targetRole = model.RoleVip
			default:
				targetRole = model.RoleFree
			}

			if err := s.userRepo.UpgradeUserRole(txCtx, userID, targetRole); err != nil {
				// 如果角色相同或已经是VIP，不算错误
				if !strings.Contains(err.Error(), "cannot downgrade") && !strings.Contains(err.Error(), "already VIP") {
					s.logger.Warn("Failed to sync user role to users table",
						zap.Error(err),
						zap.Int("user_id", userID),
						zap.String("account_role", string(state.Role)),
						zap.String("user_role", string(targetRole)))
				}
			}
		}

		return nil
	})
}

// CancelPremiumSubscription 取消Premium订阅，统一处理所有相关逻辑

func (s *accountStateService) CancelPremiumSubscription(ctx context.Context, userID int, subscriptionID int64, cancelReason model.CycleState) (*model.UserAccountState, error) {
	if cancelReason != model.CycleStateRefunded && cancelReason != model.CycleStateCancelled && cancelReason != model.CycleStateExpired {
		return nil, fmt.Errorf("invalid cancel reason: %s, must be REFUNDED, CANCELLED, or EXPIRED", cancelReason)
	}

	return s.UpdateWithTx(ctx, userID, func(txCtx context.Context, state *model.UserAccountState) error {
		// 1. 查找要取消的Premium周期（按订阅ID精确过滤）
		premiumCycles, err := s.cycleSvc.ListActiveByPremium(txCtx, userID, subscriptionID, []model.CycleState{model.CycleStateActive})
		if err != nil {
			return err
		}

		if len(premiumCycles) == 0 {
			s.logger.Info("Premium订阅已无ACTIVE周期，可能已处理过取消请求",
				zap.Int("user_id", userID),
				zap.Int64("subscription_id", subscriptionID))
			return nil // 幂等成功
		}

		// 2. 将Premium周期标记为取消/退款
		for _, cycle := range premiumCycles {
			if err := s.cycleSvc.Transition(txCtx, cycle.ID, model.CycleStateActive, cancelReason); err != nil {
				return err
			}
		}

		// 3. 清理Premium相关状态
		state.PremiumTotal = 0
		state.PremiumUsed = 0
		state.PremiumBacklog = 0
		state.PremiumCycleStart = nil
		state.PremiumCycleEnd = nil
		state.PremiumCycleIndex = 0

		// 4. 刷新账本状态，自动确定新的用户角色
		agg, err := s.refreshStateFromCycles(txCtx, userID, state)
		if err != nil {
			return err
		}
		s.applyPremiumDowngradeRole(state, agg)

		// 5. 如果降级为Free，确保不发放免费时长（因为HasEverPaid=true）
		if state.Role == model.AccountRoleFree {
			clearFreeQuota(state)
			s.ensurePaidState(state) // 确保HasEverPaid=true
		}

		s.logger.Info("Premium订阅取消完成",
			zap.Int("user_id", userID),
			zap.Int64("subscription_id", subscriptionID),
			zap.String("cancel_reason", string(cancelReason)),
			zap.String("new_role", string(state.Role)))

		return nil
	})
}

// ensurePaidState 保证 HasEverPaid 标记并清理免费额度。
func (s *accountStateService) ensurePaidState(state *model.UserAccountState) {
	if !state.HasEverPaid {
		state.HasEverPaid = true
		state.FreeTotal = 0
		state.FreeUsed = 0
		state.FreeCycleStart = nil
		state.FreeCycleEnd = nil
	}
}

func (s *accountStateService) loadCycleBuckets(ctx context.Context, userID int) (*cycleBuckets, error) {
	now := time.Now().Unix()
	cycles, err := s.cycleSvc.ListEffectiveByUserAndStates(ctx, userID, []model.CycleState{model.CycleStateActive, model.CycleStateCompleted}, now)
	if err != nil {
		return nil, err
	}

	buckets := &cycleBuckets{}
	for _, cycle := range cycles {
		if cycle == nil {
			continue
		}
		switch cycle.Tier {
		case model.AccountRoleFree:
			buckets.Free = append(buckets.Free, cycle)
		case model.AccountRolePayg:
			buckets.Payg = append(buckets.Payg, cycle)
		case model.AccountRoleSpecialOffer:
			buckets.SpecialOffer = append(buckets.SpecialOffer, cycle)
		case model.AccountRolePremium:
			buckets.Premium = append(buckets.Premium, cycle)
		case model.AccountRolePro:
			buckets.Pro = append(buckets.Pro, cycle)
		case model.AccountRoleProMini:
			buckets.ProMini = append(buckets.ProMini, cycle)
		}
	}

	return buckets, nil
}

func (s *accountStateService) buildCycleAggregate(ctx context.Context, userID int, buckets *cycleBuckets, state *model.UserAccountState) (*AccountCycleAggregate, error) {
	// 判断用户是否曾经付费，优先使用仓库结果兜底 state 缓存。
	hasPaid := state.HasEverPaid
	if !hasPaid {
		if s.userRepo != nil {
			paidBefore, err := s.userRepo.HasPaidBefore(ctx, userID)
			if err != nil {
				return nil, err
			}
			hasPaid = paidBefore
		} else if state != nil {
			hasPaid = state.HasEverPaid
		}
	}

	// 按照 Pro > ProMini > Premium > PAYG > SPECIAL_OFFER > Free 的优先级决定聚合后的角色。
	role := model.AccountRoleFree
	if len(buckets.Pro) > 0 {
		role = model.AccountRolePro
	} else if len(buckets.ProMini) > 0 {
		role = model.AccountRoleProMini
	} else if len(buckets.Premium) > 0 {
		role = model.AccountRolePremium
	} else if len(buckets.Payg) > 0 {
		role = model.AccountRolePayg
	} else if len(buckets.SpecialOffer) > 0 {
		role = model.AccountRoleSpecialOffer
	}

	// 初始化聚合对象，承载本次刷新的角色/付费标记等信息。
	agg := &AccountCycleAggregate{
		UserID:      userID,
		Role:        role,
		HasEverPaid: hasPaid,
		UpdatedAt:   state.UpdatedAt,
	}

	// Summary 统计：累加当前有效周期的 summary_total、summary_used 和剩余额度。
	// Free 仅在用户没有任何付费权益时参与统计。
	effectivePaid := hasPaid || role != model.AccountRoleFree
	summaryRemaining := 0
	summaryTotal := 0
	summaryUsed := 0
	summaryBuckets := [][]*model.UserCycleState{buckets.Payg, buckets.SpecialOffer, buckets.Premium, buckets.Pro, buckets.ProMini}
	if !effectivePaid {
		summaryBuckets = append(summaryBuckets, buckets.Free)
	}
	for _, cycles := range summaryBuckets {
		for _, cycle := range cycles {
			if cycle.SummaryTotal > 0 {
				summaryTotal += cycle.SummaryTotal
			}
			if cycle.SummaryUsed > 0 {
				used := cycle.SummaryUsed
				if used > cycle.SummaryTotal && cycle.SummaryTotal > 0 {
					used = cycle.SummaryTotal
				}
				summaryUsed += used
			}
			remain := cycle.SummaryTotal - cycle.SummaryUsed
			if remain > 0 {
				summaryRemaining += remain
			}
		}
	}
	agg.SummaryRemaining = summaryRemaining
	agg.SummaryTotal = summaryTotal
	agg.SummaryUsed = summaryUsed

	// Free 周期：只对“当前没有任何付费权益”的用户统计。
	// 注意：即便数据库中存在遗留的 FREE 周期（ACTIVE/COMPLETED）或 legacy 字段，
	// 付费用户/付费权益用户的统计口径也应将 Free 视为 0，避免污染 PAYG/Premium/Pro 统计。
	if !effectivePaid {
		if len(buckets.Free) > 0 {
			agg.Free = aggregateCycleQuota(buckets.Free)
		} else if state.FreeCycleEnd != nil && valueOrZero(state.FreeCycleEnd) > time.Now().Unix() {
			agg.Free = &CycleQuota{
				Total: state.FreeTotal,
				Used:  state.FreeUsed,
				Start: state.FreeCycleStart,
				End:   state.FreeCycleEnd,
			}
		}
	}
	// PAYG/Premium/Pro 基于“统计口径”的有效周期（ACTIVE+COMPLETED 且未过期）聚合。
	if len(buckets.Payg) > 0 {
		agg.Payg = aggregateEffectiveCycleQuota(buckets.Payg)
	}
	if len(buckets.SpecialOffer) > 0 {
		agg.SpecialOffer = aggregateEffectiveCycleQuota(buckets.SpecialOffer)
	}
	if len(buckets.Premium) > 0 {
		agg.Premium = aggregatePremiumInfo(buckets.Premium, state.PremiumBacklog)
	}
	if len(buckets.Pro) > 0 {
		agg.Pro = aggregateProInfo(buckets.Pro, state.ProExpireAt)
	}
	if len(buckets.ProMini) > 0 {
		agg.ProMini = aggregateProMiniInfo(buckets.ProMini, state.ProExpireAt)
	}

	// PAYG 额外记录明细列表，按到期时间+创建时间排序，便于前端展示。
	if len(buckets.Payg) > 0 {
		paygCycles := make([]*model.UserCycleState, len(buckets.Payg))
		copy(paygCycles, buckets.Payg)
		sort.Slice(paygCycles, func(i, j int) bool {
			iExp := valueOrZero(paygCycles[i].PeriodEnd)
			jExp := valueOrZero(paygCycles[j].PeriodEnd)
			if iExp == jExp {
				return paygCycles[i].CreatedAt < paygCycles[j].CreatedAt
			}
			return iExp < jExp
		})

		entries := make([]PaygCycleEntry, 0, len(paygCycles))
		for _, cycle := range paygCycles {
			entries = append(entries, PaygCycleEntry{
				ID:          cycle.ID,
				GrantedAt:   cycle.CreatedAt,
				ExpiresAt:   valueOrZero(cycle.PeriodEnd),
				OriginTxnID: cycle.OriginID,
				Total:       cycle.TotalSeconds,
				Used:        cycle.UsedSeconds,
			})
		}
		agg.PaygEntries = entries
	}

	// Special Offer 额外记录明细列表，按到期时间+创建时间排序，便于前端展示。
	if len(buckets.SpecialOffer) > 0 {
		specialOfferCycles := make([]*model.UserCycleState, len(buckets.SpecialOffer))
		copy(specialOfferCycles, buckets.SpecialOffer)
		sort.Slice(specialOfferCycles, func(i, j int) bool {
			iExp := valueOrZero(specialOfferCycles[i].PeriodEnd)
			jExp := valueOrZero(specialOfferCycles[j].PeriodEnd)
			if iExp == jExp {
				return specialOfferCycles[i].CreatedAt < specialOfferCycles[j].CreatedAt
			}
			return iExp < jExp
		})

		entries := make([]PaygCycleEntry, 0, len(specialOfferCycles))
		for _, cycle := range specialOfferCycles {
			entries = append(entries, PaygCycleEntry{
				ID:          cycle.ID,
				GrantedAt:   cycle.CreatedAt,
				ExpiresAt:   valueOrZero(cycle.PeriodEnd),
				OriginTxnID: cycle.OriginID,
				Total:       cycle.TotalSeconds,
				Used:        cycle.UsedSeconds,
			})
		}
		agg.SpecialOfferEntries = entries
	}

	// 1. 判断用户是否曾经付费，优先使用仓库结果兜底 state 缓存。
	// 2. 按照 Pro > ProMini > Premium > PAYG > SPECIAL_OFFER > Free 的优先级决定聚合后的角色。
	// 3. 初始化聚合对象，承载本次刷新的角色/付费标记等信息。
	// 4. Free 周期：优先使用活跃周期；若无则在未付费且仍有旧数据时保留 legacy 信息。
	// 5. PAYG/Premium/Pro 直接基于活跃周期累加。
	// 6. PAYG 额外记录明细列表，按到期时间+创建时间排序，便于前端展示。

	return agg, nil
}

func (s *accountStateService) refreshStateFromCycles(ctx context.Context, userID int, state *model.UserAccountState) (*AccountCycleAggregate, error) {
	// 加载指定用户的所有活跃周期，并按层级（Free、Payg、SpecialOffer、Premium、Pro、ProMini）分组。
	buckets, err := s.loadCycleBuckets(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 将周期桶折叠成聚合快照，用于驱动角色和配额字段。
	agg, err := s.buildCycleAggregate(ctx, userID, buckets, state)
	if err != nil {
		return nil, err
	}

	// 记录旧的 Payg 已用时长，便于处理已完成周期的遗留消耗。
	prevPaygUsed := state.PaygUsed

	// 用户身份相关标记直接取自聚合结果。
	state.Role = agg.Role
	state.HasEverPaid = agg.HasEverPaid
	state.SummaryRemaining = agg.SummaryRemaining

	// 如果聚合中有活跃的 Free 周期，则更新免费额度。
	if agg.Free != nil {
		state.FreeTotal = agg.Free.Total
		state.FreeUsed = agg.Free.Used
		state.FreeCycleStart = agg.Free.Start
		state.FreeCycleEnd = agg.Free.End
	}

	// PAYG 需要特殊处理，确保已完成周期的消耗量得以保留。
	if agg.Payg != nil {
		paygUsed := agg.Payg.Used
		if prevPaygUsed > paygUsed {
			// 计算已完成周期的已用时长并加回总额度。
			completedUsed := prevPaygUsed - paygUsed
			state.PaygTotal = agg.Payg.Total + completedUsed
			state.PaygUsed = paygUsed + completedUsed
		} else {
			// 没有已完成周期，直接使用聚合结果。
			state.PaygTotal = agg.Payg.Total
			state.PaygUsed = paygUsed
		}
	} else {
		// 没有活跃的 PAYG 周期，保留历史已用时长作为总量。
		state.PaygTotal = prevPaygUsed
		state.PaygUsed = prevPaygUsed
	}

	// Special Offer 相关字段直接同步聚合数据。
	if agg.SpecialOffer != nil {
		state.SpecialOfferTotal = agg.SpecialOffer.Total
		state.SpecialOfferUsed = agg.SpecialOffer.Used
	}

	// Premium 相关字段直接同步聚合数据（含积压与周期索引）。
	if agg.Premium != nil {
		state.PremiumTotal = agg.Premium.Quota.Total
		state.PremiumUsed = agg.Premium.Quota.Used
		state.PremiumCycleStart = agg.Premium.Quota.Start
		state.PremiumCycleEnd = agg.Premium.Quota.End
		state.PremiumCycleIndex = agg.Premium.CycleIndex
		state.PremiumBacklog = agg.Premium.BacklogSeconds
	}

	// Pro 有活跃周期时保留过期信息，否则只保留历史用量，等待其他逻辑清空。
	if agg.Pro != nil {
		state.ProTotal = agg.Pro.Quota.Total
		state.ProUsed = agg.Pro.Quota.Used
		state.ProExpireAt = agg.Pro.ExpireAt
	} else if agg.ProMini != nil {
		state.ProMiniTotal = agg.ProMini.Quota.Total
		state.ProMiniUsed = agg.ProMini.Quota.Used
		state.ProExpireAt = agg.ProMini.ExpireAt
	} else {
		// 无活跃 Pro/ProMini 周期时保留历史用量
		state.ProTotal = state.ProUsed
		state.ProMiniTotal = state.ProMiniUsed
		if state.ProExpireAt != nil && (state.ProTotal == 0 && state.ProMiniTotal == 0) {
			state.ProExpireAt = nil
		}
	}

	// 完全 Free 且无 Free 周期时，需要清空旧的免费额度。
	if state.Role == model.AccountRoleFree && agg.Free == nil {
		// 注意：用户把免费额度用完时，Free 周期可能仍处于有效窗口，但会被周期服务判定为无活跃周期。
		// 这种情况下不能清空 FreeTotal/FreeUsed，否则 /api/user/usage 会显示全 0。
		// 但对于曾经付费用户（HasEverPaid=true），不应保留/展示任何免费额度，避免旧数据残留。
		now := time.Now().Unix()
		if state.HasEverPaid || state.FreeCycleEnd == nil || valueOrZero(state.FreeCycleEnd) <= now {
			clearFreeQuota(state)
		}
	}

	return agg, nil
}

// checkAndExpireCycles 检查并处理过期的周期
func (s *accountStateService) checkAndExpireCycles(ctx context.Context, userID int) error {
	// 委托给专门的CycleExpirationService处理，避免逻辑重复
	if s.cycleExpiration != nil {
		return s.cycleExpiration.CheckAndExpireCycles(ctx, userID)
	}

	// 如果没有CycleExpirationService，保留原有逻辑作为后备
	return s.checkAndExpireCyclesLegacy(ctx, userID)
}

// checkAndExpireCyclesLegacy 原有的过期检查逻辑，作为后备
func (s *accountStateService) checkAndExpireCyclesLegacy(ctx context.Context, userID int) error {
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
				zap.Int64("expired_at", *cycle.PeriodEnd))

			// 将Pro周期标记为过期
			if err := s.cycleSvc.Transition(ctx, cycle.ID, model.CycleStateActive, model.CycleStateExpired); err != nil {
				return err
			}
		}
	}

	// 检查其他类型周期的过期（PAYG, Premium, Free）
	return s.expireOtherCycles(ctx, userID, now)
}

// expireOtherCycles 处理其他类型周期的过期
func (s *accountStateService) expireOtherCycles(ctx context.Context, userID int, now int64) error {
	// 检查PAYG周期过期
	paygCycles, err := s.cycleSvc.ListActiveByTier(ctx, userID, model.AccountRolePayg)
	if err != nil {
		return err
	}

	for _, cycle := range paygCycles {
		if cycle.PeriodEnd != nil && *cycle.PeriodEnd < now {
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
			if err := s.cycleSvc.Transition(ctx, cycle.ID, model.CycleStateActive, model.CycleStateExpired); err != nil {
				return err
			}
		}
	}

	return nil
}

func clearFreeQuota(state *model.UserAccountState) {
	state.FreeTotal = 0
	state.FreeUsed = 0
	state.FreeCycleStart = nil
	state.FreeCycleEnd = nil
}

// snapshotToLegacyState 将新的周期快照转换为旧的UserAccountState格式
func snapshotToLegacyState(snapshot *AccountCycleSnapshot) *model.UserAccountState {
	if snapshot == nil {
		return nil
	}

	state := &model.UserAccountState{
		UserID:           snapshot.UserID,
		Role:             snapshot.Role,
		HasEverPaid:      snapshot.HasEverPaid,
		SummaryRemaining: snapshot.SummaryRemaining,
		UpdatedAt:        snapshot.UpdatedAt,
	}

	// 转换Free周期信息
	if snapshot.Free != nil {
		state.FreeCycleStart = snapshot.Free.Start
		state.FreeCycleEnd = snapshot.Free.End
		state.FreeTotal = snapshot.Free.Total
		state.FreeUsed = snapshot.Free.Used
	}

	// 转换Payg周期信息
	if snapshot.Payg != nil {
		state.PaygTotal = snapshot.Payg.Total
		state.PaygUsed = snapshot.Payg.Used
	}

	// 转换SpecialOffer周期信息
	if snapshot.SpecialOffer != nil {
		state.SpecialOfferTotal = snapshot.SpecialOffer.Total
		state.SpecialOfferUsed = snapshot.SpecialOffer.Used
	}

	// 转换Premium周期信息
	if snapshot.Premium != nil {
		state.PremiumCycleIndex = snapshot.Premium.CycleIndex
		state.PremiumCycleStart = snapshot.Premium.Quota.Start
		state.PremiumCycleEnd = snapshot.Premium.Quota.End
		state.PremiumTotal = snapshot.Premium.Quota.Total
		state.PremiumUsed = snapshot.Premium.Quota.Used
		state.PremiumBacklog = snapshot.Premium.BacklogSeconds
	}

	// 转换Pro周期信息
	if snapshot.Pro != nil {
		state.ProExpireAt = snapshot.Pro.ExpireAt
		state.ProTotal = snapshot.Pro.Quota.Total
		state.ProUsed = snapshot.Pro.Quota.Used
	}

	// 转换ProMini周期信息
	if snapshot.ProMini != nil {
		state.ProExpireAt = snapshot.ProMini.ExpireAt
		state.ProMiniTotal = snapshot.ProMini.Quota.Total
		state.ProMiniUsed = snapshot.ProMini.Quota.Used
	}

	return state
}

func aggregateToSnapshot(agg *AccountCycleAggregate) *AccountCycleSnapshot {
	if agg == nil {
		return nil
	}
	return &AccountCycleSnapshot{
		UserID:              agg.UserID,
		Role:                agg.Role,
		HasEverPaid:         agg.HasEverPaid,
		UpdatedAt:           agg.UpdatedAt,
		SummaryRemaining:    agg.SummaryRemaining,
		SummaryTotal:        agg.SummaryTotal,
		SummaryUsed:         agg.SummaryUsed,
		Free:                agg.Free,
		Payg:                agg.Payg,
		SpecialOffer:        agg.SpecialOffer,
		Premium:             agg.Premium,
		Pro:                 agg.Pro,
		ProMini:             agg.ProMini, // 添加 ProMini 字段
		PaygEntries:         agg.PaygEntries,
		SpecialOfferEntries: agg.SpecialOfferEntries,
	}
}

func aggregateCycleQuota(cycles []*model.UserCycleState) *CycleQuota {
	if len(cycles) == 0 {
		return nil
	}
	total := 0
	used := 0
	var startVal int64
	var endVal int64
	hasStart := false
	hasEnd := false
	for _, cycle := range cycles {
		total += cycle.TotalSeconds
		used += cycle.UsedSeconds
		if cycle.PeriodStart != nil {
			if !hasStart || *cycle.PeriodStart < startVal {
				startVal = *cycle.PeriodStart
				hasStart = true
			}
		}
		if cycle.PeriodEnd != nil {
			if !hasEnd || *cycle.PeriodEnd > endVal {
				endVal = *cycle.PeriodEnd
				hasEnd = true
			}
		}
	}

	quota := &CycleQuota{Total: total, Used: used}
	if hasStart {
		quota.Start = pointerInt64(startVal)
	}
	if hasEnd {
		quota.End = pointerInt64(endVal)
	}
	return quota
}

func aggregatePremiumInfo(cycles []*model.UserCycleState, backlog int) *PremiumCycleInfo {
	quota := aggregateCycleQuota(cycles)
	if quota == nil {
		return nil
	}
	cycleIndex := 0
	for _, cycle := range cycles {
		if cycle.CycleNo > cycleIndex {
			cycleIndex = cycle.CycleNo
		}
	}
	return &PremiumCycleInfo{
		Quota:          *quota,
		CycleIndex:     cycleIndex,
		BacklogSeconds: backlog,
	}
}

func aggregateProInfo(cycles []*model.UserCycleState, fallbackExpire *int64) *ProCycleInfo {
	quota := aggregateCycleQuota(cycles)
	if quota == nil {
		if fallbackExpire == nil {
			return nil
		}
		return &ProCycleInfo{
			Quota:    CycleQuota{},
			ExpireAt: fallbackExpire,
		}
	}
	var expireVal int64
	hasExpire := false
	for _, cycle := range cycles {
		if cycle.PeriodEnd != nil {
			if !hasExpire || *cycle.PeriodEnd > expireVal {
				expireVal = *cycle.PeriodEnd
				hasExpire = true
			}
		}
	}
	if !hasExpire && fallbackExpire != nil {
		expireVal = *fallbackExpire
		hasExpire = true
	}
	var expirePtr *int64
	if hasExpire {
		expirePtr = pointerInt64(expireVal)
	}

	return &ProCycleInfo{
		Quota:    *quota,
		ExpireAt: expirePtr,
	}
}

func aggregateProMiniInfo(cycles []*model.UserCycleState, fallbackExpire *int64) *ProMiniCycleInfo {
	quota := aggregateCycleQuota(cycles)
	if quota == nil {
		if fallbackExpire == nil {
			return nil
		}
		return &ProMiniCycleInfo{
			Quota:    CycleQuota{},
			ExpireAt: fallbackExpire,
		}
	}
	var expireVal int64
	hasExpire := false
	for _, cycle := range cycles {
		if cycle.PeriodEnd != nil {
			if !hasExpire || *cycle.PeriodEnd > expireVal {
				expireVal = *cycle.PeriodEnd
				hasExpire = true
			}
		}
	}
	if !hasExpire && fallbackExpire != nil {
		expireVal = *fallbackExpire
		hasExpire = true
	}
	var expirePtr *int64
	if hasExpire {
		expirePtr = pointerInt64(expireVal)
	}

	return &ProMiniCycleInfo{
		Quota:    *quota,
		ExpireAt: expirePtr,
	}
}

func valueOrZero(ptr *int64) int64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

// GrantSubscriptionCycle 统一的订阅周期发放逻辑，供LifecycleWorker和Scheduler共用
func (s *accountStateService) GrantSubscriptionCycle(ctx context.Context, sub *model.Subscription, grantAt int64, billing BillingParams) error {
	// 检查订阅状态，已取消/退款/过期的订阅不应继续发放
	if sub.Status == model.SubscriptionStatusCanceled || sub.Status == model.SubscriptionStatusRefunded || sub.Status == model.SubscriptionStatusExpired {
		s.logger.Info("跳过已取消/退款/过期的订阅发放",
			zap.Int64("subscription_id", sub.ID),
			zap.Int("user_id", sub.UserID),
			zap.String("status", string(sub.Status)))
		return nil
	}

	switch sub.ProductType {
	case model.SubscriptionProductYearSub:
		return s.grantPremiumCycle(ctx, sub, grantAt, billing)
	case model.SubscriptionProductYearPro:
		return s.grantProCycle(ctx, sub, grantAt, billing)
	case model.SubscriptionProductProMini:
		return s.grantProMiniCycle(ctx, sub, grantAt, billing)
	default:
		return fmt.Errorf("unsupported subscription product type %s", sub.ProductType)
	}
}

func (s *accountStateService) grantPremiumCycle(ctx context.Context, sub *model.Subscription, grantAt int64, billing BillingParams) error {
	premiumCycleSeconds := billing.PremiumCycleSeconds
	if premiumCycleSeconds <= 0 {
		premiumCycleSeconds = int64(model.PremiumCycleDurationSeconds)
	}
	nextGrant := grantAt + premiumCycleSeconds
	periodIndex := sub.PeriodsGranted + 1
	subscriptionID := sub.ID

	snapshot, err := s.GetCycleSnapshot(ctx, sub.UserID)
	if err != nil {
		return err
	}

	// 检查是否已有覆盖当前发放时间的Premium周期
	activePremium := snapshot != nil && snapshot.Premium != nil && snapshot.Premium.Quota.End != nil && *snapshot.Premium.Quota.End > grantAt

	if activePremium {
		// 已有活跃周期时，下次发放应从现有周期结束时间开始顺延。
		end := *snapshot.Premium.Quota.End
		if err := s.subscriptionRepo.UpdateNextGrant(ctx, sub.ID, end, sub.PeriodsGranted); err != nil {
			return err
		}
		return nil
	}

	// 发放新的Premium周期
	if _, err := s.StartPremiumSubscription(ctx, sub.UserID, grantAt, billing.PremiumCycleGrantSeconds, subscriptionID); err != nil {
		if errors.Is(err, ErrPremiumCycleLimit) {
			// 处理周期上限错误：标记订阅过期并关闭自动续订
			s.logger.Info("Subscription reached premium cycle limit, expiring subscription",
				zap.Int64("subscription_id", sub.ID),
				zap.Int("user_id", sub.UserID))

			if updateErr := s.subscriptionRepo.UpdateStatus(ctx, sub.ID, model.SubscriptionStatusExpired); updateErr != nil {
				return fmt.Errorf("mark subscription %d expired: %w", sub.ID, updateErr)
			}

			if renewErr := s.subscriptionRepo.UpdateRenewalState(ctx, sub.ID, model.SubscriptionRenewalOff); renewErr != nil {
				s.logger.Warn("Failed to disable subscription auto renew after reaching limit",
					zap.Int64("subscription_id", sub.ID),
					zap.Error(renewErr))
			}
			return nil // 不返回错误，避免重试
		}
		return err
	}

	if err := s.subscriptionRepo.UpdateNextGrant(ctx, sub.ID, nextGrant, periodIndex); err != nil {
		return err
	}
	return nil
}

func (s *accountStateService) grantProCycle(ctx context.Context, sub *model.Subscription, grantAt int64, billing BillingParams) error {
	proCycleSeconds := billing.ProCycleSeconds
	if proCycleSeconds <= 0 {
		proCycleSeconds = 360 * 24 * 3600
	}
	nextGrant := grantAt + proCycleSeconds
	periodIndex := sub.PeriodsGranted + 1
	subscriptionID := sub.ID

	snapshot, err := s.GetCycleSnapshot(ctx, sub.UserID)
	if err != nil {
		return err
	}

	// 检查是否已有覆盖当前发放时间的Pro周期
	activePro := snapshot != nil && snapshot.Pro != nil && snapshot.Pro.Quota.End != nil && *snapshot.Pro.Quota.End > grantAt

	if activePro {
		// 已有活跃周期时，下次发放应从现有周期结束时间开始顺延。
		end := *snapshot.Pro.Quota.End
		if err := s.subscriptionRepo.UpdateNextGrant(ctx, sub.ID, end, sub.PeriodsGranted); err != nil {
			return err
		}
		return nil
	}

	// 发放新的Pro周期
	if _, err := s.StartProSubscription(ctx, sub.UserID, grantAt, billing.ProTotalSeconds, subscriptionID); err != nil {
		return err
	}

	if err := s.subscriptionRepo.UpdateNextGrant(ctx, sub.ID, nextGrant, periodIndex); err != nil {
		return err
	}
	return nil
}

func (s *accountStateService) grantProMiniCycle(ctx context.Context, sub *model.Subscription, grantAt int64, billing BillingParams) error {
	proMiniCycleSeconds := billing.ProMiniCycleSeconds
	if proMiniCycleSeconds <= 0 {
		proMiniCycleSeconds = 360 * 24 * 3600
	}
	nextGrant := grantAt + proMiniCycleSeconds
	periodIndex := sub.PeriodsGranted + 1
	subscriptionID := sub.ID

	snapshot, err := s.GetCycleSnapshot(ctx, sub.UserID)
	if err != nil {
		return err
	}

	// 检查是否已有覆盖当前发放时间的Pro Mini周期
	activeProMini := snapshot != nil && snapshot.ProMini != nil && snapshot.ProMini.Quota.End != nil && *snapshot.ProMini.Quota.End > grantAt

	if activeProMini {
		// 已有活跃周期时，下次发放应从现有周期结束时间开始顺延。
		end := *snapshot.ProMini.Quota.End
		if err := s.subscriptionRepo.UpdateNextGrant(ctx, sub.ID, end, sub.PeriodsGranted); err != nil {
			return err
		}
		return nil
	}

	// 发放新的Pro Mini周期
	if _, err := s.StartProMiniSubscription(ctx, sub.UserID, grantAt, billing.ProMiniTotalSeconds, subscriptionID); err != nil {
		return err
	}

	if err := s.subscriptionRepo.UpdateNextGrant(ctx, sub.ID, nextGrant, periodIndex); err != nil {
		return err
	}
	return nil
}
