package service

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/chinaxxren/gonotic/internal/dto"
	"github.com/chinaxxren/gonotic/internal/model"
	"github.com/chinaxxren/gonotic/internal/repository"
)

type AccountStateFacade interface {
	GetAccountSummary(ctx context.Context, userID int) (*dto.AccountSummary, error)
	GetUsageStats(ctx context.Context, userID int) (*dto.AccountUsageStats, error)
	ListPaygEntries(ctx context.Context, userID int) ([]dto.PaygEntrySummary, error)
	ListSpecialOfferEntries(ctx context.Context, userID int) ([]dto.PaygEntrySummary, error)
	Consume(ctx context.Context, req dto.AccountConsumeRequest) (*dto.AccountConsumeResult, error)
}

// mapCycleSnapshotToSummary is a legacy helper used by purchase/payment flows to
// attach account snapshot to transactions. It keeps backward-compatible mapping
// and does not attempt to split PAYG vs SPECIAL_OFFER (that split requires
// TransactionRepository lookups and is handled by accountStateFacade).
func mapCycleSnapshotToSummary(snapshot *AccountCycleSnapshot) *dto.AccountSummary {
	summary := &dto.AccountSummary{}
	if snapshot == nil {
		return summary
	}
	summary.Role = string(snapshot.Role)
	summary.HasEverPaid = snapshot.HasEverPaid
	summary.UpdatedAt = snapshot.UpdatedAt
	summary.SummaryRemaining = snapshot.SummaryRemaining
	summary.SummaryTotal = snapshot.SummaryTotal
	summary.SummaryUsed = snapshot.SummaryUsed
	summary.Free = cycleQuotaToDTO(snapshot.Free)
	summary.Payg = cycleQuotaToDTO(snapshot.Payg)
	if snapshot.Premium != nil {
		premium := &dto.PremiumBucket{
			CycleIndex:     snapshot.Premium.CycleIndex,
			BacklogSeconds: snapshot.Premium.BacklogSeconds,
		}
		if quota := cycleQuotaToDTO(&snapshot.Premium.Quota); quota != nil {
			premium.QuotaBucket = *quota
		}
		summary.Premium = premium
	}
	if snapshot.Pro != nil {
		pro := &dto.ProBucket{}
		if quota := cycleQuotaToDTO(&snapshot.Pro.Quota); quota != nil {
			pro.QuotaBucket = *quota
		}
		pro.ExpireAt = snapshot.Pro.ExpireAt
		summary.Pro = pro
	}
	if snapshot.ProMini != nil {
		proMini := &dto.ProMiniBucket{}
		if quota := cycleQuotaToDTO(&snapshot.ProMini.Quota); quota != nil {
			proMini.QuotaBucket = *quota
		}
		proMini.ExpireAt = snapshot.ProMini.ExpireAt
		summary.ProMini = proMini
	}
	summary.PaygEntries = make([]dto.PaygEntrySummary, 0, len(snapshot.PaygEntries))
	for _, entry := range snapshot.PaygEntries {
		summary.PaygEntries = append(summary.PaygEntries, dto.PaygEntrySummary{
			EntryID:   entry.ID,
			GrantedAt: entry.GrantedAt,
			ExpiresAt: entry.ExpiresAt,
			OriginID:  entry.OriginTxnID,
			Total:     entry.Total,
			Used:      entry.Used,
		})
	}
	return summary
}

func mapCycleSnapshotToSplitSummary(ctx context.Context, snapshot *AccountCycleSnapshot, transactionRepo repository.TransactionRepository) *dto.AccountSummary {
	if snapshot == nil {
		return &dto.AccountSummary{}
	}
	if transactionRepo == nil {
		return mapCycleSnapshotToSummary(snapshot)
	}
	facade := &accountStateFacade{transactionRepo: transactionRepo}
	return facade.mapCycleSnapshotToSummary(ctx, snapshot)
}

