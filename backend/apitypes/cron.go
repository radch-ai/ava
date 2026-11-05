package apitypes

// CronFrequency is the cron job cadence selector.
type CronFrequency string

const (
	CronFrequencyDaily  CronFrequency = "daily"
	CronFrequencyWeekly CronFrequency = "weekly"
	CronFrequencyCustom CronFrequency = "custom"
)

// CronJob is one cron job item (stored in a separate cron table).
type CronJob struct {
	ID   string `json:"id"`
	Time string `json:"time"`
	// Frequency selects the cadence; CronExpression applies when it
	// is "custom".
	Frequency      CronFrequency `json:"frequency"`
	CronExpression *string       `json:"cronExpression,omitempty"`
	Model          string        `json:"model"`
	Prompt         string        `json:"prompt"`
}
