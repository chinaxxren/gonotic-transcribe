package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/repository"
	"go.uber.org/zap"
)

// CycleDeduction 描述一次扣费中单个周期被扣减的详情。
type CycleDeduction struct {
	CycleID           int64
	Tier              model.AccountRole
	Seconds           int
	OriginTxnID       *int64
	OriginProductType *model.TransactionProductType
}

// CycleConsumeResult 聚合周期扣费结果，包含来源拆分与周期级扣减明细。
// BalanceConsumeDetail 定义在 account_state_models.go，供账户服务复用。
type CycleConsumeResult struct {
	Detail      BalanceConsumeDetail
	Deductions  []CycleDeduction
	FinishedIDs []int64 // 本次被扣完（需要标记 COMPLETED）的周期 ID
}

// CycleConsumptionService 使用 CycleStateService 对 ACTIVE 周期执行扣费逻辑。
type CycleConsumptionService interface {
	Consume(ctx context.Context, userID int, seconds int) (*CycleConsumeResult, error)

	// ConsumeWithAudit 扣费并记录审计日志，用于可追溯的消费场景
	ConsumeWithAudit(ctx context.Context, userID int, seconds int, businessID int, source string) (*CycleConsumeResult, error)
}

type cycleConsumptionService struct {
	cycleSvc        CycleStateService
	usageLedger     repository.UsageLedgerRepository // 可选，用于审计
	transactionRepo repository.TransactionRepository
	logger          *zap.Logger
}

// NewCycleConsumptionService 创建基于周期状态机的扣费服务。
func NewCycleConsumptionService(cycleSvc CycleStateService, logger *zap.Logger) CycleConsumptionService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &cycleConsumptionService{cycleSvc: cycleSvc, logger: logger}
}

// NewCycleConsumptionServiceWithAudit 创建支持审计的扣费服务。
func NewCycleConsumptionServiceWithAudit(cycleSvc CycleStateService, usageLedger repository.UsageLedgerRepository, logger *zap.Logger) CycleConsumptionService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &cycleConsumptionService{
		cycleSvc:    cycleSvc,
		usageLedger: usageLedger,
		logger:      logger,
	}
}

func NewCycleConsumptionServiceWithTransactionRepo(cycleSvc CycleStateService, transactionRepo repository.TransactionRepository, logger *zap.Logger) CycleConsumptionService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &cycleConsumptionService{cycleSvc: cycleSvc, transactionRepo: transactionRepo, logger: logger}
}

func NewCycleConsumptionServiceWithAuditAndTransactionRepo(cycleSvc CycleStateService, usageLedger repository.UsageLedgerRepository, transactionRepo repository.TransactionRepository, logger *zap.Logger) CycleConsumptionService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &cycleConsumptionService{cycleSvc: cycleSvc, usageLedger: usageLedger, transactionRepo: transactionRepo, logger: logger}
}

