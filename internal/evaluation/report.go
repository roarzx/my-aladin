package evaluation

import (
	"sort"
	"time"
)

// Summary 是评估汇总；缺失数据显式计数，不补零。
type Summary struct {
	Total                   int           `json:"total"`
	Succeeded               int           `json:"succeeded"`
	Failed                  int           `json:"failed"`
	DeliveryRate            float64       `json:"deliveryRate"`
	RecoveryRate            float64       `json:"recoveryRate"`
	DuplicateSideEffectRate float64       `json:"duplicateSideEffectRate"`
	DurationMedian          time.Duration `json:"durationMedian"`
	DurationP95             time.Duration `json:"durationP95"`
	DurationMissing         int           `json:"durationMissing"`
	TokensTotal             int64         `json:"tokensTotal"`
	TokensMissing           int           `json:"tokensMissing"`
}

// Summarize 计算交付率、恢复率、重复副作用率与耗时/成本统计。
func (r *Report) Summarize() Summary {
	summary := Summary{Total: len(r.Results)}
	if summary.Total == 0 {
		return summary
	}
	var recovered, duplicated int
	durations := make([]time.Duration, 0, len(r.Results))
	for _, result := range r.Results {
		if result.Success {
			summary.Succeeded++
		}
		if result.Recovered {
			recovered++
		}
		if result.DuplicateSideEffects > 0 {
			duplicated++
		}
		if result.DurationKnown {
			durations = append(durations, result.Duration)
		} else {
			summary.DurationMissing++
		}
		if result.TokensKnown {
			summary.TokensTotal += result.Tokens
		} else {
			summary.TokensMissing++
		}
	}
	summary.Failed = summary.Total - summary.Succeeded
	summary.DeliveryRate = float64(summary.Succeeded) / float64(summary.Total)
	summary.RecoveryRate = float64(recovered) / float64(summary.Total)
	summary.DuplicateSideEffectRate = float64(duplicated) / float64(summary.Total)

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	summary.DurationMedian = percentile(durations, 50)
	summary.DurationP95 = percentile(durations, 95)
	return summary
}

// percentile 使用 nearest-rank；输入必须已排序，空切片返回 0。
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}
