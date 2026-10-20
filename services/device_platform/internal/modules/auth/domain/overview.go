package domain

// ParentOverview is the guardian-facing home dashboard.
//
// The quota fields stay in the AI gateway's USD unit because that is the pool
// the provider actually spends from; presenting them as yuan would misstate
// what a guardian has left. Money a guardian paid is carried by the billing
// orders, in 人民币分, and is never mixed into these fields.
type ParentOverview struct {
	TodayConversationCount int64   `json:"today_conversation_count"`
	TodaySpentUSD          float64 `json:"today_spent_usd"`
	RemainingBalanceUSD    float64 `json:"remaining_balance_usd"`
	BalanceUSD             float64 `json:"balance_usd"`
	// GrantedBalanceUSD is the quota granted to this family over the whole
	// ledger, so the console can draw a remaining-against-granted bar. It was
	// previously absent, which forced the bar to use the balance as its own
	// denominator and render permanently full.
	GrantedBalanceUSD float64 `json:"granted_balance_usd"`
	DeviceCount       int64   `json:"device_count"`
	OnlineDeviceCount int64   `json:"online_device_count"`
}