type accountStateFacade struct {
	stateSvc        AccountStateService
	ledgerRepo      repository.UsageLedgerRepository
	transactionRepo repository.TransactionRepository
	logger          *zap.Logger
}

func NewAccountStateFacade(stateSvc AccountStateService, ledgerRepo repository.UsageLedgerRepository, transactionRepo repository.TransactionRepository, logger *zap.Logger) AccountStateFacade {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &accountStateFacade{
		stateSvc:        stateSvc,
		ledgerRepo:      ledgerRepo,
		transactionRepo: transactionRepo,
		logger:          logger,
	}
}

func (f *accountStateFacade) GetAccountSummary(ctx context.Context, userID int) (*dto.AccountSummary, error) {
	snapshot, err := f.stateSvc.GetCycleSnapshot(ctx, userID)
	if err != nil {
		return nil, err
	}
	return f.mapCycleSnapshotToSummary(ctx, snapshot), nil
}

func (f *accountStateFacade) GetUsageStats(ctx context.Context, userID int) (*dto.AccountUsageStats, error) {
	summary, err := f.GetAccountSummary(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 根据当前账户摘要推导统计窗口
	now := time.Now().Unix()
	start, end := resolveUsageWindowFromSummary(summary, now)
	if end > now {
		end = now
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		start = end
	}

	f.logger.Debug("统计窗口计算",
		zap.Int("user_id", userID),
		zap.Int64("window_start", start),
		zap.Int64("window_end", end),
		zap.Int64("now", now),
		zap.String("plan_id", resolvePlanIDFromSummary(summary)))

	features := dto.UsageFeatureBreakdown{}
	stats, err := f.ledgerRepo.GetAggregatedStats(ctx, userID, start, end)
	if err != nil {
		return nil, err
	}

	f.logger.Debug("聚合统计数据",
		zap.Int("user_id", userID),
		zap.Any("stats", stats))

	originStats, err := f.ledgerRepo.GetOriginProductStats(ctx, userID, start, end)
	if err != nil {
		return nil, err
	}
	if stats != nil {
		features.Transcription = int(stats.TranscriptionSeconds)
		features.Translation = int(stats.TranslationSeconds)
	}
	var expiresAt *int64

	addExpireCandidate := func(candidate *int64) {
		if candidate == nil {
			return
		}
		if expiresAt == nil || *candidate > *expiresAt {
			expiresAt = candidate
		}
	}

	// 仍然查询 usage ledger 用于 features；总配额/已用/剩余按当前所有有效周期汇总，到期时间按最高优先级计划决定。
	if summary != nil {
		if summary.Pro != nil {
			addExpireCandidate(summary.Pro.ExpireAt)
		}
		if summary.ProMini != nil {
			addExpireCandidate(summary.ProMini.ExpireAt)
		}
		if summary.Premium != nil {
			addExpireCandidate(summary.Premium.End)
		}
		if len(summary.PaygEntries) > 0 {
			latest := summary.PaygEntries[len(summary.PaygEntries)-1]
			if latest.ExpiresAt > 0 {
				addExpireCandidate(pointerInt64(latest.ExpiresAt))
			}
		}
		if len(summary.SpecialOfferEntries) > 0 {
			latest := summary.SpecialOfferEntries[len(summary.SpecialOfferEntries)-1]
			if latest.ExpiresAt > 0 {
				addExpireCandidate(pointerInt64(latest.ExpiresAt))
			}
		}
		if summary.Free != nil {
			addExpireCandidate(summary.Free.End)
		}
	}

	planID := resolvePlanIDFromSummary(summary)

	if expiresAt == nil {
		expiresAt = pointerInt64(end)
	}

	quotaBucket := summary
	totalQuota := 0 // 所有周期的总配额
	totalUsed := 0  // 所有周期的总已用
	planExpiresAt := expiresAt

	if quotaBucket != nil {
		// 计算所有有效周期的总配额和已用时长
		if quotaBucket.Pro != nil {
			totalQuota += quotaBucket.Pro.TotalSeconds
			totalUsed += quotaBucket.Pro.UsedSeconds
		}
		if quotaBucket.ProMini != nil {
			totalQuota += quotaBucket.ProMini.TotalSeconds
			totalUsed += quotaBucket.ProMini.UsedSeconds
		}
		if quotaBucket.Premium != nil {
			totalQuota += quotaBucket.Premium.TotalSeconds
			totalUsed += quotaBucket.Premium.UsedSeconds
		}
		if quotaBucket.SpecialOffer != nil {
			totalQuota += quotaBucket.SpecialOffer.TotalSeconds
			totalUsed += quotaBucket.SpecialOffer.UsedSeconds
		}
		if quotaBucket.Payg != nil {
			totalQuota += quotaBucket.Payg.TotalSeconds
			totalUsed += quotaBucket.Payg.UsedSeconds
		}
		if quotaBucket.Free != nil {
			totalQuota += quotaBucket.Free.TotalSeconds
			totalUsed += quotaBucket.Free.UsedSeconds
		}

		// 设置当前最高优先级的过期时间 - 按照统一优先级顺序：Pro > ProMini > Premium > PAYG > SPECIAL_OFFER > Free
		switch planID {
		case "YEAR_PRO": // 优先级 1
			if quotaBucket.Pro != nil && quotaBucket.Pro.ExpireAt != nil {
				planExpiresAt = quotaBucket.Pro.ExpireAt
			}
		case "YEAR_PRO_MINI": // 优先级 2
			if quotaBucket.ProMini != nil && quotaBucket.ProMini.ExpireAt != nil {
				planExpiresAt = quotaBucket.ProMini.ExpireAt
			}
		case "YEAR_SUB": // 优先级 3
			if quotaBucket.Premium != nil && quotaBucket.Premium.End != nil {
				planExpiresAt = quotaBucket.Premium.End
			}
		case "HOUR_PACK": // 优先级 4
			if len(quotaBucket.PaygEntries) > 0 {
				latest := quotaBucket.PaygEntries[len(quotaBucket.PaygEntries)-1]
				if latest.ExpiresAt > 0 {
					planExpiresAt = pointerInt64(latest.ExpiresAt)
				}
			}
		case "SPECIAL_OFFER": // 优先级 5
			if len(quotaBucket.SpecialOfferEntries) > 0 {
				latest := quotaBucket.SpecialOfferEntries[len(quotaBucket.SpecialOfferEntries)-1]
				if latest.ExpiresAt > 0 {
					planExpiresAt = pointerInt64(latest.ExpiresAt)
				}
			}
		case "FREE_MONTHLY": // 优先级 6
			if quotaBucket.Free != nil && quotaBucket.Free.End != nil {
				planExpiresAt = quotaBucket.Free.End
			}
		}
	}

	originProductUsed := map[string]int{}
	if originStats != nil {
		originProductUsed["HOUR_PACK"] = int(originStats.HourPackSeconds)
		originProductUsed["SPECIAL_OFFER"] = int(originStats.SpecialOfferSeconds)
	}

	return &dto.AccountUsageStats{
		PlanID:            planID,
		Quota:             totalQuota,                      // 所有有效周期的总配额
		Used:              totalUsed,                       // 所有有效周期的总已用
		Remaining:         maxInt(0, totalQuota-totalUsed), // 所有有效周期的总剩余
		SummaryRemaining:  summary.SummaryRemaining,
		SummaryTotal:      summary.SummaryTotal,
		SummaryUsed:       summary.SummaryUsed,
		ExpiresAt:         planExpiresAt, // 当前最高优先级的过期时间
		Features:          features,
		OriginProductUsed: originProductUsed,
	}, nil
}

func resolvePlanIDFromSummary(summary *dto.AccountSummary) string {
	planID := "FREE_MONTHLY"
	if summary == nil {
		return planID
	}
	switch model.AccountRole(summary.Role) {
	case model.AccountRolePro:
		planID = "YEAR_PRO"
	case model.AccountRoleProMini:
		planID = "YEAR_PRO_MINI"
	case model.AccountRolePremium:
		planID = "YEAR_SUB"
	case model.AccountRolePayg:
		planID = "HOUR_PACK"
	case model.AccountRoleSpecialOffer:
		planID = "SPECIAL_OFFER"
	case model.AccountRoleFree:
		planID = "FREE_MONTHLY"
	}
	return planID
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

const (
	secondsPerDay      = int64(24 * 3600)
	premiumWindowWidth = 30 * secondsPerDay
	proWindowWidth     = 365 * secondsPerDay
)

func resolveUsageWindowFromSummary(summary *dto.AccountSummary, now int64) (int64, int64) {
	// 根据当前账户摘要推导统计窗口：
	// 优化：考虑所有活跃周期类型，确保不遗漏任何使用记录
	// Pro: 以订阅到期时间为终点并向前推 365 天；
	// Premium: 优先使用周期起止时间，否则 fallback 到最近 30 天；
	// PAYG: 覆盖所有时长包的授予与到期范围；
	// Free: 默认最近 30 天或沿用免费额度的起止时间。
	if summary == nil {
		return now, now
	}

	// 特殊场景：角色为付费（通常来自“最近一次购买的角色回退”），但所有权益 bucket 都为空。
	// 此时用户在当前有效窗口内没有任何可展示/统计的权益，窗口应为空，避免转录/翻译统计落入默认30天窗口。
	if model.AccountRole(summary.Role) != model.AccountRoleFree &&
		summary.Pro == nil &&
		summary.Premium == nil &&
		summary.Payg == nil &&
		summary.SpecialOffer == nil &&
		summary.ProMini == nil &&
		len(summary.PaygEntries) == 0 &&
		len(summary.SpecialOfferEntries) == 0 &&
		summary.Free == nil {
		return now, now
	}

	// 收集所有活跃周期的时间范围
	var allStarts []int64
	var allEnds []int64

	// Pro 周期
	if summary.Pro != nil {
		var end int64 = now
		if summary.Pro.ExpireAt != nil {
			end = *summary.Pro.ExpireAt
		}
		start := end - proWindowWidth
		allStarts = append(allStarts, start)
		allEnds = append(allEnds, end)
	}

	// Pro Mini 周期 - 使用相同的 365 天窗口
	if summary.ProMini != nil {
		var end int64 = now
		if summary.ProMini.ExpireAt != nil {
			end = *summary.ProMini.ExpireAt
		}
		start := end - proWindowWidth
		allStarts = append(allStarts, start)
		allEnds = append(allEnds, end)
	}

	// Premium 周期
	if summary.Premium != nil {
		start := now - premiumWindowWidth
		if summary.Premium.Start != nil {
			start = *summary.Premium.Start
		}
		end := now
		if summary.Premium.End != nil {
			end = *summary.Premium.End
		}
		allStarts = append(allStarts, start)
		allEnds = append(allEnds, end)
	}

	// PAYG 周期（HourPack）
	if len(summary.PaygEntries) > 0 {
		granted := summary.PaygEntries[0].GrantedAt
		for _, entry := range summary.PaygEntries {
			if entry.GrantedAt < granted {
				granted = entry.GrantedAt
			}
		}
		start := granted

		end := now
		expires := summary.PaygEntries[0].ExpiresAt
		for _, entry := range summary.PaygEntries {
			if entry.ExpiresAt > expires {
				expires = entry.ExpiresAt
			}
		}
		if expires > 0 {
			end = expires
		}

		allStarts = append(allStarts, start)
		allEnds = append(allEnds, end)
	}

	// Special Offer 周期
	if len(summary.SpecialOfferEntries) > 0 {
		granted := summary.SpecialOfferEntries[0].GrantedAt
		for _, entry := range summary.SpecialOfferEntries {
			if entry.GrantedAt < granted {
				granted = entry.GrantedAt
			}
		}
		start := granted

		end := now
		expires := summary.SpecialOfferEntries[0].ExpiresAt
		for _, entry := range summary.SpecialOfferEntries {
			if entry.ExpiresAt > expires {
				expires = entry.ExpiresAt
			}
		}
		if expires > 0 {
			end = expires
		}

		allStarts = append(allStarts, start)
		allEnds = append(allEnds, end)
	}

	// Free 周期
	if summary.Free != nil {
		start := now - premiumWindowWidth
		if summary.Free.Start != nil {
			start = *summary.Free.Start
		}
		end := now
		if summary.Free.End != nil {
			end = *summary.Free.End
		}
		allStarts = append(allStarts, start)
		allEnds = append(allEnds, end)
	}

	// 如果没有任何周期，使用默认窗口
	if len(allStarts) == 0 {
		return now - premiumWindowWidth, now
	}

	// 找到最早的开始时间和最晚的结束时间
	finalStart := allStarts[0]
	finalEnd := allEnds[0]
	for _, start := range allStarts {
		if start < finalStart {
			finalStart = start
		}
	}
	for _, end := range allEnds {
		if end > finalEnd {
			finalEnd = end
		}
	}

	return finalStart, finalEnd
}

func (f *accountStateFacade) ListPaygEntries(ctx context.Context, userID int) ([]dto.PaygEntrySummary, error) {
	summary, err := f.GetAccountSummary(ctx, userID)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, nil
	}
	return summary.PaygEntries, nil
}

func (f *accountStateFacade) ListSpecialOfferEntries(ctx context.Context, userID int) ([]dto.PaygEntrySummary, error) {
	summary, err := f.GetAccountSummary(ctx, userID)
	if err != nil {
		return nil, err
	}
	if summary == nil {
		return nil, nil
	}
	return summary.SpecialOfferEntries, nil
}

func (f *accountStateFacade) Consume(ctx context.Context, req dto.AccountConsumeRequest) (*dto.AccountConsumeResult, error) {
	f.logger.Debug("扣费请求接收",
		zap.Int("user_id", req.UserID),
		zap.Int("total_seconds", req.Seconds),
		zap.Int("transcription_seconds", req.TranscriptionSeconds),
		zap.Int("translation_seconds", req.TranslationSeconds),
		zap.String("source", req.Source))

	detail, err := f.stateSvc.ConsumeBalances(ctx, req.UserID, req.Seconds)
	if err != nil {
		f.logger.Error("扣费失败", zap.Int("user_id", req.UserID), zap.Int("seconds", req.Seconds), zap.Error(err))
		return nil, err
	}

	afterSnapshot, err := f.stateSvc.GetCycleSnapshot(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	afterSummary := f.mapCycleSnapshotToSummary(ctx, afterSnapshot)

	createdAt := time.Now().Unix()
	if len(detail.Deductions) == 0 {
		// 零扣费场景：没有实际从任何周期扣费，但需要记录这次请求
		// 对于转录业务，即使是零扣费也应该有业务上下文，这里记录为系统调整
		f.logger.Warn("转录请求产生零扣费记录",
			zap.Int("user_id", req.UserID),
			zap.Int("business_id", req.BusinessID),
			zap.Int("requested_seconds", req.Seconds),
			zap.String("source", req.Source))

		// 对于零扣费，我们不创建 UsageLedger 记录，因为没有实际的用量消费
		// 这避免了创建 cycleID 为空的记录
	} else {
		// 优化版：对每个周期使用查找+更新的方式，减少记录数量
		currentBalance := afterSummary.TotalRemainingSeconds()
		for idx, deduction := range detail.Deductions {
			if deduction.Seconds <= 0 {
				continue
			}

			// 转录扣费必须有有效的周期ID
			if deduction.CycleID == 0 {
				f.logger.Error("转录扣费缺少有效的周期ID",
					zap.Int("user_id", req.UserID),
					zap.Int("business_id", req.BusinessID),
					zap.Int("seconds", deduction.Seconds))
				continue // 跳过无效的扣费记录
			}

			cycleIDPtr := pointerInt64(deduction.CycleID)

			// 查找现有记录
			existing, err := f.ledgerRepo.FindByBusinessAndCycle(ctx, req.BusinessID, cycleIDPtr)
			if err != nil {
				f.logger.Error("查找现有UsageLedger记录失败",
					zap.Int("user_id", req.UserID),
					zap.Int("business_id", req.BusinessID),
					zap.Int64("cycle_id", deduction.CycleID),
					zap.Error(err))
				// 继续处理，不中断整个流程
			}

			// 计算转录和翻译秒数（只在第一个deduction中设置）
			transcriptionSeconds := 0
			translationSeconds := 0
			if idx == 0 {
				transcriptionSeconds = req.TranscriptionSeconds
				translationSeconds = req.TranslationSeconds
			}

			if existing != nil {
				// 更新现有记录
				var originProductType *string
				if deduction.OriginProductType != nil {
					v := string(*deduction.OriginProductType)
					originProductType = &v
				}
				err = f.ledgerRepo.UpdateConsumptionWithMeta(ctx, existing.ID,
					deduction.Seconds, transcriptionSeconds, translationSeconds, currentBalance,
					originProductType)
				if err != nil {
					f.logger.Error("更新UsageLedger记录失败",
						zap.Int("user_id", req.UserID),
						zap.Int("business_id", req.BusinessID),
						zap.Int64("existing_id", existing.ID),
						zap.Int("additional_seconds", deduction.Seconds),
						zap.Error(err))
				}
			} else {
				// 创建新记录
				var originProductType *string
				if deduction.OriginProductType != nil {
					v := string(*deduction.OriginProductType)
					originProductType = &v
				}
				entry := &model.UsageLedger{
					UserID:               req.UserID,
					BusinessID:           req.BusinessID,
					CycleID:              cycleIDPtr,
					OriginProductType:    originProductType,
					Seconds:              deduction.Seconds,
					BalanceBefore:        currentBalance + deduction.Seconds, // 扣费前余额
					BalanceAfter:         currentBalance,                     // 扣费后余额
					Source:               req.Source,
					TranscriptionSeconds: transcriptionSeconds,
					TranslationSeconds:   translationSeconds,
					CreatedAt:            createdAt,
				}
				err = f.ledgerRepo.Create(ctx, entry)
				if err != nil {
					f.logger.Error("创建UsageLedger记录失败",
						zap.Int("user_id", req.UserID),
						zap.Int("business_id", req.BusinessID),
						zap.Int64("cycle_id", deduction.CycleID),
						zap.Int("seconds", deduction.Seconds),
						zap.Error(err))
				}
			}
		}
	}

	breakdown := dto.ConsumptionBreakdown{
		FromFree:         detail.FromFree,
		FromPayg:         detail.FromPayg,
		FromSpecialOffer: detail.FromSpecialOffer,
		FromPremium:      detail.FromPremium,
		FromPro:          detail.FromPro,
		FromProMini:      detail.FromProMini,
	}

	return &dto.AccountConsumeResult{
		Detail:  breakdown,
		Summary: afterSummary,
	}, nil
}

// mapCycleSnapshotToSummary 将内部的周期快照结构转换为对外使用的 AccountSummary，
// 主要用于 API 返回和 Facade 缓存；会同步各层级Quota信息及 PAYG 明细。

func (f *accountStateFacade) mapCycleSnapshotToSummary(ctx context.Context, snapshot *AccountCycleSnapshot) *dto.AccountSummary {
	summary := &dto.AccountSummary{}
	if snapshot == nil {
		return summary
	}
	summary.Role = string(snapshot.Role)
	summary.HasEverPaid = snapshot.HasEverPaid
	summary.UpdatedAt = snapshot.UpdatedAt
	summary.SummaryRemaining = snapshot.SummaryRemaining
	summary.SummaryTotal = snapshot.SummaryTotal
	summary.SummaryUsed = snapshot.SummaryUsed
	summary.Free = cycleQuotaToDTO(snapshot.Free)
	// PAYG and Special Offer are processed separately below.
	summary.Payg = nil
	summary.SpecialOffer = cycleQuotaToDTO(snapshot.SpecialOffer)
	if snapshot.Premium != nil {
		premium := &dto.PremiumBucket{
			CycleIndex:     snapshot.Premium.CycleIndex,
			BacklogSeconds: snapshot.Premium.BacklogSeconds,
		}
		if quota := cycleQuotaToDTO(&snapshot.Premium.Quota); quota != nil {
			premium.QuotaBucket = *quota
		}
		summary.Premium = premium
	}
	if snapshot.Pro != nil {
		pro := &dto.ProBucket{}
		if quota := cycleQuotaToDTO(&snapshot.Pro.Quota); quota != nil {
			pro.QuotaBucket = *quota
		}
		pro.ExpireAt = snapshot.Pro.ExpireAt
		summary.Pro = pro
	}
	if snapshot.ProMini != nil {
		proMini := &dto.ProMiniBucket{}
		if quota := cycleQuotaToDTO(&snapshot.ProMini.Quota); quota != nil {
			proMini.QuotaBucket = *quota
		}
		proMini.ExpireAt = snapshot.ProMini.ExpireAt
		summary.ProMini = proMini
	}
	paygEntries := make([]dto.PaygEntrySummary, 0, len(snapshot.PaygEntries))
	specialEntries := make([]dto.PaygEntrySummary, 0, len(snapshot.SpecialOfferEntries))

	// Process PAYG entries
	for _, entry := range snapshot.PaygEntries {
		item := dto.PaygEntrySummary{
			EntryID:   entry.ID,
			GrantedAt: entry.GrantedAt,
			ExpiresAt: entry.ExpiresAt,
			OriginID:  entry.OriginTxnID,
			Total:     entry.Total,
			Used:      entry.Used,
		}
		paygEntries = append(paygEntries, item)
	}

	// Process Special Offer entries directly from snapshot.SpecialOfferEntries
	for _, entry := range snapshot.SpecialOfferEntries {
		item := dto.PaygEntrySummary{
			EntryID:   entry.ID,
			GrantedAt: entry.GrantedAt,
			ExpiresAt: entry.ExpiresAt,
			OriginID:  entry.OriginTxnID,
			Total:     entry.Total,
			Used:      entry.Used,
		}
		specialEntries = append(specialEntries, item)
	}

	summary.PaygEntries = paygEntries
	summary.SpecialOfferEntries = specialEntries

	if len(paygEntries) > 0 {
		summary.Payg = aggregateEntriesBucket(paygEntries)
	}
	if len(specialEntries) > 0 {
		summary.SpecialOffer = aggregateEntriesBucket(specialEntries)
	}
	return summary
}

func aggregateEntriesBucket(entries []dto.PaygEntrySummary) *dto.QuotaBucket {
	if len(entries) == 0 {
		return nil
	}
	total := 0
	used := 0
	var maxExp int64
	for _, e := range entries {
		total += e.Total
		used += e.Used
		if e.ExpiresAt > maxExp {
			maxExp = e.ExpiresAt
		}
	}
	b := &dto.QuotaBucket{TotalSeconds: total, UsedSeconds: used}
	if maxExp > 0 {
		b.End = pointerInt64(maxExp)
	}
	return b
}

func cycleQuotaToDTO(quota *CycleQuota) *dto.QuotaBucket {
	if quota == nil {
		return nil
	}
	return &dto.QuotaBucket{
		TotalSeconds: quota.Total,
		UsedSeconds:  quota.Used,
		Start:        quota.Start,
		End:          quota.End,
	}
}