// Consume 会按照周期的有效期排序（越早过期越先扣）依次消耗指定秒数，并返回详细扣费明细。
func (s *cycleConsumptionService) Consume(ctx context.Context, userID int, seconds int) (*CycleConsumeResult, error) {
	if userID == 0 {
		return nil, fmt.Errorf("user_id is required")
	}
	if seconds <= 0 {
		return &CycleConsumeResult{}, nil
	}

	cycles, err := s.cycleSvc.ListConsumable(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list consumable cycles failed: %w", err)
	}

	// 调试日志：显示扣费请求和可用周期
	s.logger.Debug("consume request",
		zap.Int("user_id", userID),
		zap.Int("seconds", seconds),
		zap.Int("cycles", len(cycles)))
	for i, cycle := range cycles {
		available := cycle.TotalSeconds - cycle.UsedSeconds
		periodStart := int64(0)
		periodEnd := int64(0)
		if cycle.PeriodStart != nil {
			periodStart = *cycle.PeriodStart
		}
		if cycle.PeriodEnd != nil {
			periodEnd = *cycle.PeriodEnd
		}
		s.logger.Debug("consume cycle snapshot",
			zap.Int("index", i),
			zap.Int64("cycle_id", cycle.ID),
			zap.String("tier", string(cycle.Tier)),
			zap.String("state", string(cycle.State)),
			zap.Int("total", cycle.TotalSeconds),
			zap.Int("used", cycle.UsedSeconds),
			zap.Int("available", available),
			zap.Int64("period_start", periodStart),
			zap.Int64("period_end", periodEnd),
			zap.Int64("created_at", cycle.CreatedAt),
			zap.Int64("updated_at", cycle.UpdatedAt))
	}

	remaining := seconds
	result := &CycleConsumeResult{}

	// 统计信息
	var cyclesCleared int // 被清空的周期数
	var cyclesPartial int // 部分消费的周期数

	// 统一扣费策略：按过期时间排序（越早过期越先扣）
	// 排序规则：
	// 1. 优先按过期时间升序（越早过期越先扣）
	// 2. 过期时间相同时按创建时间升序
	// 3. 创建时间也相同时按ID升序
	sort.Slice(cycles, func(i, j int) bool {
		iExp := getExpirationTime(cycles[i])
		jExp := getExpirationTime(cycles[j])
		if iExp != jExp {
			return iExp < jExp
		}
		if cycles[i].CreatedAt == cycles[j].CreatedAt {
			return cycles[i].ID < cycles[j].ID
		}
		return cycles[i].CreatedAt < cycles[j].CreatedAt
	})

	for _, cycle := range cycles {
		if remaining <= 0 {
			break
		}
		available := cycle.TotalSeconds - cycle.UsedSeconds
		if available <= 0 {
			continue
		}
		consume := minInt(available, remaining)
		if consume <= 0 {
			continue
		}
		finished := consume == available

		// 记录扣费操作
		if consume == available {
			cyclesCleared++
		} else {
			cyclesPartial++
		}

		if err := s.cycleSvc.IncrementUsage(ctx, cycle.ID, consume); err != nil {
			// 如果是容量超限错误，继续处理下一个周期
			if strings.Contains(err.Error(), "would exceed cycle capacity") {
				continue
			}
			return nil, fmt.Errorf("increment usage failed: %w", err)
		}

		// 注意：这里不直接修改 cycle.UsedSeconds，避免与底层实现（或测试 fake）的累加重复。
		remaining -= consume
		result.Deductions = append(result.Deductions, CycleDeduction{
			CycleID:     cycle.ID,
			Tier:        cycle.Tier,
			Seconds:     consume,
			OriginTxnID: cycle.OriginID,
		})

		switch cycle.Tier {
		case model.AccountRolePro:
			result.Detail.FromPro += consume
		case model.AccountRoleProMini:
			result.Detail.FromProMini += consume
		case model.AccountRolePremium:
			result.Detail.FromPremium += consume
		case model.AccountRolePayg:
			resolvedProductType := model.TransactionProductHourPack
			// PAYG cycles should never be Special Offer
			isSpecial := false
			if isSpecial {
				resolvedProductType = model.TransactionProductSpecialOffer
				result.Detail.FromSpecialOffer += consume
			} else {
				result.Detail.FromPayg += consume
			}
			// attach resolved product type to latest deduction record (same cycle)
			if len(result.Deductions) > 0 {
				last := &result.Deductions[len(result.Deductions)-1]
				if last.CycleID == cycle.ID {
					pt := resolvedProductType
					last.OriginProductType = &pt
				}
			}
		case model.AccountRoleSpecialOffer:
			resolvedProductType := model.TransactionProductSpecialOffer
			result.Detail.FromSpecialOffer += consume
			// attach resolved product type to latest deduction record (same cycle)
			if len(result.Deductions) > 0 {
				last := &result.Deductions[len(result.Deductions)-1]
				if last.CycleID == cycle.ID {
					pt := resolvedProductType
					last.OriginProductType = &pt
				}
			}
		case model.AccountRoleFree:
			result.Detail.FromFree += consume
		}

		if finished {
			result.FinishedIDs = append(result.FinishedIDs, cycle.ID)
		}
	}
	if remaining > 0 {
		return nil, fmt.Errorf("insufficient balance, need %d more seconds", remaining)
	}

	// 输出扣费统计
	fmt.Printf("DEBUG: Consume completed - UserID=%d, CyclesCleared=%d, CyclesPartial=%d, TotalCycles=%d\n",
		userID, cyclesCleared, cyclesPartial, cyclesCleared+cyclesPartial)

	// 标记本次扣完的周期为 COMPLETED
	for _, id := range result.FinishedIDs {
		// 根据业务约定，ACTIVE -> COMPLETED
		if err := s.cycleSvc.Transition(ctx, id, model.CycleStateActive, model.CycleStateCompleted); err != nil {
			return nil, fmt.Errorf("complete cycle failed: %w", err)
		}
	}

	return result, nil
}

// ConsumeWithAudit 扣费并记录审计日志，用于可追溯的消费场景
func (s *cycleConsumptionService) ConsumeWithAudit(ctx context.Context, userID int, seconds int, businessID int, source string) (*CycleConsumeResult, error) {
	if s.usageLedger == nil {
		// 如果没有配置审计仓储，退回到普通扣费
		return s.Consume(ctx, userID, seconds)
	}

	// 先执行扣费
	result, err := s.Consume(ctx, userID, seconds)
	if err != nil {
		return nil, err
	}

	// 记录审计日志
	totalConsumed := result.Detail.FromFree + result.Detail.FromPayg + result.Detail.FromSpecialOffer + result.Detail.FromPremium + result.Detail.FromPro + result.Detail.FromProMini
	if totalConsumed > 0 {
		// 为每个被扣费的周期创建审计记录
		for _, deduction := range result.Deductions {
			var originProductType *string
			if deduction.OriginProductType != nil {
				v := string(*deduction.OriginProductType)
				originProductType = &v
			}
			ledgerEntry := &model.UsageLedger{
				UserID:            userID,
				BusinessID:        businessID,
				CycleID:           &deduction.CycleID,
				OriginProductType: originProductType,
				Seconds:           deduction.Seconds,
				Source:            source,
				CreatedAt:         time.Now().Unix(),
				// BalanceBefore 和 BalanceAfter 可以根据需要计算
				// 这里简化处理，主要记录扣费事实
			}

			if err := s.usageLedger.Create(ctx, ledgerEntry); err != nil {
				// 审计记录失败不应该影响扣费结果，只记录错误
				// 在实际系统中可能需要更复杂的错误处理策略
				fmt.Printf("Warning: failed to create usage ledger entry: %v\n", err)
			}
		}
	}

	return result, nil
}

// getExpirationTime 获取周期的过期时间，如果没有设置则返回 0，最先消费
func getExpirationTime(cycle *model.UserCycleState) int64 {
	if cycle.PeriodEnd == nil {
		return 0 // int64最小值
	}
	return *cycle.PeriodEnd
}
